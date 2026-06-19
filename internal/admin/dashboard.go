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
    .header-row { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; }
    .header-actions button { width: auto; min-width: 88px; }
    #logoutBtn { display: inline-block; margin-left: 10px; color: var(--muted); font-size: 13px; text-decoration: none; }
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
    dialog {
      border: 1px solid var(--border);
      border-radius: 14px;
      padding: 0;
      width: min(1100px, 95vw);
      max-height: 92vh;
      overflow: hidden;
      box-shadow: 0 12px 36px rgba(20, 28, 45, 0.18);
    }
    dialog::backdrop { background: rgba(15, 20, 35, 0.45); }
    .config-head {
      display: flex; justify-content: space-between; align-items: center;
      padding: 16px 20px; border-bottom: 1px solid var(--border);
    }
    .config-head h2 { margin: 0; font-size: 18px; }
    .config-body { padding: 16px 20px; overflow: auto; max-height: calc(92vh - 120px); }
    .tabs { display: flex; gap: 8px; margin-bottom: 16px; }
    .tab {
      border: 1px solid var(--border); background: #fff; color: var(--text);
      padding: 8px 14px; border-radius: 9px; cursor: pointer; width: auto; min-width: 90px; font-weight: 500;
    }
    .tab.active { background: var(--primary); border-color: var(--primary); color: #fff; }
    .panel { display: none; }
    .panel.active { display: block; }
    .row-controls { display: flex; justify-content: flex-end; margin-bottom: 10px; gap: 8px; }
    .row-controls button { width: auto; }
    table.config-table input, table.config-table select {
      width: 100%; padding: 6px 8px; font-size: 13px; border-radius: 7px;
    }
    .icon-btn { padding: 4px 9px; min-width: auto; width: auto; font-size: 12px; }
    .toast {
      position: fixed; right: 24px; bottom: 24px; z-index: 9999;
      padding: 12px 18px; border-radius: 10px; color: #fff; font-size: 14px;
      box-shadow: 0 8px 22px rgba(20, 28, 45, 0.18);
      opacity: 0; transition: opacity 0.2s ease; pointer-events: none;
    }
    .toast.show { opacity: 1; }
    .toast.ok { background: #12a87c; }
    .toast.err { background: var(--danger); }
  </style>
</head>
<body>
  <header>
    <div class="header-row">
      <div>
        <h1>apiproxy 性能分析</h1>
        <div class="subtitle">查看模型请求量、成功率、延迟、每秒 token、上下文长度下 PP/TG 速度。</div>
      </div>
      <div class="header-actions">
        <button id="configBtn">配置</button>
        <a href="/logout" id="logoutBtn">退出</a>
      </div>
    </div>
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

  <dialog id="configDialog" class="config-dialog">
    <div class="config-head">
      <h2>配置管理</h2>
      <div class="row-controls">
        <button id="closeConfigBtn" class="icon-btn">关闭</button>
        <button id="saveConfigBtn" class="icon-btn">保存</button>
      </div>
    </div>
    <div class="config-body">
      <div class="tabs">
        <button id="tab-providers" class="tab active" data-tab="providers">Providers</button>
        <button id="tab-routes" class="tab" data-tab="routes">Routes</button>
      </div>
      <div id="panel-providers" class="panel active">
        <div class="row-controls">
          <button id="addProviderBtn" class="icon-btn">+ 新增 Provider</button>
        </div>
        <div class="table-wrap">
          <table class="config-table">
            <thead>
              <tr>
                <th>Name</th><th>Type</th><th>Base URL</th><th>API Key</th><th>API Key Env</th><th>Auth Header</th><th>Timeout</th><th></th>
              </tr>
            </thead>
            <tbody id="providersBody"></tbody>
          </table>
        </div>
      </div>
      <div id="panel-routes" class="panel">
        <div class="row-controls">
          <button id="addRouteBtn" class="icon-btn">+ 新增 Route</button>
        </div>
        <div class="table-wrap">
          <table class="config-table">
            <thead>
              <tr>
                <th>Name</th><th>Strategy</th><th>Providers (provider/model/tier/weight)</th><th>Fallback</th><th></th>
              </tr>
            </thead>
            <tbody id="routesBody"></tbody>
          </table>
        </div>
      </div>
    </div>
  </dialog>

  <div id="toast" class="toast"></div>

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

// ===== Config management =====
var MASKED = "***";
var cfgState = { providers: [], routes: [], providerNames: [] };

function el(tag, attrs, kids) {
  var n = document.createElement(tag);
  if (attrs) for (var k in attrs) {
    if (k === "class") n.className = attrs[k];
    else if (k === "value") n.value = attrs[k];
    else if (k === "placeholder") n.placeholder = attrs[k];
    else if (k === "type") n.type = attrs[k];
    else if (k === "title") n.title = attrs[k];
    else n.setAttribute(k, attrs[k]);
  }
  if (kids) for (var i = 0; i < kids.length; i++) {
    var k2 = kids[i];
    if (k2 == null) continue;
    n.appendChild(typeof k2 === "string" ? document.createTextNode(k2) : k2);
  }
  return n;
}

function toast(cls, msg) {
  var t = eid("toast");
  t.className = "toast " + cls;
  t.textContent = msg;
  // force reflow so transition retriggers each call
  void t.offsetWidth;
  t.classList.add("show");
  setTimeout(function() { t.classList.remove("show"); }, 2400);
}

function loadConfig() {
  return fetch("/api/config").then(function(res) {
    if (!res.ok) return res.text().then(function(t) { throw new Error(t); });
    return res.json();
  }).then(function(data) {
    cfgState.providers = data.providers || [];
    cfgState.routes = data.routes || [];
    cfgState.providerNames = cfgState.providers.map(function(p) { return p.name; }).sort();
    renderProviders();
    renderRoutes();
  }).catch(function(err) {
    toast("err", "加载配置失败: " + (err.message || err));
  });
}

function renderProviders() {
  var body = eid("providersBody");
  body.innerHTML = "";
  for (var i = 0; i < cfgState.providers.length; i++) {
    body.appendChild(providerRow(cfgState.providers[i], i));
  }
  if (cfgState.providers.length === 0) {
    body.appendChild(el("tr", null, [el("td", {colspan: "8", class: "muted"}, ["暂无 provider，点右上角新增"])]));
  }
}

function providerRow(p, idx) {
  var typeSel = el("select", null, [
    el("option", {value: "openai"}, ["openai"]),
    el("option", {value: "anthropic"}, ["anthropic"])
  ]);
  typeSel.value = p.type || "openai";
  typeSel.addEventListener("change", function() { cfgState.providers[idx].type = typeSel.value; });

  var authSel = el("select", null, [
    el("option", {value: "x-api-key"}, ["x-api-key"]),
    el("option", {value: "authorization"}, ["authorization"]),
    el("option", {value: "both"}, ["both"])
  ]);
  authSel.value = p.auth_header || "x-api-key";
  authSel.addEventListener("change", function() { cfgState.providers[idx].auth_header = authSel.value; });

  var tierSel = el("select", null, [
    el("option", {value: ""}, ["-"]),
    el("option", {value: "advanced"}, ["advanced"]),
    el("option", {value: "standard"}, ["standard"])
  ]);
  tierSel.value = p.tier || "";
  tierSel.addEventListener("change", function() { cfgState.providers[idx].tier = tierSel.value; });

  var nameInput = el("input", {type: "text", value: p.name || ""});
  nameInput.addEventListener("input", function() {
    cfgState.providers[idx].name = nameInput.value.trim();
    cfgState.providerNames = cfgState.providers.map(function(x) { return x.name; }).filter(Boolean).sort();
    refreshRouteProviderOptions();
  });

  var urlInput = el("input", {type: "text", value: p.base_url || "", placeholder: "https://..."});
  urlInput.addEventListener("input", function() { cfgState.providers[idx].base_url = urlInput.value; });

  var keyInput = el("input", {type: "password", value: p.api_key || "", placeholder: p.api_key ? MASKED : "(空)"});
  keyInput.addEventListener("input", function() { cfgState.providers[idx].api_key = keyInput.value; });

  var keyEnvInput = el("input", {type: "text", value: p.api_key_env || "", placeholder: "可选 ENV 变量名"});
  keyEnvInput.addEventListener("input", function() { cfgState.providers[idx].api_key_env = keyEnvInput.value; });

  var timeoutInput = el("input", {type: "text", value: p.timeout || "60s"});
  timeoutInput.addEventListener("input", function() { cfgState.providers[idx].timeout = timeoutInput.value; });

  var delBtn = el("button", {class: "icon-btn", title: "删除"}, ["删除"]);
  delBtn.addEventListener("click", function() {
    if (!confirm("确认删除 provider \"" + (p.name || "") + "\"？")) return;
    cfgState.providers.splice(idx, 1);
    cfgState.providerNames = cfgState.providers.map(function(x) { return x.name; }).filter(Boolean).sort();
    renderProviders();
    refreshRouteProviderOptions();
  });

  return el("tr", null, [
    el("td", null, [nameInput]),
    el("td", null, [typeSel]),
    el("td", null, [urlInput]),
    el("td", null, [keyInput]),
    el("td", null, [keyEnvInput]),
    el("td", null, [authSel]),
    el("td", null, [timeoutInput]),
    el("td", null, [tierSel, " ", delBtn])
  ]);
}

function addProvider() {
  cfgState.providers.push({
    name: "provider-" + (cfgState.providers.length + 1),
    type: "openai",
    base_url: "",
    api_key: "",
    api_key_env: "",
    auth_header: "x-api-key",
    timeout: "60s",
    tier: ""
  });
  renderProviders();
  refreshRouteProviderOptions();
}

function renderRoutes() {
  var body = eid("routesBody");
  body.innerHTML = "";
  for (var i = 0; i < cfgState.routes.length; i++) {
    body.appendChild(routeRow(cfgState.routes[i], i));
  }
  if (cfgState.routes.length === 0) {
    body.appendChild(el("tr", null, [el("td", {colspan: "4", class: "muted"}, ["暂无 route，点右上角新增"])]));
  }
}

function strategyOptions(sel, current) {
  ["priority", "weighted", "random"].forEach(function(s) {
    var o = el("option", {value: s}, [s]);
    if (s === current) o.selected = true;
    sel.appendChild(o);
  });
}

function routeRow(r, idx) {
  var nameInput = el("input", {type: "text", value: r.name || ""});
  nameInput.addEventListener("input", function() { cfgState.routes[idx].name = nameInput.value.trim(); });

  var stratSel = el("select", null, []);
  strategyOptions(stratSel, r.strategy || "priority");
  stratSel.addEventListener("change", function() { cfgState.routes[idx].strategy = stratSel.value; });

  var providersCell = el("td", null, []);
  function refreshProviders() {
    providersCell.innerHTML = "";
    var fb = r.fallback || {};
    if (!r.fallback) r.fallback = fb;
    var fbCheck = el("input", {type: "checkbox"});
    fbCheck.checked = !!fb.enabled;
    fbCheck.addEventListener("change", function() { r.fallback.enabled = fbCheck.checked; });
    providersCell.appendChild(el("label", {style: "margin-bottom:6px"}, [
      fbCheck, " fallback ",
      "（on_status: ", (fb.on_status || []).join(",") || "—", ", ",
      "max_attempts: ", fb.max_attempts || 0, ", ",
      fb.on_timeout ? "timeout ✓" : "timeout ✗", ", ",
      fb.on_connect_error ? "conn ✓" : "conn ✗", ")"
    ]));

    for (var j = 0; j < r.providers.length; j++) {
      providersCell.appendChild(providerTargetRow(r, j));
    }
  }
  refreshProviders();

  var addTargetBtn = el("button", {class: "icon-btn"}, ["+ provider target"]);
  addTargetBtn.addEventListener("click", function() {
    r.providers.push({ provider: "", model: "", tier: "", weight: 0 });
    refreshProviders();
  });

  var delBtn = el("button", {class: "icon-btn", title: "删除 route"}, ["删除"]);
  delBtn.addEventListener("click", function() {
    if (!confirm("确认删除 route \"" + (r.name || "") + "\"？")) return;
    cfgState.routes.splice(idx, 1);
    renderRoutes();
  });

  return el("tr", null, [
    el("td", null, [nameInput]),
    el("td", null, [stratSel]),
    el("td", null, [providersCell, el("div", {style: "margin-top:6px"}, [addTargetBtn])]),
    el("td", null, [delBtn])
  ]);
}

function providerTargetRow(route, j) {
  var t = route.providers[j];
  var provSel = el("select", null, [el("option", {value: ""}, ["-"])]);
  cfgState.providerNames.forEach(function(n) {
    provSel.appendChild(el("option", {value: n}, [n]));
  });
  provSel.value = t.provider || "";
  provSel.addEventListener("change", function() { t.provider = provSel.value; });

  var modelInput = el("input", {type: "text", value: t.model || "", placeholder: "model name"});
  modelInput.addEventListener("input", function() { t.model = modelInput.value; });

  var tierSel = el("select", null, [
    el("option", {value: ""}, ["-"]),
    el("option", {value: "advanced"}, ["advanced"]),
    el("option", {value: "standard"}, ["standard"])
  ]);
  tierSel.value = t.tier || "";
  tierSel.addEventListener("change", function() { t.tier = tierSel.value; });

  var weightInput = el("input", {type: "number", value: String(t.weight || 0), style: "width:64px"});
  weightInput.addEventListener("input", function() { t.weight = parseInt(weightInput.value || "0", 10); });

  var delBtn = el("button", {class: "icon-btn", title: "删除"}, ["x"]);
  delBtn.addEventListener("click", function() {
    route.providers.splice(j, 1);
    // re-render the whole route row to refresh indices
    renderRoutes();
  });

  var wrap = el("div", {style: "display:flex; gap:6px; margin-bottom:6px; align-items:center"}, [
    el("span", {style: "min-width:64px"}, ["provider:"]), provSel,
    el("span", {style: "min-width:44px"}, ["model:"]), modelInput,
    tierSel, weightInput, delBtn
  ]);
  return wrap;
}

function refreshRouteProviderOptions() {
  // cheap refresh — just re-render routes
  renderRoutes();
}

function addRoute() {
  cfgState.routes.push({
    name: "route-" + (cfgState.routes.length + 1),
    strategy: "priority",
    fallback: {
      enabled: true, max_attempts: 3,
      on_status: [429, 500, 502, 503, 504],
      on_timeout: true, on_connect_error: true,
      allow_downgrade: false
    },
    providers: []
  });
  renderRoutes();
}

function saveConfig() {
  var saveBtn = eid("saveConfigBtn");
  saveBtn.disabled = true;
  saveBtn.textContent = "保存中...";
  fetch("/api/config", {
    method: "PUT",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({ providers: cfgState.providers, routes: cfgState.routes })
  }).then(function(res) {
    return res.json().then(function(j) {
      if (!res.ok) throw new Error(j.error || ("HTTP " + res.status));
      return j;
    });
  }).then(function() {
    toast("ok", "配置已保存并热加载");
    loadConfig();
    refresh();
  }).catch(function(err) {
    toast("err", "保存失败: " + (err.message || err));
  }).finally(function() {
    saveBtn.disabled = false;
    saveBtn.textContent = "保存";
  });
}

function openConfig() {
  eid("configDialog").showModal();
  loadConfig();
}

function closeConfig() {
  eid("configDialog").close();
}

function switchTab(name) {
  ["providers", "routes"].forEach(function(t) {
    eid("tab-" + t).classList.toggle("active", t === name);
    eid("panel-" + t).classList.toggle("active", t === name);
  });
}

eid("configBtn").addEventListener("click", openConfig);
eid("closeConfigBtn").addEventListener("click", closeConfig);
eid("saveConfigBtn").addEventListener("click", saveConfig);
eid("addProviderBtn").addEventListener("click", addProvider);
eid("addRouteBtn").addEventListener("click", addRoute);
document.querySelectorAll(".tab").forEach(function(t) {
  t.addEventListener("click", function() { switchTab(t.dataset.tab); });
});
</script>
</body>
</html>`
