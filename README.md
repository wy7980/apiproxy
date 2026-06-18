# apiproxy

> LLM API 代理中间件 — 为智能体调用大模型提供统一接入、实时性能监控与自动 fallback。

## 目标

- **统一接入**: Agent 只调用 apiproxy 的 OpenAI 兼容 API,由 apiproxy 路由到 OpenAI / DeepSeek / Qwen 等。
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
    api/                OpenAI 兼容请求解析
    router/             路由策略(priority / weighted / latency ...)
    fallback/           fallback 决策
    provider/           provider 抽象 + openai adapter
    breaker/            简单熔断状态机
    metrics/            Prometheus 指标
    log/                slog 日志初始化
    auth/               API Key 鉴权
    ratelimit/          令牌桶限流(预留)
    storage/            SQLite 持久化请求事件
    admin/              性能分析 dashboard (HTML+Chart.js)
    cli/                stats 子命令:命令行查询统计
```

## 快速开始

```bash
cd apiproxy
go mod tidy

# 设置 provider api key
export OPENAI_API_KEY=sk-xxx
export DEEPSEEK_API_KEY=sk-xxx
export DASHSCOPE_API_KEY=sk-xxx

# 启动
go run ./cmd/apiproxy -config configs/apiproxy.yaml
```

调用示例(用 route 名当 model):

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

查看指标:

```bash
curl http://localhost:8080/metrics
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
```

浏览器访问 `http://localhost:8081`，可查看：

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
./apiproxy stats -config configs/apiproxy.yaml -window 168h -provider qianxin

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

Provider  Model              Route       请求  错误  成功率    平均     P50     P95     P99    TPS  Prompt  Compl  Stream
--------  -----------------  ----------  ----  ----  ------  ------  ------  ------  ------  -----  ------  -----  ------
qianxin   DeepSeek-V4-Flash  smart-chat    36     1   97.2%  2350ms  2299ms  4254ms  4662ms  940.8   51562   2211       1

按上下文长度分桶 (PP/TG 速度)

Provider  Model                   桶  请求  Prompt  Completion  平均延迟  PP tok/s  TG tok/s
--------  -----------------  -------  ----  ------  ----------  --------  --------  --------
qianxin   DeepSeek-V4-Flash    0-128    22     296        1054    2060ms     496.6      23.6
qianxin   DeepSeek-V4-Flash  128-512     7    1405         688    3146ms       0.0      31.2
qianxin   DeepSeek-V4-Flash    2k-8k     7   49861         469    2466ms       0.0      27.2

时间序列趋势

时间                     请求  错误  平均延迟  Prompt  Completion    TPS
-----------------------  ----  ----  --------  ------  ----------  -----
2026-06-17T00:00:00.000    36     1    2350ms   51562        2211  940.8
```

**minute 粒度（精细到每分钟）**

```
时间                     请求  错误  平均延迟  Prompt  Completion    TPS
-----------------------  ----  ----  --------  ------  ----------  -----
2026-06-17T20:11:00.000     1     0    1405ms       8          31   22.1
2026-06-17T20:12:00.000    23     1    2113ms   35855        1320  624.7
2026-06-17T20:13:00.000    12     0    2884ms   15699         860  298.2
```

**JSON 模式**

```json
{
  "window_minutes": 10080,
  "interval": "day",
  "summaries": [
    {
      "provider": "qianxin",
      "model": "DeepSeek-V4-Flash",
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
      "provider": "qianxin",
      "model": "DeepSeek-V4-Flash",
      "requests": 22,
      "pp_rate": 496.6,
      "tg_rate": 23.6,
      "avg_latency_ms": 2060
    }
  ],
  "timeseries": [
    {
      "ts": "2026-06-17T00:00:00.000",
      "requests": 36,
      "errors": 1,
      "avg_latency_ms": 2350,
      "prompt_tokens": 51562,
      "completion_tokens": 2211
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
```

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
- [ ] 加权 / 低延迟 / 健康优先路由
- [ ] 自动熔断触发(目前仅有状态机)
- [ ] Anthropic `/v1/messages` 原生协议
- [ ] 成本估算 / 审计日志
- [ ] OpenTelemetry tracing
