package admin

const dashboardHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>apiproxy metrics</title>
  <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f7fb;
      --card: #fff;
      --text: #172033;
      --muted: #657085;
      --border: #e4e7ef;
      --primary: #315efb;
      --danger: #ca2d2d;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: var(--bg);
      color: var(--text);
    }
    header {
      padding: 20px 28px 8px;
    }
    h1 { margin: 0; font-size: 24px; }
    .subtitle { color: var(--muted); margin-top: 6px; }
    main { padding: 16px 28px 32px; }
    .filters, .card {
      background: var(--card);
      border: 1px solid var(--border);
      border-radius: 14px;
      box-shadow: 0 1px 2px rgba(20, 28, 45, 0.04);
    }
    .filters {
      display: grid;
      grid-template-columns: repeat(6, minmax(130px, 1fr));
      gap: 12px;
      padding: 16px;
      align-items: end;
    }
    label { display: block; font-size: 12px; color: var(--muted); margin-bottom: 6px; }
    select, input, button {
      width: 100%;
      border: 1px solid var(--border);
      border-radius: 9px;
      padding: 9px 10px;
      background: #fff;
      color: var(--text);
      font-size: 14px;
    }
    button {
      border-color: var(--primary);
      background: var(--primary);
      color: #fff;
      cursor: pointer;
      font-weight: 600;
    }
    .grid {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 16px;
      margin-top: 16px;
    }
    .card { padding: 16px; min-height: 330px; }
    .card h2 { margin: 0 0 12px; font-size: 16px; }
    .full { grid-column: 1 / -1; }
    .table-wrap { overflow: auto; }
    table { width: 100%; border-collapse: collapse; font-size: 13px; }
    th, td { padding: 9px 10px; border-bottom: 1px solid var(--border); text-align: right; white-space: nowrap; }
    th:first-child, td:first-child, th:nth-child(2), td:nth-child(2), th:nth-child(3), td:nth-child(3) { text-align: left; }
    th { color: var(--muted); font-weight: 600; }
    .error { color: var(--danger); margin-top: 12px; display: none; }
    .muted { color: var(--muted); }
    canvas { max-height: 280px; }
    @media (max-width: 1100px) {
      .filters { grid-template-columns: repeat(2, 1fr); }
      .grid { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <header>
    <h1>apiproxy 性能分析</h1>
    <div class="subtitle">查看模型请求量、成功率、延迟、每秒 token、上下文长度下 PP/TG 速度。</div>
  </header>
  <main>
    <section class="filters">
      <div>
        <label>时间范围</label>
        <select id="range">
          <option value="-1h">最近 1 小时</option>
          <option value="-6h">最近 6 小时</option>
          <option value="-24h" selected>最近 24 小时</option>
          <option value="-7d">最近 7 天</option>
          <option value="-30d">最近 30 天</option>
        </select>
      </div>
      <div>
        <label>Provider</label>
        <select id="provider"><option value="">全部</option></select>
      </div>
      <div>
        <label>Model</label>
        <select id="model"><option value="">全部</option></select>
      </div>
      <div>
        <label>Route</label>
        <select id="route"><option value="">全部</option></select>
      </div>
      <div>
        <label>粒度</label>
        <select id="interval">
          <option value="minute">分钟</option>
          <option value="hour" selected>小时</option>
          <option value="day">天</option>
        </select>
      </div>
      <button id="refresh">刷新</button>
    </section>
    <div class="error" id="error"></div>

    <section class="grid">
      <div class="card full">
        <h2>模型性能汇总</h2>
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Provider</th><th>Model</th><th>Route</th><th>请求</th><th>错误</th><th>成功率</th><th>平均延迟</th><th>P50</th><th>P95</th><th>P99</th><th>TPS</th><th>Prompt</th><th>Completion</th><th>Fallback</th>
              </tr>
            </thead>
            <tbody id="summaryBody"></tbody>
          </table>
        </div>
      </div>
      <div class="card">
        <h2>延迟趋势</h2>
        <canvas id="latencyChart"></canvas>
      </div>
      <div class="card">
        <h2>每秒 token 趋势</h2>
        <canvas id="tpsChart"></canvas>
      </div>
      <div class="card">
        <h2>不同上下文长度 PP 速度</h2>
        <canvas id="ppChart"></canvas>
      </div>
      <div class="card">
        <h2>不同上下文长度 TG 速度</h2>
        <canvas id="tgChart"></canvas>
      </div>
    </section>
  </main>

<script>
var charts = {};
function eid(id) { return document.getElementById(id); }

function fmt(n, digits) {
  if (!digits) digits = 1;
  if (n === null || n === undefined || Number.isNaN(Number(n))) return "-";
  return Number(n).toFixed(digits);
}

function queryParams() {
  var p = new URLSearchParams();
  p.set("start", eid("range").value);
  var provider = eid("provider").value;
  var model = eid("model").value;
  var route = eid("route").value;
  if (provider) p.set("provider", provider);
  if (model) p.set("model", model);
  if (route) p.set("route", route);
  return p;
}

function fetchJSON(path, params) {
  var url = path + "?" + (params ? params.toString() : "");
  return fetch(url).then(function(res) {
    if (!res.ok) return res.text().then(function(text) { throw new Error(path + " failed: " + text); });
    return res.json();
  });
}

function setError(err) {
  var el = eid("error");
  if (!err) {
    el.style.display = "none";
    el.textContent = "";
    return;
  }
  el.style.display = "block";
  el.textContent = err.message || String(err);
}

function fillSelect(id, values) {
  var el = eid(id);
  var current = el.value;
  el.innerHTML = '<option value="">全部</option>';
  for (var i = 0; i < (values || []).length; i++) {
    var v = values[i];
    if (!v) continue;
    var opt = document.createElement("option");
    opt.value = v;
    opt.textContent = v;
    el.appendChild(opt);
  }
  el.value = current;
}

function loadFilters() {
  return fetchJSON("/api/filters").then(function(data) {
    fillSelect("provider", data.providers);
    fillSelect("model", data.models);
    fillSelect("route", data.routes);
  });
}

function td(val) { return "<td>" + val + "</td>"; }

function renderSummary(rows) {
  var body = eid("summaryBody");
  body.innerHTML = "";
  if (!rows || rows.length === 0) {
    body.innerHTML = '<tr><td colspan="14" class="muted">当前筛选范围内没有数据</td></tr>';
    return;
  }
  for (var i = 0; i < rows.length; i++) {
    var r = rows[i];
    var tr = document.createElement("tr");
    tr.innerHTML =
      td(r.provider) +
      td(r.model) +
      td(r.route) +
      td(r.requests) +
      td(r.errors) +
      td(fmt((r.success_rate || 0) * 100, 1) + "%") +
      td(fmt(r.avg_latency_ms, 0) + "ms") +
      td(fmt(r.p50_latency_ms, 0) + "ms") +
      td(fmt(r.p95_latency_ms, 0) + "ms") +
      td(fmt(r.p99_latency_ms, 0) + "ms") +
      td(fmt(r.tokens_per_sec, 1)) +
      td(r.prompt_tokens) +
      td(r.completion_tokens) +
      td(r.fallbacks);
    body.appendChild(tr);
  }
}

function upsertChart(id, config) {
  if (charts[id]) charts[id].destroy();
  charts[id] = new Chart(eid(id), config);
}

function renderTimeseries(rows) {
  var labels = rows.map(function(r) { return r.ts; });
  upsertChart("latencyChart", {
    type: "line",
    data: { labels: labels, datasets: [{ label: "平均延迟 ms", data: rows.map(function(r){ return r.avg_latency_ms; }), borderColor: "#315efb", tension: 0.25 }] },
    options: { responsive: true, maintainAspectRatio: false }
  });
  var tps = rows.map(function(r) {
    var latencySeconds = (r.avg_latency_ms || 0) / 1000;
    return latencySeconds > 0 ? (r.completion_tokens || 0) / latencySeconds : 0;
  });
  upsertChart("tpsChart", {
    type: "bar",
    data: { labels: labels, datasets: [{ label: "Completion token/s", data: tps, backgroundColor: "#12a87c" }] },
    options: { responsive: true, maintainAspectRatio: false }
  });
}

function renderBuckets(rows) {
  var grouped = new Map();
  for (var i = 0; i < (rows || []).length; i++) {
    var r = rows[i];
    var key = r.provider + "/" + r.model;
    if (!grouped.has(key)) grouped.set(key, []);
    grouped.get(key).push(r);
  }
  var labelsSet = new Set();
  for (var i = 0; i < (rows || []).length; i++) labelsSet.add(rows[i].bucket);
  var labels = Array.from(labelsSet);
  var colors = ["#315efb", "#12a87c", "#f59e0b", "#c026d3", "#ef4444", "#0891b2"];
  var ppDatasets = [];
  var tgDatasets = [];
  var idx = 0;
  grouped.forEach(function(items, name) {
    var byBucket = new Map(items.map(function(x) { return [x.bucket, x]; }));
    var color = colors[idx % colors.length];
    ppDatasets.push({ label: name, data: labels.map(function(b) { var x = byBucket.get(b); return x ? x.pp_rate : 0; }), borderColor: color, backgroundColor: color, tension: 0.2 });
    tgDatasets.push({ label: name, data: labels.map(function(b) { var x = byBucket.get(b); return x ? x.tg_rate : 0; }), borderColor: color, backgroundColor: color, tension: 0.2 });
    idx++;
  });
  upsertChart("ppChart", { type: "line", data: { labels: labels, datasets: ppDatasets }, options: { responsive: true, maintainAspectRatio: false } });
  upsertChart("tgChart", { type: "line", data: { labels: labels, datasets: tgDatasets }, options: { responsive: true, maintainAspectRatio: false } });
}

function refresh() {
  setError(null);
  var params = queryParams();
  var tsParams = new URLSearchParams(params);
  tsParams.set("interval", eid("interval").value);
  Promise.all([
    fetchJSON("/api/summary", params),
    fetchJSON("/api/timeseries", tsParams),
    fetchJSON("/api/buckets", params),
  ]).then(function(results) {
    renderSummary(results[0] || []);
    renderTimeseries(results[1] || []);
    renderBuckets(results[2] || []);
  }).catch(function(err) {
    setError(err);
  });
}

eid("refresh").addEventListener("click", refresh);
var changeIds = ["range", "provider", "model", "route", "interval"];
for (var i = 0; i < changeIds.length; i++) {
  eid(changeIds[i]).addEventListener("change", refresh);
}

loadFilters().then(refresh).catch(setError);
</script>
</body>
</html>`
