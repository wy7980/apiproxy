# Switch: Protocol Conversion Between OpenAI and Anthropic Formats

## Overview

Add per-provider protocol conversion to apiproxy. Each provider target in a route
can optionally enable bidirectional conversion between OpenAI Chat Completions
format and Anthropic Messages format. When enabled, the proxy automatically
converts requests and responses so downstream clients and upstream providers
can use different API formats transparently.

## 1. Config Schema

### RouteTarget (per-provider)

Add `switch` field to the provider target inside a route's `providers` list:

```yaml
routes:
  chat:
    path: /v1/chat/completions
    providers:
      - provider: openai
        model: gpt-4o
        tier: stable
        weight: 100
        switch: openai-to-anthropic   # new field
      - provider: anthropic
        model: claude-opus-4-8
        tier: stable
        weight: 100
        # switch omitted or "" means no conversion
```

Valid values:
- `""` (empty/omitted) — no conversion, transparent proxy as today
- `"openai-to-anthropic"` — downstream uses OpenAI format, upstream is Anthropic
- `"anthropic-to-openai"` — downstream uses Anthropic format, upstream is OpenAI

### Validation (config.Validate())

- `switch` must be one of the three valid values; anything else is a 500-level
  config error caught at startup and on hot-reload
- No cross-validation with route path — a provider with `switch: openai-to-anthropic`
  can appear under any route path; the direction defines which handler processes it
  (see Section 5)

## 2. Protocol Conversion Layer (internal/switcher)

### Package Structure

```
internal/switcher/
  converter.go        # Converter interface + NewConverter()
  request.go          # Request conversion (both directions)
  response.go         # Response conversion (both directions)
  stream.go           # Streaming SSE state machine
  tools.go            # Tool name sanitization & round-trip mapping
  usage.go            # Usage extraction
```

### Converter Interface

```go
type Direction string

const (
    DirOff              Direction = ""
    DirOpenAItoAnthropic Direction = "openai-to-anthropic"
    DirAnthropicToOpenAI Direction = "anthropic-to-openai"
)

type Converter struct {
    dir Direction
}

func NewConverter(dir Direction) *Converter
func (c *Converter) ConvertRequest(ctx context.Context, body []byte) ([]byte, error)
func (c *Converter) ConvertResponse(ctx context.Context, body []byte) ([]byte, error)
func (c *Converter) ConvertStreamChunk(ctx context.Context, chunk []byte) ([]byte, error)
func (c *Converter) IsStream() bool               // from original request metadata
func (c *Converter) Usage() *Usage                 // accumulated usage from response
```

Request conversion returns 400 on unsupported features. Response conversion
tolerates unexpected shapes with warn logs and fallback defaults.

### Conversion Policies

- **Strict on request**: unrecognized fields, incompatible parameters,
  or missing required fields → return 400 error with descriptive message
- **Tolerant on response**: unexpected/missing response fields → warn log,
  use sensible defaults, never crash the stream
- **Stream-first**: non-streaming responses are assembled from the same
  conversion functions used for streaming; no separate code path

## 3. Field Mapping Rules

### OpenAI → Anthropic (request)

| OpenAI Field | Anthropic Field | Notes |
|---|---|---|
| `model` | `model` | Pass through |
| `messages` | `messages` | Role merge: consecutive `user`+`tool` → single `user` turn; system message extracted to top-level `system` param |
| `temperature` | `temperature` | Direct |
| `top_p` | `top_p` | Direct |
| `max_tokens` / `max_completion_tokens` | `max_tokens` | `max_completion_tokens` preferred |
| `stop` | `stop_sequences` | Direct |
| `tools[].function` | `tools[].input_schema` | Anthropic has no `function` wrapper; `description` maps to `description` |
| `tool_choice` | `tool_choice` | `auto`→`auto`, `required`→`any`, `tool:{name}`→`tool:{name}` |
| `parallel_tool_calls=false` | `disable_parallel_tool_use=true` | Default `true` → omit |
| `reasoning_effort` | `thinking` + `output_config.effort` | Mapped per model capability table |
| `response_format` | `output_config.format` | `json_schema` / `json_object` mapping |
| `stream` | `stream` | Pass through |
| `stream_options.include_usage` | (Anthropic always includes usage) | Anthropic streaming always sends usage in `message_delta` |
| `user` | `metadata.user_id` | Namespace mapping |
| `frequency_penalty` | — | Not supported → return 400 |
| `presence_penalty` | — | Not supported → return 400 |
| `logit_bias` | — | Not supported → return 400 |
| `seed` | — | Not supported → return 400 |
| `top_k` | `top_k` | Direct (OpenAI also supports this) |

