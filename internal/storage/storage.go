package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Event is a single recorded LLM API request.
type Event struct {
	ID               int64
	Timestamp       time.Time
	RequestID       string
	ClientID         string
	Route            string
	Provider         string
	Model            string
	StatusCode       int
	LatencyMs        float64
	FirstTokenMs     float64
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	FallbackCount    int
	FallbackFrom     string
	FallbackTo       string
	ErrorType        string
	Stream           bool
}

// EventWriter is the interface the server uses to persist events.
// Implementations must be safe for concurrent use.
type EventWriter interface {
	Record(ctx context.Context, e Event) error
	Close() error
}

// Store is a SQLite-backed store of request events.
// Events are stored in daily shards named request_events_YYYYMMDD.
type Store struct {
	db        *sql.DB
	retention time.Duration // how old a shard must be before it's dropped
}

// shardTable returns the per-day table name for the given timestamp.
// Format: request_events_YYYYMMDD
func shardTable(t time.Time) string {
	return "request_events_" + t.UTC().Format("20060102")
}

// shardDay parses the YYYYMMDD suffix from a table name back to a date.
func shardDay(tableName string) (time.Time, error) {
	suffix := strings.TrimPrefix(tableName, "request_events_")
	return time.Parse("20060102", suffix)
}

// DefaultRetention is the default retention period (7 days).
const DefaultRetention = 7 * 24 * time.Hour

// Open opens or creates the SQLite database at path and ensures the schema exists.
// retention controls how old daily shards must be before they're dropped;
// 0 means use DefaultRetention (7 days).
func Open(path string, retention time.Duration) (*Store, error) {
	if retention <= 0 {
		retention = DefaultRetention
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite is single-writer; limit to 1 writer connection but allow readers.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	s := &Store{db: db, retention: retention}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Drop shards older than retention on startup.
	if err := s.dropOldShards(context.Background()); err != nil {
		// Non-fatal: stale shards will be cleaned on the next cycle.
		slog.Warn("startup shard cleanup failed", "err", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	// No global table needed — each day gets its own shard created on demand.
	// Create today's shard so the database has a table on first run.
	return s.ensureShard(context.Background(), time.Now().UTC())
}

// ensureShard creates the per-day table and its indexes if they don't exist.
func (s *Store) ensureShard(ctx context.Context, t time.Time) error {
	table := shardTable(t)
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp        TEXT    NOT NULL,
			request_id       TEXT    NOT NULL,
			client_id        TEXT    NOT NULL DEFAULT '',
			route            TEXT    NOT NULL DEFAULT '',
			provider         TEXT    NOT NULL,
			model            TEXT    NOT NULL,
			status_code      INTEGER NOT NULL DEFAULT 0,
			latency_ms       REAL    NOT NULL DEFAULT 0,
			first_token_ms   REAL    NOT NULL DEFAULT 0,
			prompt_tokens    INTEGER NOT NULL DEFAULT 0,
			completion_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens     INTEGER NOT NULL DEFAULT 0,
			fallback_count   INTEGER NOT NULL DEFAULT 0,
			fallback_from    TEXT    NOT NULL DEFAULT '',
			fallback_to      TEXT    NOT NULL DEFAULT '',
			error_type       TEXT    NOT NULL DEFAULT '',
			stream           INTEGER NOT NULL DEFAULT 0
		)`, table),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_ts    ON %s(timestamp)`, table, table),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_model ON %s(model, timestamp)`, table, table),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_prov  ON %s(provider, timestamp)`, table, table),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_route ON %s(route, timestamp)`, table, table),
	}
	for _, q := range stmts {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("ensure shard %s: %w", table, err)
		}
	}
	return nil
}

// Record inserts a single event into the correct daily shard.
func (s *Store) Record(ctx context.Context, e Event) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	e.Timestamp = e.Timestamp.UTC()

	if err := s.ensureShard(ctx, e.Timestamp); err != nil {
		return fmt.Errorf("ensure shard: %w", err)
	}

	table := shardTable(e.Timestamp)
	q := fmt.Sprintf(`INSERT INTO %s (
		timestamp, request_id, client_id, route, provider, model,
		status_code, latency_ms, first_token_ms,
		prompt_tokens, completion_tokens, total_tokens,
		fallback_count, fallback_from, fallback_to,
		error_type, stream
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, table)
	stream := 0
	if e.Stream {
		stream = 1
	}
	_, err := s.db.ExecContext(ctx, q,
		e.Timestamp.Format(time.RFC3339Nano),
		e.RequestID, e.ClientID, e.Route, e.Provider, e.Model,
		e.StatusCode, e.LatencyMs, e.FirstTokenMs,
		e.PromptTokens, e.CompletionTokens, e.TotalTokens,
		e.FallbackCount, e.FallbackFrom, e.FallbackTo,
		e.ErrorType, stream,
	)
	if err != nil {
		return fmt.Errorf("insert event into %s: %w", table, err)
	}
	return nil
}

