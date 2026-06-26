package admin

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"log/slog"

	"github.com/wangyong/apiproxy/internal/config"
	"github.com/wangyong/apiproxy/internal/storage"
)

func setupServer(t *testing.T) (*Server, *storage.Store) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "admin.db")
	store, err := storage.Open(path, 0)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}

	ctx := context.Background()
	now := time.Now().UTC()
	events := []storage.Event{
		{Timestamp: now.Add(-30 * time.Second), RequestID: "r1", Provider: "openai", Model: "gpt-4o-mini", Route: "chat", StatusCode: 200, LatencyMs: 100, FirstTokenMs: 30, PromptTokens: 500, CompletionTokens: 100, TotalTokens: 600, Stream: true},
		{Timestamp: now.Add(-20 * time.Second), RequestID: "r2", Provider: "deepseek", Model: "deepseek-chat", Route: "chat", StatusCode: 200, LatencyMs: 80, FirstTokenMs: 20, PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200, Stream: true},
		{Timestamp: now.Add(-10 * time.Second), RequestID: "r3", Provider: "openai", Model: "gpt-4o-mini", Route: "chat", StatusCode: 500, LatencyMs: 200, ErrorType: "server_error", Stream: false},
	}
	for _, e := range events {
		if err := store.Record(ctx, e); err != nil {
			t.Fatalf("store.Record: %v", err)
		}
	}

	return New(store, slog.Default(), "", nil, "admin", "test-pass"), store
}

// loginCookie performs a POST /login with valid credentials and returns the
// Set-Cookie value sent back by the server, for use as the Cookie header in
// subsequent requests.
func loginCookie(t *testing.T, srv *Server) string {
	t.Helper()
	form := strings.NewReader("username=admin&password=test-pass")
	req := httptest.NewRequest(http.MethodPost, "/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d, want 303", w.Code)
	}
	return w.Result().Header.Get("Set-Cookie")
}

func TestIndexHTML(t *testing.T) {
	srv, store := setupServer(t)
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cookie", loginCookie(t, srv))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if len(body) == 0 {
		t.Fatal("empty body")
	}
}

func TestIndexNotFound(t *testing.T) {
	srv, store := setupServer(t)
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/random/path", nil)
	req.Header.Set("Cookie", loginCookie(t, srv))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestSummary(t *testing.T) {
	srv, store := setupServer(t)
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/summary?start=-1h", nil)
	req.Header.Set("Cookie", loginCookie(t, srv))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var rows []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no summary rows")
	}
}

func TestTimeseries(t *testing.T) {
	srv, store := setupServer(t)
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/timeseries?start=-1h&interval=minute", nil)
	req.Header.Set("Cookie", loginCookie(t, srv))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var rows []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Each row must carry provider/model grouping and the server-side speed field.
	for _, r := range rows {
		if _, ok := r["provider"]; !ok {
			t.Fatalf("row missing provider field: %v", r)
		}
		if _, ok := r["model"]; !ok {
			t.Fatalf("row missing model field: %v", r)
		}
		if _, ok := r["tokens_per_sec"]; !ok {
			t.Fatalf("row missing tokens_per_sec field: %v", r)
		}
	}
}

