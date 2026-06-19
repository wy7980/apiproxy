package admin

import (
	"bytes"
	"context"
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

	return New(store, slog.Default(), "", nil), store
}

func TestIndexHTML(t *testing.T) {
	srv, store := setupServer(t)
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
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
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var rows []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestBuckets(t *testing.T) {
	srv, store := setupServer(t)
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/buckets?start=-1h", nil)
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
	mu   sync.Mutex
	cfg  *config.Config
	err  error // error to return on next Reload call
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
			"openai":    {Type: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "sk-real-key-123", Timeout: 60 * time.Second},
			"deepseek":  {Type: "openai", BaseURL: "https://api.deepseek.com/v1", APIKeyEnv: "DEEPSEEK_KEY", Timeout: 60 * time.Second},
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
	srv := New(store, slog.Default(), cfgPath, reloader)
	return srv, reloader, cfgPath
}

func TestGETConfig_MasksKeys(t *testing.T) {
	srv, _, _ := setupConfigServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
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
			{Name: "openai", Type: "openai", BaseURL: "https://api.openai.com/v1", APIKey: maskedAPIKey, Timeout: "60s"},
		},
		Routes: []configRouteJSON{
			{Name: "chat", Strategy: "priority", Fallback: configFallbackJSON{Enabled: true, MaxAttempts: 2, OnStatus: []int{429, 500}}, Providers: []configRouteProviderJSON{{Provider: "openai", Model: "gpt-4o-mini"}}},
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
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
			{Name: "openai", Type: "openai", BaseURL: "https://api.openai.com/v1", APIKey: maskedAPIKey, Timeout: "60s"},
		},
		Routes: []configRouteJSON{
			{Name: "chat", Strategy: "priority", Providers: []configRouteProviderJSON{{Provider: "nonexistent", Model: "fake"}}},
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
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