### Anthropic → OpenAI (request)

| Anthropic Field | OpenAI Field | Notes |
|---|---|---|
| `model` | `model` | Pass through |
| `messages` | `messages` | `tool_use` content blocks → `tool_calls`; `tool_result` → `tool` role messages; `thinking` blocks stripped (no OpenAI equivalent) |
| `system` | messages[0].role=system | Prepend as system message |
| `max_tokens` | `max_completion_tokens` | Direct |
| `temperature` | `temperature` | Direct |
| `top_p` | `top_p` | Direct |
| `top_k` | `top_k` | Direct |
| `stop_sequences` | `stop` | Direct |
| `tools[]` | `tools[].function` | Anthropic `input_schema` → `function.parameters` |
| `tool_choice` | `tool_choice` | `auto`→`auto`, `any`→`required`, `tool:{name}`→`tool:{name}` |
| `thinking` | `reasoning_effort` | Budget tokens → effort level mapping |
| `output_config` | `response_format` | Format mapping |
| `metadata.user_id` | `user` | Strip namespace |
| `stream` | `stream` | Pass through |

### Tool Name Sanitization

Anthropic requires tool names matching `^[a-zA-Z0-9_-]{1,128}$`. OpenAI allows
a broader set. Processing:

1. **On OpenAI→Anthropic request**: replace invalid characters with `_`, store
   original name in an extension field returned alongside the response for
   reverse-mapping on tool_calls output
2. **On Anthropic→OpenAI response**: if the tool name was sanitized, reverse
   the mapping so downstream sees the original name
3. **Anthropic→OpenAI request**: OpenAI accepts broader names, no sanitization
   needed; pass through

## 4. Streaming SSE State Machine

### OpenAI → Anthropic (request stream to Anthropic SSE)

OpenAI streaming sends chunks as `data: {...}` SSE events. Anthropic streaming
sends `event: ...` typed SSE events. The converter maintains a state machine
for translating Anthropic SSE events back to OpenAI SSE chunks.

State machine for Anthropic SSE events → OpenAI `data:` chunks:

```
States: IDLE, PENDING_CONTENT_BLOCK, PENDING_TEXT, PENDING_THINKING,
        PENDING_TOOL_BUILD, PENDING_DELTA_CACHE

Anthropic SSE Event              → OpenAI Chunk
─────────────────────────────────────────────────────
message_start                    → chunk with role + first content block stub
content_block_start (text)       → chunk with delta.text (first text block)
content_block_start (tool_use)   → chunk with delta.tool_calls (stub)
content_block_delta (text_delta) → chunk with delta.text (append)
content_block_delta (input_json) → chunk with delta.tool_calls.function.arguments
content_block_delta (thinking)   → cached, NOT forwarded (OpenAI has no thinking)
content_block_delta (signature)  → cached for usage
content_block_stop              → maybe flush pending
message_delta                   → chunk with finish_reason + usage (if requested)
message_stop                    → stream end (already sent finish_reason)
ping                            → ignored
```

Key notes:
- `stream_options.include_usage=true` must be injected into the request so the
  converter has permission to emit `usage` in the final chunk
- Thinking blocks from Anthropic are **not emitted** to the OpenAI downstream;
  they are accumulated and discarded
- `message_delta` usage is cached and emitted as part of the final `data:`
  chunk with `finish_reason`, not as a separate event

### Anthropic → OpenAI (response SSE to OpenAI SSE)

The upstream provider returns OpenAI SSE (`data: {...}`). The converter maps
it to Anthropic SSE events. This is simpler because OpenAI streaming is
self-contained per chunk:

```
OpenAI Chunk Type                → Anthropic SSE Event
─────────────────────────────────────────────────────
chunk.choices[0].delta.role      → message_start
chunk.choices[0].delta.content   → content_block_start (text) + content_block_delta
chunk.choices[0].delta.tool_calls→ content_block_start (tool_use) + content_block_delta
chunk.choices[0].finish_reason   → message_delta (stop_reason)
[usage in final chunk]           → included in message_delta usage
[DONE]                           → message_stop
```

