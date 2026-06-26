package switcher

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// anthropicToOpenAIRequest converts an Anthropic Messages request body
// to an OpenAI Chat Completions request body.
func anthropicToOpenAIRequest(ctx context.Context, body []byte) ([]byte, error) {
	return anthropicToOpenAIRequestWithConverter(ctx, body, nil)
}

func anthropicToOpenAIRequestWithConverter(ctx context.Context, body []byte, conv *Converter) ([]byte, error) {
	var anthroReq map[string]any
	if err := json.Unmarshal(body, &anthroReq); err != nil {
		return nil, fmt.Errorf("anthropic-to-openai: parse request: %w", err)
	}

	openaiReq := make(map[string]any)

	// model: pass through
	copyField(anthroReq, openaiReq, "model")

	// Collect system text from both the top-level "system" field and
	// from "role: system" messages in the messages array.
	var systemParts []string

	// 1. Top-level system field
	if system, ok := anthroReq["system"]; ok && system != nil {
		switch s := system.(type) {
		case string:
			if s != "" {
				systemParts = append(systemParts, s)
			}
		case []any:
			for _, block := range s {
				if b, ok := block.(map[string]any); ok && b["type"] == "text" {
					if t, ok := b["text"].(string); ok {
						systemParts = append(systemParts, t)
					}
				}
			}
		}
	}

	// Build messages array, collecting inline system messages
	var messages []any
	anthroMessages, _ := anthroReq["messages"].([]any)
	for _, m := range anthroMessages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if role, _ := msg["role"].(string); role == "system" {
			// Collect system content from messages array
			switch c := msg["content"].(type) {
			case string:
				if c != "" {
					systemParts = append(systemParts, c)
				}
			case []any:
				for _, block := range c {
					if b, ok := block.(map[string]any); ok && b["type"] == "text" {
						if t, ok := b["text"].(string); ok {
							systemParts = append(systemParts, t)
						}
					}
				}
			}
			continue
		}
		openaiMsgs := convertAnthropicMessageToOpenAI(msg)
		messages = append(messages, openaiMsgs...)
	}

	// Prepend system message at the beginning (OpenAI requires system first)
	if len(systemParts) > 0 {
		combined := strings.Join(systemParts, "\n")
		messages = append([]any{map[string]any{
			"role":    "system",
			"content": combined,
		}}, messages...)
	}

	openaiReq["messages"] = messages

	// max_tokens → max_completion_tokens
	if mt, ok := anthroReq["max_tokens"]; ok {
		openaiReq["max_completion_tokens"] = mt
	}

	// temperature, top_p, top_k: pass through
	copyField(anthroReq, openaiReq, "temperature")
	copyField(anthroReq, openaiReq, "top_p")
	copyField(anthroReq, openaiReq, "top_k")

	// stop_sequences → stop
	if stop, ok := anthroReq["stop_sequences"]; ok {
		openaiReq["stop"] = stop
	}

	// stream: pass through
	copyField(anthroReq, openaiReq, "stream")

	// Tools: split web_search tools → web_search_options, rest → OpenAI function tools
	toolNameMapping := make(map[string]string)
	if tools, ok := anthroReq["tools"].([]any); ok && len(tools) > 0 {
		var webSearchTools, regularTools []any
		for _, t := range tools {
			tool, ok := t.(map[string]any)
			if !ok {
				continue
			}
			ttype, _ := tool["type"].(string)
			tname, _ := tool["name"].(string)
			if (strings.HasPrefix(ttype, "web_search")) || tname == "web_search" {
				webSearchTools = append(webSearchTools, tool)
			} else {
				regularTools = append(regularTools, tool)
			}
		}

		if len(webSearchTools) > 0 {
			openaiReq["web_search_options"] = map[string]any{}
		}

		if len(regularTools) > 0 {
			openaiTools := make([]any, 0, len(regularTools))
			for _, t := range regularTools {
				tool, ok := t.(map[string]any)
				if !ok {
					continue
				}
				ot, nm := convertAnthropicToolToOpenAIWithMapping(tool)
				if ot != nil {
					openaiTools = append(openaiTools, ot)
				}
				for k, v := range nm {
					toolNameMapping[k] = v
				}
			}
			if len(openaiTools) > 0 {
				openaiReq["tools"] = openaiTools
			}
		}
	}

	// tool_choice
	if tc, ok := anthroReq["tool_choice"]; ok {
		openaiReq["tool_choice"] = convertAnthropicToolChoiceToOpenAI(tc)
	}

	// thinking → reasoning_effort
	if thinking, ok := anthroReq["thinking"]; ok {
		if t, ok := thinking.(map[string]any); ok {
			budget := 0
			if bt, ok := t["budget_tokens"].(float64); ok {
				budget = int(bt)
			}
			effort := mapThinkingToEffort(budget)
			if effort != "" {
				openaiReq["reasoning_effort"] = effort
			}
		}
	}

	// output_config → response_format
	if oc, ok := anthroReq["output_config"]; ok {
		if ocMap, ok := oc.(map[string]any); ok {
			if format, ok := ocMap["format"]; ok {
				openaiReq["response_format"] = format
			}
		}
	}

	// metadata.user_id → user
	if metadata, ok := anthroReq["metadata"].(map[string]any); ok {
		if uid, ok := metadata["user_id"]; ok {
			openaiReq["user"] = uid
		}
	}

	// disable_parallel_tool_use → parallel_tool_calls
	if dpt, ok := anthroReq["disable_parallel_tool_use"]; ok {
		if disabled, ok := dpt.(bool); ok {
			openaiReq["parallel_tool_calls"] = !disabled
		}
	}

	// Store tool name mapping on converter for response conversion
	if len(toolNameMapping) > 0 && conv != nil {
		conv.toolNameMapping = toolNameMapping
	}

	return json.Marshal(openaiReq)
}

