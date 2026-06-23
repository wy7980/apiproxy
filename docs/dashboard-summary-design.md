# Dashboard 模型性能汇总面板设计文档

## 1. 面板概述

"模型性能汇总"是 dashboard 顶部第一个面板，以表格形式展示筛选范围内每个 `(provider, model, route)` 组合的关键性能指标，是用户快速对比不同模型整体表现的入口。

数据来源：`GET /api/summary`，对应后端 `storage.Store.ModelSummaries()`。

## 2. 展示维度与列定义

表格按 `provider, model, route` 三元组分组，每行一组：

| 列 | 字段 | 计算口径 |
| --- | --- | --- |
| Provider | `provider` | 直接取值 |
| Model | `model` | 直接取值 |
| Route | `route` | 直接取值 |
| 请求 | `requests` | `COUNT(*)`，含错误请求 |
| 错误 | `errors` | `SUM(status_code >= 400 OR error_type != '')` |
| 成功率 | `success_rate` | `(requests - errors) / requests`，百分比 |
| 平均延迟 | `avg_latency_ms` | 成功请求延迟均值（排除错误） |
| P50 / P95 / P99 | `p50/p95/p99_latency_ms` | 来自 `LatencyPercentiles()` |
| TG tok/s | `tokens_per_sec` | 见 [token-speed 设计文档](./dashboard-token-speed-design.md) |
| Prompt | `prompt_tokens` | 所有请求 prompt token 总量 |
| Completion | `completion_tokens` | 所有请求 completion token 总量 |
| Fallback | `fallbacks` | `fallback_count > 0` 的请求数 |

## 3. 关键口径说明

### 3.1 平均延迟只算成功请求

```text
avg_latency_ms = AVG(latency_ms WHERE status_code < 400 AND error_type = '')
```

错误请求（秒拒极短、超时极长）会严重扭曲延迟均值，必须排除。`errors` 计数单独展示，便于判断错误率。

### 3.2 TG tok/s 与其它面板一致

`tokens_per_sec` 使用统一口径：

```text
SUM(success_completion_tokens) / SUM(success_generation_seconds)
```

详见 [token-speed 设计文档](./dashboard-token-speed-design.md)。它只用于速度计算，不影响 `completion_tokens` 总量字段（总量仍统计所有请求）。

### 3.3 分组维度

按 `provider, model, route` 分组而非仅 `provider, model`，因为同一模型经不同 route 转发时上游可能不同（fallback 链），性能特征可能不同。route 作为分组列保留，但**不进入趋势图/分桶图的分组**，避免图表线条爆炸。

## 4. P50/P95/P99 实现

百分位单独由 `LatencyPercentiles()` 查询（`storage.go`）：

1. 拉取筛选范围内所有 `(provider, model, latency_ms)` 原始行。
2. 按 `provider\x00model` 在 Go 内聚合到切片。
3. 排序后按 `int((n-1) * p)` 取索引值。

`ModelSummaries` 的 handler (`admin.go handleSummary`) 把百分位结果用 `provider\x00model` 作 key 合并进 summary 行。百分位**不过滤错误请求**，与 avg_latency_ms 口径不同——这是有意的：百分位关注完整请求时延分布（含失败的尾部），而平均值关注正常请求的典型延迟。

## 5. 排序

```sql
ORDER BY provider, model, route
```

字典序排列，便于人工定位。不做按请求数/延迟排序，避免指标列隐藏时排序结果难以预期。

## 6. 非目标

- 不在本面板做时间趋势（趋势在独立面板）。
- 不提供列排序/分页——面板面向中等规模 provider/model 数量；超大规模场景应配合筛选器缩小范围。
