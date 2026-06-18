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
