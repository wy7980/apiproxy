// Package anthropic implements a transparent proxy provider for the native
// Anthropic Messages API (/v1/messages).
//
// Unlike the OpenAI provider, this one performs NO format conversion at all.
// It forwards the request body verbatim to an Anthropic-compatible upstream
// and streams/returns the response verbatim. The upstream is responsible for
// model adaptation and any format handling. This keeps Claude Code and other
// Anthropic clients talking to the proxy exactly as if it were api.anthropic.com.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wangyong/apiproxy/internal/provider"
)

// APIVersion is the Anthropic API version header sent on every request.
const APIVersion = "2023-06-01"

// DefaultMessagesPath is used when ChatRequest.Path is empty.
const DefaultMessagesPath = "/v1/messages"// AuthHeaderMode controls how the provider's API key is sent upstream.
type AuthHeaderMode string

const (
	// AuthXApiKey sends the key as x-api-key (official Anthropic convention).
	AuthXApiKey AuthHeaderMode = "x-api-key"
	// AuthBearer sends the key as Authorization: Bearer <key>
	// (useful for new-api / one-api gateways that only accept Bearer auth).
	AuthBearer AuthHeaderMode = "authorization"
	// AuthBoth sends the key in both headers for maximum compatibility.
	AuthBoth AuthHeaderMode = "both"
)

type Provider struct {
	name       string
	baseURL    string
	apiKey     string
	authHeader AuthHeaderMode
	timeout    time.Duration
	client     provider.HTTPDoer
}

func New(cfg provider.Config, client provider.HTTPDoer) *Provider {
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	auth := AuthXApiKey // default: official Anthropic convention
	if cfg.AuthHeader != "" {
		switch strings.ToLower(cfg.AuthHeader) {
		case "authorization", "bearer":
			auth = AuthBearer
		case "x-api-key":
			auth = AuthXApiKey
		case "both":
			auth = AuthBoth
		}
	}
	return &Provider{
		name:       cfg.Name,
		baseURL:    normalizeBaseURL(cfg.BaseURL),
		apiKey:     cfg.APIKey,
		authHeader: auth,
		timeout:    cfg.Timeout,
		client:     client,
	}
}

// normalizeBaseURL strips path suffixes so appending ChatRequest.Path doesn't duplicate /v1.
func normalizeBaseURL(raw string) string {
	s := strings.TrimRight(raw, "/")
	if strings.HasSuffix(s, "/v1/messages") {
		s = strings.TrimSuffix(s, "/v1/messages")
	}
	if strings.HasSuffix(s, "/v1") {
		s = strings.TrimSuffix(s, "/v1")
	}
	return s
}

func (p *Provider) Name() string { return p.name }

// Chat performs a non-streaming Anthropic Messages request. The response body
// is returned as-is for transparent forwarding; only usage is parsed for metrics.
func (p *Provider) Chat(ctx context.Context, req *provider.ChatRequest) (*provider.ChatResponse, error) {
	if req == nil || len(req.Body) == 0 {
		return nil, errors.New("empty request body")
	}

	httpReq, err := p.newRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	slog.Debug("anthropic upstream request",
		"provider", p.name, "url", httpReq.URL.String(),
		"method", httpReq.Method,
		"req_headers", redactHeaders(httpReq.Header),
		"req_body", truncStr(string(req.Body), maxLogBody))

	resp, err := p.client.Do(httpReq)
	if err != nil {
		slog.Debug("anthropic upstream transport error",
			"provider", p.name, "url", httpReq.URL.String(), "err", err.Error())
		return &provider.ChatResponse{Err: classifyTransport(err)}, nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	slog.Debug("anthropic upstream response",
		"provider", p.name, "url", httpReq.URL.String(),
		"status", resp.StatusCode,
		"resp_headers", redactHeaders(resp.Header),
		"resp_body", truncStr(string(body), maxLogBody))

	out := &provider.ChatResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       body,
	}
	if resp.StatusCode >= 400 {
		out.Err = classifyStatus(resp.StatusCode, string(body))
		return out, nil
	}
	if len(body) == 0 {
		slog.Warn("anthropic upstream returned HTTP 200 with empty body — check that base_url points to a real Anthropic-compatible endpoint",
			"provider", p.name, "url", p.baseURL+DefaultMessagesPath)
	}
	out.Usage = extractUsage(body)
	return out, nil
}

