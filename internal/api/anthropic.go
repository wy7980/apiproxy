package api

import (
	"encoding/json"
	"fmt"
)

// AnthropicMessagesRequest contains only the fields apiproxy needs for
// transparent routing. The full request body is forwarded unchanged.
type AnthropicMessagesRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

// ParseAnthropicMessagesRequest parses enough of an Anthropic /v1/messages
// request to route it. It intentionally does not validate or normalize the
// rest of the payload; the upstream Anthropic-compatible provider owns that.
func ParseAnthropicMessagesRequest(body []byte) (AnthropicMessagesRequest, error) {
	var req AnthropicMessagesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return req, fmt.Errorf("parse anthropic request: %w", err)
	}
	if req.Model == "" {
		return req, fmt.Errorf("anthropic request: model is required")
	}
	return req, nil
}

// AnthropicError is the error format returned by the Anthropic API.
type AnthropicError struct {
	Type  string               `json:"type"`
	Error AnthropicErrorDetail `json:"error"`
}

// AnthropicErrorDetail is the detail inside an Anthropic error.
type AnthropicErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// AnthropicErrorJSON builds an Anthropic-format error response body for
// proxy-layer errors (auth, routing, body read failures).
func AnthropicErrorJSON(errType, message string) []byte {
	err := AnthropicError{
		Type: "error",
		Error: AnthropicErrorDetail{
			Type:    errType,
			Message: message,
		},
	}
	b, _ := json.Marshal(err)
	return b
}
