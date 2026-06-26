package switcher

import (
	"context"
	"strings"
	"testing"
)

func TestAnthropicSSEToOpenAIChunk_MessageStart(t *testing.T) {
	c := NewConverter(DirOpenAItoAnthropic)
	input := []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-opus-4-8\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":10}}}\n\n")
	got, err := c.ConvertStreamChunk(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"role":"assistant"`) {
		t.Errorf("expected role=assistant in OpenAI chunk, got: %s", string(got))
	}
}

func TestAnthropicSSEToOpenAIChunk_TextDelta(t *testing.T) {
	c := NewConverter(DirOpenAItoAnthropic)
	// First need message_start to set state
	c.state.haveMessageStart = true
	c.state.haveContentBlock = true

	input := []byte("event: content_block_delta\ndata: {\"type\":\"text_delta\",\"text\":\"Hello\"}\n\n")
	got, err := c.ConvertStreamChunk(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"content":"Hello"`) {
		t.Errorf("expected content delta with Hello, got: %s", string(got))
	}
}

func TestAnthropicSSEToOpenAIChunk_MessageDelta(t *testing.T) {
	c := NewConverter(DirOpenAItoAnthropic)
	input := []byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null,\"usage\":{\"output_tokens\":5}}}\n\n")
	got, err := c.ConvertStreamChunk(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"finish_reason":"stop"`) {
		t.Errorf("expected finish_reason=stop, got: %s", string(got))
	}
}

func TestOpenAISSEToAnthropicChunk_Content(t *testing.T) {
	c := NewConverter(DirAnthropicToOpenAI)
	input := []byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n")
	got, err := c.ConvertStreamChunk(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "event: message_start") {
		t.Errorf("expected message_start event, got: %s", string(got))
	}
	if !strings.Contains(string(got), "text_delta") {
		t.Errorf("expected text_delta event, got: %s", string(got))
	}
}

func TestOpenAISSEToAnthropicChunk_Done(t *testing.T) {
	c := NewConverter(DirAnthropicToOpenAI)
	input := []byte("data: [DONE]\n\n")
	got, err := c.ConvertStreamChunk(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "event: message_stop") {
		t.Errorf("expected message_stop event, got: %s", string(got))
	}
}

func TestOpenAISSEToAnthropicChunk_ToolCalls(t *testing.T) {
	c := NewConverter(DirAnthropicToOpenAI)
	input := []byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_123\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"{\\\"loc\\\":\\\"Paris\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
	got, err := c.ConvertStreamChunk(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "event: content_block_start") {
		t.Errorf("expected content_block_start event, got: %s", string(got))
	}
	if !strings.Contains(string(got), "tool_use") {
		t.Errorf("expected tool_use in event, got: %s", string(got))
	}
}

func TestEmptyChunkIgnored(t *testing.T) {
	c := NewConverter(DirOpenAItoAnthropic)
	got, err := c.ConvertStreamChunk(context.Background(), []byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil for empty chunk, got: %s", string(got))
	}
}

func TestPingChunkIgnored(t *testing.T) {
	c := NewConverter(DirOpenAItoAnthropic)
	got, err := c.ConvertStreamChunk(context.Background(), []byte(": ping\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil for comment chunk, got: %s", string(got))
	}
}
