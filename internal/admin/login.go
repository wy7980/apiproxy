package admin

// loginHTML is the login page template. The single %s is the (HTML-escaped)
// "next" path to redirect to after a successful login.
const loginHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>apiproxy 登录</title>
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
      min-height: 100vh;
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .login-card {
      background: var(--card);
      border: 1px solid var(--border);
      border-radius: 14px;
      box-shadow: 0 8px 24px rgba(20, 28, 45, 0.08);
      padding: 32px 36px;
      width: 360px;
      max-width: calc(100vw - 32px);
    }
    h1 { margin: 0 0 8px; font-size: 22px; }
    .subtitle { color: var(--muted); margin: 0 0 24px; font-size: 13px; }
    label { display: block; font-size: 12px; color: var(--muted); margin: 0 0 6px; }
    input[type="text"], input[type="password"] {
      width: 100%;
      border: 1px solid var(--border);
      border-radius: 9px;
      padding: 10px 12px;
      background: #fff;
      color: var(--text);
      font-size: 14px;
      margin-bottom: 16px;
    }
    input:focus { outline: none; border-color: var(--primary); }
    button {
      width: 100%;
      border: 1px solid var(--primary);
      border-radius: 9px;
      padding: 10px 12px;
      background: var(--primary);
      color: #fff;
      cursor: pointer;
      font-weight: 600;
      font-size: 14px;
    }
    button:hover { filter: brightness(1.05); }
    .err {
      color: var(--danger);
      background: rgba(202, 45, 45, 0.08);
      border: 1px solid rgba(202, 45, 45, 0.25);
      border-radius: 8px;
      padding: 8px 12px;
      font-size: 13px;
      margin-bottom: 16px;
      display: block;
    }
  </style>
</head>
<body>
  <form class="login-card" method="POST" action="/login">
    <h1>apiproxy 管理后台</h1>
    <p class="subtitle">请输入账号和密码以访问仪表板。</p>
    <label>账号</label>
    <input type="text" name="username" autofocus required autocomplete="username" />
    <label>密码</label>
    <input type="password" name="password" required autocomplete="current-password" />
    <input type="hidden" name="next" value="%s" />
    <button type="submit">登录</button>
  </form>
</body>
</html>`

// loginHTMLWithErr is the same as loginHTML but shows an error banner. It
// takes two %s: the escaped "next" path and the (already-safe) error message.
const loginHTMLWithErr = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>apiproxy 登录</title>
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
      min-height: 100vh;
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .login-card {
      background: var(--card);
      border: 1px solid var(--border);
      border-radius: 14px;
      box-shadow: 0 8px 24px rgba(20, 28, 45, 0.08);
      padding: 32px 36px;
      width: 360px;
      max-width: calc(100vw - 32px);
    }
    h1 { margin: 0 0 8px; font-size: 22px; }
    .subtitle { color: var(--muted); margin: 0 0 24px; font-size: 13px; }
    label { display: block; font-size: 12px; color: var(--muted); margin: 0 0 6px; }
    input[type="text"], input[type="password"] {
      width: 100%;
      border: 1px solid var(--border);
      border-radius: 9px;
      padding: 10px 12px;
      background: #fff;
      color: var(--text);
      font-size: 14px;
      margin-bottom: 16px;
    }
    input:focus { outline: none; border-color: var(--primary); }
    button {
      width: 100%;
      border: 1px solid var(--primary);
      border-radius: 9px;
      padding: 10px 12px;
      background: var(--primary);
      color: #fff;
      cursor: pointer;
      font-weight: 600;
      font-size: 14px;
    }
    button:hover { filter: brightness(1.05); }
    .err {
      color: var(--danger);
      background: rgba(202, 45, 45, 0.08);
      border: 1px solid rgba(202, 45, 45, 0.25);
      border-radius: 8px;
      padding: 8px 12px;
      font-size: 13px;
      margin-bottom: 16px;
      display: block;
    }
  </style>
</head>
<body>
  <form class="login-card" method="POST" action="/login">
    <h1>apiproxy 管理后台</h1>
    <p class="subtitle">请输入账号和密码以访问仪表板。</p>
    <span class="err">%s</span>
    <label>账号</label>
    <input type="text" name="username" autofocus required autocomplete="username" />
    <label>密码</label>
    <input type="password" name="password" required autocomplete="current-password" />
    <input type="hidden" name="next" value="%s" />
    <button type="submit">登录</button>
  </form>
</body>
</html>`
