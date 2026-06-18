package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

// ChatRequest is the normalized request passed to a provider.
type ChatRequest struct {
	// Raw body as sent by the client. OpenAI-compatible providers forward it as-is.
	Body []byte

	// Model is the upstream model name to call.
	Model string

	// Stream indicates whether streaming was requested.
	Stream bool
}

// ChatResponse is the normalized non-streaming response from a provider.
type ChatResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	Usage      Usage
	Err        *Error
}

// StreamChunk is one piece of a streaming response.
type StreamChunk struct {
	Data []byte // raw SSE bytes, e.g. "data: {...}\n\n"
	Err  error  // non-nil on stream error
}

// Usage holds token usage.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Error is a normalized provider error used by the fallback engine.
type Error struct {
	// StatusCode is the upstream HTTP status, or 0 for transport errors.
	StatusCode int
	// Kind classifies the error for fallback decisions.
	Kind ErrorKind
	// Message is a short human-readable message.
	Message string
	// Cause is the underlying error.
	Cause error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type ErrorKind string

const (
	KindNone          ErrorKind = ""
	KindTimeout       ErrorKind = "timeout"
	KindConnectError  ErrorKind = "connect_error"
	KindRateLimited   ErrorKind = "rate_limited"
	KindServerError   ErrorKind = "server_error"
	KindClientError   ErrorKind = "client_error"
	KindStreamError   ErrorKind = "stream_error"
	KindUnknown       ErrorKind = "unknown"
)

// Provider is the unified interface every backend implements.
type Provider interface {
	Name() string
	// Chat performs a non-streaming request.
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	// ChatStream performs a streaming request and returns a channel of chunks.
	// The channel is closed when the stream ends; an error chunk (Err != nil) is sent before close on failure.
	ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error)
}

// HTTPDoer allows mocking the underlying HTTP client.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Config holds the per-provider configuration.
type Config struct {
	Name    string
	BaseURL string
	APIKey  string
	Timeout time.Duration
}

// readAllAndClose is a small helper.
func readAllAndClose(r io.ReadCloser) ([]byte, error) {
	defer r.Close()
	return io.ReadAll(r)
}

// ErrNoProvider is returned when no provider is configured.
var ErrNoProvider = errors.New("no provider configured")
