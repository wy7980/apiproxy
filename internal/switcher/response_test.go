package switcher

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAnthropicToOpenAIResponse(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"model": "claude-opus-4-8",
		"content": [{"type": "text", "text": "Hello!"}],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`)

	// DirOpenAItoAnthropic means: upstream is Anthropic → convert to OpenAI
	c := NewConverter(DirOpenAItoAnthropic)
	got, err := c.ConvertResponse(ctx, body)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	json.Unmarshal(got, &result)
	if result["object"] != "chat.completion" {
		t.Errorf("object = %v, want chat.completion", result["object"])
	}
	if result["id"] != "msg_123" {
		t.Errorf("id = %v, want msg_123", result["id"])
	}
	choices, _ := result["choices"].([]any)
	if len(choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(choices))
	}
	first := choices[0].(map[string]any)
	if first["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v, want stop", first["finish_reason"])
	}
}

func TestOpenAItoAnthropicResponse(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": "Hi!"},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`)

	// DirAnthropicToOpenAI means: upstream is OpenAI → convert to Anthropic
	c := NewConverter(DirAnthropicToOpenAI)
	got, err := c.ConvertResponse(ctx, body)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	json.Unmarshal(got, &result)
	if result["type"] != "message" {
		t.Errorf("type = %v, want message", result["type"])
	}
	if result["stop_reason"] != "end_turn" {
		t.Errorf("stop_reason = %v, want end_turn", result["stop_reason"])
	}
	usage, _ := result["usage"].(map[string]any)
	if usage["input_tokens"] != float64(10) {
		t.Errorf("input_tokens = %v, want 10", usage["input_tokens"])
	}
	if usage["output_tokens"] != float64(5) {
		t.Errorf("output_tokens = %v, want 5", usage["output_tokens"])
	}
}

func TestResponse_ParseErrorTolerant(t *testing.T) {
	ctx := context.Background()
	c := NewConverter(DirOpenAItoAnthropic)
	// Invalid JSON should be returned as-is (tolerant)
	got, err := c.ConvertResponse(ctx, []byte(`not json`))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "not json" {
		t.Errorf("tolerant parse: got %s, want original", string(got))
	}
}

func TestOffDirectionPassthrough(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{"test": "data"}`)
	c := NewConverter(DirOff)
	got, err := c.ConvertResponse(ctx, body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Errorf("off direction: expected passthrough, got %s", string(got))
	}
}
