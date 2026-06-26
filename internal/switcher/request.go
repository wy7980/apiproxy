package switcher

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// openaiToAnthropicRequest converts an OpenAI Chat Completions request body
// to an Anthropic Messages request body. Returns descriptive 400-level error
// on unsupported fields.
func openaiToAnthropicRequest(ctx context.Context, body []byte) ([]byte, error) {
	var openaiReq map[string]any
	if err := json.Unmarshal(body, &openaiReq); err != nil {
		return nil, fmt.Errorf("openai-to-anthropic: parse request: %w", err)
	}

	anthropicReq := make(map[string]any)

	// model: pass through
	if model, ok := openaiReq["model"]; ok {
		anthropicReq["model"] = model
	}

	// system: extract from messages where role=system
	messages, _ := openaiReq["messages"].([]any)
	var systemParts []string
	var chatMessages []any
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role == "system" {
			content, _ := msg["content"].(string)
			if content != "" {
				systemParts = append(systemParts, content)
			}
			continue // skip system messages in the anthropic messages array
		}
		// Convert OpenAI messages to Anthropic format
		chatMessages = append(chatMessages, convertOpenAIMessageToAnthropic(msg))
	}
	if len(systemParts) > 0 {
		if len(systemParts) == 1 {
			anthropicReq["system"] = systemParts[0]
		} else {
			anthropicReq["system"] = systemParts
		}
	}
	// Role-merge consecutive user+tool messages into one user turn
	chatMessages = mergeAnthropicRoles(chatMessages)
	anthropicReq["messages"] = chatMessages

	// max_tokens / max_completion_tokens → max_tokens
	maxTokens := openaiReq["max_completion_tokens"]
	if maxTokens == nil {
		maxTokens = openaiReq["max_tokens"]
	}
	if maxTokens != nil {
		anthropicReq["max_tokens"] = maxTokens
	}

	// temperature, top_p: pass through
	copyField(openaiReq, anthropicReq, "temperature")
	copyField(openaiReq, anthropicReq, "top_p")
	copyField(openaiReq, anthropicReq, "top_k")

	// stop → stop_sequences
	if stop, ok := openaiReq["stop"]; ok {
		anthropicReq["stop_sequences"] = stop
	}

	// stream: pass through
	copyField(openaiReq, anthropicReq, "stream")

	// stream_options: remove (Anthropic handles this internally)
	delete(openaiReq, "stream_options")

	// tools
	if tools, ok := openaiReq["tools"].([]any); ok && len(tools) > 0 {
		anthropicTools := make([]any, 0, len(tools))
		for _, t := range tools {
			tool, ok := t.(map[string]any)
			if !ok {
				continue
			}
			at, err := convertOpenAIToolToAnthropic(tool)
			if err != nil {
				return nil, err
			}
			anthropicTools = append(anthropicTools, at)
		}
		if len(anthropicTools) > 0 {
			anthropicReq["tools"] = anthropicTools
		}
	}

	// tool_choice
	if tc, ok := openaiReq["tool_choice"]; ok {
		anthropicReq["tool_choice"] = convertToolChoiceToAnthropic(tc)
	}

	// parallel_tool_calls → disable_parallel_tool_use
	if ptc, ok := openaiReq["parallel_tool_calls"]; ok {
		if ptcBool, ok := ptc.(bool); ok && !ptcBool {
			anthropicReq["disable_parallel_tool_use"] = true
		}
	}

	// reasoning_effort → thinking + output_config.effort
	if re, ok := openaiReq["reasoning_effort"]; ok {
		effort, ok := re.(string)
		if ok {
			_, outputEffort := mapReasoningEffort(effort)
			if outputEffort != "" {
				anthropicReq["output_config"] = map[string]any{
					"effort": outputEffort,
				}
			}
		}
	}

	// response_format → output_config.format
	if rf, ok := openaiReq["response_format"]; ok {
		rfMap, ok := rf.(map[string]any)
		if ok {
			if anthropicReq["output_config"] == nil {
				anthropicReq["output_config"] = make(map[string]any)
			}
			oc, _ := anthropicReq["output_config"].(map[string]any)
			oc["format"] = rfMap
		}
	}

	// user → metadata.user_id
	if user, ok := openaiReq["user"]; ok {
		anthropicReq["metadata"] = map[string]any{
			"user_id": user,
		}
	}

	// Unsupported fields: return 400
	unsupported := []string{"frequency_penalty", "presence_penalty", "logit_bias", "seed", "n", "top_logprobs"}
	for _, field := range unsupported {
		if _, ok := openaiReq[field]; ok {
			return nil, fmt.Errorf("openai-to-anthropic: field %q is not supported by Anthropic", field)
		}
	}

	return json.Marshal(anthropicReq)
}