func TestBuckets(t *testing.T) {
	srv, store := setupServer(t)
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/buckets?start=-1h", nil)
	req.Header.Set("Cookie", loginCookie(t, srv))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestFilters(t *testing.T) {
	srv, store := setupServer(t)
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/filters", nil)
	req.Header.Set("Cookie", loginCookie(t, srv))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Providers []string `json:"providers"`
		Models    []string `json:"models"`
		Routes    []string `json:"routes"`
		Clients   []string `json:"client_ids"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Providers) != 2 {
		t.Fatalf("providers = %v, want 2", resp.Providers)
	}
}

func TestParseTimeRFC3339(t *testing.T) {
	tm, err := parseTime("2026-01-02T15:04:05Z")
	if err != nil {
		t.Fatalf("parseTime: %v", err)
	}
	if tm.Year() != 2026 {
		t.Fatalf("year = %d", tm.Year())
	}
}

func TestParseTimeRelative(t *testing.T) {
	before := time.Now()
	tm, err := parseTime("-1h")
	if err != nil {
		t.Fatalf("parseTime: %v", err)
	}
	if !tm.Before(before) {
		t.Fatalf("relative time should be in the past")
	}
}

func TestParseTimeRelativeDays(t *testing.T) {
	before := time.Now()
	tm, err := parseTime("-7d")
	if err != nil {
		t.Fatalf("parseTime: %v", err)
	}
	if !tm.Before(before.Add(-6 * 24 * time.Hour)) {
		t.Fatalf("7d should be ~7 days before now")
	}
}

func TestParseTimeInvalid(t *testing.T) {
	_, err := parseTime("not-a-time")
	if err == nil {
		t.Fatal("expected error for invalid time")
	}
}

// mockReloader implements admin.Reloader for testing config API.
type mockReloader struct {
	mu  sync.Mutex
	cfg *config.Config
	err error // error to return on next Reload call
}

func (m *mockReloader) Reload(cfg *config.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		err := m.err
		m.err = nil
		return err
	}
	m.cfg = cfg
	return nil
}

func (m *mockReloader) CurrentConfig() *config.Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

func testReloaderConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{Listen: ":0"},
		Providers: map[string]config.Provider{
			"openai":   {BaseURL: "https://api.openai.com/v1", APIKey: "sk-real-key-123", Timeout: 60 * time.Second},
			"deepseek": {BaseURL: "https://api.deepseek.com/v1", APIKeyEnv: "DEEPSEEK_KEY", Timeout: 60 * time.Second},
		},
		Routes: map[string]config.Route{
			"chat": {
				Strategy:  "priority",
				Fallback:  config.FallbackConfig{Enabled: true, MaxAttempts: 2, OnStatus: []int{429, 500, 502, 503}},
				Providers: []config.RouteTarget{{Provider: "openai", Model: "gpt-4o-mini"}, {Provider: "deepseek", Model: "deepseek-chat"}},
			},
		},
	}
}

func setupConfigServer(t *testing.T) (*Server, *mockReloader, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "admin.db")
	store, err := storage.Open(path, 0)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	cfgPath := filepath.Join(dir, "apiproxy.yaml")
	// Write a minimal valid YAML so the config PUT can round-trip.
	yamlContent := []byte("server:\n  listen: ':0'\nproviders:\n  openai:\n    type: openai\n    base_url: https://api.openai.com/v1\n    api_key: sk-real-key-123\n    timeout: 60s\nroutes:\n  chat:\n    strategy: priority\n    providers:\n      - provider: openai\n        model: gpt-4o-mini\n")
	if err := os.WriteFile(cfgPath, yamlContent, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	reloader := &mockReloader{cfg: testReloaderConfig()}
	srv := New(store, slog.Default(), cfgPath, reloader, "admin", "test-pass")
	return srv, reloader, cfgPath
}

func TestGETConfig_MasksKeys(t *testing.T) {
	srv, _, _ := setupConfigServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.Header.Set("Cookie", loginCookie(t, srv))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp configResponseJSON
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Provider with inline key should be masked.
	for _, p := range resp.Providers {
		if p.Name == "openai" {
			if p.APIKey != maskedAPIKey {
				t.Fatalf("openai api_key = %q, want %q", p.APIKey, maskedAPIKey)
			}
		}
	}

	// Provider with env-var key (no inline) should show empty.
	for _, p := range resp.Providers {
		if p.Name == "deepseek" {
			if p.APIKey != "" {
				t.Fatalf("deepseek api_key = %q, want empty (env-var only)", p.APIKey)
			}
		}
	}

	if len(resp.Providers) != 2 {
		t.Fatalf("providers count = %d, want 2", len(resp.Providers))
	}
	if len(resp.Routes) != 1 {
		t.Fatalf("routes count = %d, want 1", len(resp.Routes))
	}
}

func TestPUTConfig_PreservesMaskedKey(t *testing.T) {
	srv, reloader, cfgPath := setupConfigServer(t)

	// Read the current YAML to verify it before PUT.
	origYAML, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Build a PUT payload where the API key is the masked placeholder.
	payload := configResponseJSON{
		Providers: []configProviderJSON{
			{Name: "openai", BaseURL: "https://api.openai.com/v1", APIKey: maskedAPIKey, Timeout: "60s"},
		},
		Routes: []configRouteJSON{
			{Name: "chat", Strategy: "priority", Fallback: configFallbackJSON{Enabled: true, MaxAttempts: 2, OnStatus: []int{429, 500}}, Providers: []configRouteProviderJSON{{Provider: "openai", Model: "gpt-4o-mini"}}},
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	req.Header.Set("Cookie", loginCookie(t, srv))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	// The reloaded config should have the real key restored.
	cfg := reloader.CurrentConfig()
	key := cfg.ProviderAPIKey("openai")
	if key != "sk-real-key-123" {
		t.Fatalf("restored key = %q, want sk-real-key-123", key)
	}

	// The YAML file should now contain the real key (not "***").
	newYAML, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile after PUT: %v", err)
	}
	if string(newYAML) == string(origYAML) {
		t.Fatal("YAML file should have been updated")
	}
	if strings.Contains(string(newYAML), maskedAPIKey) {
		t.Fatal("YAML file should contain the real key, not the masked placeholder")
	}
}

func TestPUTConfig_RejectsInvalid(t *testing.T) {
	srv, _, cfgPath := setupConfigServer(t)

	// Read the current YAML to verify it stays untouched after invalid PUT.
	origYAML, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Route references a provider that doesn't exist in providers map.
	payload := configResponseJSON{
		Providers: []configProviderJSON{
			{Name: "openai", BaseURL: "https://api.openai.com/v1", APIKey: maskedAPIKey, Timeout: "60s"},
		},
		Routes: []configRouteJSON{
			{Name: "chat", Strategy: "priority", Providers: []configRouteProviderJSON{{Provider: "nonexistent", Model: "fake"}}},
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	req.Header.Set("Cookie", loginCookie(t, srv))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}

	// YAML file should be unchanged.
	newYAML, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile after invalid PUT: %v", err)
	}
	if string(newYAML) != string(origYAML) {
		t.Fatal("YAML file should be unchanged after invalid config")
	}
}

func TestUnauthenticatedRedirectsToLogin(t *testing.T) {
	srv, store := setupServer(t)
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	loc := w.Result().Header.Get("Location")
	if !strings.HasPrefix(loc, "/login") {
		t.Fatalf("location = %q, want /login...", loc)
	}
}

func TestUnauthenticatedAPIReturns401(t *testing.T) {
	srv, store := setupServer(t)
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/summary?start=-1h", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestLoginGETRendersForm(t *testing.T) {
	srv, store := setupServer(t)
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "name=\"username\"") || !strings.Contains(body, "name=\"password\"") {
		t.Fatal("login form missing required fields")
	}
}

func TestLoginPOSTInvalidShowsError(t *testing.T) {
	srv, store := setupServer(t)
	defer store.Close()

	form := strings.NewReader("username=admin&password=wrong")
	req := httptest.NewRequest(http.MethodPost, "/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Invalid username or password") {
		t.Fatalf("body should contain error message, got: %s", body)
	}
}

func TestLoginPOSTValidSetsCookieAndRedirects(t *testing.T) {
	srv, store := setupServer(t)
	defer store.Close()

	form := strings.NewReader("username=admin&password=test-pass&next=/api/summary")
	req := httptest.NewRequest(http.MethodPost, "/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	if loc := w.Result().Header.Get("Location"); loc != "/api/summary" {
		t.Fatalf("location = %q, want /api/summary", loc)
	}
	cookie := w.Result().Header.Get("Set-Cookie")
	if !strings.HasPrefix(cookie, "apiproxy_admin=") {
		t.Fatalf("Set-Cookie = %q, want apiproxy_admin=...", cookie)
	}
}

func TestLoginRejectsOpenRedirect(t *testing.T) {
	srv, store := setupServer(t)
	defer store.Close()

	form := strings.NewReader("username=admin&password=test-pass&next=https://evil.example/")
	req := httptest.NewRequest(http.MethodPost, "/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	if loc := w.Result().Header.Get("Location"); loc != "/" {
		t.Fatalf("location = %q, want / (open redirect blocked)", loc)
	}
}

func TestLogoutClearsCookieAndRedirects(t *testing.T) {
	srv, store := setupServer(t)
	defer store.Close()

	// After logout, the response must clear the apiproxy_admin cookie and
	// redirect to /login. We test the response contract; the cleared cookie
	// is what causes subsequent unauthenticated requests to redirect.
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.Header.Set("Cookie", loginCookie(t, srv))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("logout status = %d, want 303", w.Code)
	}
	if loc := w.Result().Header.Get("Location"); loc != "/login" {
		t.Fatalf("location = %q, want /login", loc)
	}
	cookie := w.Result().Header.Get("Set-Cookie")
	if !strings.Contains(cookie, "apiproxy_admin=") || !strings.Contains(cookie, "Max-Age=0") {
		t.Fatalf("Set-Cookie should clear apiproxy_admin, got: %q", cookie)
	}

	// Simulate the browser honoring the Set-Cookie: subsequent request
	// has no apiproxy_admin cookie → must redirect to /login.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("post-logout status = %d, want 303 (redirect to login)", w.Code)
	}
}

// ---- Login throttle + cookie-attribute tests ----

func TestClientIPUsesForwardedFor(t *testing.T) {
	cases := []struct {
		name       string
		xff        string
		remoteAddr string
		want       string
	}{
		{name: "single xff", xff: "203.0.113.5", remoteAddr: "10.0.0.1:1234", want: "203.0.113.5"},
		{name: "multi xff uses first", xff: "203.0.113.5, 70.0.0.1", remoteAddr: "10.0.0.1:1234", want: "203.0.113.5"},
		{name: "no xff falls back to RemoteAddr", xff: "", remoteAddr: "10.0.0.1:1234", want: "10.0.0.1"},
		{name: "no xff no port returns raw", xff: "", remoteAddr: "10.0.0.1", want: "10.0.0.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			got := clientIP(req)
			if got != tc.want {
				t.Fatalf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRecordFailureLocksAndSuccessClears verifies the throttle state machine:
// maxFailures-1 failures do not lock, the maxFailures-th triggers lockout,
// and recordSuccess clears the state.
func TestRecordFailureLocksAndSuccessClears(t *testing.T) {
	srv, store := setupServer(t)
	defer store.Close()

	const ip = "192.0.2.10"
	now := time.Now()

	for i := 1; i < maxFailures; i++ {
		srv.recordFailure(ip, now)
		if srv.locked(ip, now) {
			t.Fatalf("locked after %d failures, expected only after %d", i, maxFailures)
		}
	}

	srv.recordFailure(ip, now)
	if !srv.locked(ip, now) {
		t.Fatalf("expected lock after %d failures", maxFailures)
	}
	if !srv.locked(ip, now.Add(lockDuration-1*time.Second)) {
		t.Fatalf("should still be locked just before lockDuration elapses")
	}
	if srv.locked(ip, now.Add(lockDuration+1*time.Second)) {
		t.Fatalf("should be unlocked after lockDuration elapses")
	}

	srv.recordSuccess(ip)
	if srv.locked(ip, now) {
		t.Fatal("locked after recordSuccess")
	}
}

// TestLoginThrottleReturns429 verifies that after maxFailures bad logins from
// the same IP, subsequent attempts return 429 instead of 401.
func TestLoginThrottleReturns429(t *testing.T) {
	srv, store := setupServer(t)
	defer store.Close()

	doPost := func() int {
		form := strings.NewReader("username=admin&password=wrong")
		req := httptest.NewRequest(http.MethodPost, "/login", form)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "198.51.100.7:5555"
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		return w.Code
	}

	for i := 0; i < maxFailures; i++ {
		if code := doPost(); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", i+1, code)
		}
	}
	if code := doPost(); code != http.StatusTooManyRequests {
		t.Fatalf("post-lockout status = %d, want 429", code)
	}
}

// TestLoginPOSTValidSetsCookieAttributes verifies HttpOnly, SameSite=Lax, and
// Max-Age=sessionMaxAge on the session cookie. Secure is validated separately
// because httptest.NewRequest does not populate r.TLS on its own.
func TestLoginPOSTValidSetsCookieAttributes(t *testing.T) {
	srv, store := setupServer(t)
	defer store.Close()

	form := strings.NewReader("username=admin&password=test-pass")
	req := httptest.NewRequest(http.MethodPost, "/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d, want 303", w.Code)
	}

	cookies := w.Result().Cookies()
	var c *http.Cookie
	for _, ck := range cookies {
		if ck.Name == sessionCookieName {
			c = ck
			break
		}
	}
	if c == nil {
		t.Fatalf("no %s cookie in response", sessionCookieName)
	}
	if !c.HttpOnly {
		t.Errorf("HttpOnly = false, want true")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.MaxAge != int(sessionMaxAge.Seconds()) {
		t.Errorf("MaxAge = %d, want %d", c.MaxAge, int(sessionMaxAge.Seconds()))
	}
	if c.Secure {
		t.Errorf("Secure = true, want false for non-TLS request")
	}
}

// TestLoginPOSTSetsSecureCookieWhenTLS verifies the Secure flag is set when
// the login arrives over TLS.
func TestLoginPOSTSetsSecureCookieWhenTLS(t *testing.T) {
	srv, store := setupServer(t)
	defer store.Close()

	form := strings.NewReader("username=admin&password=test-pass")
	req := httptest.NewRequest(http.MethodPost, "/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.TLS = &tls.ConnectionState{Version: tls.VersionTLS12}

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d, want 303", w.Code)
	}
	var c *http.Cookie
	for _, ck := range w.Result().Cookies() {
		if ck.Name == sessionCookieName {
			c = ck
			break
		}
	}
	if c == nil {
		t.Fatalf("no %s cookie in response", sessionCookieName)
	}
	if !c.Secure {
		t.Errorf("Secure = false for TLS request, want true")
	}
}


func TestConfigRouteProviderSwitchRoundTrip(t *testing.T) {
	in := configRouteProviderJSON{
		Provider: "test",
		Model:    "gpt-4",
		Tier:     "stable",
		Weight:   100,
		Switch:   "openai-to-anthropic",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out configRouteProviderJSON
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Switch != "openai-to-anthropic" {
		t.Errorf("Switch round-trip: got %q, want %q", out.Switch, "openai-to-anthropic")
	}
}
