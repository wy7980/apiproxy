# apiproxy

> LLM API proxy middleware — unified access, real-time performance monitoring, and automatic fallback for intelligent agent LLM calls.

[English](README.md) | [中文](README.zh-CN.md)

## Goals

- **Unified Access**: Agents only call apiproxy's OpenAI-compatible API; apiproxy routes to OpenAI / DeepSeek / Qwen, etc.
- **Transparent Proxy**: Both `/v1/chat/completions` and `/v1/messages` are passed through as-is by default — the protocol is determined by the client's request path. Per route target, an optional `switch` field enables bidirectional protocol conversion (OpenAI ↔ Anthropic), allowing an OpenAI-format client to reach an Anthropic backend transparently.
- **Real-time Monitoring**: Prometheus metrics + JSON structured logging covering latency, first-token latency, token usage, error rate, and fallback count.
- **Automatic Fallback**: Falls back to the next provider by priority on timeout / 429 / 5xx / connection errors.
- **Circuit Breaker**: Simple per-provider circuit breaker state machine (auto-switching not enabled by default).
- **Transparent to Agents**: Agents do not need to be aware of backend provider switches.

## Directory Structure

```text
apiproxy/
  cmd/apiproxy/         Program entry point
  configs/              Example configurations
  internal/
    server/             HTTP server + routes + handlers
    config/             YAML config loading + defaults + validation
    api/                Minimal parsing and error formatting for OpenAI / Anthropic requests
    router/             Routing strategies (priority / weighted / latency ...)
    fallback/           Fallback decision logic
    provider/           Provider abstraction + transparent HTTP provider (mirrors upstream path by client path)
    breaker/            Simple circuit breaker state machine
    metrics/            Prometheus metrics
    log/                slog logger initialization
    auth/               API Key authentication
    ratelimit/          Token bucket rate limiting (reserved)
    storage/            SQLite persistence for request events
    admin/              Performance analytics dashboard (HTML+Chart.js)
    cli/                stats subcommand: command line stats query
    switcher/           Bidirectional protocol conversion (OpenAI ↔ Anthropic)
```

## Quick Start

```bash
cd apiproxy
go mod tidy

# Set provider API keys
export OPENAI_API_KEY=sk-xxx
export DEEPSEEK_API_KEY=sk-xxx
export DASHSCOPE_API_KEY=sk-xxx
export ANTHROPIC_API_KEY=sk-ant-xxx

# Set admin login credentials (required when admin is enabled)
export APIPROXY_ADMIN_USER=admin
export APIPROXY_ADMIN_PASS=$(openssl rand -base64 24)

# Start
go run ./cmd/apiproxy -config configs/apiproxy.yaml
```

Call example (use route name as model; both OpenAI and Anthropic clients can use the same provider configuration):

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer demo-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "smart-chat",
    "messages": [{"role":"user","content":"hello"}]
  }'
```

Streaming:

```bash
curl -N http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer demo-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "smart-chat",
    "stream": true,
    "messages": [{"role":"user","content":"hello"}]
  }'
```

Anthropic `/v1/messages` transparent forwarding (use route name as model):

```bash
curl http://localhost:8080/v1/messages \
  -H "x-api-key: demo-key" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-code",
    "max_tokens": 1024,
    "messages": [{"role":"user","content":"hello"}]
  }'
```

`/v1/messages` passes the request and response bodies through as-is by default — only the `model` field is replaced. The protocol is entirely determined by the client's request path — the same provider can serve both `/v1/chat/completions` (OpenAI format) and `/v1/messages` (Anthropic format) without needing a `type` field configuration. To enable format conversion between OpenAI and Anthropic protocols on a per-target basis, use the `switch` field (see [Protocol Switch](#protocol-switch)).

View metrics:

```bash
curl http://localhost:8080/metrics
```

## Packaging and Distribution

A packaging script is provided:

```bash
./scripts/package.sh
```

Default behavior:

- Builds for the current platform (uses `go env GOOS/GOARCH` by default)
- Version number uses the current git tag by priority (e.g., `v1.2.3` → `1.2.3`), otherwise uses the short commit hash
- Outputs to `dist/`
- Package contents include:
  - `apiproxy` binary
  - `configs/apiproxy.yaml` example config
  - `README.md`

Generated artifact example:

```text
dist/
  apiproxy-1.2.3-linux-amd64.tar.gz
