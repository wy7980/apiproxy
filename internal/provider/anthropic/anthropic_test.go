package anthropic

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/wangyong/apiproxy/internal/provider"
)

type mockDoer struct {
	resp *http.Response
	err  error
	req  *http.Request
}

func (m *mockDoer) Do(req *http.Request) (*http.Response, error) {
	m.req = req
	return m.resp, m.err
}

func TestChatSuccessAnthropicPath(t *testing.T) {
	body := `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":1}}`
	doer := &mockDoer{
		resp: &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(body))),
		},
	}
	p := New(provider.Config{Name: "test", BaseURL: "https://api.anthropic.com", APIKey: "sk-test"}, doer)

	resp, perr := p.Chat(context.Background(), &provider.ChatRequest{
		Body:  []byte(`{"model":"claude-sonnet-4-5","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`),
		Path:  "/v1/messages",
		Model: "claude-sonnet-4-5",
	})
	if perr != nil {
		t.Fatalf("Chat() perr = %v", perr)
	}
	if resp.Err != nil {
		t.Fatalf("Chat() err = %v", resp.Err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("Chat() status = %d", resp.StatusCode)
	}
	// Anthropic usage: input_tokens / output_tokens.
	if resp.Usage.PromptTokens != 5 || resp.Usage.CompletionTokens != 1 || resp.Usage.TotalTokens != 6 {
		t.Fatalf("Chat() usage = %+v", resp.Usage)
	}

	// Verify the URL path mirrors the client path.
	if doer.req == nil {
		t.Fatal("no request recorded")
	}
	if doer.req.URL.Path != "/v1/messages" {
		t.Fatalf("URL path = %q, want /v1/messages", doer.req.URL.Path)
	}
	// Default auth_header is "both": both x-api-key and Authorization should be set.
	if doer.req.Header.Get("x-api-key") != "sk-test" {
		t.Fatalf("x-api-key = %q, want sk-test", doer.req.Header.Get("x-api-key"))
	}
	if doer.req.Header.Get("Authorization") != "Bearer sk-test" {
		t.Fatalf("Authorization = %q, want Bearer sk-test", doer.req.Header.Get("Authorization"))
	}
}

func TestChatSuccessOpenAIPath(t *testing.T) {
	body := `{"id":"c1","choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`
	doer := &mockDoer{
		resp: &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(body))),
		},
	}
	p := New(provider.Config{Name: "test", BaseURL: "https://api.test.com", APIKey: "sk-test"}, doer)

	resp, perr := p.Chat(context.Background(), &provider.ChatRequest{
		Body:  []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`),
		Path:  "/v1/chat/completions",
		Model: "gpt-4o",
	})
	if perr != nil {
		t.Fatalf("Chat() perr = %v", perr)
	}
	if resp.Err != nil {
		t.Fatalf("Chat() err = %v", resp.Err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("Chat() status = %d", resp.StatusCode)
	}
	// OpenAI usage: prompt_tokens / completion_tokens / total_tokens.
	if resp.Usage.PromptTokens != 5 || resp.Usage.CompletionTokens != 1 || resp.Usage.TotalTokens != 6 {
		t.Fatalf("Chat() usage = %+v", resp.Usage)
	}

	// Verify the URL path mirrors the client path.
	if doer.req == nil {
		t.Fatal("no request recorded")
	}
	if doer.req.URL.Path != "/v1/chat/completions" {
		t.Fatalf("URL path = %q, want /v1/chat/completions", doer.req.URL.Path)
	}
}

func TestChatSuccessWithClientHeaders(t *testing.T) {
	body := `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":1}}`
	doer := &mockDoer{
		resp: &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(body))),
		},
	}
	p := New(provider.Config{Name: "test", BaseURL: "https://api.anthropic.com", APIKey: "sk-test"}, doer)

	clientHeaders := http.Header{}
	clientHeaders.Set("Anthropic-Version", "2023-06-01")
	clientHeaders.Set("Anthropic-Beta", "prompt-caching-2024-07-31")
	clientHeaders.Set("Some-Custom-Header", "custom-value")

	resp, perr := p.Chat(context.Background(), &provider.ChatRequest{
		Body:   []byte(`{"model":"claude-sonnet-4-5","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`),
		Path:   "/v1/messages",
		Header: clientHeaders,
		Model:  "claude-sonnet-4-5",
	})
	if perr != nil {
		t.Fatalf("Chat() perr = %v", perr)
	}
	if resp.Err != nil {
		t.Fatalf("Chat() err = %v", resp.Err)
	}

	// Verify client headers were forwarded transparently.
	if doer.req.Header.Get("Anthropic-Version") != "2023-06-01" {
		t.Fatalf("Anthropic-Version = %q, want 2023-06-01", doer.req.Header.Get("Anthropic-Version"))
	}
	if doer.req.Header.Get("Anthropic-Beta") != "prompt-caching-2024-07-31" {
		t.Fatalf("Anthropic-Beta = %q", doer.req.Header.Get("Anthropic-Beta"))
	}
	if doer.req.Header.Get("Some-Custom-Header") != "custom-value" {
		t.Fatalf("Some-Custom-Header = %q, want custom-value", doer.req.Header.Get("Some-Custom-Header"))
	}
}

