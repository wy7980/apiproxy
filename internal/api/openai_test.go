package api

import "testing"

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
