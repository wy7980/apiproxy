# Dashboard 延迟趋势与生成速度趋势（TG）设计文档

## 1. 面板概述

两个趋势面板分别展示：

- **延迟趋势**：成功请求的平均延迟随时间变化。
- **生成速度趋势（TG）**：completion token 的生成吞吐速率随时间变化。

两者都按 `provider/model` 分多条折线，使用分页 legend 控制。

数据来源：`GET /api/timeseries`，对应后端 `storage.Store.TimeSeries()`。

## 2. 数据结构与分组

API 返回按 `(时间桶, provider, model)` 三元组聚合的行。每行代表一个模型在某时间段内的指标快照：

```json
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
}
```

前端按 `provider/model` 分组，收集所有时间桶作为 X 轴 labels。同一时间桶内如果缺少某个模型的样本，该点填 `null`（Chart.js 不画点，而非画成 0）。

## 3. 延迟趋势图

### 3.1 Y 轴字段

`avg_latency_ms`：**只含成功请求**（`status_code < 400 AND error_type = ''`）的延迟均值。错误请求（秒拒极短、超时极长）不参与均值计算。

### 3.2 图表配置

- 类型：`line`
- 多折线：每个 `provider/model` 一条线
- `tension: 0.25`：轻微曲线平滑
- 缺失点：`null` → Chart.js 跳过不画
- Legend：分页 legend，LEGEND_PAGE_SIZE=3

## 4. 生成速度趋势（TG）图

### 4.1 Y 轴字段

`tokens_per_sec`：后端直接计算，公式与 Summary 表、分桶 TG 图完全一致：

```text
SUM(success_completion_tokens) / SUM(success_generation_seconds)
```

详见 [token-speed 设计文档](./dashboard-token-speed-design.md)。

### 4.2 前端不再自行计算

旧版本前端用 `completion_tokens / avg_latency_ms_seconds` 推导 TPS，这是数学上不正确的 `SUM/AVG` 组合。新版本直接使用后端返回的 `tokens_per_sec`，前端不做任何除法。

### 4.3 图表配置

- 类型：`line`（旧版本用 `bar`）
- 多折线：同延迟趋势
- `tension: 0.25`
- 缺失点：`null`
- Legend：分页 legend

### 4.4 标题变更

| 旧 | 新 |
| --- | --- |
| 每秒 token 趋势 | 生成速度趋势（TG） |
| Completion token/s | provider/model（legend chip） |
| bar | line |

## 5. 不按 route 分线

route 通过筛选器过滤。如果按 route 分线，一个 3 provider × 3 route 的配置就有 9 条线，图表几乎不可读。用户关心的是"这个模型性能如何"，而非"这条 route 性能如何"。

## 6. 与分桶图的关系

| 图表 | X 轴 | 分组 | Y 轴 |
| --- | --- | --- | --- |
| 延迟趋势 | 时间桶 | provider/model | avg_latency_ms |
| TG 趋势 | 时间桶 | provider/model | tokens_per_sec |
| PP/TG 分桶 | prompt bucket | provider/model | pp_rate / tg_rate |

趋势图展示"随时间变化"，分桶图展示"随上下文长度变化"。两者 Y 轴口径相同（TG tok/s），只是维度不同。
