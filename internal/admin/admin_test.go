package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"log/slog"

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

	return New(store, slog.Default()), store
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
