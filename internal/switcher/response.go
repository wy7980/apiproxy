package switcher

import (
	"context"
	"encoding/json"
	"time"
)

// anthropicToOpenAIResponse converts an Anthropic non-streaming response to
// OpenAI Chat Completions format.
func anthropicToOpenAIResponse(ctx context.Context, body []byte) ([]byte, error) {
	var anthroResp map[string]any
	if err := json.Unmarshal(body, &anthroResp); err != nil {
		// Tolerant: return original on parse error
		return body, nil
	}

	openaiResp := map[string]any{
		"id":      anthroResp["id"],
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   anthroResp["model"],
	}

	// Choices
	var choices []any
	choice := map[string]any{
		"index":         0,
		"finish_reason": mapAnthropicStopReason(anthroResp["stop_reason"]),
	}

	// Message
	msg := map[string]any{"role": "assistant"}
	content, _ := anthroResp["content"].([]any)
	var text string
	var toolCalls []any
	for _, c := range content {
		block, ok := c.(map[string]any)
		if !ok {
			continue
		}
		switch block["type"] {
		case "text":
			text, _ = block["text"].(string)
		case "tool_use":
			id, _ := block["id"].(string)
			name, _ := block["name"].(string)
			input, _ := block["input"].(map[string]any)
			args, _ := json.Marshal(input)
			toolCalls = append(toolCalls, map[string]any{
				"id":   id,
				"type": "function",
				"function": map[string]any{
					"name":      name,
					"arguments": string(args),
				},
			})
		}
	}
	msg["content"] = text
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}
	choice["message"] = msg
	choices = append(choices, choice)
	openaiResp["choices"] = choices

	// Usage
	if usage, ok := anthroResp["usage"].(map[string]any); ok {
		inTokens := toInt(usage["input_tokens"])
		outTokens := toInt(usage["output_tokens"])
		openaiResp["usage"] = map[string]any{
			"prompt_tokens":     inTokens,
			"completion_tokens": outTokens,
			"total_tokens":      inTokens + outTokens,
		}
	}

	return json.Marshal(openaiResp)
}

// openaiToAnthropicResponse converts an OpenAI non-streaming response to
// Anthropic Messages format.
func openaiToAnthropicResponse(ctx context.Context, body []byte) ([]byte, error) {
	var openaiResp map[string]any
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		return body, nil
	}

	anthroResp := map[string]any{
		"id":      openaiResp["id"],
		"type":    "message",
		"role":    "assistant",
		"model":   openaiResp["model"],
		"content": []any{},
	}

	// Extract content and tool_calls from choices
	if choices, ok := openaiResp["choices"].([]any); ok && len(choices) > 0 {
		first, _ := choices[0].(map[string]any)
		anthroResp["stop_reason"] = mapOpenAIStopReason(first["finish_reason"])
		anthroResp["stop_sequence"] = nil

		if msg, ok := first["message"].(map[string]any); ok {
			var contentBlocks []any

			// Text content
			textContent, _ := msg["content"].(string)
			if textContent != "" {
				contentBlocks = append(contentBlocks, map[string]any{
					"type": "text",
					"text": textContent,
				})
			}

			// Tool calls
			if toolCalls, ok := msg["tool_calls"].([]any); ok {
				for _, tc := range toolCalls {
					t, ok := tc.(map[string]any)
					if !ok {
						continue
					}
					fn, _ := t["function"].(map[string]any)
					name, _ := fn["name"].(string)
					args, _ := fn["arguments"].(string)
					var input any
					json.Unmarshal([]byte(args), &input)
					contentBlocks = append(contentBlocks, map[string]any{
						"type":  "tool_use",
						"id":    t["id"],
						"name":  name,
						"input": input,
					})
				}
			}

			if len(contentBlocks) == 0 {
				contentBlocks = append(contentBlocks, map[string]any{
					"type": "text",
					"text": "",
				})
			}
			anthroResp["content"] = contentBlocks
		}
	}

	// Usage
	if usage, ok := openaiResp["usage"].(map[string]any); ok {
		inTokens := toInt(usage["prompt_tokens"])
		outTokens := toInt(usage["completion_tokens"])
		anthroResp["usage"] = map[string]any{
			"input_tokens":  inTokens,
			"output_tokens": outTokens,
		}
	}

	return json.Marshal(anthroResp)
}

// mapAnthropicStopReason maps Anthropic stop_reason to OpenAI finish_reason.
func mapAnthropicStopReason(reason any) string {
	switch r := reason.(type) {
	case string:
		switch r {
		case "end_turn":
			return "stop"
		case "max_tokens":
			return "length"
		case "tool_use":
			return "tool_calls"
		case "stop_sequence":
			return "stop"
		}
	}
	return "stop"
}

// mapOpenAIStopReason maps OpenAI finish_reason to Anthropic stop_reason.
func mapOpenAIStopReason(reason any) string {
	switch r := reason.(type) {
	case string:
		switch r {
		case "stop":
			return "end_turn"
		case "length":
			return "max_tokens"
		case "tool_calls":
			return "tool_use"
		case "content_filter":
			return "end_turn"
		}
	}
	return "end_turn"
}

// toInt converts a JSON number value to int.
func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}
