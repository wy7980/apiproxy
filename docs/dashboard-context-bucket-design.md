# Dashboard 上下文长度 PP/TG 分桶图设计文档

## 1. 面板概述

上下文长度分桶图用于分析模型在不同 prompt 长度下的速度表现，分为两个面板：

- **不同上下文长度 PP 速度**：Prefill / Prompt Processing 速度。
- **不同上下文长度 TG 速度**：Token Generation 速度。

数据来源：`GET /api/buckets`，对应后端 `storage.Store.SpeedByPromptBucket()`。

## 2. 分桶定义

默认 buckets 位于 `storage.DefaultBuckets`：

| Label | Min | Max |
| --- | ---: | ---: |
| `0-128` | 0 | 128 |
| `128-512` | 128 | 512 |
| `512-2k` | 512 | 2048 |
| `2k-8k` | 2048 | 8192 |
| `8k-32k` | 8192 | 32768 |
| `32k+` | 32768 | 1<<30 |

分桶依据为请求的 `prompt_tokens`。

## 3. API 返回结构

每行代表一个 `(provider, model, bucket)` 聚合结果：

```json
{
  "bucket": "2k-8k",
  "bucket_min": 2048,
  "bucket_max": 8192,
  "provider": "example-provider",
  "model": "deepseek-v3",
  "requests": 7,
  "prompt_tokens": 49861,
  "completion_tokens": 469,
  "total_latency_ms": 17262,
  "avg_latency_ms": 2466,
  "pp_rate": 0,
  "tg_rate": 27.2
}
```

## 4. PP 速度计算

PP 速度用于衡量 prompt prefill 处理能力：

```text
PP tok/s = SUM(success_stream_prompt_tokens) / SUM(success_stream_first_token_seconds)
```

只纳入：

```text
status_code < 400
AND error_type = ''
AND stream = 1
AND first_token_ms > 0
```

非流式请求没有 first-token 观测，无法准确计算 PP 速度，因此不参与 PP 速度分子/分母。

## 5. TG 速度计算

TG 速度与 Summary / Timeseries 完全一致：

```text
TG tok/s = SUM(success_completion_tokens) / SUM(success_generation_seconds)
```

成功请求条件：

```text
status_code < 400
AND error_type = ''
AND completion_tokens > 0
```

生成耗时规则：

- 流式：`latency_ms - first_token_ms`
- 非流式：`latency_ms`（退化近似）

详见 [token-speed 设计文档](./dashboard-token-speed-design.md)。

## 6. 总量字段口径

`requests`、`prompt_tokens`、`completion_tokens`、`total_latency_ms` 保持总量统计（含错误请求），用于了解该 bucket 的整体流量规模。只有 `pp_rate` / `tg_rate` 使用成功有效样本子集。

## 7. 前端展示

### 7.1 X 轴

X 轴是按 `bucket_min` 排序后的 bucket labels。

### 7.2 多折线

每个 `provider/model` 一条线：

```javascript
var key = r.provider + "/" + r.model;
```

缺失 bucket 使用 0（而不是 null），因为这里的 X 轴是离散 bucket，0 表示该模型在此 bucket 没有可计算速度。

### 7.3 Legend

使用分页 legend，最多显示 3 个模型 chip。点击 chip 隐藏/显示对应模型线，左右箭头翻页。

## 8. 与趋势图的区别

| 图表 | X 轴 | 目的 |
| --- | --- | --- |
| TG 趋势 | 时间桶 | 看模型生成速度随时间变化 |
| TG 分桶 | prompt bucket | 看上下文长度对生成速度的影响 |
| PP 分桶 | prompt bucket | 看上下文长度对 prefill 速度的影响 |

## 9. 非目标

- 不按 route 分线，route 通过筛选器过滤。
- 不计算非流式 PP 速度，因为缺少 first-token 观测。
