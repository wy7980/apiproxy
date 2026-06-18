package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseAnthropicMessagesRequest(t *testing.T) {
	body := `{"model":"claude-sonnet-4-5","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]}`
	got, err := ParseAnthropicMessagesRequest([]byte(body))
	if err != nil {
		t.Fatalf("ParseAnthropicMessagesRequest() error = %v", err)
	}
	if got.Model != "claude-sonnet-4-5" || got.Stream {
		t.Fatalf("ParseAnthropicMessagesRequest() = %+v", got)
	}
}

func TestParseAnthropicMessagesRequestStream(t *testing.T) {
	body := `{"model":"claude-sonnet-4-5","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	got, err := ParseAnthropicMessagesRequest([]byte(body))
	if err != nil {
		t.Fatalf("ParseAnthropicMessagesRequest() error = %v", err)
	}
	if got.Model != "claude-sonnet-4-5" || !got.Stream {
		t.Fatalf("ParseAnthropicMessagesRequest() = %+v", got)
	}
}

func TestParseAnthropicMessagesRequestMissingModel(t *testing.T) {
	_, err := ParseAnthropicMessagesRequest([]byte(`{"max_tokens":1024,"messages":[]}`))
	if err == nil {
		t.Fatal("expected error for missing model")
	}
}

func TestAnthropicErrorJSON(t *testing.T) {
	out := AnthropicErrorJSON("invalid_request_error", "bad model")
	var e AnthropicError
	if err := json.Unmarshal(out, &e); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if e.Type != "error" || e.Error.Type != "invalid_request_error" || e.Error.Message != "bad model" {
		t.Fatalf("error = %+v", e)
	}
	if !strings.Contains(string(out), `"type":"error"`) {
		t.Fatalf("missing type wrapper: %s", out)
	}
}
