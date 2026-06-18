package openai

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
}

func (m *mockDoer) Do(req *http.Request) (*http.Response, error) {
	return m.resp, m.err
}

func TestChatSuccess(t *testing.T) {
	body := `{"id":"chatcmpl-1","choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`
	p := New(provider.Config{Name: "test", BaseURL: "https://api.test.com/v1", APIKey: "key"}, &mockDoer{
		resp: &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(body))),
		},
	})

	resp, perr := p.Chat(context.Background(), &provider.ChatRequest{
		Body:  []byte(`{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`),
		Model: "test-model",
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
	if resp.Usage.PromptTokens != 5 || resp.Usage.CompletionTokens != 1 || resp.Usage.TotalTokens != 6 {
		t.Fatalf("Chat() usage = %+v", resp.Usage)
	}
}

func TestChatServerError(t *testing.T) {
	p := New(provider.Config{Name: "test", BaseURL: "https://api.test.com/v1", APIKey: "key"}, &mockDoer{
		resp: &http.Response{
			StatusCode: 503,
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"error":{"message":"overloaded","type":"server_error"}}`))),
		},
	})

	resp, _ := p.Chat(context.Background(), &provider.ChatRequest{
		Body:  []byte(`{"model":"m","messages":[]}`),
		Model: "m",
	})
	if resp.Err == nil {
		t.Fatal("Chat() err = nil, want error for 503")
	}
	if resp.Err.Kind != provider.KindServerError {
		t.Fatalf("Chat() err kind = %q", resp.Err.Kind)
	}
	if resp.Err.StatusCode != 503 {
		t.Fatalf("Chat() err status = %d", resp.Err.StatusCode)
	}
	if resp.Err.Message != "overloaded" {
		t.Fatalf("Chat() err message = %q", resp.Err.Message)
	}
}

func TestChatRateLimited(t *testing.T) {
	p := New(provider.Config{Name: "test", BaseURL: "https://api.test.com/v1", APIKey: "key"}, &mockDoer{
		resp: &http.Response{
			StatusCode: 429,
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"error":{"message":"rate limited"}}`))),
		},
	})

	resp, _ := p.Chat(context.Background(), &provider.ChatRequest{
		Body: []byte(`{"model":"m","messages":[]}`), Model: "m",
	})
	if resp.Err.Kind != provider.KindRateLimited {
		t.Fatalf("Chat() err kind = %q", resp.Err.Kind)
	}
}

func TestChatTransportTimeout(t *testing.T) {
	p := New(provider.Config{Name: "test", BaseURL: "https://api.test.com/v1", APIKey: "key"}, &mockDoer{
		err: context.DeadlineExceeded,
	})

	resp, _ := p.Chat(context.Background(), &provider.ChatRequest{
		Body: []byte(`{"model":"m","messages":[]}`), Model: "m",
	})
	if resp.Err.Kind != provider.KindTimeout {
		t.Fatalf("Chat() err kind = %q", resp.Err.Kind)
	}
}

func TestChatConnectError(t *testing.T) {
	p := New(provider.Config{Name: "test", BaseURL: "https://api.test.com/v1", APIKey: "key"}, &mockDoer{
		err: io.ErrUnexpectedEOF,
	})

	resp, _ := p.Chat(context.Background(), &provider.ChatRequest{
		Body: []byte(`{"model":"m","messages":[]}`), Model: "m",
	})
	if resp.Err.Kind != provider.KindConnectError {
		t.Fatalf("Chat() err kind = %q", resp.Err.Kind)
	}
}

func TestChatStreamSuccess(t *testing.T) {
	sseData := "data: {" +
		"\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{" +
		"\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {" +
		"\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{" +
		"\"delta\":{\"content\":\"there\"}}]," +
		"\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n\n"
	p := New(provider.Config{Name: "test", BaseURL: "https://api.test.com/v1", APIKey: "key"}, &mockDoer{
		resp: &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(sseData))),
		},
	})

	ch, err := p.ChatStream(context.Background(), &provider.ChatRequest{
		Body: []byte(`{"model":"m","stream":true,"messages":[]}`), Model: "m", Stream: true,
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
	if chunkCount != 2 {
		t.Fatalf("ChatStream() chunks = %d, want 2", chunkCount)
	}
}

func TestChatStreamOpenError(t *testing.T) {
	p := New(provider.Config{Name: "test", BaseURL: "https://api.test.com/v1", APIKey: "key"}, &mockDoer{
		resp: &http.Response{
			StatusCode: 500,
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"error":{"message":"fail"}}`))),
		},
	})

	_, err := p.ChatStream(context.Background(), &provider.ChatRequest{
		Body: []byte(`{"model":"m","stream":true,"messages":[]}`), Model: "m", Stream: true,
	})
	if err == nil {
		t.Fatal("ChatStream() err = nil, want error for 500")
	}
}
