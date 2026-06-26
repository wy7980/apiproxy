# apiproxy

> LLM API 代理中间件 — 为智能体调用大模型提供统一接入、实时性能监控与自动 fallback。

[English](README.md) | [中文](README.zh-CN.md)

## 目标

- **统一接入**: Agent 只调用 apiproxy 的 OpenAI 兼容 API,由 apiproxy 路由到 OpenAI / DeepSeek / Qwen 等。
- **透明代理**: `/v1/chat/completions` 和 `/v1/messages` 默认原样透传——协议由 client 的 request path 决定。每个路由目标可通过 `switch` 字段可选启用双向协议转换（OpenAI ↔ Anthropic），使 OpenAI 格式客户端可直接访问 Anthropic 后端。
- **实时监控**: Prometheus 指标 + JSON 结构化日志,覆盖延迟、首 token 时延、token 用量、错误率、fallback 次数。
- **自动 fallback**: 超时 / 429 / 5xx / 连接错误时按优先级 fallback 到下一个 provider。
- **熔断**: 按 provider 维度的简单熔断状态机(默认未启用自动切换)。
- **对 Agent 透明**: Agent 不需要感知后端 provider 切换。

## 目录结构

```text
apiproxy/
  cmd/apiproxy/         程序入口
  configs/              示例配置
  internal/
    server/             HTTP server + 路由 + handler
    config/             YAML 配置加载 + 默认值 + 校验
    api/                OpenAI / Anthropic 请求的最小解析与错误格式
    router/             路由策略(priority / weighted / latency ...)
    fallback/           fallback 决策
    provider/           provider 抽象 + 透明 HTTP provider(按 client path 镜像上游路径)
    breaker/            简单熔断状态机
    metrics/            Prometheus 指标
    log/                slog 日志初始化
    auth/               API Key 鉴权
    ratelimit/          令牌桶限流(预留)
    storage/            SQLite 持久化请求事件
    admin/              性能分析 dashboard (HTML+Chart.js)
    cli/                stats 子命令:命令行查询统计
    switcher/           双向协议转换（OpenAI ↔ Anthropic）
```

## 快速开始

```bash
cd apiproxy
go mod tidy

# 设置 provider api key
export OPENAI_API_KEY=sk-xxx
export DEEPSEEK_API_KEY=sk-xxx
export DASHSCOPE_API_KEY=sk-xxx
export ANTHROPIC_API_KEY=sk-ant-xxx

# 设置 admin 登录凭据（启用 admin 时必填）
export APIPROXY_ADMIN_USER=admin
export APIPROXY_ADMIN_PASS=$(openssl rand -base64 24)

# 启动
go run ./cmd/apiproxy -config configs/apiproxy.yaml
```

调用示例(用 route 名当 model，OpenAI 和 Anthropic 客户端都可走同一套 provider 配置):

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer demo-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "smart-chat",
    "messages": [{"role":"user","content":"hello"}]
  }'
```

流式:

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

Anthropic `/v1/messages` 透明转发(用 route 名当 model):

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

`/v1/messages` 默认将请求体和响应体原样透传，只替换 `model` 字段。协议完全由 client 的 request path 决定——同一个 provider 可以同时服务 `/v1/chat/completions`（OpenAI 格式）和 `/v1/messages`（Anthropic 格式），无需配置 `type` 字段。如需在路由目标级别启用 OpenAI 与 Anthropic 协议之间的格式转换，请使用 `switch` 字段（参见[协议转换](#协议转换)）。

查看指标:

```bash
curl http://localhost:8080/metrics
```

## 打包分发

提供一个打包脚本：

```bash
./scripts/package.sh
```

默认行为：

- 使用当前平台构建（默认取 `go env GOOS/GOARCH`）
- 版本号优先取当前 git tag（如 `v1.2.3` → `1.2.3`），否则取短 commit hash
- 输出到 `dist/`
- 打包内容包括：
  - `apiproxy` 二进制
  - `configs/apiproxy.yaml` 示例配置
  - `README.md`

生成产物示例：

```text
dist/
  apiproxy-1.2.3-linux-amd64.tar.gz
