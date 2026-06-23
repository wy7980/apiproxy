# Dashboard 模型 Token 总量面板设计文档

## 1. 面板概述

"模型 Token 总量"面板以表格 + 柱状图双重形式展示筛选范围内每个模型的累计 token 使用量。表格侧重数值精确度，柱状图侧重直观对比。

数据来源：`GET /api/summary`（与汇总面板共享同一 payload），前端 `renderTokens()` 按 `(provider, model)` 聚合。

## 2. 表格列定义

| 列 | 计算方式 |
| --- | --- |
| Model | `(provider || "") + "/" + (model || "")` 分组后的 `model` 值 |
| 输入 Prompt | 同 model 下所有行的 `prompt_tokens` 累加 |
| 输出 Completion | 同 model 下所有行的 `completion_tokens` 累加 |
| 合计 | `prompt + completion` |
| 输入占比 | `prompt / total * 100%` |
| 输出占比 | `completion / total * 100%` |

### 2.1 聚合维度

按 `(provider, model)` 聚合，不考虑 route。同一模型经不同 route 转发的 token 使用量合并到同一行，因为用户关心的是"这个模型一共用了多少 token"而非"这条 route 用了多少"。

### 2.2 总量口径

`prompt_tokens` 和 `completion_tokens` **统计所有请求（含错误）**。这是有意的：即使请求失败（如 429/500），上游仍可能已经消耗了 token（部分流式失败场景），总量字段需要如实反映。

### 2.3 数值格式

`fmtTokens()` 自动缩写：≥1M → `x.xxM`，≥1K → `x.xK`，其余整数。避免表格出现 6 位以上数字影响可读性。

## 3. 柱状图设计

### 3.1 数据集结构

每个模型对应一个 Chart.js dataset。X 轴标签为 `model + " 输入"` 和 `model + " 输出"`，每个模型占据两个相邻柱位：

```javascript
catLabels = ["gpt-5.4 输入", "gpt-5.4 输出", "deepseek-v3 输入", "deepseek-v3 输出", ...]
```

dataset 数量 = 模型数量，每个 dataset 的数据只在自己的两个柱位有值，其余为 0。这样：

- 每个模型的所有柱子使用同一颜色，视觉上关联。
- 点击 legend chip 可以切换整个模型的可见性（隐藏时 prompt 和 completion 柱子同时消失）。

### 3.2 Legend

使用分页 legend（`paginateLegend`），最多同时展示 `LEGEND_PAGE_SIZE=3` 个模型 chip，左右有 ‹ › 翻页箭头。隐藏的 chip 半透明（opacity 0.35），点击可恢复。详见 [chart-legend-pagination 设计文档](./dashboard-chart-legend-pagination-design.md)。

### 3.3 不使用 stacked bar

Prompt 和 Completion 不做堆叠，而是并列展示。堆叠会让输入占比视觉上难以比较（长条 + 短条叠加后看不出各自比例）。并列可以直观察看每个模型的 prompt/completion 比例差异。

## 4. 与其它面板的关系

Token 总量表和汇总表共享数据源，但聚合维度不同：

| 面板 | 聚合维度 | 核心指标 |
| --- | --- | --- |
| 模型性能汇总 | provider, model, route | 速度、延迟、成功率 |
| 模型 Token 总量 | provider, model | 累计 token 使用 |

route 维度在 token 总量面板被折叠，因为 token 用量是"模型消耗了多少"而非"route 分配了多少"。

## 5. 非目标

- 不做时间趋势——这是累计总量，不是变化率。
- 不区分 prompt/completion 占比的堆叠图——并列更直观。
