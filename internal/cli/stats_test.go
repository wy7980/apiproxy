package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wangyong/apiproxy/internal/storage"
)

func seedDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.db")
	s, err := storage.Open(path, 0)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	events := []storage.Event{
		{Timestamp: now.Add(-30 * time.Second), RequestID: "r1", Provider: "openai", Model: "gpt-4o-mini", Route: "chat", StatusCode: 200, LatencyMs: 100, FirstTokenMs: 30, PromptTokens: 50, CompletionTokens: 100, TotalTokens: 150, Stream: true},
		{Timestamp: now.Add(-20 * time.Second), RequestID: "r2", Provider: "deepseek", Model: "deepseek-chat", Route: "chat", StatusCode: 200, LatencyMs: 80, FirstTokenMs: 20, PromptTokens: 3000, CompletionTokens: 200, TotalTokens: 3200, Stream: true},
		{Timestamp: now.Add(-10 * time.Second), RequestID: "r3", Provider: "openai", Model: "gpt-4o-mini", Route: "chat", StatusCode: 500, LatencyMs: 200, ErrorType: "server_error"},
	}
	for _, e := range events {
		if err := s.Record(ctx, e); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	return path
}

func TestPrintStatsDefault(t *testing.T) {
	path := seedDB(t)
	var buf bytes.Buffer
	err := PrintStats(context.Background(), &buf, StatsOptions{Window: 10 * time.Minute, DBPath: path})
	if err != nil {
		t.Fatalf("PrintStats: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "最近") {
		t.Errorf("output missing header, got:\n%s", out)
	}
	if !strings.Contains(out, "openai") || !strings.Contains(out, "gpt-4o-mini") {
		t.Errorf("output missing openai row:\n%s", out)
	}
	if !strings.Contains(out, "按上下文长度分桶") {
		t.Errorf("output missing buckets section:\n%s", out)
	}
	if !strings.Contains(out, "时间序列趋势") {
		t.Errorf("output missing timeseries section:\n%s", out)
	}
}

func TestPrintStatsJSON(t *testing.T) {
	path := seedDB(t)
	var buf bytes.Buffer
	err := PrintStats(context.Background(), &buf, StatsOptions{Window: 10 * time.Minute, DBPath: path, AsJSON: true})
	if err != nil {
		t.Fatalf("PrintStats: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if got := payload["window_minutes"]; got != float64(10) {
		t.Errorf("window_minutes = %v, want 10", got)
	}
	if got := payload["interval"]; got != "minute" {
		t.Errorf("interval = %v, want minute", got)
	}
	if _, ok := payload["timeseries"]; !ok {
		t.Errorf("payload missing timeseries: %#v", payload)
	}
}

func TestPrintStatsMissingDB(t *testing.T) {
	var buf bytes.Buffer
	err := PrintStats(context.Background(), &buf, StatsOptions{DBPath: ""})
	if err == nil {
		t.Fatal("expected error for missing DB")
	}
}

func TestPrintStatsNoSuchFile(t *testing.T) {
	var buf bytes.Buffer
	err := PrintStats(context.Background(), &buf, StatsOptions{DBPath: "/nonexistent/path.db"})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestDisplayWidth(t *testing.T) {
	if w := displayWidth("abc"); w != 3 {
		t.Errorf("displayWidth(abc) = %d, want 3", w)
	}
	if w := displayWidth("模型"); w != 4 {
		t.Errorf("displayWidth(模型) = %d, want 4", w)
	}
}

func TestPrintStatsWithFilters(t *testing.T) {
	path := seedDB(t)
	var buf bytes.Buffer
	err := PrintStats(context.Background(), &buf, StatsOptions{
		Window:   10 * time.Minute,
		DBPath:   path,
		Provider: "openai",
	})
	if err != nil {
		t.Fatalf("PrintStats: %v", err)
	}
	if strings.Contains(buf.String(), "deepseek") {
		t.Errorf("filtered output should not contain deepseek:\n%s", buf.String())
	}
}

// Ensure the helper does not error on a real file path.
func TestRegisterStatsFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(&bytes.Buffer{})
	opts := StatsOptions{}
	RegisterStatsFlags(fs, &opts)
	if err := fs.Parse([]string{"-window", "5m", "-db", "/tmp/x.db", "-provider", "p", "-interval", "minute"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if opts.Window != 5*time.Minute {
		t.Errorf("window = %v, want 5m", opts.Window)
	}
	if opts.DBPath != "/tmp/x.db" {
		t.Errorf("db = %v", opts.DBPath)
	}
	if opts.Provider != "p" {
		t.Errorf("provider = %v", opts.Provider)
	}
	if opts.Interval != "minute" {
		t.Errorf("interval = %v", opts.Interval)
	}
}
