# Dashboard Token/s 指标统计设计文档

## 1. 问题背景

Dashboard 当前"每秒 token 趋势"在前端用以下公式计算：

```text
TPS = SUM(completion_tokens) / AVG(latency_ms_seconds)
```

即同一时间桶内所有请求的 completion token 总量除以延迟的算术平均值。该公式存在以下问题：

### 1.1 数学口径不正确

`SUM / AVG` 不是真正的平均速率。当同一时间桶内请求延迟差异较大时，会被系统性高估。

示例：某小时桶内两个请求：

| 请求 | latency_ms | first_token_ms | completion_tokens | 实际 TG speed |
|------|-----------|----------------|-------------------|---------------|
| A    | 1100      | 100            | 100               | 100 tok/s     |
| B    | 5200      | 200            | 500               | 100 tok/s     |

真正平均 TG speed = (100 + 100) / 2 = **100 tok/s**

旧公式算出：`600 / avg(1100, 5200)ms` = `600 / 3.15s` ≈ **190.5 tok/s**（偏差近 2 倍）

### 1.2 包含 Prefill 时间

`latency_ms` 是请求总延迟（含排队、PP 处理、TG 生成），而 `completion_tokens / latency_seconds` 得到的是混合速率，不是纯生成速度。与下方"不同上下文长度 TG 速度"图表的口径不一致（该图表扣减了 first_token_ms）。

### 1.3 错误请求污染统计

SQL 中没有过滤 `status_code >= 400` 或 `error_type != ''` 的请求。失败请求的延迟（秒拒极短、超时极长）会扭曲平均值。

### 1.4 三处指标口径不一致

| 位置 | 当前计算方式 | 含义 |
|------|------------|------|
| Summary 表 TPS | `SUM(completion_tokens) / AVG(latency_ms)` | 混合吞吐（含 PP） |
| 趋势图 TPS | 同上（前端计算） | 同上 |
| 分桶 TG 速度 | `SUM(completion_tokens) / SUM(latency - first_token)` | 纯生成速度 |

同一模型在不同面板中数值差异巨大，用户无法互相对照。

### 1.5 单线聚合丢失模型维度

趋势图把所有 provider/model 聚合成一条线，无法区分不同模型的性能差异。与分桶图（按 provider/model 分线）的交互模式不一致。

---

## 2. 统一指标定义

### 2.1 核心定义

所有展示 "token/s" 的位置统一表示为：

> **TG tok/s = 成功请求的 completion token 生成速度**

计算公式：

```text
TG tok/s = SUM(success_completion_tokens) / SUM(success_generation_seconds)
```

即聚合吞吐口径：把所有有效样本的 token 总量除以生成耗时总量。

### 2.2 成功请求条件

只纳入满足以下条件的请求参与速度计算：

```text
status_code < 400
AND error_type = ''
AND completion_tokens > 0
```

### 2.3 生成耗时（generation_seconds）规则

| 请求类型 | 条件 | generation_seconds |
|----------|------|--------------------|
| 流式（stream=1） | `first_token_ms > 0 AND latency_ms > first_token_ms` | `(latency_ms - first_token_ms) / 1000` |
| 流式（stream=1） | `first_token_ms = 0 OR latency_ms <= first_token_ms` | **排除**，不参与速度计算 |
| 非流式（stream=0） | `latency_ms > 0` | `latency_ms / 1000`（退化近似） |
| 非流式（stream=0） | `latency_ms = 0` | **排除**，不参与速度计算 |

### 2.4 总量字段不变

`prompt_tokens`、`completion_tokens`、`total_tokens`、`requests`、`errors` **继续保持原有口径**——统计筛选范围内所有请求（含错误），用于"模型 Token 总量"表和基础请求计数。只有速度指标使用成功有效样本子集计算。

### 2.5 avg_latency_ms 改为成功请求均值

```text
avg_latency_ms = AVG(latency_ms WHERE status_code < 400 AND error_type = '')
```

错误请求的延迟（秒拒/超时）会严重扭曲平均值，应排除。`errors` 计数单独保留。

---

## 3. 展示维度

### 3.1 趋势图按 provider/model 多折线

所有趋势图（延迟 + TG token/s）按 `provider + model` 分折线展示，与"不同上下文长度 PP/TG 速度"的分组方式一致。

| 图表 | X 轴 | 每条线 | 线标签 | Y 轴 |
|------|------|--------|--------|------|
| 延迟趋势 | 时间桶 | provider/model | `example-provider/deepseek-v3` | 成功请求 avg_latency_ms |
| 生成速度趋势（TG） | 时间桶 | provider/model | `example-provider/deepseek-v3` | tokens_per_sec |

空缺时间点使用 `null`，不使用 `0`，避免把"无请求"误画成性能跌到 0。

### 3.2 不按 route 分线

route 通过筛选器过滤即可，避免图表线条数量爆炸。

---

## 4. 三处指标口径统一对照