Key notes:
- `content_block_stop` events are inserted between content blocks to keep the
  Anthropic protocol valid
- Usage must be aggregated across streaming chunks (OpenAI only sends usage
  in the final chunk when `include_usage=true`)

## 5. Server / Fallback / Metrics Integration

### Request Flow Overview

```
Downstream Client
       │
       ▼
  ┌─────────────────┐
  │  Route Handler   │  (/v1/chat/completions or /v1/messages)
  │  picks provider  │
  └────────┬────────┘
           │
           ▼
  ┌─────────────────┐
  │  Check Switch    │  target.Switch != ""
  │  Create Converter│
  └────────┬────────┘
           │
           ▼
  ┌─────────────────┐
  │  Convert Request │  400 on failure (no fallback)
  └────────┬────────┘
           │
           ▼
  ┌─────────────────┐
  │  Provider.Chat  │  normal fallback on upstream error
  │  or .ChatStream │
  └────────┬────────┘
           │
           ▼
  ┌─────────────────┐
  │  Convert Response│ warn+tolerant on conversion error
  │  (or stream conv)│ → provider failure (triggers fallback)
  └────────┬────────┘
           │
           ▼
  ┌─────────────────┐
  │  Write Response  │  normal response path
  └─────────────────┘
```

### Header Management

- **OpenAI→Anthropic**: add `anthropic-version`, `x-api-key` (from provider
  config), strip OpenAI-specific headers
- **Anthropic→OpenAI**: strip `anthropic-version`, `x-api-key`, `anthropic-beta`;
  add OpenAI `Authorization` header
- Beta headers are auto-managed based on features used in the converted request
  (see LiteLLM `_update_headers_with_anthropic_beta`)

### Fallback Behavior

- **Request conversion failure**: return 400 immediately, do NOT attempt
  fallback to next provider (the request is invalid, not the provider)
- **Upstream provider error** (timeout, 5xx, network): normal fallback to next
  provider in the route's provider list
- **Response conversion failure**: treat as provider error, trigger fallback
- Streaming errors during conversion: close stream with error event

### Metrics

- Usage for cost tracking is read from the Converter's accumulated `Usage()`,
  not from the provider's raw response (usage fields differ between formats)

### Content-Type

- Non-streaming: `application/json` (converted body)
- Streaming (OpenAI→Anthropic): `text/event-stream` with SSE events translated
  to OpenAI `data:` chunks — no `[DONE]` emitted by converter layer (handler
  adds it if needed by downstream protocol)
- Streaming (Anthropic→OpenAI): `text/event-stream` with OpenAI SSE
  translated to Anthropic SSE events

## 6. Admin UI

### Provider Target Card — 5th Column

Add a Switch column to the provider target card grid with:
- A checkbox to enable/disable conversion
- A dropdown to select direction (enabled only when checkbox is checked)
- Direction options: "openai → anthropic", "anthropic → openai"

### UI Behavior

- Checkbox unchecked + disabled dropdown = switch off (default)
- Checkbox checked → dropdown enabled, default to "openai → anthropic"
- Changing checkbox/dropdown updates the JSON model and triggers config save

### Admin API Changes

- `routeTargetJSON` struct: add `Switch string \`json:"switch"\``
- Default new provider target: `Switch: ""`
- No special validation in admin handler (delegated to config.Validate())

---

## Implementation Order

1. Config: add `Switch` field to RouteTarget, update Validate(), update example
   configs
2. Admin API: serialize/deserialize Switch in GET/PUT config handlers
3. Admin UI: checkbox + select in providerTargetCard(), CSS grid 5 columns,
   JS event handlers, i18n labels
4. Switcher package: Converter interface + Direction constants + NewConverter
5. Switcher request conversion: OpenAI→Anthropic field mapping
6. Switcher request conversion: Anthropic→OpenAI field mapping
7. Switcher response conversion: both directions (non-streaming)
8. Switcher streaming conversion: SSE state machine + tool name handling
9. Server integration: switch check in handlers, ConvertRequest before
   provider call, ConvertResponse/ConvertStreamChunk after response
10. Fallback integration: request conversion 400 skip, response error = provider
    failure
11. Metrics: use Converter.Usage() instead of raw provider usage
12. First light test with a real provider pair
