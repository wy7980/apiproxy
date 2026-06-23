package api

import (
	"encoding/json"
	"testing"
)

func TestParseChatCompletionRequest(t *testing.T) {
	got, err := ParseChatCompletionRequest([]byte(`{"model":"chat","stream":true}`))
	if err != nil {
		t.Fatalf("ParseChatCompletionRequest() error = %v", err)
	}
	if got.Model != "chat" || !got.Stream {
		t.Fatalf("ParseChatCompletionRequest() = %+v", got)
	}
}

func TestReplaceModel(t *testing.T) {
	out, err := ReplaceModel([]byte(`{"model":"chat","messages":[]}`), "gpt-4o")
	if err != nil {
		t.Fatalf("ReplaceModel() error = %v", err)
	}
	if !contains(out, `"model":"gpt-4o"`) {
		t.Fatalf("ReplaceModel() output = %s", out)
	}
}

func contains(haystack []byte, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if string(haystack[i:i+len(needle)]) == needle {
				return true
			}
		}
		return false
	}())
}

func TestInjectStreamUsageOptionsStreaming(t *testing.T) {
	body := []byte(`{"model":"m1","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	out, err := InjectStreamUsageOptions(body)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	so, _ := obj["stream_options"].(map[string]any)
	if so == nil || so["include_usage"] != true {
		t.Fatalf("stream_options = %v, want include_usage=true", so)
	}
}

func TestInjectStreamUsageOptionsNonStreaming(t *testing.T) {
	body := []byte(`{"model":"m1","stream":false,"messages":[{"role":"user","content":"hi"}]}`)
	out, err := InjectStreamUsageOptions(body)
	if err != nil {
		t.Fatal(err)
	}
	// Non-streaming: should return the body unchanged (no stream_options injection).
	if string(out) != string(body) {
		t.Fatalf("non-streaming body was modified: %s", out)
	}
}

func TestInjectStreamUsageOptionsPreservesExisting(t *testing.T) {
	body := []byte(`{"model":"m1","stream":true,"stream_options":{"include_usage":false},"messages":[{"role":"user","content":"hi"}]}`)
	out, err := InjectStreamUsageOptions(body)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	json.Unmarshal(out, &obj)
	so, _ := obj["stream_options"].(map[string]any)
	if so["include_usage"] != true {
		t.Fatalf("include_usage should be forced to true, got %v", so["include_usage"])
	}
}
