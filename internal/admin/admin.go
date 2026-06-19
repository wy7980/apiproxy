// Package admin provides a separate HTTP server that serves a dashboard UI
// and JSON API for inspecting recorded request statistics and managing config.
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wangyong/apiproxy/internal/config"
	"github.com/wangyong/apiproxy/internal/storage"
	"gopkg.in/yaml.v3"
)

// Reloader is the interface the admin server uses to hot-reload the proxy.
type Reloader interface {
	Reload(cfg *config.Config) error
	CurrentConfig() *config.Config
}

// Server is the admin dashboard HTTP server.
type Server struct {
	store      *storage.Store
	logger     *slog.Logger
	mux        *http.ServeMux
	configPath string
	reloader   Reloader
}

// New constructs an admin server bound to the given SQLite store and config
// reloader. configPath is the YAML file path used for atomic config writes.
func New(store *storage.Store, logger *slog.Logger, configPath string, r Reloader) *Server {
	s := &Server{
		store:      store,
		logger:     logger,
		mux:        http.NewServeMux(),
		configPath: configPath,
		reloader:   r,
	}
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/api/summary", s.handleSummary)
	s.mux.HandleFunc("/api/percentiles", s.handlePercentiles)
	s.mux.HandleFunc("/api/buckets", s.handleBuckets)
	s.mux.HandleFunc("/api/timeseries", s.handleTimeseries)
	s.mux.HandleFunc("/api/filters", s.handleFilters)
	s.mux.HandleFunc("/api/config", s.handleConfigAPI)
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

// handleConfigAPI dispatches GET (read current config) and PUT (write + reload).
func (s *Server) handleConfigAPI(w http.ResponseWriter, r *http.Request) {
	if s.reloader == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "config management not available")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleGetConfig(w, r)
	case http.MethodPut:
		s.handlePutConfig(w, r)
	default:
		w.Header().Set("Allow", "GET, PUT")
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// maskedAPIKey is the placeholder value sent to the UI for any provider with
// a non-empty API key. On PUT, the server restores the real key when the UI
// sends back the same placeholder.
const maskedAPIKey = "***"

// configProviderJSON is the wire format for provider entries in /api/config.
type configProviderJSON struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	BaseURL    string `json:"base_url"`
	APIKey     string `json:"api_key"`
	APIKeyEnv  string `json:"api_key_env"`
	AuthHeader string `json:"auth_header"`
	Timeout    string `json:"timeout"`
	Tier       string `json:"tier"`
}

// configRouteProviderJSON is the wire format for route provider targets.
type configRouteProviderJSON struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Tier     string `json:"tier"`
	Weight   int    `json:"weight"`
}

// configFallbackJSON is the wire format for fallback policy.
type configFallbackJSON struct {
	Enabled        bool    `json:"enabled"`
	MaxAttempts    int     `json:"max_attempts"`
	OnStatus       []int   `json:"on_status"`
	OnTimeout      bool    `json:"on_timeout"`
	OnConnectError bool    `json:"on_connect_error"`
	AllowDowngrade bool    `json:"allow_downgrade"`
}

// configRouteJSON is the wire format for a route entry.
type configRouteJSON struct {
	Name      string                  `json:"name"`
	Strategy  string                  `json:"strategy"`
	Fallback  configFallbackJSON      `json:"fallback"`
	Providers []configRouteProviderJSON `json:"providers"`
}

// configResponseJSON is the full wire format returned by GET /api/config.
type configResponseJSON struct {
	Providers []configProviderJSON `json:"providers"`
	Routes    []configRouteJSON    `json:"routes"`
}