// convertAnthropicMessageToOpenAI converts one Anthropic message to 0+ OpenAI messages.
// tool_result blocks in user messages are expanded into separate role:"tool" messages.
func convertAnthropicMessageToOpenAI(msg map[string]any) []any {
	role, _ := msg["role"].(string)
	content := msg["content"]

	switch role {
	case "user":
		var result []any
		var textParts []string
		hasImage := false

		switch c := content.(type) {
		case string:
			result = append(result, map[string]any{"role": "user", "content": c})
			return result
		case []any:
			for _, block := range c {
				b, ok := block.(map[string]any)
				if !ok {
					continue
				}
				switch b["type"] {
				case "text":
					text, _ := b["text"].(string)
					textParts = append(textParts, text)
				case "image":
					hasImage = true
					source, _ := b["source"].(map[string]any)
					if source != nil {
						data, _ := source["data"].(string)
						mediaType, _ := source["media_type"].(string)
						if mediaType == "" {
							mediaType = "image/jpeg"
						}
						textParts = append(textParts, fmt.Sprintf("data:%s;base64,%s", mediaType, data))
					}
				case "tool_result":
					tc, ok := b["content"]
					toolUseID, _ := b["tool_use_id"].(string)
					if !ok || toolUseID == "" {
						continue
					}
					toolContent := ""
					switch ctc := tc.(type) {
					case string:
						toolContent = ctc
					case []any:
						for _, ct := range ctc {
							if ctb, ok := ct.(map[string]any); ok && ctb["type"] == "text" {
								if t, ok := ctb["text"].(string); ok {
									toolContent += t
								}
							}
						}
					}
					result = append(result, map[string]any{
						"role":         "tool",
						"tool_call_id": toolUseID,
						"content":      toolContent,
					})
				case "compaction":
					continue
				}
			}

			if len(textParts) == 0 && len(result) > 0 {
				return result
			}

			userContent := any(strings.Join(textParts, ""))
			if hasImage {
				userContent = textParts
			}
			result = append(result, map[string]any{"role": "user", "content": userContent})
			return result
		}

	case "assistant":
		return []any{convertAnthropicAssistantToOpenAI(content)}
	default:
		return []any{map[string]any{"role": role, "content": content}}
	}
	return nil
}

