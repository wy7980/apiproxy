// Package anthropic implements a transparent proxy provider.
//
// It is NOT Anthropic-specific despite the package name (historical): it
// forwards the downstream client's request path, headers and body verbatim to
// an upstream, and streams/returns the response verbatim. The protocol spoken
// (OpenAI Chat Completions vs Anthropic Messages) is decided entirely by the
// client's request path — whatever path the client used is the path sent
// upstream, so one provider can serve both /v1/chat/completions and
// /v1/messages transparently. The only thing this provider contributes is the
// upstream host, the auth header, and timeout.
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

// DefaultMessagesPath is used when ChatRequest.Path is empty. We default to the
// Anthropic messages path because the proxy historically served Claude Code,
// but in practice the handlers always pass the client path through.
const DefaultMessagesPath = "/v1/messages"

// AuthHeaderMode controls how the provider's API key is sent upstream.
type AuthHeaderMode string

const (
	// AuthXApiKey sends the key as x-api-key (official Anthropic convention).
	AuthXApiKey AuthHeaderMode = "x-api-key"
	// AuthBearer sends the key as Authorization: Bearer <key>.
	AuthBearer AuthHeaderMode = "authorization"
	// AuthBoth sends the key in both headers for maximum compatibility. This is
	// the default for a transparent proxy: gateways like new-api/one-api often
	// accept only one of the two, so emitting both means "just works".
	AuthBoth AuthHeaderMode = "both"
)

// hopByHopHeaders are stripped per RFC 7230 §6.1; they are per-connection and
// must not be forwarded by a proxy.
var hopByHopHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

type Provider struct {
	name       string
	baseURL    string
	apiKey     string
	authHeader AuthHeaderMode
	timeout    time.Duration
	client     provider.HTTPDoer
}