// NoopWriter discards all events. Useful when storage is disabled.
type NoopWriter struct{}

func (NoopWriter) Record(context.Context, Event) error { return nil }
func (NoopWriter) Close() error                         { return nil }

// Shard discovery -----------------------------------------------------------

// listShards returns all daily shard table names sorted by date.
func (s *Store) listShards(ctx context.Context) ([]string, error) {
	q := `SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'request_events_%' ORDER BY name`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		// Filter out any name that doesn't parse as YYYYMMDD.
		if _, err := shardDay(name); err != nil {
			continue
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// shardsInRange returns shard table names that cover [start, end].
// Both start and end are inclusive on the day boundary.
func shardsInRange(start, end time.Time, allShards []string) []string {
	startDay := start.UTC().Truncate(24 * time.Hour)
	endDay := end.UTC().Truncate(24 * time.Hour)
	var out []string
	for _, name := range allShards {
		day, err := shardDay(name)
		if err != nil {
			continue
		}
		day = day.UTC()
		if day.Before(startDay) || day.After(endDay) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// unionFrom builds a (SELECT … FROM shard1 UNION ALL SELECT … FROM shard2 …)
// subquery that covers all shards in range. The WHERE clause from filter
// is applied inside each sub-select so SQLite can use per-shard indexes.
// Returns the full FROM clause and the args slice.
func unionFrom(clauses string, args []any, shards []string) (string, []any) {
	if len(shards) == 0 {
		return `(SELECT
			0 AS id, '' AS timestamp, '' AS request_id, '' AS client_id, '' AS route,
			'' AS provider, '' AS model, 0 AS status_code, 0.0 AS latency_ms,
			0.0 AS first_token_ms, 0 AS prompt_tokens, 0 AS completion_tokens,
			0 AS total_tokens, 0 AS fallback_count, '' AS fallback_from,
			'' AS fallback_to, '' AS error_type, 0 AS stream
			WHERE 1=0) AS events`, nil
	}
	var sb strings.Builder
	sb.WriteByte('(')
	for i, sh := range shards {
		if i > 0 {
			sb.WriteString(" UNION ALL ")
		}
		sb.WriteString(fmt.Sprintf("SELECT * FROM %s %s", sh, clauses))
	}
	sb.WriteString(") AS events")
	return sb.String(), repeatArgs(args, len(shards))
}

// repeatArgs repeats the args slice n times (one copy per UNION ALL branch).
func repeatArgs(args []any, n int) []any {
	out := make([]any, 0, len(args)*n)
	for i := 0; i < n; i++ {
		out = append(out, args...)
	}
	return out
}

// Shard cleanup -------------------------------------------------------------

// dropOldShards drops daily shards whose day is older than (now - retention).
func (s *Store) dropOldShards(ctx context.Context) error {
	cutoff := time.Now().UTC().Add(-s.retention).Truncate(24 * time.Hour)
	shards, err := s.listShards(ctx)
	if err != nil {
		return err
	}
	for _, sh := range shards {
		day, err := shardDay(sh)
		if err != nil {
			continue
		}
		if day.Before(cutoff) {
			q := fmt.Sprintf("DROP TABLE IF EXISTS %s", sh)
			if _, err := s.db.ExecContext(ctx, q); err != nil {
				slog.Warn("drop shard failed", "table", sh, "err", err)
			} else {
				slog.Info("dropped old shard", "table", sh, "date", day.Format("2006-01-02"))
			}
		}
	}
	return nil
}

// StartCleanupLoop launches a background goroutine that drops old shards
// every hour. It stops when the context is cancelled.
func (s *Store) StartCleanupLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.dropOldShards(ctx); err != nil {
					slog.Warn("periodic shard cleanup failed", "err", err)
				}
			}
		}
	}()
}

// Queries ----------------------------------------------------------------

// QueryFilter narrows the events considered by aggregate queries.
type QueryFilter struct {
	Start    time.Time
	End      time.Time
	Provider string // empty = all
	Model    string // empty = all
	Route    string // empty = all
	ClientID string // empty = all
	Stream   *bool  // nil = all
}