func TestChatServerError(t *testing.T) {
	p := New(provider.Config{Name: "test", BaseURL: "https://api.anthropic.com", APIKey: "key"}, &mockDoer{
		resp: &http.Response{
			StatusCode: 500,
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"type":"error","error":{"type":"server_error","message":"overloaded"}}`))),
		},
	})

	resp, _ := p.Chat(context.Background(), &provider.ChatRequest{
		Body:  []byte(`{"model":"m","max_tokens":10,"messages":[]}`),
		Path:  "/v1/messages",
		Model: "m",
	})
	if resp.Err == nil {
		t.Fatal("Chat() err = nil, want error for 500")
	}
	if resp.Err.Kind != provider.KindServerError {
		t.Fatalf("Chat() err kind = %q", resp.Err.Kind)
	}
	if resp.Err.StatusCode != 500 {
		t.Fatalf("Chat() err status = %d", resp.Err.StatusCode)
	}
	if resp.Err.Message != "overloaded" {
		t.Fatalf("Chat() err message = %q", resp.Err.Message)
	}
}

func TestChat529Overloaded(t *testing.T) {
	p := New(provider.Config{Name: "test", BaseURL: "https://api.anthropic.com", APIKey: "key"}, &mockDoer{
		resp: &http.Response{
			StatusCode: 529,
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))),
		},
	})

	resp, _ := p.Chat(context.Background(), &provider.ChatRequest{
		Body: []byte(`{"model":"m","max_tokens":10,"messages":[]}`), Path: "/v1/messages", Model: "m",
	})
	if resp.Err.Kind != provider.KindServerError {
		t.Fatalf("Chat() err kind = %q, want server_error for 529", resp.Err.Kind)
	}
}

func TestChatRateLimited(t *testing.T) {
	p := New(provider.Config{Name: "test", BaseURL: "https://api.anthropic.com", APIKey: "key"}, &mockDoer{
		resp: &http.Response{
			StatusCode: 429,
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`))),
		},
	})

	resp, _ := p.Chat(context.Background(), &provider.ChatRequest{
		Body: []byte(`{"model":"m","max_tokens":10,"messages":[]}`), Path: "/v1/messages", Model: "m",
	})
	if resp.Err.Kind != provider.KindRateLimited {
		t.Fatalf("Chat() err kind = %q", resp.Err.Kind)
	}
}

func TestChatTransportTimeout(t *testing.T) {
	p := New(provider.Config{Name: "test", BaseURL: "https://api.anthropic.com", APIKey: "key"}, &mockDoer{
		err: context.DeadlineExceeded,
	})

	resp, _ := p.Chat(context.Background(), &provider.ChatRequest{
		Body: []byte(`{"model":"m","max_tokens":10,"messages":[]}`), Path: "/v1/messages", Model: "m",
	})
	if resp.Err.Kind != provider.KindTimeout {
		t.Fatalf("Chat() err kind = %q", resp.Err.Kind)
	}
}

func TestChatConnectError(t *testing.T) {
	p := New(provider.Config{Name: "test", BaseURL: "https://api.anthropic.com", APIKey: "key"}, &mockDoer{
		err: io.ErrUnexpectedEOF,
	})

	resp, _ := p.Chat(context.Background(), &provider.ChatRequest{
		Body: []byte(`{"model":"m","max_tokens":10,"messages":[]}`), Path: "/v1/messages", Model: "m",
	})
	if resp.Err.Kind != provider.KindConnectError {
		t.Fatalf("Chat() err kind = %q", resp.Err.Kind)
	}
}

func TestChatStreamSuccess(t *testing.T) {
	sseData := "event: message_start\ndata: " +
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[],"usage":{"input_tokens":5,"output_tokens":0}}}` +
		"\n\n" +
		"event: content_block_start\ndata: " +
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` +
		"\n\n" +
		"event: content_block_delta\ndata: " +
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}` +
		"\n\n" +
		"event: content_block_stop\ndata: " +
		`{"type":"content_block_stop","index":0}` +
		"\n\n" +
		"event: message_delta\ndata: " +
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}` +
		"\n\n" +
		"event: message_stop\ndata: " +
		`{"type":"message_stop"}` +
		"\n\n"

	p := New(provider.Config{Name: "test", BaseURL: "https://api.anthropic.com", APIKey: "key"}, &mockDoer{
		resp: &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(sseData))),
		},
	})

	ch, err := p.ChatStream(context.Background(), &provider.ChatRequest{
		Body: []byte(`{"model":"m","stream":true,"max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`), Path: "/v1/messages", Model: "m", Stream: true,
	})
	if err != nil {
		t.Fatalf("ChatStream() err = %v", err)
	}

	chunkCount := 0
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error = %v", chunk.Err)
		}
		chunkCount++
	}
	if chunkCount != 6 {
		t.Fatalf("ChatStream() chunks = %d, want 6", chunkCount)
	}
}