```

常见用法：

```bash
# 当前平台
./scripts/package.sh

# 指定版本号
./scripts/package.sh -v 1.0.0

# 指定输出目录
./scripts/package.sh -o /tmp/dist

# 跨平台打包
./scripts/package.sh --os linux --arch arm64

# 一次打包 4 个常用平台
./scripts/package.sh --all-platforms

# 跳过测试
./scripts/package.sh --skip-test
```

`--all-platforms` 会生成：

- `linux/amd64`
- `linux/arm64`
- `darwin/amd64`
- `darwin/arm64`

打包前默认执行：

```bash
go test ./...
```

如果只是临时本地验证，可加 `--skip-test`。

## 安全

### 凭据

- Provider API key 通过环境变量注入（`api_key_env`），YAML 里不落密钥。
- `auth_header` 决定上游鉴权头写法：`both`（默认，兼容性最好）、`authorization`（只发 Bearer）或 `x-api-key`（只发 Anthropic 风格 header）。
- Admin 登录用户名/密码通过 `APIPROXY_ADMIN_USER` / `APIPROXY_ADMIN_PASS` 环境变量注入，不写进配置文件。
- Admin session cookie 是 HMAC 签名随机 token，`HttpOnly` + `SameSite=Lax`，HTTPS 下自动 `Secure`。

### HTTP 加固

- **登录限流**: 同一用户名连续 5 次失败后锁定 15 分钟，期间返回 429。
- **请求体大小限制**: 请求 body 超过 32 MiB 直接返回 413，防止内存打爆。
- **Cookie 安全**: session cookie 强制 `HttpOnly`、`SameSite=Lax`，TLS 下 `Secure`。
- **Admin 鉴权**: `/api/*` 未登录返回 401，其余未登录路径 303 重定向到 `/login`。

## 日志轮转

`logging.file.enabled: true`（默认开启）时，除 stdout 外把日志写入 `dir/detail-YYYYMMDD.log`：

- 非当天的日志文件自动 gzip 为 `.log.gz`。
- 超过 `max_days` 的文件（含压缩）自动删除。
- 单文件超过 `max_size` MB 时按序号滚动为 `detail-YYYYMMDD.N.log`。
- **注意**: LLM 请求/响应明细（包括 client request、upstream request/response、stream chunk 等）当前使用 `debug` 级别日志；如果 `logging.level: info`，这些明细不会写入文件，只会记录 `info / warn / error` 级别日志。仪表板和 stats 的请求统计来自 SQLite `storage`，不依赖 debug 日志。

```yaml
logging:
  level: info
  format: json
  file:
    enabled: true
    dir: "logs"
    max_days: 7
    max_size: 100   # 单文件 100 MB
```

如果希望日志文件记录每次 LLM 调用的完整明细，请改为：

```yaml
logging:
  level: debug
```

## 数据持久化

配置 `storage` 即可把所有请求事件写入 SQLite，供 dashboard 和 CLI stats 查询。

事件按天分表存储（表名 `request_events_YYYYMMDD`），超过保留天数的分表自动 DROP，磁盘空间即刻回收。

```yaml
storage:
  enabled: true
  path: "data/apiproxy.db"
  retention: 168h   # 7 天，默认值。可改为 72h(3天)、720h(30天)等
```

| 字段 | 默认值 | 说明 |
|---|---|---|
| `path` | `data/apiproxy.db` | SQLite 文件路径 |
| `enabled` | `false` | 是否启用持久化 |
| `retention` | `168h`（7 天） | 保留天数，超期分表自动删除 |

默认使用 WAL 模式，支持并发读写。后台每小时清理过期分表。

## 性能分析 Dashboard

启用 `admin` 后，在独立端口提供可视化 dashboard：

```yaml
admin:
  enabled: true
  listen: ":8081"
  username_env: "APIPROXY_ADMIN_USER"
  password_env: "APIPROXY_ADMIN_PASS"
```

启动前设置登录凭据：

```bash
export APIPROXY_ADMIN_USER=admin
export APIPROXY_ADMIN_PASS=$(openssl rand -base64 24)
```

浏览器访问 `http://localhost:8081`，登录后可查看：

- 模型性能汇总（请求量、成功率、延迟 P50/P95/P99、TPS）
- 延迟 & token/s 时间趋势图
- 不同上下文长度下的 PP/TG 速度图
- 按时间范围 / provider / model / route 筛选

## CLI 统计子命令

不启动服务即可查看最近统计数据：

```bash
# 默认最近 10 分钟，表格输出
./apiproxy stats -config configs/apiproxy.yaml

# 指定窗口 & 粒度
./apiproxy stats -config configs/apiproxy.yaml -window 168h -interval minute

# 按 provider 过滤
./apiproxy stats -config configs/apiproxy.yaml -window 168h -provider example-provider

# JSON 输出
./apiproxy stats -config configs/apiproxy.yaml -window 168h -json
```

### 参数说明

| 参数 | 默认值 | 说明 |
|---|---|---|
| `-config` | `configs/apiproxy.yaml` | 配置文件路径（从中读取 `storage.path`） |
| `-db` | 空 | 直接指定 SQLite 路径（覆盖 `-config`） |
| `-window` | `10m` | 回溯窗口，如 `5m`、`1h`、`168h` |
| `-interval` | 自动 | 时间序列粒度：`minute` / `hour` / `day` |
| `-provider` | 空 | 按 provider 过滤 |
| `-model` | 空 | 按 model 过滤 |
| `-route` | 空 | 按 route 过滤 |
| `-json` | false | 输出 JSON 代替表格 |

`-interval` 缺省时根据窗口自动选择：≤2h → minute，≤48h → hour，其余 → day。

### 示例输出

**表格模式（7 天窗口，day 粒度）**

```
最近 168 小时 的统计

Provider  Model              Route       请求  错误  成功率    平均     P50     P95     P99    TG tok/s  Prompt  Compl  Stream
--------  -----------------  ----------  ----  ----  ------  ------  ------  ------  ------  --------  ------  -----  ------
example-provider   deepseek-v3  smart-chat    36     1   97.2%  2350ms  2299ms  4254ms  4662ms    940.8   51562   2211       1

按上下文长度分桶 (PP/TG 速度)

Provider  Model                   桶  请求  Prompt  Completion  平均延迟  PP tok/s  TG tok/s
--------  -----------------  -------  ----  ------  ----------  --------  --------  --------
example-provider   deepseek-v3    0-128    22     296        1054    2060ms     496.6      23.6
example-provider   deepseek-v3  128-512     7    1405         688    3146ms       0.0      31.2
example-provider   deepseek-v3    2k-8k     7   49861         469    2466ms       0.0      27.2

时间序列趋势

时间                     Provider  Model               请求  错误  平均延迟  Prompt  Completion  TG tok/s
-----------------------  --------  ------------------  ----  ----  --------  ------  ----------  --------
2026-06-17T00:00:00.000  example-provider   deepseek-v3     36     1    2350ms   51562        2211     940.8
```

**minute 粒度（精细到每分钟）**

```
时间                     Provider  Model               请求  错误  平均延迟  Prompt  Completion  TG tok/s
-----------------------  --------  ------------------  ----  ----  --------  ------  ----------  --------
2026-06-17T20:11:00.000  example-provider   deepseek-v3      1     0    1405ms       8          31      22.1
2026-06-17T20:12:00.000  example-provider   deepseek-v3     23     1    2113ms   35855        1320     624.7
2026-06-17T20:13:00.000  example-provider   deepseek-v3     12     0    2884ms   15699         860     298.2
```

> **TG tok/s 口径说明**：`tokens_per_sec` / TG tok/s 是生成阶段吞吐速率，计算公式为
> `SUM(成功请求 completion_tokens) / SUM(成功请求 generation_seconds)`。
> 生成耗时对流式请求取 `latency_ms - first_token_ms`，对非流式请求退化用 `latency_ms`。
> 成功请求条件：`status_code < 400 AND error_type = '' AND completion_tokens > 0`。
> 该指标只用于速度计算；`prompt_tokens` / `completion_tokens` / `total_tokens` 等总量字段仍统计所有请求。

**JSON 模式**

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

## 路由策略

当前 MVP 实现 `priority` 策略 — 按配置顺序依次尝试。

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
        switch: openai-to-anthropic  # 将 OpenAI 格式转换为 Anthropic
```

### 协议转换

每个路由的 provider 目标可以通过 `switch` 字段可选启用协议转换。这让使用 OpenAI 格式的客户端可以直接调用 Anthropic 后端（或反过来），客户端代码无需任何修改：

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
        switch: openai-to-anthropic  # 客户端发送 OpenAI 格式，代理自动转换为 Anthropic
```

| `switch` 值 | 转换方向 |
|---|---|
| *空（默认）* | 不转换——原样透传 |
| `openai-to-anthropic` | OpenAI → Anthropic 请求/响应格式 |
| `anthropic-to-openai` | Anthropic → OpenAI 请求/响应格式 |

**转换覆盖范围：**

- **消息**: role、system prompt、content blocks（文本、图片、tool result）
- **工具**: 函数定义、tool_choice、并行工具调用
- **参数**: `max_tokens` ↔ `max_completion_tokens`、`stop` ↔ `stop_sequences`、`reasoning_effort` ↔ `thinking`、`response_format` ↔ `output_config`
- **流式**: 完整 SSE 事件转换与状态跟踪（message_start、content_block_start/delta/stop、message_delta 等）
- **HTTP 头**: 自动清理跨协议头（如向 OpenAI 客户端隐藏 `anthropic-version`，向 Anthropic 客户端隐藏 `Authorization`）

非流式和流式响应均支持转换。转换失败时代理会降级为透传原始上游响应并记录警告日志。

后续会扩展 `weighted` / `latency` / `health` 策略。

## 指标

| 指标 | 含义 |
|---|---|
| `apiproxy_request_total` | 请求计数 |
| `apiproxy_request_duration_seconds` | 请求耗时 |
| `apiproxy_first_token_duration_seconds` | 首 token 时延(仅 streaming) |
| `apiproxy_token_total` | token 用量(prompt/completion) |
| `apiproxy_error_total` | 错误计数 |
| `apiproxy_fallback_total` | fallback 触发次数 |
| `apiproxy_circuit_breaker_state` | 熔断状态 |

## Roadmap

- [x] OpenAI-compatible API
- [x] 非流式 + 流式
- [x] priority fallback
- [x] Prometheus metrics + 结构化日志
- [x] API Key 鉴权
- [x] SQLite 持久化存储（按天分表 + 可配置保留天数）
- [x] 性能分析 dashboard (HTML+Chart.js)
- [x] CLI stats 子命令（表格 + JSON + 时间序列）
- [x] 透明代理：按 client path 镜像上游路径，同时支持 OpenAI 和 Anthropic 协议
- [x] Admin 登录鉴权（session cookie + HMAC 签名）
- [x] Admin 登录限流（连续失败锁定）
- [x] HTTP 安全加固（请求体大小限制、Cookie 安全属性）
- [x] 凭据环境变量注入（provider API key、admin 用户名密码）
- [x] 文件日志轮转（按天分文件 + gzip 压缩 + 自动清理）
- [x] E2E 集成测试（proxy + admin + SQLite 真实 TCP）
- [x] 协议转换：每个路由目标可独立启用 OpenAI ↔ Anthropic 双向格式转换
- [ ] 加权 / 低延迟 / 健康优先路由
- [ ] 自动熔断触发(目前仅有状态机)
- [ ] 成本估算 / 审计日志
- [ ] OpenTelemetry tracing