// convertAnthropicAssistantToOpenAI converts Anthropic assistant content to OpenAI format.
func convertAnthropicAssistantToOpenAI(content any) map[string]any {
	msg := map[string]any{"role": "assistant"}
	var text string
	var toolCalls []any

	switch c := content.(type) {
	case string:
		text = c
	case []any:
		for _, block := range c {
			b, ok := block.(map[string]any)
			if !ok {
				continue
			}
			switch b["type"] {
			case "text":
				text, _ = b["text"].(string)
				if cc, ok := b["cache_control"]; ok {
					msg["cache_control"] = cc
				}
			case "thinking":
			case "tool_use":
				id, _ := b["id"].(string)
				name, _ := b["name"].(string)
				input, _ := b["input"].(map[string]any)
				inputJSON, _ := json.Marshal(input)
				tc := map[string]any{
					"id":   id,
					"type": "function",
					"function": map[string]any{
						"name":      truncateToolName(name),
						"arguments": string(inputJSON),
					},
				}
				if cc, ok := b["cache_control"]; ok {
					tc["cache_control"] = cc
				}
				toolCalls = append(toolCalls, tc)
			}
		}
	}

	msg["content"] = text
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}
	return msg
}

// ---------- Tool name truncation (OpenAI 64-char limit) ----------

const (
	openaiMaxToolNameLength = 64
	toolNameHashLength      = 8
	toolNamePrefixLength    = openaiMaxToolNameLength - toolNameHashLength - 1
)

// truncateToolName truncates tool names over 64 chars using a hash suffix.
// LiteLLM compatible: {55-char-prefix}_{8-char-hash}
func truncateToolName(name string) string {
	if len(name) <= openaiMaxToolNameLength {
		return name
	}
	hash := sha256.Sum256([]byte(name))
	hashHex := fmt.Sprintf("%x", hash)[:toolNameHashLength]
	return name[:toolNamePrefixLength] + "_" + hashHex
}

// convertAnthropicToolToOpenAIWithMapping converts tool to OpenAI format with name mapping.
func convertAnthropicToolToOpenAIWithMapping(tool map[string]any) (map[string]any, map[string]string) {
	mapping := make(map[string]string)
	originalName, _ := tool["name"].(string)
	if originalName == "" {
		originalName = "tool"
	}
	truncatedName := truncateToolName(originalName)
	if truncatedName != originalName {
		mapping[truncatedName] = originalName
	}

	fn := map[string]any{
		"name": truncatedName,
	}
	if desc, ok := tool["description"]; ok {
		fn["description"] = desc
	}
	if schema, ok := tool["input_schema"]; ok {
		fn["parameters"] = schema
	}
	// LiteLLM: pass through cache_control on tool definitions
	if cc, ok := tool["cache_control"]; ok {
		fn["cache_control"] = cc
	}
	return map[string]any{
		"type":     "function",
		"function": fn,
	}, mapping
}

// convertAnthropicToolToOpenAI (simple version without mapping).
func convertAnthropicToolToOpenAI(tool map[string]any) map[string]any {
	out, _ := convertAnthropicToolToOpenAIWithMapping(tool)
	return out
}

// convertAnthropicToolChoiceToOpenAI converts Anthropic tool_choice to OpenAI format.
func convertAnthropicToolChoiceToOpenAI(tc any) any {
	switch v := tc.(type) {
	case map[string]any:
		ttype, _ := v["type"].(string)
		switch ttype {
		case "auto":
			return "auto"
		case "any":
			return "required"
		case "tool":
			name, _ := v["name"].(string)
			return map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": truncateToolName(name),
				},
			}
		case "none":
			return "none"
		}
	}
	return "auto"
}

// mapThinkingToEffort maps Anthropic thinking budget_tokens to OpenAI reasoning_effort.
// Matches LiteLLM: >=10000 → high, >=5000 → medium, >=2000 → low.
// Returns empty string if budget is 0 (no thinking).
func mapThinkingToEffort(budget int) string {
	if budget <= 0 {
		return ""
	}
	switch {
	case budget >= 10000:
		return "high"
	case budget >= 5000:
		return "medium"
	default:
		return "low"
	}
}