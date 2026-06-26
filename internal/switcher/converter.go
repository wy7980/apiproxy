// Package switcher converts between OpenAI Chat Completions format and Anthropic
// Messages format bidirectionally.
package switcher

import (
	"context"
	"fmt"
)

// Direction encodes the conversion direction. The zero value means no conversion.
type Direction string

const (
	DirOff              Direction = ""
	DirOpenAItoAnthropic Direction = "openai-to-anthropic"
	DirAnthropicToOpenAI Direction = "anthropic-to-openai"
)

// ParseDirection validates and returns a Direction from a config string.
func ParseDirection(s string) (Direction, error) {
	switch Direction(s) {
	case DirOff, DirOpenAItoAnthropic, DirAnthropicToOpenAI:
		return Direction(s), nil
	default:
		return DirOff, fmt.Errorf("invalid switch direction: %q", s)
	}
}

// Converter performs protocol conversion for requests, responses, and stream chunks.
type Converter struct {
	dir              Direction
	state            StreamState
	toolNameMapping  map[string]string
}

// NewConverter creates a converter for the given direction.
func NewConverter(dir Direction) *Converter {
	return &Converter{dir: dir}
}

// ConvertRequest converts a request body from the downstream format to the
// upstream format. Returns an error if any field cannot be converted.
func (c *Converter) ConvertRequest(ctx context.Context, body []byte) ([]byte, error) {
	if c.dir == DirOff {
		return body, nil
	}
	return c.convertRequest(ctx, body)
}

// ConvertResponse converts a non-streaming response body from the upstream
// format to the downstream format.
func (c *Converter) ConvertResponse(ctx context.Context, body []byte) ([]byte, error) {
	if c.dir == DirOff {
		return body, nil
	}
	return c.convertResponse(ctx, body)
}

// ConvertStreamChunk converts a single SSE chunk from the upstream format to
// the downstream format. It manages internal state for multi-chunk sequences.
func (c *Converter) ConvertStreamChunk(ctx context.Context, chunk []byte) ([]byte, error) {
	if c.dir == DirOff {
		return chunk, nil
	}
	return c.convertStreamChunk(ctx, chunk)
}

// Direction returns the configured conversion direction.
func (c *Converter) Direction() Direction { return c.dir }

// Usage returns the accumulated token usage from response/stream conversion.
func (c *Converter) Usage() (promptTokens, completionTokens int) {
	return c.state.inputTokens, c.state.outputTokens
}

// convertRequest dispatches based on direction.
func (c *Converter) convertRequest(ctx context.Context, body []byte) ([]byte, error) {
	switch c.dir {
	case DirOpenAItoAnthropic:
		return openaiToAnthropicRequest(ctx, body)
	case DirAnthropicToOpenAI:
		return anthropicToOpenAIRequestWithConverter(ctx, body, c)
	default:
		return body, nil
	}
}

// convertResponse dispatches based on direction.
func (c *Converter) convertResponse(ctx context.Context, body []byte) ([]byte, error) {
	switch c.dir {
	case DirOpenAItoAnthropic:
		return anthropicToOpenAIResponse(ctx, body)
	case DirAnthropicToOpenAI:
		return openaiToAnthropicResponse(ctx, body)
	default:
		return body, nil
	}
}

// convertStreamChunk dispatches based on direction.
func (c *Converter) convertStreamChunk(ctx context.Context, chunk []byte) ([]byte, error) {
	switch c.dir {
	case DirOpenAItoAnthropic:
		return anthropicSSEToOpenAIChunk(c, ctx, chunk)
	case DirAnthropicToOpenAI:
		return openaiSSEToAnthropicChunk(c, ctx, chunk)
	default:
		return chunk, nil
	}
}