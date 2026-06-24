package admin

const dashboardHTML = `<!doctype html>
<html lang="en">
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
    .view-hidden { display: none; }
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
    table.summary-table th:first-child, table.summary-table td:first-child,
    table.summary-table th:nth-child(2), table.summary-table td:nth-child(2),
    table.summary-table th:nth-child(3), table.summary-table td:nth-child(3) { text-align: left; }
    table.tokens-table th:first-child, table.tokens-table td:first-child { text-align: left; }
    th { color: var(--muted); font-weight: 600; }
    .error { color: var(--danger); margin-top: 12px; display: none; }
    .muted { color: var(--muted); }
    .chart-legend { display: flex; align-items: center; gap: 6px; margin-bottom: 10px; flex-wrap: nowrap; overflow: hidden; }
    .legend-arrow { cursor: pointer; user-select: none; font-size: 16px; color: var(--muted); border: 1px solid var(--border); border-radius: 6px; width: 24px; text-align: center; line-height: 24px; }
    .legend-arrow:hover { color: var(--primary); border-color: var(--primary); }
    .legend-items { display: flex; gap: 4px; overflow: hidden; flex: 1; }
    .legend-item { cursor: pointer; font-size: 11px; padding: 3px 8px; border-radius: 6px; border: 1px solid var(--border); white-space: nowrap; transition: opacity 0.15s; }
    .legend-item:hover { border-color: var(--primary); }
    .legend-item.hidden { opacity: 0.35; }
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
    .config-body { padding: 16px 20px; }
    .config-section {
      background: var(--card);
      border: 1px solid var(--border);
      border-radius: 14px;
      box-shadow: 0 1px 2px rgba(20, 28, 45, 0.04);
    }
    .config-section .config-head { border-radius: 14px 14px 0 0; }
    .config-section .config-body { padding: 20px 24px; }
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
    table.config-table th, table.config-table td { vertical-align: top; }
    table.config-table input, table.config-table select {
      width: 100%; padding: 6px 8px; font-size: 13px; border-radius: 7px;
    }
    .route-target-row { display: grid; grid-template-columns: 72px minmax(140px, 1fr) 52px minmax(180px, 2fr) 100px 76px 34px; gap: 6px; margin-bottom: 6px; align-items: center; }
    .route-fallback { line-height: 1.8; white-space: normal; }
    .icon-btn { padding: 4px 9px; min-width: auto; width: auto; font-size: 12px; }

    /* ===== Provider cards — multi-column grid with color strips ===== */
    .providers-list {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(480px, 1fr));
      gap: 14px;
    }
    .provider-config-card {
      display: flex;
      background: #fff;
      border: 1px solid var(--border);
      border-radius: 10px;
      overflow: hidden;
      transition: box-shadow 0.2s ease;
    }
    .provider-color-bar {
      width: 4px;
      flex-shrink: 0;
    }
    .provider-config-body {
      flex: 1;
      min-width: 0;
      padding: 14px 16px;
    }
    .provider-config-card:hover {
      box-shadow: 0 2px 8px rgba(49, 94, 251, 0.08);
    }
    .provider-config-head {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
      margin-bottom: 14px;
      padding-bottom: 10px;
      border-bottom: 1px solid var(--border);
    }
    .provider-config-title {
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .provider-config-title .provider-icon {
      font-size: 16px;
    }
    .provider-config-title .provider-name-display {
      font-weight: 600;
      font-size: 15px;
      color: var(--text);
    }
    .provider-config-actions {
      display: flex;
      align-items: center;
      gap: 6px;
    }
    .provider-config-actions button {
      width: auto;
      min-width: auto;
      padding: 4px 10px;
      font-size: 12px;
      line-height: 1.4;
    }
    .provider-duplicate-btn {
      background: #fff;
      border-color: var(--border);
      color: var(--muted);
    }
    .provider-duplicate-btn:hover {
      border-color: var(--primary);
      color: var(--primary);
    }
    /* Single-column form rows */
    .provider-form-row {
      display: flex;
      align-items: center;
      gap: 10px;
      padding: 6px 0;
    }
    .provider-form-row + .provider-form-row {
      border-top: 1px solid #f0f1f5;
    }
    .provider-form-label {
      width: 110px;
      flex-shrink: 0;
      font-size: 13px;
      color: var(--muted);
      text-align: right;
    }
    .provider-form-input {
      flex: 1;
      min-width: 0;
    }
    .provider-form-input input,
    .provider-form-input select {
      width: 100%;
      min-width: 0;
      padding: 7px 10px;
      font-size: 13px;
    }
    .api-key-wrapper {
      position: relative;
      display: flex;
      align-items: center;
    }
    .api-key-wrapper input {
      padding-right: 32px;
    }
    .api-key-toggle {
      position: absolute;
      right: 6px;
      top: 50%;
      transform: translateY(-50%);
      background: none;
      border: none;
      color: var(--muted);
      cursor: pointer;
      padding: 2px 4px;
      font-size: 14px;
      line-height: 1;
      width: auto;
      min-width: auto;
    }
    .api-key-toggle:hover { color: var(--primary); }

    /* ===== Route cards — multi-column grid ===== */
    .routes-list {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(520px, 1fr));
      gap: 14px;
    }
    .route-card {
      background: #fff;
      border: 1px solid var(--border);
      border-radius: 10px;
      overflow: hidden;
      transition: box-shadow 0.2s ease;
    }
    .route-card:hover {
      box-shadow: 0 2px 8px rgba(49, 94, 251, 0.08);
    }
    .route-card-header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 12px;
      cursor: pointer;
      user-select: none;
      padding: 10px 14px;
      background: #f8f9fc;
      border-bottom: 1px solid var(--border);
    }
    .route-card-header:hover { background: rgba(49, 94, 251, 0.06); }
    .route-card-title { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
    .route-name { font-weight: 600; font-size: 14px; }
    .route-summary { font-size: 11px; color: var(--muted); }
    .route-toggle { font-size: 12px; color: var(--muted); transition: transform 0.15s; }
    .route-card.collapsed .route-toggle { transform: rotate(-90deg); }
    .route-card.collapsed .route-card-content { display: none; }
    .route-card-header-actions { display: flex; align-items: center; gap: 6px; }
    .route-card-content { padding: 12px 14px; }

    /* Provider target cards (reorderable) */
    .provider-list { margin-bottom: 8px; }
    .provider-card {
      background: #f8f9fc;
      border: 1px solid #e0e4f0;
      border-radius: 8px;
      padding: 10px 12px;
      margin-bottom: 8px;
      display: flex;
      align-items: center;
      gap: 10px;
      transition: box-shadow 0.15s, opacity 0.15s;
    }
    .provider-card.dragging { opacity: 0.4; box-shadow: 0 6px 16px rgba(0,0,0,0.18); }
    .provider-card.drag-over { border-color: var(--primary); border-style: dashed; }
    .drag-handle {
      cursor: grab; color: #9aa3b2; font-size: 16px; user-select: none;
      padding: 4px 6px; border-radius: 4px; line-height: 1;
    }
    .drag-handle:active { cursor: grabbing; }
    .drag-handle:hover { background: #e9efff; color: var(--primary); }
    .provider-fields {
      flex: 1;
      display: grid;
      grid-template-columns: minmax(120px, 1fr) minmax(120px, 1.2fr) 90px 70px;
      gap: 8px;
      align-items: center;
    }
    .provider-fields .field { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
    .provider-fields .field-label { font-size: 11px; color: var(--muted); }
    .provider-fields input, .provider-fields select {
      width: 100%;
      min-width: 0;
      padding: 5px 8px;
      font-size: 13px;
    }
    .provider-actions { display: flex; flex-direction: column; gap: 4px; }
    .sort-btn {
      padding: 1px 7px; min-width: auto; width: auto; font-size: 12px;
      line-height: 1.4; background: #fff; border-color: var(--border); color: var(--muted);
    }
    .sort-btn:hover { border-color: var(--primary); color: var(--primary); }
    .provider-del-btn {
      padding: 2px 8px; min-width: auto; width: auto; font-size: 13px;
      line-height: 1.4; background: #fff; border-color: var(--border); color: var(--danger);
    }
    .provider-del-btn:hover { background: var(--danger); color: #fff; border-color: var(--danger); }

    .fallback-section {
      margin-top: 14px;
      padding-top: 12px;
      border-top: 1px dashed var(--border);
    }
    .fallback-grid {
      display: flex;
      flex-direction: column;
      gap: 6px;
      margin-top: 10px;
    }
    .fallback-grid .fb-item {
      display: flex;
      align-items: center;
      gap: 8px;
      font-size: 13px;
      padding: 4px 0;
    }
    .fallback-grid input[type="number"] { width: 80px; }
    .fallback-grid input[type="text"] { width: 100%; }

    @media (max-width: 900px) {
      .provider-fields { grid-template-columns: 1fr 1fr; }
    }
    @media (max-width: 560px) {
      .provider-fields { grid-template-columns: 1fr; }
    }
    .toast {
      position: fixed; right: 24px; bottom: 24px; z-index: 9999;
      padding: 12px 18px; border-radius: 10px; color: #fff; font-size: 14px;
      box-shadow: 0 8px 22px rgba(20, 28, 45, 0.18);
      opacity: 0; transition: opacity 0.2s ease; pointer-events: none;
    }
    .toast.show { opacity: 1; }
    .toast.ok { background: #12a87c; }
    .toast.err { background: var(--danger); }
    .lang-btn {
      padding: 4px 10px;
      min-width: auto;
      width: auto;
      font-size: 11px;
      background: #fff;
      border-color: var(--border);
      color: var(--text);
      margin-right: 8px;
    }
    .lang-btn:hover { border-color: var(--primary); color: var(--primary); }
  </style>
</head>
<body>
  <header>
    <div class="header-row">
      <div>
        <h1 id="page-title">apiproxy Performance Analytics</h1>
        <div class="subtitle" id="page-subtitle">View model request volume, success rate, latency, tokens per second, and PP/TG speed across context lengths.</div>
      </div>
      <div class="header-actions">
        <button id="langBtn" class="lang-btn">中文</button>
        <button id="configBtn">Config</button>
        <button id="backBtn" class="view-hidden" style="background:#fff;color:var(--primary);">Back to Dashboard</button>
        <a href="/logout" id="logoutBtn">Logout</a>
      </div>
    </div>
  </header>
  <main id="dashboardView">
    <section class="filters">
      <div>
        <label id="label-range">Time Range</label>
        <select id="range">
          <option value="-1h" data-i18n="range_1h">Last 1 hour</option>
          <option value="-6h" data-i18n="range_6h">Last 6 hours</option>
          <option value="-24h" selected data-i18n="range_24h">Last 24 hours</option>
          <option value="-7d" data-i18n="range_7d">Last 7 days</option>
          <option value="-30d" data-i18n="range_30d">Last 30 days</option>
        </select>
      </div>
      <div>
        <label>Provider</label>
        <select id="provider"><option value="" data-i18n="filter_all">All</option></select>
      </div>
      <div>
        <label>Model</label>
        <select id="model"><option value="" data-i18n="filter_all">All</option></select>
      </div>
      <div>
        <label>Route</label>
        <select id="route"><option value="" data-i18n="filter_all">All</option></select>
      </div>
      <div>
        <label id="label-interval">Granularity</label>
        <select id="interval">
          <option value="minute" data-i18n="gran_minute">Minute</option>
          <option value="hour" selected data-i18n="gran_hour">Hour</option>
          <option value="day" data-i18n="gran_day">Day</option>
        </select>
      </div>
      <button id="refresh" data-i18n="btn_refresh">Refresh</button>
    </section>
    <div class="error" id="error"></div>

    <section class="grid">
      <div class="card full">
        <h2 data-i18n="title_summary">Model Performance Summary</h2>
        <div class="table-wrap">
          <table class="summary-table">
            <thead>
              <tr>
                <th>Provider</th><th>Model</th><th>Route</th>
                <th data-i18n="col_requests">Requests</th>
                <th data-i18n="col_errors">Errors</th>
                <th data-i18n="col_successrate">Success Rate</th>
                <th data-i18n="col_avglatency">Avg Latency</th>
                <th>P50</th><th>P95</th><th>P99</th>
                <th>TG tok/s</th><th>Prompt</th><th>Completion</th><th>Fallback</th>
              </tr>
            </thead>
            <tbody id="summaryBody"></tbody>
          </table>
        </div>
      </div>
      <div class="card full">
        <h2 data-i18n="title_tokens">Model Token Totals</h2>
        <div class="table-wrap">
          <table class="tokens-table">
            <thead>
              <tr>
                <th>Model</th>
                <th data-i18n="col_prompt">Input Prompt</th>
                <th data-i18n="col_completion">Output Completion</th>
                <th data-i18n="col_total">Total</th>
                <th data-i18n="col_inratio">Input %</th>
                <th data-i18n="col_outratio">Output %</th>
              </tr>
            </thead>
            <tbody id="tokensBody"></tbody>
          </table>
        </div>
        <div id="tokensChartLegend" class="chart-legend" style="margin-top:14px"></div>
        <canvas id="tokensChart"></canvas>
      </div>
      <div class="card">
        <h2 data-i18n="title_latency">Latency Trend</h2>
        <div id="latencyChartLegend" class="chart-legend"></div>
        <canvas id="latencyChart"></canvas>
      </div>
      <div class="card">
        <h2 data-i18n="title_tg_speed">Generation Speed Trend (TG)</h2>
        <div id="tpsChartLegend" class="chart-legend"></div>
        <canvas id="tpsChart"></canvas>
      </div>
      <div class="card">
        <h2 data-i18n="title_pp_by_ctx">PP Speed by Context Length</h2>
        <div id="ppChartLegend" class="chart-legend"></div>
        <canvas id="ppChart"></canvas>
      </div>
      <div class="card">
        <h2 data-i18n="title_tg_by_ctx">TG Speed by Context Length</h2>
        <div id="tgChartLegend" class="chart-legend"></div>
        <canvas id="tgChart"></canvas>
      </div>
    </section>
  </main>

  <section id="configView" class="view-hidden" style="padding:16px 28px 32px;">
    <div class="config-section">
      <div class="config-head">
        <h2 data-i18n="title_config">Config Management</h2>
        <div class="row-controls">
          <button id="addProviderBtn" class="icon-btn" data-i18n="btn_add_provider">+ Add Provider</button>
          <button id="addRouteBtn" class="icon-btn" data-i18n="btn_add_route">+ Add Route</button>
          <button id="saveConfigBtn" class="icon-btn" data-i18n="btn_save">Save</button>
        </div>
      </div>
      <div class="config-body">
        <div class="tabs">
          <button id="tab-providers" class="tab active" data-tab="providers">Providers</button>
          <button id="tab-routes" class="tab" data-tab="routes">Routes</button>
        </div>
        <div id="panel-providers" class="panel active">
          <div id="providersBody" class="providers-list"></div>
        </div>
        <div id="panel-routes" class="panel">
          <div id="routesBody" class="routes-list"></div>
        </div>
      </div>
    </div>
  </section>

  <div id="toast" class="toast"></div>

<script>
// ===== i18n System =====
const i18n = {
  en: {
    page_title: "apiproxy Performance Analytics",
    page_subtitle: "View model request volume, success rate, latency, tokens per second, and PP/TG speed across context lengths.",
    btn_config: "Config",
    btn_back: "Back to Dashboard",
    btn_logout: "Logout",
    btn_refresh: "Refresh",
    btn_save: "Save",
    btn_save_saving: "Saving...",
    btn_add_provider: "+ Add Provider",
    btn_add_route: "+ Add Route",
    btn_delete: "Delete",
    label_range: "Time Range",
    range_1h: "Last 1 hour",
    range_6h: "Last 6 hours",
    range_24h: "Last 24 hours",
    range_7d: "Last 7 days",
    range_30d: "Last 30 days",
    label_interval: "Granularity",
    gran_minute: "Minute",
    gran_hour: "Hour",
    gran_day: "Day",
    filter_all: "All",
    no_data: "No data matches current filter",
    no_providers: "No providers yet — click Add to create one",
    label_provider_name: "Name",
    label_base_url: "Base URL",
    label_api_key: "API Key",
    label_api_key_env: "API Key Env",
    label_auth_header: "Auth Header",
    label_timeout: "Timeout",
    label_tier: "Tier",
    label_providers: "Providers",
    no_routes: "No routes yet — click Add to create one",
    label_providers_ordered: "Providers (tried in order)",
    btn_add_provider_target: "+ Add provider target",
    label_fallback_policy: "Fallback Policy",
    label_allow_downgrade: "Allow downgrade",
    tooltip_drag_to_reorder: "Drag to reorder",
    tooltip_move_up: "Move up",
    tooltip_move_down: "Move down",
    summary_strategy: "strategy",
    summary_providers_one: "provider",
    summary_providers_many: "providers",
    title_summary: "Model Performance Summary",
    title_tokens: "Model Token Totals",
    title_latency: "Latency Trend",
    title_tg_speed: "Generation Speed Trend (TG)",
    title_pp_by_ctx: "PP Speed by Context Length",
    title_tg_by_ctx: "TG Speed by Context Length",
    title_config: "Config Management",
    col_requests: "Requests",
    col_errors: "Errors",
    col_successrate: "Success Rate",
    col_avglatency: "Avg Latency",
    col_prompt: "Input Prompt",
    col_completion: "Output Completion",
    col_total: "Total",
    col_inratio: "Input %",
    col_outratio: "Output %",
    legend_prev: "Previous",
    legend_next: "Next",
    legend_hide: "Click to hide",
    legend_show: "Click to show",
    toast_saved: "Config saved and reloaded",
    toast_save_failed: "Save failed: ",
    toast_load_failed: "Failed to load config: ",
    confirm_delete_provider: "Delete provider \"",
    confirm_delete_route: "Delete route \"",
    confirm_end: "\"?",
    label_chart_token_input: " input",
    label_chart_token_output: " output",
  },
  zh: {
    page_title: "apiproxy 性能分析",
    page_subtitle: "查看模型请求量、成功率、延迟、每秒 token、上下文长度下 PP/TG 速度。",
    btn_config: "配置",
    btn_back: "返回仪表板",
    btn_logout: "退出",
    btn_refresh: "刷新",
    btn_save: "保存",
    btn_save_saving: "保存中...",
    btn_add_provider: "+ 新增 Provider",
    btn_add_route: "+ 新增 Route",
    btn_delete: "删除",
    label_range: "时间范围",
    range_1h: "最近 1 小时",
    range_6h: "最近 6 小时",
    range_24h: "最近 24 小时",
    range_7d: "最近 7 天",
    range_30d: "最近 30 天",
    label_interval: "粒度",
    gran_minute: "分钟",
    gran_hour: "小时",
    gran_day: "天",
    filter_all: "全部",
    no_data: "当前筛选范围内没有数据",
    no_providers: "暂无 provider，点右上角新增",
    label_provider_name: "名称",
    label_base_url: "Base URL",
    label_api_key: "API Key",
    label_api_key_env: "API Key Env",
    label_auth_header: "鉴权头",
    label_timeout: "超时",
    label_tier: "Tier",
    label_providers: "Providers",
    no_routes: "暂无 route，点右上角新增",
    label_providers_ordered: "Providers（按顺序尝试）",
    btn_add_provider_target: "+ 新增 provider 目标",
    label_fallback_policy: "Fallback 策略",
    label_allow_downgrade: "允许降级",
    tooltip_drag_to_reorder: "拖拽排序",
    tooltip_move_up: "上移",
    tooltip_move_down: "下移",
    summary_strategy: "策略",
    summary_providers_one: "个 provider",
    summary_providers_many: "个 provider",
    title_summary: "模型性能汇总",
    title_tokens: "模型 Token 总量",
    title_latency: "延迟趋势",
    title_tg_speed: "生成速度趋势（TG）",
    title_pp_by_ctx: "不同上下文长度 PP 速度",
    title_tg_by_ctx: "不同上下文长度 TG 速度",
    title_config: "配置管理",
    col_requests: "请求",
    col_errors: "错误",
    col_successrate: "成功率",
    col_avglatency: "平均延迟",
    col_prompt: "输入 Prompt",
    col_completion: "输出 Completion",
    col_total: "合计",
    col_inratio: "输入占比",
    col_outratio: "输出占比",
    legend_prev: "上一组",
    legend_next: "下一组",
    legend_hide: "点击隐藏",
    legend_show: "点击显示",
    toast_saved: "配置已保存并热加载",
    toast_save_failed: "保存失败: ",
    toast_load_failed: "加载配置失败: ",
    confirm_delete_provider: "确认删除 provider \"",
    confirm_delete_route: "确认删除 route \"",
    confirm_end: "\"？",
    label_chart_token_input: " 输入",
    label_chart_token_output: " 输出",
  }
};

let currentLang = localStorage.getItem("apiproxy_lang") || (navigator.language.startsWith("zh") ? "zh" : "en");

function applyLang() {
  const lang = i18n[currentLang];

  function t(key) { return lang[key] || i18n.en[key] || key; }

  document.documentElement.lang = currentLang === "zh" ? "zh-CN" : "en";

  document.getElementById("langBtn").textContent = currentLang === "zh" ? "English" : "中文";
  document.getElementById("page-title").textContent = t("page_title");
  document.getElementById("page-subtitle").textContent = t("page_subtitle");
  document.getElementById("configBtn").textContent = t("btn_config");
  document.getElementById("backBtn").textContent = t("btn_back");
  document.getElementById("logoutBtn").textContent = t("btn_logout");
  document.getElementById("refresh").textContent = t("btn_refresh");
  document.getElementById("saveConfigBtn").textContent = t("btn_save");
  document.getElementById("addProviderBtn").textContent = t("btn_add_provider");
  document.getElementById("addRouteBtn").textContent = t("btn_add_route");
  document.getElementById("label-range").textContent = t("label_range");
  document.getElementById("label-interval").textContent = t("label_interval");

  document.querySelectorAll("[data-i18n]").forEach(function(el) {
    const key = el.getAttribute("data-i18n");
    if (key) el.textContent = t(key);
  });

  // Store current language in cookie for server side
  document.cookie = "lang=" + currentLang + "; path=/; max-age=" + (365*24*60*60);
}

document.getElementById("langBtn").addEventListener("click", function() {
  currentLang = currentLang === "en" ? "zh" : "en";
  localStorage.setItem("apiproxy_lang", currentLang);
  applyLang();
});

applyLang();

// ===== Dashboard Logic =====
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
  var t = i18n[currentLang];
  el.innerHTML = '<option value="">' + (t.filter_all || "All") + '</option>';
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
    var t = i18n[currentLang];
    body.innerHTML = '<tr><td colspan="14" class="muted">' + (t.no_data || "No data matches current filter") + '</td></tr>';
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
  var st = legendState[id];
  if (st && st.hidden) {
    for (var i = 0; i < charts[id].data.datasets.length; i++) {
      charts[id].getDatasetMeta(i).hidden = !!st.hidden[i];
    }
    charts[id].update();
  }
  paginateLegend(id);
}

function multiSeriesOpts() {
  return {
    responsive: true,
    maintainAspectRatio: false,
    plugins: { legend: { display: false } }
  };
}

var CHART_COLORS = ["#315efb", "#12a87c", "#f59e0b", "#c026d3", "#ef4444", "#0891b2"];

var legendState = {};
var LEGEND_PAGE_SIZE = 3;

function paginateLegend(chartId) {
  var chart = charts[chartId];
  var host = eid(chartId + "Legend");
  if (!chart || !host) return;
  var st = legendState[chartId] || { hidden: {}, start: 0 };
  legendState[chartId] = st;
  var t = i18n[currentLang];

  var datasets = chart.data.datasets;
  var total = datasets.length;
  var maxStart = Math.max(0, total - LEGEND_PAGE_SIZE);
  if (st.start > maxStart) st.start = maxStart;
  if (st.start < 0) st.start = 0;
  var end = Math.min(total, st.start + LEGEND_PAGE_SIZE);

  host.innerHTML = "";
  if (total === 0) return;

  if (st.start > 0) {
    var prev = document.createElement("span");
    prev.className = "legend-arrow";
    prev.textContent = "‹";
    prev.title = t.legend_prev || "Previous";
    prev.addEventListener("click", function() { st.start = Math.max(0, st.start - LEGEND_PAGE_SIZE); paginateLegend(chartId); });
    host.appendChild(prev);
  }

  var wrap = document.createElement("div");
  wrap.className = "legend-items";
  for (var i = st.start; i < end; i++) {
    (function(idx) {
      var meta = chart.getDatasetMeta(idx);
      var hidden = !!st.hidden[idx] || meta.hidden;
      var color = datasets[idx].borderColor || datasets[idx].backgroundColor;
      var chip = document.createElement("span");
      chip.className = "legend-item" + (hidden ? " hidden" : "");
      chip.style.borderLeft = "4px solid " + color;
      chip.textContent = datasets[idx].label;
      chip.title = hidden ? (t.legend_show || "Click to show") : (t.legend_hide || "Click to hide");
      chip.addEventListener("click", function() {
        st.hidden[idx] = !hidden;
        meta.hidden = !hidden;
        chart.update();
        paginateLegend(chartId);
      });
      wrap.appendChild(chip);
    })(i);
  }
  host.appendChild(wrap);

  if (end < total) {
    var next = document.createElement("span");
    next.className = "legend-arrow";
    next.textContent = "›";
    next.title = t.legend_next || "Next";
    next.addEventListener("click", function() { st.start = Math.min(maxStart, st.start + LEGEND_PAGE_SIZE); paginateLegend(chartId); });
    host.appendChild(next);
  }
}

function toLocalISO(ts) {
  var d = new Date(ts + "Z");
  if (isNaN(d.getTime())) return ts;
  var pad = function(n) { return n < 10 ? "0" + n : "" + n; };
  return d.getFullYear() + "-" + pad(d.getMonth()+1) + "-" + pad(d.getDate()) + "T" + pad(d.getHours()) + ":" + pad(d.getMinutes());
}

function renderTimeseries(rows) {
  var tsSet = new Map();
  for (var i = 0; i < rows.length; i++) {
    tsSet.set(rows[i].ts, true);
  }
  var allTs = Array.from(tsSet.keys()).sort();

  var grouped = new Map();
  for (var i = 0; i < rows.length; i++) {
    var r = rows[i];
    var key = r.provider + "/" + r.model;
    if (!grouped.has(key)) grouped.set(key, new Map());
    grouped.get(key).set(r.ts, r);
  }

  var labels = allTs.map(function(ts) { return toLocalISO(ts); });

  var latDatasets = [];
  var idx = 0;
  grouped.forEach(function(byTs, name) {
    latDatasets.push({
      label: name,
      data: allTs.map(function(ts) { var x = byTs.get(ts); return x ? x.avg_latency_ms : null; }),
      borderColor: CHART_COLORS[idx % CHART_COLORS.length],
      tension: 0.25
    });
    idx++;
  });
  upsertChart("latencyChart", {
    type: "line",
    data: { labels: labels, datasets: latDatasets },
    options: multiSeriesOpts()
  });

  var tpsDatasets = [];
  idx = 0;
  grouped.forEach(function(byTs, name) {
    tpsDatasets.push({
      label: name,
      data: allTs.map(function(ts) { var x = byTs.get(ts); return x ? x.tokens_per_sec : null; }),
      borderColor: CHART_COLORS[idx % CHART_COLORS.length],
      tension: 0.25
    });
    idx++;
  });
  upsertChart("tpsChart", {
    type: "line",
    data: { labels: labels, datasets: tpsDatasets },
    options: multiSeriesOpts()
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
  var bucketMap = new Map();
  for (var i = 0; i < (rows || []).length; i++) {
    var r = rows[i];
    if (!bucketMap.has(r.bucket)) bucketMap.set(r.bucket, Number(r.bucket_min) || 0);
  }
  var labels = Array.from(bucketMap.entries()).sort(function(a, b) { return a[1] - b[1]; }).map(function(x) { return x[0]; });

  var ppDatasets = [];
  var tgDatasets = [];
  var idx = 0;
  grouped.forEach(function(items, name) {
    var byBucket = new Map(items.map(function(x) { return [x.bucket, x]; }));
    var color = CHART_COLORS[idx % CHART_COLORS.length];
    ppDatasets.push({ label: name, data: labels.map(function(b) { var x = byBucket.get(b); return x ? x.pp_rate : 0; }), borderColor: color, backgroundColor: color, tension: 0.2 });
    tgDatasets.push({ label: name, data: labels.map(function(b) { var x = byBucket.get(b); return x ? x.tg_rate : 0; }), borderColor: color, backgroundColor: color, tension: 0.2 });
    idx++;
  });
  upsertChart("ppChart", { type: "line", data: { labels: labels, datasets: ppDatasets }, options: multiSeriesOpts() });
  upsertChart("tgChart", { type: "line", data: { labels: labels, datasets: tgDatasets }, options: multiSeriesOpts() });
}

function fmtTokens(n) {
  if (n === null || n === undefined) return "-";
  n = Number(n);
  if (n >= 1e6) return (n / 1e6).toFixed(2) + "M";
  if (n >= 1e3) return (n / 1e3).toFixed(1) + "K";
  return String(n);
}

function renderTokens(rows) {
  var map = new Map();
  for (var i = 0; i < (rows || []).length; i++) {
    var r = rows[i];
    var key = (r.provider || "") + "/" + (r.model || "");
    var cur = map.get(key) || { model: r.model, prompt: 0, completion: 0 };
    cur.prompt += Number(r.prompt_tokens) || 0;
    cur.completion += Number(r.completion_tokens) || 0;
    map.set(key, cur);
  }
  var items = Array.from(map.values());
  var t = i18n[currentLang];

  var body = eid("tokensBody");
  body.innerHTML = "";
  if (items.length === 0) {
    body.innerHTML = '<tr><td colspan="6" class="muted">' + (t.no_data || "No data matches current filter") + '</td></tr>';
    return;
  }
  for (var i = 0; i < items.length; i++) {
    var m = items[i];
    var rowTotal = m.prompt + m.completion;
    var pPct = rowTotal > 0 ? (m.prompt / rowTotal * 100).toFixed(1) + "%" : "-";
    var cPct = rowTotal > 0 ? (m.completion / rowTotal * 100).toFixed(1) + "%" : "-";
    var tr = document.createElement("tr");
    tr.innerHTML = td(m.model) + td(fmtTokens(m.prompt)) + td(fmtTokens(m.completion)) + td(fmtTokens(rowTotal)) + td(pPct) + td(cPct);
    body.appendChild(tr);
  }

  var modelNames = items.map(function(m) { return m.model; });
  var catLabels = [];
  var inputLabel = t.label_chart_token_input || " input";
  var outputLabel = t.label_chart_token_output || " output";
  items.forEach(function(m) {
    catLabels.push(m.model + inputLabel);
    catLabels.push(m.model + outputLabel);
  });
  var tokenDatasets = [];
  for (var i = 0; i < items.length; i++) {
    (function(i) {
      var m = items[i];
      var color = CHART_COLORS[i % CHART_COLORS.length];
      var promptIdx = i * 2;
      var completionIdx = i * 2 + 1;
      var data = new Array(catLabels.length).fill(0);
      data[promptIdx] = m.prompt;
      data[completionIdx] = m.completion;
      tokenDatasets.push({
        label: m.model,
        data: data,
        backgroundColor: color,
        hidden: false
      });
    })(i);
  }
  upsertChart("tokensChart", {
    type: "bar",
    data: { labels: catLabels, datasets: tokenDatasets },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      plugins: { legend: { display: false } },
      scales: { x: { stacked: false }, y: { stacked: false, beginAtZero: true } }
    }
  });
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
    renderTokens(results[0] || []);
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
    if (typeof k2 === "string" || typeof k2 === "number" || typeof k2 === "boolean") k2 = document.createTextNode(String(k2));
    n.appendChild(k2);
  }
  return n;
}

function toast(cls, msg) {
  var t = eid("toast");
  t.className = "toast " + cls;
  t.textContent = msg;
  void t.offsetWidth;
  t.classList.add("show");
  setTimeout(function() { t.classList.remove("show"); }, 2400);
}

function loadConfig() {
  var t = i18n[currentLang];
  return fetch("/api/config").then(function(res) {
    if (!res.ok) return res.text().then(function(txt) { throw new Error(txt); });
    return res.json();
  }).then(function(data) {
    cfgState.providers = Array.isArray(data.providers) ? data.providers : [];
    cfgState.routes = Array.isArray(data.routes) ? data.routes : [];
    cfgState.providerNames = cfgState.providers.map(function(p) { return p.name; }).sort();
    renderProviders();
    renderRoutes();
  }).catch(function(err) {
    toast("err", (t.toast_load_failed || "Failed to load config: ") + (err.message || err));
  });
}

function renderProviders() {
  var t = i18n[currentLang];
  var body = eid("providersBody");
  body.innerHTML = "";
  for (var i = 0; i < cfgState.providers.length; i++) {
    body.appendChild(providerConfigCard(cfgState.providers[i], i));
  }
  if (cfgState.providers.length === 0) {
    body.appendChild(el("div", {class: "muted", style: "padding: 20px 0; text-align: center"}, [t.no_providers || "No providers yet — click Add to create one"]));
  }
}

function providerConfigCard(p, idx) {
  var t = i18n[currentLang];

  // Single-column form row: label (right-aligned) + input
  function formRow(labelText, control) {
    return el("div", {class: "provider-form-row"}, [
      el("span", {class: "provider-form-label"}, [labelText]),
      el("div", {class: "provider-form-input"}, [control])
    ]);
  }

  var nameInput = el("input", {type: "text", value: p.name || "", placeholder: t.label_provider_name || "Name"});
  nameInput.addEventListener("input", function() {
    cfgState.providers[idx].name = nameInput.value.trim();
    cfgState.providerNames = cfgState.providers.map(function(x) { return x.name; }).filter(Boolean).sort();
    refreshRouteProviderOptions();
  });

  var urlInput = el("input", {type: "text", value: p.base_url || "", placeholder: "https://..."});
  urlInput.addEventListener("input", function() { cfgState.providers[idx].base_url = urlInput.value; });

  // API Key with eye toggle
  var keyInput = el("input", {type: "password", value: p.api_key || "", placeholder: p.api_key ? MASKED : "(empty)"});
  keyInput.addEventListener("input", function() { cfgState.providers[idx].api_key = keyInput.value; });
  var keyWrapper = el("div", {class: "api-key-wrapper"}, [keyInput]);
  var keyToggle = el("button", {class: "api-key-toggle", title: "Toggle visibility"}, ["👁"]);
  keyToggle.addEventListener("click", function(e) {
    e.preventDefault();
    if (keyInput.type === "password") {
      keyInput.type = "text";
      if (cfgState.providers[idx].api_key) keyInput.value = cfgState.providers[idx].api_key;
    } else {
      keyInput.type = "password";
      if (cfgState.providers[idx].api_key) keyInput.value = MASKED;
    }
  });
  keyWrapper.appendChild(keyToggle);

  var keyEnvInput = el("input", {type: "text", value: p.api_key_env || "", placeholder: "ENV var name (optional)"});
  keyEnvInput.addEventListener("input", function() { cfgState.providers[idx].api_key_env = keyEnvInput.value; });

  var authSel = el("select", null, [
    el("option", {value: "both"}, ["both (recommended)"]),
    el("option", {value: "authorization"}, ["authorization"]),
    el("option", {value: "x-api-key"}, ["x-api-key"])
  ]);
  authSel.value = p.auth_header || "both";
  authSel.addEventListener("change", function() { cfgState.providers[idx].auth_header = authSel.value; });

  var timeoutInput = el("input", {type: "text", value: p.timeout || "60s"});
  timeoutInput.addEventListener("input", function() { cfgState.providers[idx].timeout = timeoutInput.value; });

  var tierSel = el("select", null, [
    el("option", {value: ""}, ["-"]),
    el("option", {value: "advanced"}, ["advanced"]),
    el("option", {value: "standard"}, ["standard"])
  ]);
  tierSel.value = p.tier || "";
  tierSel.addEventListener("change", function() { cfgState.providers[idx].tier = tierSel.value; });

  // Duplicate button
  var dupBtn = el("button", {class: "provider-duplicate-btn icon-btn", title: "Duplicate"}, ["⧉"]);
  dupBtn.addEventListener("click", function() {
    var copy = {
      name: p.name + "-copy",
      base_url: p.base_url,
      api_key: p.api_key,
      api_key_env: p.api_key_env,
      auth_header: p.auth_header,
      timeout: p.timeout,
      tier: p.tier
    };
    cfgState.providers.splice(idx + 1, 0, copy);
    cfgState.providerNames = cfgState.providers.map(function(x) { return x.name; }).filter(Boolean).sort();
    renderProviders();
    refreshRouteProviderOptions();
  });

  // Delete button
  var delBtn = el("button", {class: "icon-btn", title: t.btn_delete || "Delete"}, [t.btn_delete || "Delete"]);
  delBtn.addEventListener("click", function() {
    if (!confirm((t.confirm_delete_provider || "Delete provider \"") + (p.name || "") + (t.confirm_end || "\"?"))) return;
    cfgState.providers.splice(idx, 1);
    cfgState.providerNames = cfgState.providers.map(function(x) { return x.name; }).filter(Boolean).sort();
    renderProviders();
    refreshRouteProviderOptions();
  });

  // Header with name display + actions
  var head = el("div", {class: "provider-config-head"}, [
    el("div", {class: "provider-config-title"}, [
      el("span", {class: "provider-icon"}, ["⚡"]),
      el("span", {class: "provider-name-display"}, [p.name || (t.label_provider_name || "Name")])
    ]),
    el("div", {class: "provider-config-actions"}, [dupBtn, delBtn])
  ]);

  // Color bar + body wrapper
  var colorIdx = idx % CHART_COLORS.length;
  var colorBar = el("div", {class: "provider-color-bar", style: "background:" + CHART_COLORS[colorIdx]});
  var body = el("div", {class: "provider-config-body"}, [
    head,
    formRow(t.label_provider_name || "Name", nameInput),
    formRow(t.label_base_url || "Base URL", urlInput),
    formRow(t.label_api_key || "API Key", keyWrapper),
    formRow(t.label_api_key_env || "API Key Env", keyEnvInput),
    formRow(t.label_auth_header || "Auth Header", authSel),
    formRow(t.label_timeout || "Timeout", timeoutInput),
    formRow(t.label_tier || "Tier", tierSel)
  ]);

  return el("div", {class: "provider-config-card"}, [colorBar, body]);
}

function addProvider() {
  cfgState.providers.push({
    name: "provider-" + (cfgState.providers.length + 1),
    base_url: "",
    api_key: "",
    api_key_env: "",
    auth_header: "both",
    timeout: "60s",
    tier: ""
  });
  switchTab("providers");
  renderProviders();
  refreshRouteProviderOptions();
}

function renderRoutes() {
  var t = i18n[currentLang];
  var body = eid("routesBody");
  body.innerHTML = "";
  for (var i = 0; i < cfgState.routes.length; i++) {
    body.appendChild(routeCard(cfgState.routes[i], i));
  }
  if (cfgState.routes.length === 0) {
    body.appendChild(el("div", {class: "muted", style: "padding: 20px 0; text-align: center"}, [t.no_routes || "No routes yet — click Add to create one"]));
  }
}

function strategyOptions(sel, current) {
  ["priority", "weighted", "random"].forEach(function(s) {
    var o = el("option", {value: s}, [s]);
    if (s === current) o.selected = true;
    sel.appendChild(o);
  });
}

function routeSummary(r) {
  var t = i18n[currentLang];
  var n = r.providers ? r.providers.length : 0;
  var unit = n === 1 ? (t.summary_providers_one || "provider") : (t.summary_providers_many || "providers");
  return (r.strategy || "priority") + " • " + n + " " + unit;
}

function routeCard(r, idx) {
  var t = i18n[currentLang];
  if (!r) r = {};
  if (!Array.isArray(r.providers)) r.providers = [];
  if (!r.fallback) r.fallback = {};

  // Color the route header background
  var routeColor = CHART_COLORS[idx % CHART_COLORS.length];
  var card = el("div", {class: "route-card"});
  card.dataset.routeIdx = String(idx);
  var content = el("div", {class: "route-card-content"});

  // --- Header (collapsible) ---
  var nameInput = el("input", {type: "text", value: r.name || "", style: "width:170px; font-weight:600; background:transparent; border:1px solid transparent; outline:none; padding:2px 6px; border-radius:6px"});
  nameInput.addEventListener("focus", function() { nameInput.style.borderColor = "var(--border)"; });
  nameInput.addEventListener("blur", function() { nameInput.style.borderColor = "transparent"; });
  nameInput.addEventListener("click", function(e) { e.stopPropagation(); });
  nameInput.addEventListener("input", function() {
    cfgState.routes[idx].name = nameInput.value.trim();
    var summ = header.querySelector(".route-summary");
    if (summ) summ.textContent = routeSummary(cfgState.routes[idx]);
  });

  var stratSel = el("select", {style: "font-size:12px; padding:2px 6px; width:auto; border-color:var(--border)"}, []);
  strategyOptions(stratSel, r.strategy || "priority");
  stratSel.addEventListener("change", function() {
    cfgState.routes[idx].strategy = stratSel.value;
    var summ = header.querySelector(".route-summary");
    if (summ) summ.textContent = routeSummary(cfgState.routes[idx]);
  });
  stratSel.addEventListener("click", function(e) { e.stopPropagation(); });

  var summary = el("span", {class: "route-summary"}, [routeSummary(r)]);
  var toggle = el("span", {class: "route-toggle"}, ["▼"]);

  var delBtn = el("button", {class: "icon-btn", title: t.btn_delete || "Delete route"}, [t.btn_delete || "Delete"]);
  delBtn.addEventListener("click", function(e) {
    e.stopPropagation();
    if (!confirm((t.confirm_delete_route || "Delete route \"") + (r.name || "") + (t.confirm_end || "\"?"))) return;
    cfgState.routes.splice(idx, 1);
    renderRoutes();
  });

  var titleLeft = el("div", {class: "route-card-title"}, [toggle, nameInput, stratSel, summary]);
  var header = el("div", {class: "route-card-header"}, [
    titleLeft,
    el("div", {class: "route-card-header-actions"}, [delBtn])
  ]);
  // Apply color tint to header
  var rgb = hexToRgb(routeColor);
  header.style.background = "rgba(" + rgb.r + "," + rgb.g + "," + rgb.b + ", 0.06)";
  header.addEventListener("click", function() {
    card.classList.toggle("collapsed");
  });

  // --- Providers section ---
  var providerList = el("div", {class: "provider-list"}, []);
  var providerLabel = el("div", {style: "font-size:12px; color:var(--muted); margin-bottom:8px; font-weight:600"}, [t.label_providers_ordered || "Providers (tried in order)"]);

  function refreshProviders() {
    providerList.innerHTML = "";
    for (var j = 0; j < r.providers.length; j++) {
      providerList.appendChild(providerTargetCard(r, j, refreshProviders));
    }
    summary.textContent = routeSummary(r);
  }

  var addTargetBtn = el("button", {class: "icon-btn"}, [t.btn_add_provider_target || "+ Add provider target"]);
  addTargetBtn.addEventListener("click", function() {
    r.providers.push({ provider: "", model: "", tier: "", weight: 0 });
    refreshProviders();
  });

  // --- Fallback section ---
  var fbSection = el("div", {class: "fallback-section"}, []);
  buildFallback(fbSection, r);

  content.appendChild(providerLabel);
  content.appendChild(providerList);
  content.appendChild(addTargetBtn);
  content.appendChild(fbSection);

  card.appendChild(header);
  card.appendChild(content);
  refreshProviders();
  return card;
}

// providerTargetCard renders one reorderable provider target. onReorder is a
// closure that re-renders the parent route's provider list after any mutation.
function providerTargetCard(route, j, onReorder) {
  var t = i18n[currentLang];
  var target = route.providers[j];
  if (!target) return el("div");

  function field(labelText, control) {
    return el("div", {class: "field"}, [
      el("span", {class: "field-label"}, [labelText]),
      control
    ]);
  }

  var provSel = el("select", null, [el("option", {value: ""}, ["-"])]);
  cfgState.providerNames.forEach(function(n) {
    provSel.appendChild(el("option", {value: n}, [n]));
  });
  provSel.value = target.provider || "";
  provSel.addEventListener("change", function() { target.provider = provSel.value; });

  var modelInput = el("input", {type: "text", value: target.model || "", placeholder: "model name"});
  modelInput.addEventListener("input", function() { target.model = modelInput.value; });

  var tierSel = el("select", null, [
    el("option", {value: ""}, ["-"]),
    el("option", {value: "advanced"}, ["advanced"]),
    el("option", {value: "standard"}, ["standard"])
  ]);
  tierSel.value = target.tier || "";
  tierSel.addEventListener("change", function() { target.tier = tierSel.value; });

  var weightInput = el("input", {type: "number", value: String(target.weight || 0), placeholder: "weight"});
  weightInput.addEventListener("input", function() { target.weight = parseInt(weightInput.value || "0", 10); });

  // Order controls: up/down buttons + drag handle.
  var upBtn = el("button", {class: "sort-btn", title: t.tooltip_move_up || "Move up"}, ["↑"]);
  upBtn.addEventListener("click", function() { moveProvider(route, j, -1, onReorder); });
  var downBtn = el("button", {class: "sort-btn", title: t.tooltip_move_down || "Move down"}, ["↓"]);
  downBtn.addEventListener("click", function() { moveProvider(route, j, 1, onReorder); });

  var delBtn = el("button", {class: "provider-del-btn", title: t.btn_delete || "Delete"}, ["x"]);
  delBtn.addEventListener("click", function() {
    route.providers.splice(j, 1);
    onReorder();
  });

  var handle = el("span", {class: "drag-handle", title: t.tooltip_drag_to_reorder || "Drag to reorder"}, ["☰"]);

  var card = el("div", {class: "provider-card"}, []);
  card.dataset.idx = String(j);

  var fields = el("div", {class: "provider-fields"}, [
    field("Provider", provSel),
    field("Model", modelInput),
    field("Tier", tierSel),
    field("Weight", weightInput)
  ]);

  var actions = el("div", {class: "provider-actions"}, [upBtn, downBtn, delBtn]);

  card.appendChild(handle);
  card.appendChild(fields);
  card.appendChild(actions);

  // --- Drag & drop wiring (drag is enabled only while pressing the handle) ---
  handle.addEventListener("mousedown", function() { card.draggable = true; });
  handle.addEventListener("mouseup", function() { card.draggable = false; });

  card.addEventListener("dragstart", function(e) {
    dragState.route = route;
    dragState.fromIdx = j;
    dragState.onReorder = onReorder;
    card.classList.add("dragging");
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = "move";
      try { e.dataTransfer.setData("text/plain", String(j)); } catch (err) {}
    }
  });
  card.addEventListener("dragend", function() {
    card.draggable = false;
    card.classList.remove("dragging");
    var list = card.parentNode;
    if (list) {
      var siblings = list.querySelectorAll(".provider-card.drag-over");
      for (var i = 0; i < siblings.length; i++) siblings[i].classList.remove("drag-over");
    }
    dragState.route = null;
    dragState.fromIdx = -1;
    dragState.onReorder = null;
  });
  card.addEventListener("dragover", function(e) {
    if (dragState.route !== route) return;
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
    card.classList.add("drag-over");
  });
  card.addEventListener("dragleave", function() {
    card.classList.remove("drag-over");
  });
  card.addEventListener("drop", function(e) {
    e.preventDefault();
    card.classList.remove("drag-over");
    if (dragState.route !== route) return;
    var from = dragState.fromIdx;
    var to = j;
    if (from === to || from < 0) return;
    var moved = route.providers.splice(from, 1)[0];
    route.providers.splice(to, 0, moved);
    if (dragState.onReorder) dragState.onReorder();
  });

  return card;
}

// dragState holds the in-flight drag source for provider reordering.
var dragState = { route: null, fromIdx: -1, onReorder: null };

// moveProvider swaps a provider target with its neighbor in the given direction.
function moveProvider(route, j, dir, onReorder) {
  var target = j + dir;
  if (target < 0 || target >= route.providers.length) return;
  var tmp = route.providers[j];
  route.providers[j] = route.providers[target];
  route.providers[target] = tmp;
  onReorder();
}

function hexToRgb(hex) {
  var result = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex);
  return result ? {
    r: parseInt(result[1], 16),
    g: parseInt(result[2], 16),
    b: parseInt(result[3], 16)
  } : { r: 0, g: 0, b: 0 };
}

// buildFallback renders the fallback policy block inside host for route r.
function buildFallback(host, r) {
  var t = i18n[currentLang];
  if (!r.fallback) r.fallback = {};
  var fb = r.fallback;

  // Aligned form row: label (110px right-aligned) + input area
  function fbRow(labelText, control) {
    return el("div", {class: "provider-form-row"}, [
      el("span", {class: "provider-form-label"}, [labelText]),
      el("div", {class: "provider-form-input"}, [control])
    ]);
  }

  var fbCheck = el("input", {type: "checkbox", style: "width:auto;"});
  fbCheck.checked = !!fb.enabled;
  fbCheck.addEventListener("change", function() { fb.enabled = fbCheck.checked; });

  var maxInput = el("input", {type: "number", value: String(fb.max_attempts || 0), style: "width:80px;"});
  maxInput.addEventListener("input", function() { fb.max_attempts = parseInt(maxInput.value || "0", 10); });

  var toCheck = el("input", {type: "checkbox", style: "width:auto;"});
  toCheck.checked = !!fb.on_timeout;
  toCheck.addEventListener("change", function() { fb.on_timeout = toCheck.checked; });

  var connCheck = el("input", {type: "checkbox", style: "width:auto;"});
  connCheck.checked = !!fb.on_connect_error;
  connCheck.addEventListener("change", function() { fb.on_connect_error = connCheck.checked; });

  var downCheck = el("input", {type: "checkbox", style: "width:auto;"});
  downCheck.checked = !!fb.allow_downgrade;
  downCheck.addEventListener("change", function() { fb.allow_downgrade = downCheck.checked; });

  var statusInput = el("input", {type: "text", value: (fb.on_status || []).join(","), placeholder: "e.g. 429,500,503"});
  statusInput.addEventListener("input", function() {
    fb.on_status = statusInput.value.split(",").map(function(s) { return parseInt(s.trim(), 10); }).filter(function(n) { return !isNaN(n); });
  });

  host.appendChild(el("div", {style: "font-size:12px; color:var(--muted); font-weight:600; margin-bottom:10px;"}, [t.label_fallback_policy || "Fallback Policy"]));
  host.appendChild(fbRow("Enabled", fbCheck));
  host.appendChild(fbRow("Max Attempts", maxInput));
  host.appendChild(fbRow("On Timeout", toCheck));
  host.appendChild(fbRow("On Conn Error", connCheck));
  host.appendChild(fbRow("Allow Downgrade", downCheck));
  host.appendChild(fbRow("On Status", statusInput));
}

function refreshRouteProviderOptions() {
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
  switchTab("routes");
  renderRoutes();
}

function saveConfig() {
  var t = i18n[currentLang];
  var saveBtn = eid("saveConfigBtn");
  saveBtn.disabled = true;
  saveBtn.textContent = t.btn_save_saving || "Saving...";
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
    toast("ok", t.toast_saved || "Config saved and reloaded");
    loadConfig();
    refresh();
  }).catch(function(err) {
    toast("err", (t.toast_save_failed || "Save failed: ") + (err.message || err));
  }).finally(function() {
    saveBtn.disabled = false;
    saveBtn.textContent = t.btn_save || "Save";
  });
}

function showDashboard() {
  eid("dashboardView").classList.remove("view-hidden");
  eid("configView").classList.add("view-hidden");
  eid("configBtn").classList.remove("view-hidden");
  eid("backBtn").classList.add("view-hidden");
}

function showConfig() {
  eid("dashboardView").classList.add("view-hidden");
  eid("configView").classList.remove("view-hidden");
  eid("configBtn").classList.add("view-hidden");
  eid("backBtn").classList.remove("view-hidden");
  loadConfig();
}

function switchTab(name) {
  ["providers", "routes"].forEach(function(t) {
    eid("tab-" + t).classList.toggle("active", t === name);
    eid("panel-" + t).classList.toggle("active", t === name);
  });
}

eid("configBtn").addEventListener("click", showConfig);
eid("backBtn").addEventListener("click", showDashboard);
eid("saveConfigBtn").addEventListener("click", saveConfig);
eid("addProviderBtn").addEventListener("click", addProvider);
eid("addRouteBtn").addEventListener("click", addRoute);
document.querySelectorAll(".tab").forEach(function(t) {
  t.addEventListener("click", function() { switchTab(t.dataset.tab); });
});
</script>
</body>
</html>`