```

Common usage:

```bash
# Current platform
./scripts/package.sh

# Specify version
./scripts/package.sh -v 1.0.0

# Specify output directory
./scripts/package.sh -o /tmp/dist

# Cross-platform build
./scripts/package.sh --os linux --arch arm64

# Build 4 common platforms at once
./scripts/package.sh --all-platforms

# Skip tests
./scripts/package.sh --skip-test
```

`--all-platforms` generates:

- `linux/amd64`
- `linux/arm64`
- `darwin/amd64`
- `darwin/arm64`

By default, the following is executed before packaging:

```bash
go test ./...
```

Use `--skip-test` for temporary local validation.

## Security

### Credentials

- Provider API keys are injected via environment variables (`api_key_env`), no keys are stored in YAML.
- `auth_header` determines the upstream authentication header format: `both` (default, best compatibility), `authorization` (Bearer only), or `x-api-key` (Anthropic-style header only).
- Admin login username/password are injected via `APIPROXY_ADMIN_USER` / `APIPROXY_ADMIN_PASS` environment variables, not written to configuration files.
- Admin session cookies are HMAC-signed random tokens with `HttpOnly` + `SameSite=Lax`, and automatically `Secure` under HTTPS.

### HTTP Hardening

- **Login Rate Limiting**: 5 consecutive failures locks the same username for 15 minutes, returning 429 during the lockout.
- **Request Body Size Limit**: Request bodies exceeding 32 MiB return 413 immediately to prevent memory exhaustion.
- **Cookie Security**: Session cookies enforce `HttpOnly`, `SameSite=Lax`, and `Secure` under TLS.
- **Admin Authentication**: Unauthenticated `/api/*` routes return 401; other unauthenticated paths return 303 redirect to `/login`.

## Log Rotation

When `logging.file.enabled: true` (enabled by default), logs are written to `dir/detail-YYYYMMDD.log` in addition to stdout:

- Non-current day log files are automatically gzipped to `.log.gz`.
- Files older than `max_days` (including compressed files) are automatically deleted.
- Files roll to `detail-YYYYMMDD.N.log` by sequence number when exceeding `max_size` MB.
- **Note**: LLM request/response details (including client requests, upstream requests/responses, stream chunks, etc.) currently use `debug` level logging; if `logging.level: info`, these details are not written to file — only `info / warn / error` level logs are recorded. Dashboard and stats request metrics come from SQLite `storage`, not debug logs.

```yaml
logging:
  level: info
  format: json
  file:
    enabled: true
    dir: "logs"
    max_days: 7
    max_size: 100   # 100 MB per file
```

To log full details of each LLM call:

```yaml
logging:
  level: debug
```

## Data Persistence

Configure `storage` to write all request events to SQLite for dashboard and CLI stats queries.

Events are stored in daily-sharded tables (table name `request_events_YYYYMMDD`). Tables exceeding the retention period are automatically DROPPED, reclaiming disk space immediately.

```yaml
storage:
  enabled: true
  path: "data/apiproxy.db"
  retention: 168h   # 7 days, default. Can be changed to 72h (3 days), 720h (30 days), etc.
```

| Field | Default | Description |
|---|---|---|
| `path` | `data/apiproxy.db` | SQLite file path |
| `enabled` | `false` | Whether persistence is enabled |
| `retention` | `168h` (7 days) | Retention period; expired tables are automatically deleted |

WAL mode is used by default, supporting concurrent reads and writes. Expired tables are cleaned up hourly in the background.

## Performance Analytics Dashboard

When `admin` is enabled, a visual dashboard is provided on a separate port:

```yaml
admin:
  enabled: true
  listen: ":8081"
  username_env: "APIPROXY_ADMIN_USER"
  password_env: "APIPROXY_ADMIN_PASS"
```

Set login credentials before starting:

```bash
export APIPROXY_ADMIN_USER=admin
export APIPROXY_ADMIN_PASS=$(openssl rand -base64 24)
```

Visit `http://localhost:8081` in your browser. After logging in, you can view:

- Model performance summary (request volume, success rate, latency P50/P95/P99, TPS)
- Latency & token/s time series charts
- PP/TG speed charts across different context lengths
- Filter by time range / provider / model / route

## CLI Stats Subcommand

View recent statistical data without starting the server:

```bash
# Default last 10 minutes, table output
./apiproxy stats -config configs/apiproxy.yaml

# Specify window & interval
./apiproxy stats -config configs/apiproxy.yaml -window 168h -interval minute

# Filter by provider
./apiproxy stats -config configs/apiproxy.yaml -window 168h -provider example-provider

# JSON output
./apiproxy stats -config configs/apiproxy.yaml -window 168h -json
```

### Parameter Reference

| Parameter | Default | Description |
|---|---|---|
| `-config` | `configs/apiproxy.yaml` | Config file path (reads `storage.path` from it) |
| `-db` | empty | Directly specify SQLite path (overrides `-config`) |
| `-window` | `10m` | Lookback window, e.g., `5m`, `1h`, `168h` |
| `-interval` | auto | Time series granularity: `minute` / `hour` / `day` |
| `-provider` | empty | Filter by provider |
| `-model` | empty | Filter by model |
| `-route` | empty | Filter by route |
| `-json` | false | Output JSON instead of table |

`-interval` auto-selects based on window: ≤2h → minute, ≤48h → hour, otherwise → day.

### Example Output

**Table mode (7-day window, day granularity)**

```
Last 168 hours statistics

Provider  Model              Route       Requests  Errors  Success%  Avg     P50     P95     P99     TG tok/s  Prompt  Compl  Stream
--------  -----------------  ----------  --------  ------  --------  ------  ------  ------  ------  --------  ------  -----  ------
example-provider   deepseek-v3  smart-chat    36     1   97.2%  2350ms  2299ms  4254ms  4662ms    940.8   51562   2211       1

By context length bucket (PP/TG speed)

Provider  Model                   Bucket  Requests  Prompt  Completion  Avg Latency  PP tok/s  TG tok/s
--------  -----------------  -------  --------  ------  ----------  -----------  --------  --------
example-provider   deepseek-v3    0-128    22     296        1054    2060ms     496.6      23.6
example-provider   deepseek-v3  128-512     7    1405         688    3146ms       0.0      31.2
example-provider   deepseek-v3    2k-8k     7   49861         469    2466ms       0.0      27.2

Time series trend

Time                     Provider  Model               Requests  Errors  Avg Latency  Prompt  Completion  TG tok/s
-----------------------  --------  ------------------  --------  ------  -----------  ------  ----------  --------
2026-06-17T00:00:00.000  example-provider   deepseek-v3     36     1    2350ms   51562        2211     940.8
```

**minute granularity (per-minute detail)**

```
Time                     Provider  Model               Requests  Errors  Avg Latency  Prompt  Completion  TG tok/s
-----------------------  --------  ------------------  --------  ------  -----------  ------  ----------  --------
2026-06-17T20:11:00.000  example-provider   deepseek-v3      1     0    1405ms       8          31      22.1
2026-06-17T20:12:00.000  example-provider   deepseek-v3     23     1    2113ms   35855        1320     624.7
2026-06-17T20:13:00.000  example-provider   deepseek-v3     12     0    2884ms   15699         860     298.2
```

> **TG tok/s Definition**: `tokens_per_sec` / TG tok/s is the generation phase throughput rate, calculated as
> `SUM(successful request completion_tokens) / SUM(successful request generation_seconds)`.
> Generation duration for streaming requests uses `latency_ms - first_token_ms`; degrades to `latency_ms` for non-streaming requests.
> Success condition for requests: `status_code < 400 AND error_type = '' AND completion_tokens > 0`.
> This metric is used exclusively for speed calculation; total fields like `prompt_tokens` / `completion_tokens` / `total_tokens` still count all requests.

**JSON mode**

```json
{
  "window_minutes": 10080,
  "interval": "day",
  "summaries": [
    {
      "provider": "example-provider",
      "model": "deepseek-v3",
      "route": "smart-chat",
      "requests": 36,
      "errors": 1,
      "success_rate": 0.9722222222222222,
      "avg_latency_ms": 2350.222222222222,
      "p50_latency_ms": 2299,
      "p95_latency_ms": 4254,
      "p99_latency_ms": 4662,
      "tokens_per_sec": 940.8,
      "prompt_tokens": 51562,
      "completion_tokens": 2211
    }
  ],
  "buckets": [
    {
      "bucket": "0-128",
      "provider": "example-provider",
      "model": "deepseek-v3",
      "requests": 22,
      "pp_rate": 496.6,
      "tg_rate": 23.6,
      "avg_latency_ms": 2060
    }
  ],
  "timeseries": [
    {
      "ts": "2026-06-17T00:00:00.000",
      "provider": "example-provider",
      "model": "deepseek-v3",
      "requests": 36,
      "errors": 1,
      "avg_latency_ms": 2350,
      "prompt_tokens": 51562,
      "completion_tokens": 2211,
      "tokens_per_sec": 940.8
    }
  ]
}
```

## Routing Strategies

Current MVP implements the `priority` strategy — attempts in configuration order sequentially.

```yaml
routes:
  smart-chat:
    strategy: priority
    fallback:
      enabled: true
      max_attempts: 3
      on_status: [429, 500, 502, 503, 504]
      on_timeout: true
      on_connect_error: true
      allow_downgrade: false
    providers:
      - provider: openai
        model: gpt-4o-mini
        tier: advanced
      - provider: deepseek
        model: deepseek-chat
        tier: standard
      - provider: anthropic
        model: claude-sonnet-4-6
        tier: advanced
        switch: openai-to-anthropic  # convert OpenAI format to Anthropic
```

### Protocol Switch

Each route provider target can optionally specify a `switch` field to enable protocol conversion. This allows an OpenAI-format client to call an Anthropic backend (or vice versa) without any code changes on the client side:

```yaml
providers:
  anthropic:
    base_url: https://api.anthropic.com
    api_key_env: ANTHROPIC_API_KEY

routes:
  openai-to-claude:
    strategy: priority
    providers:
      - provider: anthropic
        model: claude-sonnet-4-6
        tier: advanced
        switch: openai-to-anthropic  # client sends OpenAI format, proxy converts to Anthropic
```

| `switch` value | Conversion direction |
|---|---|
| *(empty, default)* | No conversion — transparent pass-through |
| `openai-to-anthropic` | OpenAI → Anthropic request/response format |
| `anthropic-to-openai` | Anthropic → OpenAI request/response format |

**What gets converted:**

- **Messages**: roles, system prompts, content blocks (text, images, tool results)
- **Tools**: function definitions, `tool_choice`, parallel tool calls
- **Parameters**: `max_tokens` ↔ `max_completion_tokens`, `stop` ↔ `stop_sequences`, `reasoning_effort` ↔ `thinking`, `response_format` ↔ `output_config`
- **Streaming**: full SSE event conversion with state tracking (message_start, content_block_start/delta/stop, message_delta, etc.)
- **Headers**: provider-specific headers removed on the opposite side (e.g., `anthropic-version` hidden from OpenAI clients, `Authorization` hidden from Anthropic clients)

Conversion works for both non-streaming and streaming responses. If conversion fails at any point, the proxy falls back to passing through the original upstream response with a warning log.

Future extensions include `weighted` / `latency` / `health` strategies.

## Metrics

| Metric | Description |
|---|---|
| `apiproxy_request_total` | Request count |
| `apiproxy_request_duration_seconds` | Request duration |
| `apiproxy_first_token_duration_seconds` | First token latency (streaming only) |
| `apiproxy_token_total` | Token usage (prompt/completion) |
| `apiproxy_error_total` | Error count |
| `apiproxy_fallback_total` | Fallback trigger count |
| `apiproxy_circuit_breaker_state` | Circuit breaker state |

## Roadmap

- [x] OpenAI-compatible API
- [x] Non-streaming + streaming
- [x] Priority fallback
- [x] Prometheus metrics + structured logging
- [x] API Key authentication
- [x] SQLite persistent storage (daily sharding + configurable retention)
- [x] Performance analytics dashboard (HTML+Chart.js)
- [x] CLI stats subcommand (table + JSON + time series)
- [x] Transparent proxy: upstream path mirroring by client path, supporting both OpenAI and Anthropic protocols simultaneously
- [x] Admin authentication (session cookie + HMAC signature)
- [x] Admin login rate limiting (consecutive failure lockout)
- [x] HTTP security hardening (request body size limit, Cookie security attributes)
- [x] Credential environment variable injection (provider API key, admin username/password)
- [x] File log rotation (daily file splitting + gzip compression + auto-cleanup)
- [x] E2E integration tests (proxy + admin + SQLite real TCP)
- [x] Protocol switch: bidirectional OpenAI ↔ Anthropic format conversion per route target
- [ ] Weighted / low-latency / health-first routing
- [ ] Automatic circuit breaker triggering (state machine exists only currently)
- [ ] Cost estimation / audit logs
- [ ] OpenTelemetry tracing
