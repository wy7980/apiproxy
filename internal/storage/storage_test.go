package storage

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func openTestDB(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s, err := Open(path, DefaultRetention)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}
func TestOpenAndMigrate(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()

	// Verify the table exists by inserting a row.
	ctx := context.Background()
	err := s.Record(ctx, Event{
		RequestID:       "req_test1",
		ClientID:         "client1",
		Route:            "chat",
		Provider:         "openai",
		Model:            "gpt-4o-mini",
		StatusCode:       200,
		LatencyMs:        120.5,
		FirstTokenMs:     45.3,
		PromptTokens:     500,
		CompletionTokens: 100,
		TotalTokens:      600,
		Stream:           true,
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
}

func TestRecordDefaults(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()

	ctx := context.Background()
	err := s.Record(ctx, Event{
		RequestID: "req_test2",
		Provider:  "deepseek",
		Model:     "deepseek-chat",
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
}

func TestModelSummaries(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()

	ctx := context.Background()
	now := time.Now().UTC()

	events := []Event{
		{Timestamp: now.Add(-5 * time.Minute), RequestID: "r1", Provider: "openai", Model: "gpt-4o-mini", Route: "chat", StatusCode: 200, LatencyMs: 100, FirstTokenMs: 30, PromptTokens: 500, CompletionTokens: 100, TotalTokens: 600, Stream: true},
		{Timestamp: now.Add(-3 * time.Minute), RequestID: "r2", Provider: "openai", Model: "gpt-4o-mini", Route: "chat", StatusCode: 500, LatencyMs: 200, ErrorType: "server_error", Stream: false},
		{Timestamp: now.Add(-1 * time.Minute), RequestID: "r3", Provider: "deepseek", Model: "deepseek-chat", Route: "chat", StatusCode: 200, LatencyMs: 80, FirstTokenMs: 20, PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200, Stream: true, FallbackCount: 1, FallbackFrom: "openai:gpt-4o-mini"},
	}
	for _, e := range events {
		if err := s.Record(ctx, e); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	f := QueryFilter{Start: now.Add(-10 * time.Minute)}
	rows, err := s.ModelSummaries(ctx, f)
	if err != nil {
		t.Fatalf("ModelSummaries: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("ModelSummaries returned no rows")
	}

	found := false
	for _, r := range rows {
		if r.Provider == "openai" && r.Model == "gpt-4o-mini" {
			found = true
			if r.Requests != 2 {
				t.Fatalf("openai requests = %d, want 2", r.Requests)
			}
			if r.Errors != 1 {
				t.Fatalf("openai errors = %d, want 1", r.Errors)
			}
		}
	}
	if !found {
		t.Fatal("openai/gpt-4o-mini row not found")
	}
}

func TestLatencyPercentiles(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()

	ctx := context.Background()
	now := time.Now().UTC()

	latencies := []float64{50, 80, 100, 120, 200, 300, 500, 800, 1000, 1500}
	for i, lat := range latencies {
		if err := s.Record(ctx, Event{
			Timestamp: now.Add(-time.Duration(i) * time.Minute),
			RequestID: "rp" + strconv.Itoa(i),
			Provider:  "openai", Model: "gpt-4o-mini",
			StatusCode: 200, LatencyMs: lat,
		}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	f := QueryFilter{Start: now.Add(-15 * time.Minute)}
	rows, err := s.LatencyPercentiles(ctx, f)
	if err != nil {
		t.Fatalf("LatencyPercentiles: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("LatencyPercentiles returned no rows")
	}
	row := rows[0]
	if row.P50LatencyMs == 0 {
		t.Fatal("p50 is 0")
	}
	if row.P95LatencyMs == 0 {
		t.Fatal("p95 is 0")
	}
}

func TestTimeSeries(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()

	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 5; i++ {
		if err := s.Record(ctx, Event{
			Timestamp: now.Add(-time.Duration(i*30) * time.Minute),
			RequestID: "ts" + strconv.Itoa(i),
			Provider:  "openai", Model: "gpt-4o-mini",
			StatusCode: 200, LatencyMs: 100 + float64(i)*10,
			PromptTokens: 500, CompletionTokens: 100,
		}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	f := QueryFilter{Start: now.Add(-3 * time.Hour)}
	rows, err := s.TimeSeries(ctx, f, "hour")
	if err != nil {
		t.Fatalf("TimeSeries: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("TimeSeries returned no rows")
	}
}

func TestDistinctValues(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()

	ctx := context.Background()
	now := time.Now().UTC()

	provs := []string{"openai", "deepseek", "qwen"}
	for _, p := range provs {
		if err := s.Record(ctx, Event{
			Timestamp: now, RequestID: "dv_" + p,
			Provider: p, Model: p + "-model",
			StatusCode: 200, LatencyMs: 50,
		}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	f := QueryFilter{Start: now.Add(-1 * time.Hour)}
	vals, err := s.DistinctValues(ctx, "provider", f)
	if err != nil {
		t.Fatalf("DistinctValues: %v", err)
	}
	if len(vals) != 3 {
		t.Fatalf("DistinctValues providers = %d, want 3", len(vals))
	}
}

func TestSpeedByPromptBucket(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()

	ctx := context.Background()
	now := time.Now().UTC()

	events := []Event{
		{Timestamp: now, RequestID: "b1", Provider: "openai", Model: "gpt-4o-mini", StatusCode: 200, LatencyMs: 500, FirstTokenMs: 100, PromptTokens: 500, CompletionTokens: 200, Stream: true},
		{Timestamp: now, RequestID: "b2", Provider: "openai", Model: "gpt-4o-mini", StatusCode: 200, LatencyMs: 1000, FirstTokenMs: 200, PromptTokens: 4000, CompletionTokens: 500, Stream: true},
	}
	for _, e := range events {
		if err := s.Record(ctx, e); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	f := QueryFilter{Start: now.Add(-1 * time.Hour)}
	rows, err := s.SpeedByPromptBucket(ctx, f, nil)
	if err != nil {
		t.Fatalf("SpeedByPromptBucket: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("SpeedByPromptBucket returned no rows")
	}
}

func TestNoopWriter(t *testing.T) {
	var w NoopWriter
	if err := w.Record(context.Background(), Event{}); err != nil {
		t.Fatalf("NoopWriter.Record: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("NoopWriter.Close: %v", err)
	}
}

func TestRecordRoutesToCorrectShard(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()

	ctx := context.Background()
	ts := time.Date(2026, 6, 15, 13, 45, 0, 0, time.UTC)
	if err := s.Record(ctx, Event{Timestamp: ts, RequestID: "shard", Provider: "openai", Model: "gpt"}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_events_20260615`).Scan(&count); err != nil {
		t.Fatalf("query shard: %v", err)
	}
	if count != 1 {
		t.Fatalf("rows in shard = %d, want 1", count)
	}
}

func TestQueryAcrossDailyShards(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()

	ctx := context.Background()
	base := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	events := []Event{
		{Timestamp: base, RequestID: "d1", Provider: "openai", Model: "gpt", Route: "chat", StatusCode: 200, LatencyMs: 100, CompletionTokens: 10},
		{Timestamp: base.Add(24 * time.Hour), RequestID: "d2", Provider: "openai", Model: "gpt", Route: "chat", StatusCode: 200, LatencyMs: 200, CompletionTokens: 20},
	}
	for _, e := range events {
		if err := s.Record(ctx, e); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	rows, err := s.ModelSummaries(ctx, QueryFilter{Start: base.Add(-time.Hour), End: base.Add(25 * time.Hour)})
	if err != nil {
		t.Fatalf("ModelSummaries: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Requests != 2 {
		t.Fatalf("requests = %d, want 2", rows[0].Requests)
	}
}

func TestDropOldShards(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()

	ctx := context.Background()
	old := time.Now().UTC().Add(-10 * 24 * time.Hour)
	fresh := time.Now().UTC()
	if err := s.Record(ctx, Event{Timestamp: old, RequestID: "old", Provider: "openai", Model: "gpt"}); err != nil {
		t.Fatalf("Record old: %v", err)
	}
	if err := s.Record(ctx, Event{Timestamp: fresh, RequestID: "fresh", Provider: "openai", Model: "gpt"}); err != nil {
		t.Fatalf("Record fresh: %v", err)
	}

	s.retention = 7 * 24 * time.Hour
	if err := s.dropOldShards(ctx); err != nil {
		t.Fatalf("dropOldShards: %v", err)
	}

	shards, err := s.listShards(ctx)
	if err != nil {
		t.Fatalf("listShards: %v", err)
	}
	oldTable := shardTable(old)
	freshTable := shardTable(fresh)
	for _, sh := range shards {
		if sh == oldTable {
			t.Fatalf("old shard %s still exists: %v", oldTable, shards)
		}
	}
	foundFresh := false
	for _, sh := range shards {
		if sh == freshTable {
			foundFresh = true
		}
	}
	if !foundFresh {
		t.Fatalf("fresh shard %s missing: %v", freshTable, shards)
	}
}