func (s *Server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	cfg := s.reloader.CurrentConfig()
	if cfg == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "no config loaded")
		return
	}

	resp := configResponseJSON{}
	for name, p := range cfg.Providers {
		key := p.APIKey
		if key == "" && p.APIKeyEnv != "" {
			key = os.Getenv(p.APIKeyEnv)
		}
		display := ""
		if key != "" {
			display = maskedAPIKey
		}
		resp.Providers = append(resp.Providers, configProviderJSON{
			Name:       name,
			Type:       p.Type,
			BaseURL:    p.BaseURL,
			APIKey:     display,
			APIKeyEnv:  p.APIKeyEnv,
			AuthHeader: p.AuthHeader,
			Timeout:    p.Timeout.String(),
			Tier:       p.Tier,
		})
	}
	for name, r := range cfg.Routes {
		rt := configRouteJSON{
			Name:     name,
			Strategy: r.Strategy,
			Fallback: configFallbackJSON{
				Enabled:        r.Fallback.Enabled,
				MaxAttempts:    r.Fallback.MaxAttempts,
				OnStatus:       append([]int(nil), r.Fallback.OnStatus...),
				OnTimeout:      r.Fallback.OnTimeout,
				OnConnectError: r.Fallback.OnConnectError,
				AllowDowngrade: r.Fallback.AllowDowngrade,
			},
		}
		for _, t := range r.Providers {
			rt.Providers = append(rt.Providers, configRouteProviderJSON{
				Provider: t.Provider,
				Model:    t.Model,
				Tier:     t.Tier,
				Weight:   t.Weight,
			})
		}
		resp.Routes = append(resp.Routes, rt)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var in configResponseJSON
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20)) // 4 MiB limit
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	if err := json.Unmarshal(body, &in); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	current := s.reloader.CurrentConfig()

	// Build a new Config from the wire payload.
	newCfg := config.Config{
		Server:         current.Server,
		Admin:          current.Admin,
		Auth:           current.Auth,
		CircuitBreaker: current.CircuitBreaker,
		Metrics:        current.Metrics,
		Logging:        current.Logging,
		Storage:        current.Storage,
		Providers:      map[string]config.Provider{},
		Routes:         map[string]config.Route{},
	}

	for _, p := range in.Providers {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			writeJSONError(w, http.StatusBadRequest, "provider name is required")
			return
		}
		timeout, err := time.ParseDuration(p.Timeout)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("provider %q: invalid timeout %q: %v", name, p.Timeout, err))
			return
		}
		// Restore masked keys from the running config when unchanged.
		key := p.APIKey
		if key == maskedAPIKey {
			key = current.ProviderAPIKey(name)
		}
		newCfg.Providers[name] = config.Provider{
			Type:       p.Type,
			BaseURL:    p.BaseURL,
			APIKey:     key,
			APIKeyEnv:  p.APIKeyEnv,
			Timeout:    timeout,
			Tier:       p.Tier,
			AuthHeader: p.AuthHeader,
		}
	}

	for _, rt := range in.Routes {
		name := strings.TrimSpace(rt.Name)
		if name == "" {
			writeJSONError(w, http.StatusBadRequest, "route name is required")
			return
		}
		r := config.Route{
			Strategy: rt.Strategy,
			Fallback: config.FallbackConfig{
				Enabled:        rt.Fallback.Enabled,
				MaxAttempts:    rt.Fallback.MaxAttempts,
				OnStatus:       append([]int(nil), rt.Fallback.OnStatus...),
				OnTimeout:      rt.Fallback.OnTimeout,
				OnConnectError: rt.Fallback.OnConnectError,
				AllowDowngrade: rt.Fallback.AllowDowngrade,
			},
		}
		for _, t := range rt.Providers {
			r.Providers = append(r.Providers, config.RouteTarget{
				Provider: t.Provider,
				Model:    t.Model,
				Tier:     t.Tier,
				Weight:   t.Weight,
			})
		}
		newCfg.Routes[name] = r
	}

	// Validate before touching disk.
	if err := newCfg.Validate(); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	newCfg.ApplyDefaults()

	// Marshal to YAML and write atomically (temp file + rename).
	out, err := yaml.Marshal(toYAMLConfig(&newCfg))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "marshal config: "+err.Error())
		return
	}
	if err := atomicWriteFile(s.configPath, out, 0o644); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "write config: "+err.Error())
		return
	}

	// Re-load from disk to get the same parsing path as bootstrap.
	fresh, err := config.Load(s.configPath)
	if err != nil {
		// Best effort: keep the running config intact.
		writeJSONError(w, http.StatusInternalServerError, "reload parse failed (running config unchanged): "+err.Error())
		return
	}
	if err := s.reloader.Reload(fresh); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "reload failed: "+err.Error())
		return
	}

	s.logger.Info("config updated via admin API", "providers", len(newCfg.Providers), "routes", len(newCfg.Routes))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"message": "config reloaded",
	})
}

// atomicWriteFile writes data to path via a temp file in the same directory,
// then renames it into place. Rename is atomic on POSIX filesystems.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".cfg-*.yaml.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		// Best effort cleanup if anything failed before rename.
		if _, statErr := os.Stat(tmpName); statErr == nil {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// toYAMLConfig converts the runtime Config back into the YAML-friendly shape.
// It preserves user-facing fields (api_key_env, auth_header, tier) and omits
// empty API keys so the YAML file stays clean.
func toYAMLConfig(c *config.Config) *yamlConfig {
	out := &yamlConfig{
		Server:         c.Server,
		Admin:          c.Admin,
		Auth:           c.Auth,
		CircuitBreaker: c.CircuitBreaker,
		Metrics:        c.Metrics,
		Logging:        c.Logging,
		Storage:        c.Storage,
		Providers:      map[string]config.Provider{},
		Routes:         map[string]config.Route{},
	}
	for name, p := range c.Providers {
		out.Providers[name] = p
	}
	for name, r := range c.Routes {
		out.Routes[name] = r
	}
	return out
}

// yamlConfig is a thin wrapper around config.Config with yaml tags that match
// the on-disk layout. Embedding config.Config directly also works, but this
// makes the YAML layout explicit and keeps us independent of future struct
// changes to the yaml tags.
type yamlConfig = config.Config

// ErrStoreNotConfigured is returned when admin endpoints are hit before storage
// has been wired up.
var ErrStoreNotConfigured = errors.New("storage not configured")
