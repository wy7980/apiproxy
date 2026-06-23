package api

import "encoding/json"

type ChatCompletionRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

func ParseChatCompletionRequest(body []byte) (ChatCompletionRequest, error) {
	var req ChatCompletionRequest
	err := json.Unmarshal(body, &req)
	return req, err
}

func ReplaceModel(body []byte, model string) ([]byte, error) {
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, err
	}
	obj["model"] = model
	return json.Marshal(obj)
}

// InjectStreamUsageOptions injects "stream_options": {"include_usage": true}
// into a streaming request body so that the upstream provider includes token
// usage in the final SSE chunk. Many upstream gateways (DeepSeek, Qwen, etc.)
// only report usage when this flag is explicitly requested; without it the
// usage field stays null throughout the stream, causing all token counts to
// be recorded as zero.
func InjectStreamUsageOptions(body []byte) ([]byte, error) {
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, err
	}
	// Only inject for streaming requests; non-streaming responses already
	// include usage in the top-level response body.
	stream, _ := obj["stream"].(bool)
	if !stream {
		return body, nil
	}
	// Preserve any existing stream_options the client already set, only
	// forcing include_usage=true.
	existing, _ := obj["stream_options"].(map[string]any)
	if existing == nil {
		existing = map[string]any{}
	}
	existing["include_usage"] = true
	obj["stream_options"] = existing
	return json.Marshal(obj)
}