func (f QueryFilter) clauses() (string, []any) {
	var sb strings.Builder
	sb.WriteString("WHERE 1=1")
	args := []any{}
	if !f.Start.IsZero() {
		sb.WriteString(" AND timestamp >= ?")
		args = append(args, f.Start.UTC().Format(time.RFC3339Nano))
	}
	if !f.End.IsZero() {
		sb.WriteString(" AND timestamp <= ?")
		args = append(args, f.End.UTC().Format(time.RFC3339Nano))
	}
	if f.Provider != "" {
		sb.WriteString(" AND provider = ?")
		args = append(args, f.Provider)
	}
	if f.Model != "" {
		sb.WriteString(" AND model = ?")
		args = append(args, f.Model)
	}
	if f.Route != "" {
		sb.WriteString(" AND route = ?")
		args = append(args, f.Route)
	}
	if f.ClientID != "" {
		sb.WriteString(" AND client_id = ?")
		args = append(args, f.ClientID)
	}
	if f.Stream != nil {
		sb.WriteString(" AND stream = ?")
		if *f.Stream {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	return sb.String(), args
}

// resolveShards discovers shards that overlap with the filter's time range
// and returns them sorted by name.
func (s *Store) resolveShards(ctx context.Context, f QueryFilter) ([]string, error) {
	all, err := s.listShards(ctx)
	if err != nil {
		return nil, err
	}
	start := f.Start.UTC()
	end := f.End.UTC()
	if end.IsZero() {
		end = time.Now().UTC()
	}
	return shardsInRange(start, end, all), nil
}

// ModelSummary aggregates per-model performance over a window.
type ModelSummary struct {
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	Route            string  `json:"route"`
	Requests         int     `json:"requests"`
	Errors           int     `json:"errors"`
	Fallbacks        int     `json:"fallbacks"`
	SuccessRate      float64 `json:"success_rate"`
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
	P50LatencyMs     float64 `json:"p50_latency_ms"`
	P95LatencyMs     float64 `json:"p95_latency_ms"`
	FirstTokenMs     float64 `json:"avg_first_token_ms"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	TokensPerSec     float64 `json:"tokens_per_sec"`
	StreamRequests   int     `json:"stream_requests"`
}

// ModelSummaries returns one row per (provider, model, route) in the window.
func (s *Store) ModelSummaries(ctx context.Context, f QueryFilter) ([]ModelSummary, error) {
	where, args := f.clauses()
	shards, err := s.resolveShards(ctx, f)
	if err != nil {
		return nil, err
	}
	from, fromArgs := unionFrom(where, args, shards)

	q := fmt.Sprintf(`
		SELECT
			provider, model, route,
			COUNT(*)                                       AS requests,
			COALESCE(SUM(CASE WHEN status_code >= 400 OR error_type != '' THEN 1 ELSE 0 END), 0) AS errors,
			COALESCE(SUM(CASE WHEN fallback_count > 0 THEN 1 ELSE 0 END), 0)                      AS fallbacks,
			COALESCE(AVG(latency_ms), 0)                  AS avg_latency_ms,
			COALESCE(AVG(CASE WHEN stream = 1 THEN first_token_ms END), 0) AS avg_first_token_ms,
			COALESCE(SUM(prompt_tokens), 0)               AS prompt_tokens,
			COALESCE(SUM(completion_tokens), 0)           AS completion_tokens,
			COALESCE(SUM(total_tokens), 0)                AS total_tokens,
			COALESCE(SUM(CASE WHEN stream = 1 THEN 1 ELSE 0 END), 0)        AS stream_requests
		FROM %s
		GROUP BY provider, model, route
		ORDER BY provider, model, route`, from)
	rows, err := s.db.QueryContext(ctx, q, fromArgs...)
	if err != nil {
		return nil, fmt.Errorf("model summaries: %w", err)
	}
	defer rows.Close()

	var out []ModelSummary
	for rows.Next() {
		var m ModelSummary
		if err := rows.Scan(&m.Provider, &m.Model, &m.Route,
			&m.Requests, &m.Errors, &m.Fallbacks,
			&m.AvgLatencyMs, &m.FirstTokenMs,
			&m.PromptTokens, &m.CompletionTokens, &m.TotalTokens,
			&m.StreamRequests,
		); err != nil {
			return nil, err
		}
		if m.Requests > 0 {
			m.SuccessRate = float64(m.Requests-m.Errors) / float64(m.Requests)
		}
		if m.AvgLatencyMs > 0 && m.CompletionTokens > 0 {
			m.TokensPerSec = float64(m.CompletionTokens) / (m.AvgLatencyMs / 1000.0)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// PercentileRow holds p50/p95 latency stats for a (provider, model) pair.
type PercentileRow struct {
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	P50LatencyMs float64 `json:"p50_latency_ms"`
	P95LatencyMs float64 `json:"p95_latency_ms"`
	P99LatencyMs float64 `json:"p99_latency_ms"`
}

// LatencyPercentiles computes p50/p95/p99 latency per (provider, model).
func (s *Store) LatencyPercentiles(ctx context.Context, f QueryFilter) ([]PercentileRow, error) {
	where, args := f.clauses()
	shards, err := s.resolveShards(ctx, f)
	if err != nil {
		return nil, err
	}
	from, fromArgs := unionFrom(where, args, shards)

	q := fmt.Sprintf(`SELECT provider, model, latency_ms FROM %s`, from)
	rows, err := s.db.QueryContext(ctx, q, fromArgs...)
	if err != nil {
		return nil, fmt.Errorf("latency percentiles: %w", err)
	}
	defer rows.Close()

	groups := map[string][]float64{}
	for rows.Next() {
		var prov, model string
		var lat float64
		if err := rows.Scan(&prov, &model, &lat); err != nil {
			return nil, err
		}
		key := prov + "\x00" + model
		groups[key] = append(groups[key], lat)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]PercentileRow, 0, len(groups))
	for key, vals := range groups {
		prov, model := splitKey(key)
		out = append(out, PercentileRow{
			Provider:     prov,
			Model:        model,
			P50LatencyMs: percentile(vals, 0.50),
			P95LatencyMs: percentile(vals, 0.95),
			P99LatencyMs: percentile(vals, 0.99),
		})
	}
	return out, nil
}

// BucketRow aggregates speed by prompt-token bucket.
type BucketRow struct {
	Bucket          string  `json:"bucket"`
	BucketMin       int     `json:"bucket_min"`
	BucketMax       int     `json:"bucket_max"`
	Provider        string  `json:"provider"`
	Model           string  `json:"model"`
	Requests        int     `json:"requests"`
	PromptTokens    int64   `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	TotalLatencyMs  float64 `json:"total_latency_ms"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
	PPRate          float64 `json:"pp_rate"`
	TGRate          float64 `json:"tg_rate"`
}

func (s *Store) SpeedByPromptBucket(ctx context.Context, f QueryFilter, buckets []Bucket) ([]BucketRow, error) {
	if len(buckets) == 0 {
		buckets = DefaultBuckets
	}
	where, args := f.clauses()
	shards, err := s.resolveShards(ctx, f)
	if err != nil {
		return nil, err
	}
	from, fromArgs := unionFrom(where, args, shards)

	q := fmt.Sprintf(`
		SELECT provider, model, prompt_tokens, completion_tokens,
		       latency_ms, first_token_ms, stream
		FROM %s`, from)
	rows, err := s.db.QueryContext(ctx, q, fromArgs...)
	if err != nil {
		return nil, fmt.Errorf("speed by prompt bucket: %w", err)
	}
	defer rows.Close()

	type agg struct {
		count       int
		prompt      int64
		completion  int64
		totalLatMs  float64
		totalFTMs   float64
		streamCount int
	}
	groups := map[string]*agg{}

	for rows.Next() {
		var prov, model string
		var prompt, completion int64
		var latMs, ftMs float64
		var stream int
		if err := rows.Scan(&prov, &model, &prompt, &completion, &latMs, &ftMs, &stream); err != nil {
			return nil, err
		}
		b := bucketFor(int(prompt), buckets)
		key := prov + "\x00" + model + "\x00" + b.Label
		a, ok := groups[key]
		if !ok {
			a = &agg{}
			groups[key] = a
		}
		a.count++
		a.prompt += prompt
		a.completion += completion
		a.totalLatMs += latMs
		a.totalFTMs += ftMs
		if stream == 1 {
			a.streamCount++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]BucketRow, 0, len(groups))
	for key, a := range groups {
		prov, model, label := splitKey3(key)
		b := bucketByLabel(label, buckets)
		row := BucketRow{
			Bucket:           label,
			BucketMin:        b.Min,
			BucketMax:        b.Max,
			Provider:         prov,
			Model:            model,
			Requests:         a.count,
			PromptTokens:     a.prompt,
			CompletionTokens: a.completion,
			TotalLatencyMs:   a.totalLatMs,
			AvgLatencyMs:     a.totalLatMs / float64(a.count),
		}
		if a.totalFTMs > 0 {
			row.PPRate = float64(a.prompt) / (a.totalFTMs / 1000.0)
		}
		genSec := (a.totalLatMs - a.totalFTMs) / 1000.0
		if genSec > 0 {
			row.TGRate = float64(a.completion) / genSec
		}
		out = append(out, row)
	}
	return out, nil
}

// TimeSeriesPoint is one data point in a time series.
type TimeSeriesPoint struct {
	Ts              string  `json:"ts"`
	Requests        int     `json:"requests"`
	Errors          int     `json:"errors"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
	CompletionTokens int64  `json:"completion_tokens"`
	PromptTokens    int64   `json:"prompt_tokens"`
}

// TimeSeries returns per-bucket aggregates ordered by time.
func (s *Store) TimeSeries(ctx context.Context, f QueryFilter, interval string) ([]TimeSeriesPoint, error) {
	fmtSpec := "%Y-%m-%dT%H:%M:00.000"
	switch interval {
	case "hour":
		fmtSpec = "%Y-%m-%dT%H:00:00.000"
	case "day":
		fmtSpec = "%Y-%m-%dT00:00:00.000"
	case "minute", "":
		fmtSpec = "%Y-%m-%dT%H:%M:00.000"
	}
	where, args := f.clauses()
	shards, err := s.resolveShards(ctx, f)
	if err != nil {
		return nil, err
	}
	from, fromArgs := unionFrom(where, args, shards)

	q := fmt.Sprintf(`
		SELECT
			strftime('%s', timestamp)                AS bucket,
			COUNT(*)                                 AS requests,
			COALESCE(SUM(CASE WHEN status_code >= 400 OR error_type != '' THEN 1 ELSE 0 END), 0) AS errors,
			COALESCE(AVG(latency_ms), 0)             AS avg_latency_ms,
			COALESCE(SUM(completion_tokens), 0)      AS completion_tokens,
			COALESCE(SUM(prompt_tokens), 0)          AS prompt_tokens
		FROM %s
		GROUP BY bucket
		ORDER BY bucket ASC`, fmtSpec, from)
	rows, err := s.db.QueryContext(ctx, q, fromArgs...)
	if err != nil {
		return nil, fmt.Errorf("time series: %w", err)
	}
	defer rows.Close()

	var out []TimeSeriesPoint
	for rows.Next() {
		var p TimeSeriesPoint
		if err := rows.Scan(&p.Ts, &p.Requests, &p.Errors,
			&p.AvgLatencyMs, &p.CompletionTokens, &p.PromptTokens,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DistinctValues returns distinct values for a column in the window.
func (s *Store) DistinctValues(ctx context.Context, column string, f QueryFilter) ([]string, error) {
	allowed := map[string]string{
		"provider":  "provider",
		"model":     "model",
		"route":     "route",
		"client_id": "client_id",
	}
	col, ok := allowed[column]
	if !ok {
		return nil, errors.New("invalid column")
	}
	where, args := f.clauses()
	shards, err := s.resolveShards(ctx, f)
	if err != nil {
		return nil, err
	}
	from, fromArgs := unionFrom(where, args, shards)

	q := fmt.Sprintf(`SELECT DISTINCT %s FROM %s ORDER BY %s`, col, from, col)
	rows, err := s.db.QueryContext(ctx, q, fromArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Bucket represents a prompt-token bucket for grouping requests.
type Bucket struct {
	Label string
	Min   int
	Max   int
}

// DefaultBuckets covers typical LLM context-length ranges.
var DefaultBuckets = []Bucket{
	{"0-128", 0, 128},
	{"128-512", 128, 512},
	{"512-2k", 512, 2048},
	{"2k-8k", 2048, 8192},
	{"8k-32k", 8192, 32768},
	{"32k+", 32768, 1 << 30},
}

func bucketFor(prompt int, buckets []Bucket) Bucket {
	for _, b := range buckets {
		if prompt >= b.Min && prompt < b.Max {
			return b
		}
	}
	return buckets[len(buckets)-1]
}

func bucketByLabel(label string, buckets []Bucket) Bucket {
	for _, b := range buckets {
		if b.Label == label {
			return b
		}
	}
	return Bucket{Label: label}
}

// helpers ----------------------------------------------------------------

func splitKey(k string) (string, string) {
	parts := strings.SplitN(k, "\x00", 2)
	if len(parts) != 2 {
		return k, ""
	}
	return parts[0], parts[1]
}

func splitKey3(k string) (string, string, string) {
	parts := strings.SplitN(k, "\x00", 3)
	if len(parts) != 3 {
		return k, "", ""
	}
	return parts[0], parts[1], parts[2]
}

func percentile(vals []float64, p float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := append([]float64(nil), vals...)
	sortFloats(sorted)
	idx := int(float64(len(sorted)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func sortFloats(a []float64) {
	for i := 1; i < len(a); i++ {
		j := i
		for j > 0 && a[j-1] > a[j] {
			a[j-1], a[j] = a[j], a[j-1]
			j--
		}
	}
}
