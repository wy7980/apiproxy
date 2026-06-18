// Package openai implements a provider for OpenAI-compatible chat completion APIs.
// Examples of compatible upstreams: OpenAI, DeepSeek, Qwen (DashScope compatible mode), OpenRouter.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wangyong/apiproxy/internal/provider"
)

type Provider struct {
	name    string
	baseURL string
	apiKey  string
	timeout time.Duration
	client  provider.HTTPDoer
}

func New(cfg provider.Config, client provider.HTTPDoer) *Provider {
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	return &Provider{
		name:    cfg.Name,
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		timeout: cfg.Timeout,
		client:  client,
	}
}

func (p *Provider) Name() string { return p.name }

// Chat performs a non-streaming chat completion.
func (p *Provider) Chat(ctx context.Context, req *provider.ChatRequest) (*provider.ChatResponse, error) {
	if req == nil || len(req.Body) == 0 {
		return nil, errors.New("empty request body")
	}

	url := p.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(req.Body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return &provider.ChatResponse{Err: classifyTransport(err)}, nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	out := &provider.ChatResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       body,
	}

	if resp.StatusCode >= 400 {
		out.Err = classifyStatus(resp.StatusCode, string(body))
		return out, nil
	}

	out.Usage = extractUsage(body)
	return out, nil
}

// ChatStream performs a streaming chat completion.
// On success, returns a channel emitting raw SSE chunks as received from upstream.
func (p *Provider) ChatStream(ctx context.Context, req *provider.ChatRequest) (<-chan provider.StreamChunk, error) {
	if req == nil || len(req.Body) == 0 {
		return nil, errors.New("empty request body")
	}

	url := p.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(req.Body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, classifyTransport(err)
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, classifyStatus(resp.StatusCode, string(body))
	}

	ch := make(chan provider.StreamChunk, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		buf := make([]byte, 4096)
		var carry []byte
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
				return
			}
		}
	}()

	return ch, nil
}

func (p *Provider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
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
		// best-effort: extract OpenAI error message field
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
	case code >= 500:
		kind = provider.KindServerError
	case code >= 400:
		kind = provider.KindClientError
	default:
		kind = provider.KindUnknown
	}
	return &provider.Error{StatusCode: code, Kind: kind, Message: msg}
}

// extractUsage pulls token usage from the OpenAI-compatible response body.
// Returns zero usage if the body does not contain it.
func extractUsage(body []byte) provider.Usage {
	var parsed struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return provider.Usage{}
	}
	return provider.Usage{
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		TotalTokens:      parsed.Usage.TotalTokens,
	}
}
