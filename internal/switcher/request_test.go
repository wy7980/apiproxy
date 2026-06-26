package switcher

import (
	"context"
	"encoding/json"
	"testing"
)

func TestOpenAItoAnthropicRequest_Basic(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{
		"model": "gpt-4",
		"messages": [
			{"role": "system", "content": "You are helpful"},
			{"role": "user", "content": "Hello"}
		],
		"max_tokens": 100,
		"temperature": 0.7
	}`)

	c := NewConverter(DirOpenAItoAnthropic)
	got, err := c.ConvertRequest(ctx, body)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatal(err)
	}

	if result["model"] != "gpt-4" {
		t.Errorf("model = %v, want gpt-4", result["model"])
	}
	if result["max_tokens"] != float64(100) {
		t.Errorf("max_tokens = %v, want 100", result["max_tokens"])
	}
	// system should be set
	if result["system"] != "You are helpful" {
		t.Errorf("system = %v, want 'You are helpful'", result["system"])
	}
}

func TestOpenAItoAnthropicRequest_UnsupportedField(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}],"frequency_penalty":0.5}`)
	c := NewConverter(DirOpenAItoAnthropic)
	_, err := c.ConvertRequest(ctx, body)
	if err == nil {
		t.Error("expected error for unsupported field frequency_penalty")
	}
}

func TestSanitizeToolName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my_tool-1", "my_tool-1"},
		{"My Tool@#$", "My_Tool___"},
		{"", "tool"},
	}
	for _, tt := range tests {
		got := sanitizeToolName(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeToolName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestOpenAItoAnthropicRequest_WithTools(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": "What's the weather?"}],
		"tools": [{
			"type": "function",
			"function": {
				"name": "get_weather",
				"description": "Get weather for a location",
				"parameters": {"type": "object", "properties": {"loc": {"type": "string"}}}
			}
		}],
		"tool_choice": "auto"
	}`)

	c := NewConverter(DirOpenAItoAnthropic)
	got, err := c.ConvertRequest(ctx, body)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatal(err)
	}

	tools, _ := result["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool, _ := tools[0].(map[string]any)
	if tool["name"] != "get_weather" {
		t.Errorf("tool name = %v, want get_weather", tool["name"])
	}
}

func TestOpenAItoAnthropicRequest_ToolCallsInResponse(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{
		"model": "gpt-4",
		"messages": [
			{"role": "user", "content": "What's the weather in Paris?"},
			{"role": "assistant", "content": "", "tool_calls": [
				{"id": "call_123", "type": "function", "function": {"name": "get_weather", "arguments": "{\"loc\":\"Paris\"}"}}
			]},
			{"role": "tool", "content": "Sunny", "tool_call_id": "call_123"},
			{"role": "user", "content": "Thanks"}
		]
	}`)

	c := NewConverter(DirOpenAItoAnthropic)
	got, err := c.ConvertRequest(ctx, body)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatal(err)
	}

	messages, _ := result["messages"].([]any)
	// After conversion: user msg, assistant msg (with tool_use), user msg (tool_result + Thanks merged)
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages (merged consecutive user roles), got %d", len(messages))
	}

	// Check the third message merged role is user with tool_result content
	merged := messages[2].(map[string]any)
	if merged["role"] != "user" {
		t.Errorf("merged message role = %v, want user", merged["role"])
	}
}

func TestAnthropicToOpenAIRequest_Basic(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{
		"model": "claude-opus-4-8",
		"messages": [
			{"role": "user", "content": "Hello"}
		],
		"max_tokens": 200,
		"temperature": 1.0
	}`)

	c := NewConverter(DirAnthropicToOpenAI)
	got, err := c.ConvertRequest(ctx, body)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatal(err)
	}

	if result["max_completion_tokens"] != float64(200) {
		t.Errorf("max_completion_tokens = %v, want 200", result["max_completion_tokens"])
	}
}

func TestAnthropicToOpenAIRequest_Tools(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{
		"model": "claude-opus-4-8",
		"messages": [{"role": "user", "content": "hi"}],
		"max_tokens": 100,
		"tools": [{
			"name": "get_weather",
			"description": "Get weather",
			"input_schema": {"type": "object", "properties": {"loc": {"type": "string"}}}
		}]
	}`)

	c := NewConverter(DirAnthropicToOpenAI)
	got, err := c.ConvertRequest(ctx, body)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatal(err)
	}

	tools, _ := result["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool, _ := tools[0].(map[string]any)
	fn, _ := tool["function"].(map[string]any)
	if fn["name"] != "get_weather" {
		t.Errorf("tool function name = %v, want get_weather", fn["name"])
	}
}

func TestAnthropicToOpenAIRequest_SystemMessage(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{
		"model": "claude-opus-4-8",
		"system": "You are a helpful assistant",
		"messages": [{"role": "user", "content": "Hi"}],
		"max_tokens": 100
	}`)

	c := NewConverter(DirAnthropicToOpenAI)
	got, err := c.ConvertRequest(ctx, body)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatal(err)
	}

	messages, _ := result["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(messages))
	}
	sysMsg := messages[0].(map[string]any)
	if sysMsg["role"] != "system" {
		t.Errorf("first message role = %v, want system", sysMsg["role"])
	}
}
func TestTruncateToolName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"short", "short"},
		{"a very long tool name that exceeds the sixty four character limit for openai function names!", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateToolName(tt.name)
			if len(got) > 64 {
				t.Errorf("truncateToolName(%q) = %q (len=%d), want <= 64", tt.name, got, len(got))
			}
			if tt.want != "" && got != tt.want {
				t.Errorf("truncateToolName(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}