| 位置 | 指标名 | 分子 | 分母 | 分组维度 |
|------|--------|------|------|----------|
| Summary 表 | TG tok/s | 成功请求 completion token 总量 | 成功请求 generation_seconds 总量 | provider, model, route |
| 趋势图 | tokens_per_sec | 同上 | 同上 | 时间桶, provider, model |
| 分桶 TG 图 | tg_rate | 同上 | 同上 | provider, model, prompt bucket |

三者分子/分母/筛选条件完全一致，只是分组维度不同。

---

## 5. API 返回结构

### 5.1 `/api/timeseries`

```json
[
  {
    "ts": "2026-06-22T10:00:00.000",
    "provider": "example-provider",
    "model": "deepseek-v3",
    "requests": 36,
    "errors": 1,
    "avg_latency_ms": 2300,
    "prompt_tokens": 51562,
    "completion_tokens": 2211,
    "tokens_per_sec": 47.3
  },
  {
    "ts": "2026-06-22T10:00:00.000",
    "provider": "openai",
    "model": "gpt-4o-mini",
    "requests": 8,
    "errors": 0,
    "avg_latency_ms": 900,
    "prompt_tokens": 4000,
    "completion_tokens": 600,
    "tokens_per_sec": 88.2
  }
]
```

新增字段：`provider`、`model`、`tokens_per_sec`

`tokens_per_sec` 由后端直接计算，前端与 CLI 不再自行推导。

### 5.2 `/api/summary`

`tokens_per_sec` 字段已存在，只改计算语义（从 `SUM/AVG` 改为 `SUM/SUM`），JSON 格式不变。

### 5.3 `/api/buckets`

`tg_rate` 已存在，只增加错误请求排除逻辑，JSON 格式不变。

---

## 6. 前端渲染逻辑

### 6.1 延迟趋势与 TG token/s 趋势

两图统一使用 `provider/model` 分组渲染多折线：

```javascript
// 按 provider/model 分组
var grouped = new Map();
rows.forEach(function(r) {
  var key = r.provider + "/" + r.model;
  if (!grouped.has(key)) grouped.set(key, new Map());
  grouped.get(key).set(r.ts, r);
});

// 收集所有时间点作为 labels
var allTs = [];
rows.forEach(function(r) { if (allTs.indexOf(r.ts) < 0) allTs.push(r.ts); });
allTs.sort();

// 每组一条折线
var colors = ["#315efb", "#12a87c", "#f59e0b", "#c026d3", "#ef4444", "#0891b2"];
var datasets = [];
var idx = 0;
grouped.forEach(function(byTs, name) {
  datasets.push({
    label: name,
    data: allTs.map(function(ts) {
      var x = byTs.get(ts);
      return x ? x.tokens_per_sec : null;  // null = 不画点
    }),
    borderColor: colors[idx % colors.length],
    tension: 0.25
  });
  idx++;
});
```

不再在前端做任何除法计算，直接使用后端 `tokens_per_sec` 字段。

### 6.2 图表配置变更

| 项目 | 旧 | 新 |
|------|----|----|
| 图表标题 | 每秒 token 趋势 | 生成速度趋势（TG） |
| dataset label | Completion token/s | TG token/s |
| 图表类型 | bar | line |
| Summary 表头 | TPS | TG tok/s |

---

## 7. 非流式请求的退化处理

非流式请求没有 `first_token_ms` 观测，因此使用 `latency_ms` 作为生成时间的近似。这虽然不如流式请求精确（prefill 时间被计入），但比完全排除非流式请求更有用。在混合流式与非流式请求的场景中，非流式请求的"速度"会偏低，但不会像旧公式那样产生系统性偏差。

---

## 8. 数值验证示例

假设某个时间桶内有以下请求：

| 请求 | provider/model | stream | status | latency_ms | first_token_ms | completion_tokens |
|------|---------------|--------|--------|-----------|----------------|-------------------|
| A | example-provider/DS-V3 | 1 | 200 | 1100 | 100 | 100 |
| B | example-provider/DS-V3 | 1 | 200 | 5200 | 200 | 500 |
| C | example-provider/DS-V3 | 0 | 500 | 10 | 0 | 1000 |

计算过程：

- 请求 C：status=500 → **排除**，不参与速度
- 请求 A：generation = 1100 - 100 = 1000ms，completion = 100
- 请求 B：generation = 5200 - 200 = 5000ms，completion = 500

```text
tokens_per_sec = (100 + 500) / ((1000 + 5000) / 1000) = 600 / 6 = 100
```

旧公式会算出：

```text
600 / avg(1100, 5200)ms ≈ 190.5  ← 偏高近 2 倍
```

---

## 9. 不做什么

- 不按 route 分线；route 通过现有筛选器过滤
- 不做 SQLite schema migration；现有字段已足够计算
- 不改变 token 总量统计口径
- 非流式请求退化为用 `latency_ms` 作为 generation time 近似