// copyField copies field from src to dst if present.
func copyField(src, dst map[string]any, field string) {
	if v, ok := src[field]; ok {
		dst[field] = v
	}
}

// convertOpenAIMessageToAnthropic converts one OpenAI message to Anthropic format.
func convertOpenAIMessageToAnthropic(msg map[string]any) map[string]any {
	role, _ := msg["role"].(string)
	anthro := map[string]any{"role": role}

	switch role {
	case "user":
		content := msg["content"]
		anthro["content"] = convertUserContentToAnthropic(content)
	case "assistant":
		content := msg["content"]
		// If there are tool_calls, merge them into content blocks
		toolCalls, _ := msg["tool_calls"].([]any)
		anthro["content"] = convertAssistantContentToAnthropic(content, toolCalls)
	case "tool":
		anthro["role"] = "user" // Anthropic uses user role with tool_result content
		content, _ := msg["content"].(string)
		toolCallID, _ := msg["tool_call_id"].(string)
		anthro["content"] = []any{
			map[string]any{
				"type":       "tool_result",
				"tool_use_id": toolCallID,
				"content":    content,
			},
		}
	}

	return anthro
}

// convertUserContentToAnthropic handles text, images for user messages.
func convertUserContentToAnthropic(content any) any {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		blocks := make([]any, 0, len(c))
		for _, block := range c {
			b, ok := block.(map[string]any)
			if !ok {
				continue
			}
			btype, _ := b["type"].(string)
			switch btype {
			case "text":
				text, _ := b["text"].(string)
				if text == "" {
					text = " " // Anthropic rejects empty text blocks
				}
				blocks = append(blocks, map[string]any{"type": "text", "text": text})
			case "image_url":
				url, _ := b["image_url"].(map[string]any)
				u, _ := url["url"].(string)
				imageBlock := convertOpenAIImageToAnthropic(u)
				if imageBlock != nil {
					blocks = append(blocks, imageBlock)
				}
			}
		}
		return blocks
	default:
		return ""
	}
}

func convertOpenAIImageToAnthropic(url string) map[string]any {
	// Handle data: URIs
	if strings.HasPrefix(url, "data:") {
		parts := strings.SplitN(url, ",", 2)
		if len(parts) != 2 {
			return nil
		}
		mediaType := "image/jpeg"
		if strings.HasPrefix(parts[0], "data:image/") {
			mediaType = strings.TrimPrefix(parts[0], "data:")
			mediaType = strings.TrimSuffix(mediaType, ";base64")
		}
		return map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": mediaType,
				"data":       parts[1],
			},
		}
	}
	return nil
}

// convertAssistantContentToAnthropic merges text content with tool_calls.
func convertAssistantContentToAnthropic(content any, toolCalls []any) []any {
	var blocks []any

	// Add text content
	switch c := content.(type) {
	case string:
		if c != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": c})
		}
	case []any:
		for _, block := range c {
			if b, ok := block.(map[string]any); ok {
				btype, _ := b["type"].(string)
				if btype == "text" {
					text, _ := b["text"].(string)
					if text == "" {
						text = " "
					}
					blocks = append(blocks, map[string]any{"type": "text", "text": text})
				}
			}
		}
	}

	// Add tool_calls as tool_use content blocks
	for _, tc := range toolCalls {
		t, ok := tc.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := t["function"].(map[string]any)
		name, _ := fn["name"].(string)
		arguments, _ := fn["arguments"].(string)

		var input map[string]any
		if arguments != "" {
			json.Unmarshal([]byte(arguments), &input)
		}
		if input == nil {
			input = make(map[string]any)
		}

		id, _ := t["id"].(string)
		blocks = append(blocks, map[string]any{
			"type":  "tool_use",
			"id":    id,
			"name":  sanitizeToolName(name),
			"input": input,
		})
	}

	if len(blocks) == 0 {
		return []any{map[string]any{"type": "text", "text": " "}}
	}
	return blocks
}