// ChatStream performs a streaming Anthropic Messages request. On success it
// returns a channel emitting the raw SSE chunks received from upstream, which
// are forwarded verbatim.
func (p *Provider) ChatStream(ctx context.Context, req *provider.ChatRequest) (<-chan provider.StreamChunk, error) {
	if req == nil || len(req.Body) == 0 {
		return nil, errors.New("empty request body")
	}

	httpReq, err := p.newRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	slog.Debug("anthropic upstream stream request",
		"provider", p.name, "url", httpReq.URL.String(),
		"method", httpReq.Method,
		"req_headers", redactHeaders(httpReq.Header),
		"req_body", truncStr(string(req.Body), maxLogBody))

	resp, err := p.client.Do(httpReq)
	if err != nil {
		slog.Debug("anthropic upstream stream transport error",
			"provider", p.name, "url", httpReq.URL.String(), "err", err.Error())
		return nil, classifyTransport(err)
	}

	slog.Debug("anthropic upstream stream response headers",
		"provider", p.name, "url", httpReq.URL.String(),
		"status", resp.StatusCode,
		"resp_headers", redactHeaders(resp.Header))

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		slog.Debug("anthropic upstream stream error body",
			"provider", p.name, "status", resp.StatusCode,
			"body", truncStr(string(body), maxLogBody))
		return nil, classifyStatus(resp.StatusCode, string(body))
	}

	ch := make(chan provider.StreamChunk, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		buf := make([]byte, 4096)
		var carry []byte
		chunkCount := 0
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				data := append(carry, buf[:n]...)
				// Split on \n\n boundaries to forward complete SSE events.
				for {
					idx := bytes.Index(data, []byte("\n\n"))
					if idx < 0 {
						break
					}
					event := data[:idx+2]
					ch <- provider.StreamChunk{Data: append([]byte(nil), event...)}
					chunkCount++
					if chunkCount <= 3 {
						slog.Debug("anthropic upstream stream chunk",
							"provider", p.name, "chunk_num", chunkCount,
							"data", truncStr(string(event), maxLogChunk))
					}
					data = data[idx+2:]
				}
				carry = data
			}
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) {
					ch <- provider.StreamChunk{Err: fmt.Errorf("stream read: %w", readErr)}
				}
				if len(carry) > 0 {
					ch <- provider.StreamChunk{Data: append([]byte(nil), carry...)}
				}
				slog.Debug("anthropic upstream stream ended",
					"provider", p.name, "total_chunks", chunkCount,
					"eof", errors.Is(readErr, io.EOF))
				return
			}
		}
	}()

	return ch, nil
}

// newRequest builds the upstream POST, merging client Anthropic headers into
// the upstream request. Only the auth header(s) are replaced with the
// provider's own API key; all other Anthropic headers are forwarded as-is.
// The request path is a transparent passthrough: whatever path the downstream
// client sent (e.g. /v1/messages or /v1/messages/count_tokens) is forwarded
// to the upstream as-is, making the proxy truly transparent.
func (p *Provider) newRequest(ctx context.Context, req *provider.ChatRequest) (*http.Request, error) {
	path := req.Path
	if path == "" {
		path = DefaultMessagesPath
	}
	url := p.baseURL + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(req.Body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	p.applyHeaders(httpReq, req.Header)
	return httpReq, nil
}

// applyHeaders sets the upstream request headers. It always sets Content-Type
// and Accept, then merges Anthropic-specific headers from the client request.
// The auth header is set based on the provider's authHeader mode.
func (p *Provider) applyHeaders(req *http.Request, clientHeaders http.Header) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Forward Anthropic protocol headers from the client.
	// This is critical: Claude Code sends anthropic-version and anthropic-beta
	// headers that upstream services (like new-api) need to process correctly.
	if clientHeaders != nil {
		for _, h := range []string{
			"Anthropic-Version",
			"Anthropic-Beta",
		} {
			if vals := clientHeaders.Values(h); len(vals) > 0 {
				for _, v := range vals {
					req.Header.Add(h, v)
				}
			}
		}
	}

	// If the client didn't send Anthropic-Version, add our default.
	if req.Header.Get("Anthropic-Version") == "" {
		req.Header.Set("Anthropic-Version", APIVersion)
	}

	// Auth: set the provider's API key based on authHeader mode.
	if p.apiKey != "" {
		switch p.authHeader {
		case AuthXApiKey:
			req.Header.Set("x-api-key", p.apiKey)
		case AuthBearer:
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		case AuthBoth:
			req.Header.Set("x-api-key", p.apiKey)
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		}
	}
}

func classifyTransport(err error) *provider.Error {
	msg := err.Error()
	if isTimeout(msg, err) {
		return &provider.Error{Kind: provider.KindTimeout, Message: "request timeout", Cause: err}
	}
	return &provider.Error{Kind: provider.KindConnectError, Message: "connect error", Cause: err}
}

func isTimeout(msg string, err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded")
}

func classifyStatus(code int, body string) *provider.Error {
	msg := fmt.Sprintf("upstream returned %d", code)
	if body != "" {
		var parsed struct {
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(body), &parsed) == nil && parsed.Error.Message != "" {
			msg = parsed.Error.Message
		}
	}

	var kind provider.ErrorKind
	switch {
	case code == 429:
		kind = provider.KindRateLimited
	case code == 529:
		kind = provider.KindServerError
	case code >= 500:
		kind = provider.KindServerError
	case code >= 400:
		kind = provider.KindClientError
	default:
		kind = provider.KindUnknown
	}
	return &provider.Error{StatusCode: code, Kind: kind, Message: msg}
}

// extractUsage pulls token usage from an Anthropic Messages response body.
func extractUsage(body []byte) provider.Usage {
	var parsed struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return provider.Usage{}
	}
	in, out := parsed.Usage.InputTokens, parsed.Usage.OutputTokens
	return provider.Usage{
		PromptTokens:     in,
		CompletionTokens: out,
		TotalTokens:      in + out,
	}
}

// ---------- debug logging helpers ----------

const (
	maxLogBody  = 4096 // truncate request/response bodies in debug logs
	maxLogChunk = 1024 // truncate a single SSE chunk in debug logs
)

// redactHeaders returns a copy of h with secret-bearing header values masked
// so the debug log never leaks full API keys or bearer tokens.
func redactHeaders(h http.Header) map[string]string {
	const (
		masked = "***REDACTED***"
	)
	out := make(map[string]string, len(h))
	for k, vs := range h {
		switch strings.ToLower(k) {
		case "authorization", "x-api-key", "cookie", "set-cookie", "anthropic-auth-token":
			out[k] = masked
		default:
			out[k] = strings.Join(vs, ", ")
		}
	}
	return out
}

// truncStr caps s to n runes, appending an ellipsis indicator when truncated.
func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("...(%d more bytes)", len(s)-n)
}
