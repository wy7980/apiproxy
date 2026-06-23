# Dashboard 图表 Legend 分页设计文档

## 1. 问题背景

Dashboard 中多个图表按 `provider/model` 展示多条线或多个模型柱。随着模型数量增加，Chart.js 默认 legend 会把 `newapi/gpt-5.4` 这类较长标签全部展开到图表上方，导致：

1. legend 占据大量垂直空间。
2. 标签换行导致图表区域变小。
3. 用户难以定位某个模型。
4. 模型 Token 总量柱状图原先只有 `输入 Prompt` / `输出 Completion` 两个 legend，无法按模型隐藏/显示。

因此需要自定义分页 legend：一次只展示约 3 个模型标签，通过 ‹ › 箭头翻页，并支持点击隐藏/显示。

## 2. 适用图表

分页 legend 适用于 dashboard 中 5 个图表：

| 图表 | DOM id | Legend 容器 | 分组维度 |
| --- | --- | --- | --- |
| 模型 Token 总量 | `tokensChart` | `tokensChartLegend` | model |
| 延迟趋势 | `latencyChart` | `latencyChartLegend` | provider/model |
| 生成速度趋势（TG） | `tpsChart` | `tpsChartLegend` | provider/model |
| 不同上下文长度 PP 速度 | `ppChart` | `ppChartLegend` | provider/model |
| 不同上下文长度 TG 速度 | `tgChart` | `tgChartLegend` | provider/model |

## 3. DOM 结构

每个 chart canvas 上方增加一个 legend 容器：

```html
<div id="latencyChartLegend" class="chart-legend"></div>
<canvas id="latencyChart"></canvas>
```

自定义 legend 渲染结构：

```html
<div class="chart-legend">
  <span class="legend-arrow">‹</span>
  <div class="legend-items">
    <span class="legend-item">provider/model-a</span>
    <span class="legend-item hidden">provider/model-b</span>
    <span class="legend-item">provider/model-c</span>
  </div>
  <span class="legend-arrow">›</span>
</div>
```

## 4. 分页行为

配置：

```javascript
var LEGEND_PAGE_SIZE = 3;
```

行为：

- 每页最多显示 3 个 legend chip。
- `‹` 向前翻一页。
- `›` 向后翻一页。
- 当前页开始位置保存为 `legendState[chartId].start`。
- 图表刷新后保留当前分页位置（若数据集数量减少，自动 clamp 到合法范围）。

## 5. 显示/隐藏行为

点击 legend chip 切换对应 dataset 的可见性：

```javascript
var meta = chart.getDatasetMeta(idx);
meta.hidden = !meta.hidden;
chart.update();
```

隐藏状态保存在：

```javascript
legendState[chartId].hidden[idx] = true/false;
```

图表刷新重建后，通过 `upsertChart()` 恢复：

```javascript
charts[id].getDatasetMeta(i).hidden = !!st.hidden[i];
```

隐藏的 chip 增加 `.hidden` class，半透明展示。

## 6. Chart.js 配置

所有使用分页 legend 的图表禁用 Chart.js 内置 legend：

```javascript
plugins: { legend: { display: false } }
```

避免双 legend 并存。

公共配置函数：

```javascript
function multiSeriesOpts() {
  return {
    responsive: true,
    maintainAspectRatio: false,
    plugins: { legend: { display: false } }
  };
}
```

## 7. 模型 Token 总量柱状图特殊处理

原图结构：

- X 轴：models
- datasets：`输入 Prompt`、`输出 Completion`
- legend 只能隐藏 prompt 或 completion，无法隐藏某个模型。

新图结构：

- X 轴：每个模型拆成两个 category：`model 输入`、`model 输出`
- datasets：每个模型一个 dataset
- 每个 dataset 只在自己两个 category 上有值，其它 category 为 0
- legend chip 对应模型，点击即可同时隐藏该模型输入/输出两根柱子

这样满足"类似延迟趋势图，通过模型名称点选展示/隐藏相关模型柱子"的需求。

## 8. 样式

核心样式：

```css
.chart-legend { display: flex; align-items: center; gap: 6px; }
.legend-arrow { cursor: pointer; width: 24px; text-align: center; }
.legend-items { display: flex; gap: 4px; overflow: hidden; flex: 1; }
.legend-item { cursor: pointer; font-size: 11px; white-space: nowrap; }
.legend-item.hidden { opacity: 0.35; }
```

## 9. 非目标

- 不实现搜索模型名称；分页已能解决主要拥挤问题。
- 不实现拖拽排序；数据集顺序由后端/前端聚合顺序决定。
- 不用 Chart.js 默认 legend，因为它不支持分页。