func TestChatStreamOpenError(t *testing.T) {
	p := New(provider.Config{Name: "test", BaseURL: "https://api.anthropic.com", APIKey: "key"}, &mockDoer{
		resp: &http.Response{
			StatusCode: 500,
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"type":"error","error":{"type":"server_error","message":"fail"}}`))),
		},
	})

	_, err := p.ChatStream(context.Background(), &provider.ChatRequest{
		Body: []byte(`{"model":"m","stream":true,"max_tokens":10,"messages":[]}`), Path: "/v1/messages", Model: "m", Stream: true,
	})
	if err == nil {
		t.Fatal("ChatStream() err = nil, want error for 500")
	}
}

func TestAuthHeaderXApiKey(t *testing.T) {
	p := New(provider.Config{Name: "test", BaseURL: "https://api.anthropic.com", APIKey: "sk-test", AuthHeader: "x-api-key"}, &mockDoer{
		resp: &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"id":"x"}`))),
		},
	})
	_, _ = p.Chat(context.Background(), &provider.ChatRequest{Body: []byte(`{"model":"m"}`), Path: "/v1/messages"})
	if doer, ok := p.client.(*mockDoer); ok && doer.req != nil {
		if doer.req.Header.Get("x-api-key") != "sk-test" {
			t.Fatalf("x-api-key = %q", doer.req.Header.Get("x-api-key"))
		}
		if doer.req.Header.Get("Authorization") != "" {
			t.Fatalf("Authorization should be empty for x-api-key mode, got %q", doer.req.Header.Get("Authorization"))
		}
	}
}

func TestAuthHeaderBearer(t *testing.T) {
	p := New(provider.Config{Name: "test", BaseURL: "https://api.anthropic.com", APIKey: "sk-test", AuthHeader: "authorization"}, &mockDoer{
		resp: &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"id":"x"}`))),
		},
	})
	_, _ = p.Chat(context.Background(), &provider.ChatRequest{Body: []byte(`{"model":"m"}`), Path: "/v1/messages"})
	if doer, ok := p.client.(*mockDoer); ok && doer.req != nil {
		if doer.req.Header.Get("Authorization") != "Bearer sk-test" {
			t.Fatalf("Authorization = %q", doer.req.Header.Get("Authorization"))
		}
		if doer.req.Header.Get("x-api-key") != "" {
			t.Fatalf("x-api-key should be empty for bearer mode, got %q", doer.req.Header.Get("x-api-key"))
		}
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://api.anthropic.com", "https://api.anthropic.com"},
		{"https://api.anthropic.com/", "https://api.anthropic.com"},
		{"https://api.anthropic.com/v1", "https://api.anthropic.com"},
		{"https://api.anthropic.com/v1/", "https://api.anthropic.com"},
		{"https://api.anthropic.com/v1/messages", "https://api.anthropic.com"},
		{"https://api.anthropic.com/v1/chat/completions", "https://api.anthropic.com"},
		{"https://newapi.example.com", "https://newapi.example.com"},
		{"https://newapi.example.com/v1", "https://newapi.example.com"},
	}
	for _, c := range cases {
		got := normalizeBaseURL(c.in)
		if got != c.want {
			t.Errorf("normalizeBaseURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHopByHopHeadersStripped(t *testing.T) {
	p := New(provider.Config{Name: "test", BaseURL: "https://api.anthropic.com", APIKey: "key"}, &mockDoer{
		resp: &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"id":"x"}`))),
		},
	})
	clientHeaders := http.Header{}
	clientHeaders.Set("Connection", "keep-alive")
	clientHeaders.Set("Transfer-Encoding", "chunked")
	clientHeaders.Set("Proxy-Authorization", "basic abc")
	clientHeaders.Set("X-Custom", "should-pass")

	_, _ = p.Chat(context.Background(), &provider.ChatRequest{
		Body:   []byte(`{"model":"m"}`),
		Path:   "/v1/messages",
		Header: clientHeaders,
	})
	if doer, ok := p.client.(*mockDoer); ok && doer.req != nil {
		if doer.req.Header.Get("Connection") != "" {
			t.Fatal("Connection header should be stripped")
		}
		if doer.req.Header.Get("Transfer-Encoding") != "" {
			t.Fatal("Transfer-Encoding header should be stripped")
		}
		if doer.req.Header.Get("Proxy-Authorization") != "" {
			t.Fatal("Proxy-Authorization header should be stripped")
		}
		if doer.req.Header.Get("X-Custom") != "should-pass" {
			t.Fatalf("X-Custom = %q, want should-pass", doer.req.Header.Get("X-Custom"))
		}
	}
}