func convertOpenAIToolToAnthropic(tool map[string]any) (map[string]any, error) {
	fn, ok := tool["function"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tool missing function definition")
	}
	name, _ := fn["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("tool function name is required")
	}
	desc, _ := fn["description"].(string)
	params, _ := fn["parameters"].(map[string]any)

	anthropicTool := map[string]any{
		"name":         sanitizeToolName(name),
		"description":  desc,
		"input_schema": params,
	}

	// Handle the "type" field: OpenAI uses "function" wrapper, Anthropic uses direct types
	ttype, _ := tool["type"].(string)
	if ttype != "" && ttype != "function" {
		anthropicTool["type"] = ttype
	}

	return anthropicTool, nil
}

func convertToolChoiceToAnthropic(tc any) map[string]any {
	switch v := tc.(type) {
	case string:
		switch v {
		case "auto":
			return map[string]any{"type": "auto"}
		case "required":
			return map[string]any{"type": "any"}
		case "none":
			return map[string]any{"type": "none"}
		}
	case map[string]any:
		tcType, _ := v["type"].(string)
		if tcType == "function" {
			fnName, _ := v["function"].(map[string]any)
			if name, ok := fnName["name"]; ok {
				return map[string]any{
					"type": "tool",
					"name": sanitizeToolName(fmt.Sprintf("%v", name)),
				}
			}
		}
	}
	return map[string]any{"type": "auto"}
}

// mergeAnthropicRoles merges consecutive user+tool messages into single user turns.
func mergeAnthropicRoles(messages []any) []any {
	merged := make([]any, 0, len(messages))
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			merged = append(merged, m)
			continue
		}
		role, _ := msg["role"].(string)
		if role == "user" && len(merged) > 0 {
			last := merged[len(merged)-1]
			if lastMsg, ok := last.(map[string]any); ok {
				lastRole, _ := lastMsg["role"].(string)
				if lastRole == "user" {
					// Merge content
					mergedContent := mergeContentBlocks(lastMsg["content"], msg["content"])
					lastMsg["content"] = mergedContent
					continue
				}
			}
		}
		merged = append(merged, msg)
	}
	return merged
}

func mergeContentBlocks(a, b any) any {
	aBlocks, aOK := toContentBlocks(a)
	bBlocks, bOK := toContentBlocks(b)
	if !aOK || !bOK {
		return b
	}
	return append(aBlocks, bBlocks...)
}

func toContentBlocks(v any) ([]any, bool) {
	switch val := v.(type) {
	case string:
		if val == "" {
			return []any{map[string]any{"type": "text", "text": " "}}, true
		}
		return []any{map[string]any{"type": "text", "text": val}}, true
	case []any:
		return val, true
	}
	return nil, false
}

// mapReasoningEffort maps OpenAI reasoning_effort to Anthropic thinking/output_config.
// Returns (thinking map or nil, effort string).
func mapReasoningEffort(effort string) (map[string]any, string) {
	switch effort {
	case "low":
		return map[string]any{"type": "enabled", "budget_tokens": 2048}, "low"
	case "medium":
		return map[string]any{"type": "enabled", "budget_tokens": 8192}, "medium"
	case "high":
		return map[string]any{"type": "enabled", "budget_tokens": 16384}, "high"
	case "none":
		return nil, "" // disable thinking
	default:
		return nil, ""
	}
}

// sanitizeToolName ensures the name matches Anthropic's regex: ^[a-zA-Z0-9_-]{1,128}$
func sanitizeToolName(name string) string {
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			result.WriteRune(r)
		} else {
			result.WriteRune('_')
		}
	}
	s := result.String()
	if len(s) > 128 {
		s = s[:128]
	}
	if s == "" {
		s = "tool"
	}
	return s
}
