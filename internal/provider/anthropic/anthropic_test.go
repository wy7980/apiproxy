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

func TestChatSuccess(t *testing.T) {
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

	// Verify Anthropic-specific headers were set.
	if doer.req == nil {
		t.Fatal("no request recorded")
	}
	if doer.req.Header.Get("x-api-key") != "sk-test" {
		t.Fatalf("x-api-key = %q, want sk-test", doer.req.Header.Get("x-api-key"))
	}
	if doer.req.Header.Get("Anthropic-Version") != APIVersion {
		t.Fatalf("Anthropic-Version = %q, want %q", doer.req.Header.Get("Anthropic-Version"), APIVersion)
	}
	if doer.req.URL.Path != "/v1/messages" {
		t.Fatalf("URL path = %q, want /v1/messages", doer.req.URL.Path)
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
		Body: []byte(`{"model":"m","max_tokens":10,"messages":[]}`), Model: "m",
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
		Body: []byte(`{"model":"m","max_tokens":10,"messages":[]}`), Model: "m",
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
		Body: []byte(`{"model":"m","max_tokens":10,"messages":[]}`), Model: "m",
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
		Body: []byte(`{"model":"m","max_tokens":10,"messages":[]}`), Model: "m",
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
		Body: []byte(`{"model":"m","stream":true,"max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`), Model: "m", Stream: true,
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
		Body: []byte(`{"model":"m","stream":true,"max_tokens":10,"messages":[]}`), Model: "m", Stream: true,
	})
	if err == nil {
		t.Fatal("ChatStream() err = nil, want error for 500")
	}
}
