// Package admin provides a separate HTTP server that serves a dashboard UI
// and JSON API for inspecting recorded request statistics.
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wangyong/apiproxy/internal/storage"
)

// Server is the admin dashboard HTTP server.
type Server struct {
	store  *storage.Store
	logger *slog.Logger
	mux    *http.ServeMux
}

// New constructs an admin server bound to the given SQLite store.
func New(store *storage.Store, logger *slog.Logger) *Server {
	s := &Server{
		store:  store,
		logger: logger,
		mux:    http.NewServeMux(),
	}
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/api/summary", s.handleSummary)
	s.mux.HandleFunc("/api/percentiles", s.handlePercentiles)
	s.mux.HandleFunc("/api/buckets", s.handleBuckets)
	s.mux.HandleFunc("/api/timeseries", s.handleTimeseries)
	s.mux.HandleFunc("/api/filters", s.handleFilters)
	return s
}

// Handler returns the HTTP handler for the admin dashboard.
func (s *Server) Handler() http.Handler { return s.mux }

// ListenAndServe starts the admin HTTP server.
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.ListenAndServe()
}

// parseFilter extracts query parameters into a storage.QueryFilter.
// Supports: start, end (RFC3339 or relative like -24h), provider, model,
// route, client_id, stream.
func parseFilter(r *http.Request) (storage.QueryFilter, error) {
	q := r.URL.Query()
	f := storage.QueryFilter{}

	if v := q.Get("start"); v != "" {
		t, err := parseTime(v)
		if err != nil {
			return f, fmt.Errorf("invalid start: %w", err)
		}
		f.Start = t
	}
	if v := q.Get("end"); v != "" {
		t, err := parseTime(v)
		if err != nil {
			return f, fmt.Errorf("invalid end: %w", err)
		}
		f.End = t
	}
	f.Provider = strings.TrimSpace(q.Get("provider"))
	f.Model = strings.TrimSpace(q.Get("model"))
	f.Route = strings.TrimSpace(q.Get("route"))
	f.ClientID = strings.TrimSpace(q.Get("client_id"))
	if v := q.Get("stream"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return f, fmt.Errorf("invalid stream: %w", err)
		}
		f.Stream = &b
	}
	return f, nil
}

// parseTime accepts RFC3339 absolute times or relative durations like
// "-24h", "-7d" measured from now.
func parseTime(s string) (time.Time, error) {
	if strings.HasPrefix(s, "-") {
		d, err := parseDuration(s[1:])
		if err != nil {
			return time.Time{}, err
		}
		return time.Now().Add(-d), nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time format %q", s)
}

// parseDuration extends time.ParseDuration with "d" (day) and "w" (week).
func parseDuration(s string) (time.Duration, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	// Allow forms like "7d" or "2w".
	if len(s) >= 2 {
		suffix := s[len(s)-1]
		numStr := s[:len(s)-1]
		n, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, fmt.Errorf("unsupported duration %q", s)
		}
		switch suffix {
		case 'd':
			return time.Duration(n * 24 * float64(time.Hour)), nil
		case 'w':
			return time.Duration(n * 7 * 24 * float64(time.Hour)), nil
		}
	}
	return 0, fmt.Errorf("unsupported duration %q", s)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilter(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	summaries, err := s.store.ModelSummaries(ctx, f)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Attach percentile rows so the client gets a single payload.
	pcts, err := s.store.LatencyPercentiles(ctx, f)
	if err != nil {
		s.logger.Warn("percentiles query failed", "err", err)
		pcts = nil
	}
	pctMap := make(map[string]storage.PercentileRow, len(pcts))
	for _, p := range pcts {
		pctMap[p.Provider+"\x00"+p.Model] = p
	}

	type summaryWithPercentiles struct {
		storage.ModelSummary
		P50LatencyMs float64 `json:"p50_latency_ms"`
		P95LatencyMs float64 `json:"p95_latency_ms"`
		P99LatencyMs float64 `json:"p99_latency_ms"`
	}
	out := make([]summaryWithPercentiles, 0, len(summaries))
	for _, m := range summaries {
		row := summaryWithPercentiles{ModelSummary: m}
		if p, ok := pctMap[m.Provider+"\x00"+m.Model]; ok {
			row.P50LatencyMs = p.P50LatencyMs
			row.P95LatencyMs = p.P95LatencyMs
			row.P99LatencyMs = p.P99LatencyMs
		}
		out = append(out, row)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePercentiles(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilter(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	rows, err := s.store.LatencyPercentiles(ctx, f)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)
}

func (s *Server) handleBuckets(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilter(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	rows, err := s.store.SpeedByPromptBucket(ctx, f, nil)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)
}

func (s *Server) handleTimeseries(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilter(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	interval := r.URL.Query().Get("interval")
	if interval == "" {
		interval = "hour"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	rows, err := s.store.TimeSeries(ctx, f, interval)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)
}

func (s *Server) handleFilters(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Use a wide window (last 30 days) for filter dropdowns so the UI
	// can populate select boxes even without explicit time range.
	f := storage.QueryFilter{Start: time.Now().Add(-30 * 24 * time.Hour)}

	type filterValues struct {
		Providers []string `json:"providers"`
		Models    []string `json:"models"`
		Routes    []string `json:"routes"`
		Clients   []string `json:"client_ids"`
	}
	out := filterValues{}

	provs, err := s.store.DistinctValues(ctx, "provider", f)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out.Providers = provs

	models, err := s.store.DistinctValues(ctx, "model", f)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out.Models = models

	routes, err := s.store.DistinctValues(ctx, "route", f)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out.Routes = routes

	clients, err := s.store.DistinctValues(ctx, "client_id", f)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out.Clients = clients

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
}

// ErrStoreNotConfigured is returned when admin endpoints are hit before storage
// has been wired up.
var ErrStoreNotConfigured = errors.New("storage not configured")