// New builds a transparent provider. authHeader defaults to AuthBoth when
// unset, for maximum upstream compatibility.
func New(cfg provider.Config, client provider.HTTPDoer) *Provider {
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	auth := AuthBoth
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

// normalizeBaseURL strips known API path suffixes so appending ChatRequest.Path
// (which already carries the client's /v1/... path) doesn't duplicate them.
// "https://h", "https://h/", "https://h/v1", "https://h/v1/messages",
// "https://h/v1/chat/completions" all normalize to "https://h".
func normalizeBaseURL(raw string) string {
	s := strings.TrimRight(raw, "/")
	for _, suffix := range []string{"/v1/chat/completions", "/v1/messages", "/chat/completions", "/messages", "/v1"} {
		if strings.HasSuffix(s, suffix) {
			s = strings.TrimSuffix(s, suffix)
			break
		}
	}
	return strings.TrimRight(s, "/")
}

func (p *Provider) Name() string { return p.name }

// Chat performs a non-streaming request. The response body is returned as-is
// for transparent forwarding; only usage is parsed for metrics.
func (p *Provider) Chat(ctx context.Context, req *provider.ChatRequest) (*provider.ChatResponse, error) {
	if req == nil || len(req.Body) == 0 {
		return nil, errors.New("empty request body")
	}

	httpReq, err := p.newRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	slog.Debug("transparent upstream request",
		"provider", p.name, "url", httpReq.URL.String(),
		"method", httpReq.Method,
		"req_headers", redactHeaders(httpReq.Header),
		"req_body", truncStr(string(req.Body), maxLogBody))

	resp, err := p.client.Do(httpReq)
	if err != nil {
		slog.Debug("transparent upstream transport error",
			"provider", p.name, "url", httpReq.URL.String(), "err", err.Error())
		return &provider.ChatResponse{Err: classifyTransport(err)}, nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	slog.Debug("transparent upstream response",
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
		slog.Warn("transparent upstream returned HTTP 200 with empty body — check that base_url points to a real API endpoint",
			"provider", p.name, "url", httpReq.URL.String())
	}
	out.Usage = extractUsage(body)
	return out, nil
}

// ChatStream performs a streaming request. On success it returns a channel
// emitting the raw SSE chunks received from upstream, which are forwarded
// verbatim.
func (p *Provider) ChatStream(ctx context.Context, req *provider.ChatRequest) (<-chan provider.StreamChunk, error) {
	if req == nil || len(req.Body) == 0 {
		return nil, errors.New("empty request body")
	}

	httpReq, err := p.newRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	slog.Debug("transparent upstream stream request",
		"provider", p.name, "url", httpReq.URL.String(),
		"method", httpReq.Method,
		"req_headers", redactHeaders(httpReq.Header),
		"req_body", truncStr(string(req.Body), maxLogBody))

	resp, err := p.client.Do(httpReq)
	if err != nil {
		slog.Debug("transparent upstream stream transport error",
			"provider", p.name, "url", httpReq.URL.String(), "err", err.Error())
		return nil, classifyTransport(err)
	}

	slog.Debug("transparent upstream stream response headers",
		"provider", p.name, "url", httpReq.URL.String(),
		"status", resp.StatusCode,
		"resp_headers", redactHeaders(resp.Header))

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		slog.Debug("transparent upstream stream error body",
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
						slog.Debug("transparent upstream stream chunk",
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
				slog.Debug("transparent upstream stream ended",
					"provider", p.name, "total_chunks", chunkCount,
					"eof", errors.Is(readErr, io.EOF))
				return
			}
		}
	}()

	return ch, nil
}

// newRequest builds the upstream POST. The URL is normalize(host) + client
// path, making the proxy fully transparent: whatever path the downstream client
// sent (e.g. /v1/messages or /v1/chat/completions) is forwarded to the upstream
// as-is. Client headers are copied through verbatim except for hop-by-hop
// headers, Content-Length / Content-Encoding (which are recomputed), and auth
// (which is replaced with the provider's own key).
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

// applyHeaders copies the client's request headers onto the upstream request
// verbatim (minus hop-by-hop, Content-Length, Content-Encoding and auth), then
// injects the provider's API key per the configured auth mode. This makes the
// proxy transparent for BOTH OpenAI-style clients (whose Authorization we
// overwrite with the provider key) and Anthropic-style clients (whose
// anthropic-version / anthropic-beta headers ride along untouched).
func (p *Provider) applyHeaders(req *http.Request, clientHeaders http.Header) {
	if clientHeaders != nil {
		for k, vs := range clientHeaders {
			lk := strings.ToLower(k)
			switch lk {
			case "content-length", "content-encoding":
				continue
			}
			skip := false
			for _, h := range hopByHopHeaders {
				if strings.EqualFold(k, h) {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
	}

	// Sensible defaults if the client didn't send them.
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}

	// Auth: replace any client-supplied credential with the provider's own key.
	if p.apiKey != "" {
		switch p.authHeader {
		case AuthXApiKey:
			req.Header.Set("x-api-key", p.apiKey)
			req.Header.Del("Authorization")
		case AuthBearer:
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
			req.Header.Del("x-api-key")
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

// extractUsage pulls token usage from a response body. It tries both the
// OpenAI field names (prompt_tokens / completion_tokens / total_tokens) and the
// Anthropic field names (input_tokens / output_tokens) so it works regardless
// of which protocol the client spoke. Best-effort: a parse failure just yields
// zero usage and never affects the response.
func extractUsage(body []byte) provider.Usage {
	var parsed struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
			InputTokens      int `json:"input_tokens"`
			OutputTokens     int `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return provider.Usage{}
	}
	in := parsed.Usage.PromptTokens
	if in == 0 {
		in = parsed.Usage.InputTokens
	}
	out := parsed.Usage.CompletionTokens
	if out == 0 {
		out = parsed.Usage.OutputTokens
	}
	total := parsed.Usage.TotalTokens
	if total == 0 {
		total = in + out
	}
	return provider.Usage{
		PromptTokens:     in,
		CompletionTokens: out,
		TotalTokens:      total,
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
