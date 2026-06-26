package switcher

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// StreamState tracks the SSE state machine for streaming conversion.
type StreamState struct {
	pendingToolUseID string
	pendingToolName  string
	pendingToolInput string
	haveContentBlock bool
	haveMessageStart bool
	inputTokens      int
	outputTokens     int
}

// anthropicSSEToOpenAIChunk converts one Anthropic SSE event line to 0+ OpenAI
// SSE data chunks. Multiple OpenAI chunks may be emitted for one Anthropic event.
func anthropicSSEToOpenAIChunk(c *Converter, ctx context.Context, chunk []byte) ([]byte, error) {
	line := strings.TrimSpace(string(chunk))
	if line == "" || strings.HasPrefix(line, ":") {
		// Comment/ping → ignore
		return nil, nil
	}

	// Parse SSE event
	var eventType string
	var dataStr string
	for _, l := range strings.Split(line, "\n") {
		if strings.HasPrefix(l, "event: ") {
			eventType = strings.TrimPrefix(l, "event: ")
		} else if strings.HasPrefix(l, "data: ") {
			dataStr = strings.TrimPrefix(l, "data: ")
		}
	}
	if dataStr == "" {
		return nil, nil
	}

	state := &c.state
	var out []byte
	var err error

	switch eventType {
	case "message_start":
		var msg struct {
			Message struct {
				ID    string `json:"id"`
				Model string `json:"model"`
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if uerr := json.Unmarshal([]byte(dataStr), &msg); uerr != nil {
			return nil, uerr
		}
		state.haveMessageStart = true
		state.inputTokens = msg.Message.Usage.InputTokens
		state.outputTokens = msg.Message.Usage.OutputTokens
		// Emit: chunk with role=assistant
		chunkData := map[string]any{
			"choices": []any{
				map[string]any{
					"index": 0,
					"delta": map[string]any{
						"role":    "assistant",
						"content": "",
					},
					"finish_reason": nil,
				},
			},
		}
		out = append(out, formatOpenAIChunk(chunkData)...)

	case "content_block_start":
		var block struct {
			Type  string         `json:"type"`
			Text  string         `json:"text"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		}
		if uerr := json.Unmarshal([]byte(dataStr), &block); uerr != nil {
			return nil, uerr
		}
		state.haveContentBlock = true
		switch block.Type {
		case "text":
			if block.Text != "" {
				chunkData := map[string]any{
					"choices": []any{
						map[string]any{
							"index": 0,
							"delta": map[string]any{
								"content": block.Text,
							},
						},
					},
				}
				out = append(out, formatOpenAIChunk(chunkData)...)
			}
		case "tool_use":
			state.pendingToolUseID = block.ID
			state.pendingToolName = block.Name
			inputJSON, _ := json.Marshal(block.Input)
			state.pendingToolInput = string(inputJSON)
			chunkData := map[string]any{
				"choices": []any{
					map[string]any{
						"index": 0,
						"delta": map[string]any{
							"tool_calls": []any{
								map[string]any{
									"index": 0,
									"id":    block.ID,
									"type":  "function",
									"function": map[string]any{
										"name":      block.Name,
										"arguments": string(inputJSON),
									},
								},
							},
						},
					},
				},
			}
			out = append(out, formatOpenAIChunk(chunkData)...)
		}

	case "content_block_delta":
		var delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			PartialJSON string `json:"partial_json"`
		}
		if uerr := json.Unmarshal([]byte(dataStr), &delta); uerr != nil {
			return nil, uerr
		}
		switch delta.Type {
		case "text_delta":
			chunkData := map[string]any{
				"choices": []any{
					map[string]any{
						"index": 0,
						"delta": map[string]any{
							"content": delta.Text,
						},
					},
				},
			}
			out = append(out, formatOpenAIChunk(chunkData)...)
		case "input_json_delta":
			if delta.PartialJSON != "" {
				chunkData := map[string]any{
					"choices": []any{
						map[string]any{
							"index": 0,
							"delta": map[string]any{
								"tool_calls": []any{
									map[string]any{
										"index": 0,
										"function": map[string]any{
											"arguments": delta.PartialJSON,
										},
									},
								},
							},
						},
					},
				}
				out = append(out, formatOpenAIChunk(chunkData)...)
			}
		case "thinking_delta":
			// Strip thinking blocks — no OpenAI equivalent
		case "signature_delta":
			// Cached for usage, not forwarded
		}

	case "content_block_stop":
		// No output needed; just state transition

	case "message_delta":
		var delta struct {
			StopReason string `json:"stop_reason"`
			StopSeq    any    `json:"stop_sequence"`
			Usage      struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if uerr := json.Unmarshal([]byte(dataStr), &delta); uerr != nil {
			return nil, uerr
		}
		chunkData := map[string]any{
			"choices": []any{
				map[string]any{
					"index":         0,
					"delta":         map[string]any{},
					"finish_reason": mapAnthropicStopReason(delta.StopReason),
				},
			},
		}
		if delta.Usage.OutputTokens > 0 {
			state.outputTokens = delta.Usage.OutputTokens
		}
		out = append(out, formatOpenAIChunk(chunkData)...)

	case "message_stop":
		// No output; stream is complete

	case "ping":
		// No output
	}

	return out, err
}

// openaiSSEToAnthropicChunk converts an OpenAI SSE chunk to Anthropic SSE events.
// Each OpenAI chunk may produce 0+ Anthropic events, separated by \n\n.
func openaiSSEToAnthropicChunk(c *Converter, ctx context.Context, chunk []byte) ([]byte, error) {
	// Parse data: {...}\n\n
	line := strings.TrimSpace(string(chunk))
	if !strings.HasPrefix(line, "data: ") {
		return nil, nil
	}
	dataStr := strings.TrimPrefix(line, "data: ")
	if dataStr == "[DONE]" {
		// Emit message_stop
		return []byte("event: message_stop\ndata: {}\n\n"), nil
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, nil
	}

	state := &c.state
	var events []string

	choices, _ := data["choices"].([]any)
	if len(choices) == 0 {
		return nil, nil
	}
	firstChoice, _ := choices[0].(map[string]any)
	d, _ := firstChoice["delta"].(map[string]any)

	// role present → message_start
	if role, ok := d["role"]; ok && !state.haveMessageStart {
		state.haveMessageStart = true
		msgEvent := map[string]any{
			"message": map[string]any{
				"id":    data["id"],
				"type":  "message",
				"role":  role,
				"model": data["model"],
				"content": []any{},
			},
		}
		msgJSON, _ := json.Marshal(msgEvent)
		events = append(events, "event: message_start\ndata: "+string(msgJSON))
	}

	// content present → text block start + delta
	if content, ok := d["content"]; ok {
		if contentStr, ok := content.(string); ok && contentStr != "" {
			if !state.haveContentBlock {
				state.haveContentBlock = true
				blockStart := map[string]any{
					"type": "text",
					"text": "",
				}
				bsJSON, _ := json.Marshal(blockStart)
				events = append(events, "event: content_block_start\ndata: "+string(bsJSON))
			}
			deltaEvent := map[string]any{
				"type": "text_delta",
				"text": contentStr,
			}
			deJSON, _ := json.Marshal(deltaEvent)
			events = append(events, "event: content_block_delta\ndata: "+string(deJSON))
		}
	}

	// tool_calls present → tool_use block
	if toolCalls, ok := d["tool_calls"].([]any); ok {
		for _, tc := range toolCalls {
			t, _ := tc.(map[string]any)
			fn, _ := t["function"].(map[string]any)
			name, _ := fn["name"].(string)
			argsStr, _ := fn["arguments"].(string)

			if name != "" {
				// Start new tool_use block
				state.pendingToolUseID, _ = t["id"].(string)
				state.pendingToolName = name
				state.pendingToolInput = ""
				if state.pendingToolUseID == "" {
					state.pendingToolUseID = "toolu_" + fmt.Sprintf("%x", time.Now().UnixNano())
				}
				blockStart := map[string]any{
					"type": "tool_use",
					"id":   state.pendingToolUseID,
					"name": name,
					"input": map[string]any{},
				}
				bsJSON, _ := json.Marshal(blockStart)
				events = append(events, "event: content_block_start\ndata: "+string(bsJSON))
			}

			// arguments delta (may be partial)
			if argsStr != "" {
				deltaEvent := map[string]any{
					"type":         "input_json_delta",
					"partial_json": argsStr,
				}
				deJSON, _ := json.Marshal(deltaEvent)
				events = append(events, "event: content_block_delta\ndata: "+string(deJSON))
			}

			// Stop the tool_use block if arguments is complete JSON
			if name != "" && json.Valid([]byte(argsStr)) {
				events = append(events, "event: content_block_stop\ndata: {}")
				state.haveContentBlock = false
			}
		}
	}

	// finish_reason present → message_delta with stop_reason
	if fr, ok := firstChoice["finish_reason"]; ok && fr != nil {
		msgDelta := map[string]any{
			"stop_reason":   mapOpenAIStopReason(fr),
			"stop_sequence": nil,
		}
		// Include usage if present
		if usage, ok := data["usage"].(map[string]any); ok && len(usage) > 0 {
			msgDelta["usage"] = map[string]any{
				"output_tokens": toInt(usage["completion_tokens"]),
			}
			if pt := toInt(usage["prompt_tokens"]); pt > 0 {
				state.inputTokens = pt
			}
		}
		mdJSON, _ := json.Marshal(msgDelta)
		events = append(events, "event: message_delta\ndata: "+string(mdJSON))
	}

	return []byte(strings.Join(events, "\n\n") + "\n\n"), nil
}

// formatOpenAIChunk formats a map as an OpenAI SSE data chunk.
func formatOpenAIChunk(data map[string]any) []byte {
	b, _ := json.Marshal(data)
	return []byte("data: " + string(b) + "\n\n")
}