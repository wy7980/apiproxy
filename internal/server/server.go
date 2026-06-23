package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/wangyong/apiproxy/internal/api"
	"github.com/wangyong/apiproxy/internal/auth"
	"github.com/wangyong/apiproxy/internal/breaker"
	"github.com/wangyong/apiproxy/internal/config"
	"github.com/wangyong/apiproxy/internal/fallback"
	"github.com/wangyong/apiproxy/internal/metrics"
	"github.com/wangyong/apiproxy/internal/provider"
	"github.com/wangyong/apiproxy/internal/provider/anthropic"
	"github.com/wangyong/apiproxy/internal/router"
	"github.com/wangyong/apiproxy/internal/storage"
)

// snapshot holds the immutable runtime state that is atomically swapped on
// config reload. Each request handler loads the snapshot once at entry and
// reads only from that snapshot — in-flight requests keep using the old one,
// new requests pick up the new one.
type snapshot struct {
	cfg       *config.Config
	router    *router.Router
	authStore *auth.KeyStore
	providers map[string]provider.Provider
}

type Server struct {
	snap    atomic.Pointer[snapshot]
	logger  *slog.Logger
	breaker *breaker.Breaker
	store   storage.EventWriter
}

func New(cfg *config.Config, logger *slog.Logger) (*Server, error) {
	snap, err := buildSnapshot(cfg)
	if err != nil {
		return nil, err
	}
	s := &Server{
		logger:  logger,
		breaker: breaker.New(),
		store:   storage.NoopWriter{},
	}
	s.snap.Store(snap)
	return s, nil
}

// buildSnapshot constructs an immutable snapshot from the given config.
func buildSnapshot(cfg *config.Config) (*snapshot, error) {
	authStore := buildAuthStore(cfg)

	providers := make(map[string]provider.Provider, len(cfg.Providers))
	for name, p := range cfg.Providers {
		apiKey := cfg.ProviderAPIKey(name)
		httpClient := &http.Client{Timeout: p.Timeout}
		// Single transparent provider: protocol (OpenAI vs Anthropic) is decided
		// by the client's request path, not by a static type field. One provider
		// serves both /v1/chat/completions and /v1/messages.
		providers[name] = anthropic.New(provider.Config{
			Name:       name,
			BaseURL:    p.BaseURL,
			APIKey:     apiKey,
			Timeout:    p.Timeout,
			AuthHeader: p.AuthHeader,
		}, httpClient)
	}

	return &snapshot{
		cfg:       cfg,
		router:    router.New(cfg.Routes),
		authStore: authStore,
		providers: providers,
	}, nil
}

// Reload atomically swaps the runtime snapshot with one built from newCfg.
// In-flight requests continue using the old snapshot; new requests pick up
// the new one.
func (s *Server) Reload(cfg *config.Config) error {
	snap, err := buildSnapshot(cfg)
	if err != nil {
		return fmt.Errorf("build snapshot: %w", err)
	}
	s.snap.Store(snap)
	s.logger.Info("config reloaded",
		"providers", len(cfg.Providers),
		"routes", len(cfg.Routes))
	return nil
}

// CurrentConfig returns the config from the active snapshot.
func (s *Server) CurrentConfig() *config.Config {
	return s.snap.Load().cfg
}

// WithStore attaches a storage writer for durable request recording.
func (s *Server) WithStore(w storage.EventWriter) *Server {
	s.store = w
	return s
}

// NewWithProviders builds a Server using pre-constructed providers (for testing).
func NewWithProviders(cfg *config.Config, logger *slog.Logger, provs map[string]provider.Provider) *Server {
	snap := &snapshot{
		cfg:       cfg,
		router:    router.New(cfg.Routes),
		authStore: buildAuthStore(cfg),
		providers: provs,
	}
	s := &Server{
		logger:  logger,
		breaker: breaker.New(),
		store:   storage.NoopWriter{},
	}
	s.snap.Store(snap)
	return s
}

func (s *Server) Routes() http.Handler {
	snap := s.snap.Load()
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)

	if snap.cfg.Metrics.Prometheus.Enabled {
		path := snap.cfg.Metrics.Prometheus.Path
		mux.Handle(path, promhttp.Handler())
	}

	mux.HandleFunc("/v1/chat/completions", s.withMiddleware(s.handleChatCompletions))
	mux.HandleFunc("/v1/messages", s.withMiddleware(s.handleMessages))
	mux.HandleFunc("/v1/messages/", s.withMiddleware(s.handleMessages))
	mux.HandleFunc("/v1/models", s.withMiddleware(s.handleModels))

	return mux
}

func (s *Server) withMiddleware(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snap := s.snap.Load()
		w.Header().Set("X-Apiproxy", "1")
		clientID := "anonymous"
		if snap.cfg.Auth.Enabled {
			// Try OpenAI-style Bearer auth, then fall back to Anthropic-style x-api-key.
			id, ok := snap.authStore.Authenticate(r)
			if !ok {
				id, ok = snap.authStore.AuthenticateAnthropic(r)
			}
			if !ok {
				writeJSON(w, http.StatusUnauthorized, map[string]any{
					"error": map[string]any{
						"type":    "invalid_api_key",
						"message": "missing or invalid API key",
					},
				})
				return
			}
			clientID = id
		}
		r = withClientID(r, clientID)
		h(w, r)
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleModels(w http.ResponseWriter, _ *http.Request) {
	snap := s.snap.Load()
	models := make([]map[string]any, 0, len(snap.cfg.Routes))
	for name := range snap.cfg.Routes {
		models = append(models, map[string]any{
			"id": name, "object": "model", "owned_by": "apiproxy",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": models})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}

	clientID := clientIDFromContext(r.Context())
	requestID := newRequestID()
	start := time.Now()

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		status := http.StatusBadRequest
		msg := err.Error()
		if errors.Is(err, http.ErrBodyReadAfterClose) || strings.Contains(msg, "http: request body too large") {
			status = http.StatusRequestEntityTooLarge
			msg = "request body too large"
		}
		writeOpenAIError(w, status, "read_body_error", msg)
		return
	}

	parsed, err := api.ParseChatCompletionRequest(body)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	snap := s.snap.Load()
	resolved, err := snap.router.Resolve(parsed.Model)
	if err != nil {
		writeOpenAIError(w, http.StatusNotFound, "model_not_found", err.Error())
		return
	}

	if parsed.Stream {
		s.handleStream(w, r, requestID, clientID, resolved, body, parsed, start)
		return
	}
	s.handleNonStream(w, r, requestID, clientID, resolved, body, parsed, start)
}

// handleNonStream runs the priority fallback chain for non-streaming requests.
func (s *Server) handleNonStream(
	w http.ResponseWriter, r *http.Request,
	requestID, clientID string,
	resolved *router.ResolvedRoute,
	body []byte,
	parsed api.ChatCompletionRequest,
	start time.Time,
) {
	targets := resolved.OrderedTargets()
	maxAttempts := resolved.Fallback.MaxAttempts
	if maxAttempts <= 0 || maxAttempts > len(targets) {
		maxAttempts = len(targets)
	}

	var lastErr *provider.Error
	var fallbackFrom, fallbackTo string

	for i := 0; i < maxAttempts; i++ {
		target := targets[i]
	snap := s.snap.Load()
		prov, ok := snap.providers[target.Provider]
		if !ok {
			continue
		}
		if !s.breaker.Allow(breakerKey(target.Provider, target.Model)) {
			lastErr = &provider.Error{Kind: provider.KindServerError, Message: "circuit open", StatusCode: 503}
			continue
		}

		// Rewrite the model to the upstream target model.
		upstreamBody, err := api.ReplaceModel(body, target.Model)
		if err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), s.providerTimeout(snap, target.Provider))
		chReq := &provider.ChatRequest{Body: upstreamBody, Header: r.Header, Path: r.URL.Path, Model: target.Model, Stream: false}
		resp, perr := prov.Chat(ctx, chReq)
		cancel()

		if i > 0 && fallbackFrom == "" {
			fallbackFrom = targets[0].Provider + ":" + targets[0].Model
		}

		if perr == nil && resp.Err == nil {
			fallbackTo = target.Provider + ":" + target.Model
			for k, v := range resp.Header {
				if strings.EqualFold(k, "Content-Length") || strings.EqualFold(k, "Content-Encoding") {
					continue
				}
				w.Header()[k] = v
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Request-Id", requestID)
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(resp.Body)

			s.logger.Debug("openai proxy downstream response (non-stream)",
				"request_id", requestID, "client_id", clientID,
				"provider", target.Provider, "model", target.Model,
				"status", resp.StatusCode, "body_bytes", len(resp.Body),
				"resp_body", truncStr(string(resp.Body), maxLogBody))

			metrics.RecordRequest(metrics.RequestLog{
				RequestID:        requestID,
				ClientID:         clientID,
				Route:            resolved.Name,
				Provider:         target.Provider,
				Model:            target.Model,
				StatusCode:       resp.StatusCode,
				LatencyMs:        float64(time.Since(start).Milliseconds()),
				PromptTokens:     resp.Usage.PromptTokens,
				CompletionTokens: resp.Usage.CompletionTokens,
				TotalTokens:      resp.Usage.TotalTokens,
				FallbackCount:    i,
				FallbackFrom:     fallbackFrom,
				FallbackTo:       fallbackTo,
				Stream:           false,
			})
			s.recordEvent(storage.Event{
				RequestID:        requestID,
				ClientID:         clientID,
				Route:            resolved.Name,
				Provider:         target.Provider,
				Model:            target.Model,
				StatusCode:       resp.StatusCode,
				LatencyMs:        float64(time.Since(start).Milliseconds()),
				PromptTokens:     resp.Usage.PromptTokens,
				CompletionTokens: resp.Usage.CompletionTokens,
				TotalTokens:      resp.Usage.TotalTokens,
				FallbackCount:    i,
				FallbackFrom:     fallbackFrom,
				FallbackTo:       fallbackTo,
				Stream:           false,
			})
			return
		}

		if perr != nil {
			lastErr = &provider.Error{Kind: provider.KindConnectError, Message: perr.Error(), Cause: perr}
		} else {
			lastErr = resp.Err
		}

		if !fallback.ShouldFallback(resolved.Fallback, lastErr) {
			break
		}
	}

	statusCode := http.StatusBadGateway
	errType := "upstream_error"
	errMsg := "all providers failed"
	if lastErr != nil {
		if lastErr.StatusCode != 0 {
			statusCode = lastErr.StatusCode
		}
		errMsg = lastErr.Message
		errType = string(lastErr.Kind)
	}
	writeOpenAIError(w, statusCode, errType, errMsg)
}

// handleStream proxies a streaming response and applies fallback only before the first chunk is sent.
func (s *Server) handleStream(
	w http.ResponseWriter, r *http.Request,
	requestID, clientID string,
	resolved *router.ResolvedRoute,
	body []byte,
	parsed api.ChatCompletionRequest,
	start time.Time,
) {
	targets := resolved.OrderedTargets()
	maxAttempts := resolved.Fallback.MaxAttempts
	if maxAttempts <= 0 || maxAttempts > len(targets) {
		maxAttempts = len(targets)
	}

	var firstTokenTime time.Time
	var fallbackFrom, fallbackTo string
	var promptTokens, completionTokens int

	for i := 0; i < maxAttempts; i++ {
		target := targets[i]
	snap := s.snap.Load()
		prov, ok := snap.providers[target.Provider]
		if !ok {
			continue
		}
		if !s.breaker.Allow(breakerKey(target.Provider, target.Model)) {
			continue
		}

		upstreamBody, err := api.ReplaceModel(body, target.Model)
		if err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		upstreamBody, err = api.InjectStreamUsageOptions(upstreamBody)
		if err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), s.providerTimeout(snap, target.Provider))
		chReq := &provider.ChatRequest{Body: upstreamBody, Header: r.Header, Path: r.URL.Path, Model: target.Model, Stream: true}
		ch, perr := prov.ChatStream(ctx, chReq)

		if perr != nil {
			cancel()
			s.logger.Warn("stream open failed",
				"request_id", requestID, "provider", target.Provider, "err", perr.Error())
			if i == 0 {
				fallbackFrom = target.Provider + ":" + target.Model
			}
			continue
		}

		// We have a stream. Once we write the first chunk we lose fallback ability.
		flusher, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Request-Id", requestID)
		w.WriteHeader(http.StatusOK)

		fallbackTo = target.Provider + ":" + target.Model
		var streamFailed bool
		for chunk := range ch {
			if chunk.Err != nil {
				streamFailed = true
				s.logger.Warn("stream chunk error",
					"request_id", requestID, "provider", target.Provider, "err", chunk.Err.Error())
				break
			}
			if firstTokenTime.IsZero() {
				firstTokenTime = time.Now()
			}
			// Extract usage from the final chunk if present.
			promptTokens, completionTokens = maybeExtractUsage(chunk.Data, promptTokens, completionTokens)
			_, _ = w.Write(chunk.Data)
			if flusher != nil {
				flusher.Flush()
			}
		}
		cancel()

		if !streamFailed {
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
			latMs := float64(time.Since(start).Milliseconds())
			ftMs := float64(firstTokenTime.Sub(start).Milliseconds())
			metrics.RecordRequest(metrics.RequestLog{
				RequestID:        requestID,
				ClientID:         clientID,
				Route:            resolved.Name,
				Provider:         target.Provider,
				Model:            target.Model,
				StatusCode:       http.StatusOK,
				LatencyMs:        latMs,
				FirstTokenMs:     ftMs,
				PromptTokens:     promptTokens,
				CompletionTokens: completionTokens,
				FallbackCount:    i,
				FallbackFrom:     fallbackFrom,
				FallbackTo:       fallbackTo,
				Stream:           true,
			})
			s.recordEvent(storage.Event{
				RequestID:        requestID,
				ClientID:         clientID,
				Route:            resolved.Name,
				Provider:         target.Provider,
				Model:            target.Model,
				StatusCode:       http.StatusOK,
				LatencyMs:        latMs,
				FirstTokenMs:     ftMs,
				PromptTokens:     promptTokens,
				CompletionTokens: completionTokens,
				TotalTokens:      promptTokens + completionTokens,
				FallbackCount:    i,
				FallbackFrom:     fallbackFrom,
				FallbackTo:       fallbackTo,
				Stream:           true,
			})
			return
		}

		// Stream failed mid-way: we already wrote a 200, so we cannot fallback.
		latMs := float64(time.Since(start).Milliseconds())
		ftMs := float64(firstTokenTime.Sub(start).Milliseconds())
		metrics.RecordRequest(metrics.RequestLog{
			RequestID:        requestID,
			ClientID:         clientID,
			Route:            resolved.Name,
			Provider:         target.Provider,
			Model:            target.Model,
			StatusCode:       http.StatusOK,
			LatencyMs:        latMs,
			FirstTokenMs:     ftMs,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			FallbackCount:    i,
			FallbackFrom:     fallbackFrom,
			FallbackTo:       fallbackTo,
			Stream:           true,
			ErrorType:        "stream_error",
		})
		s.recordEvent(storage.Event{
			RequestID:        requestID,
			ClientID:         clientID,
			Route:            resolved.Name,
			Provider:         target.Provider,
			Model:            target.Model,
			StatusCode:       http.StatusOK,
			LatencyMs:        latMs,
			FirstTokenMs:     ftMs,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
			FallbackCount:    i,
			FallbackFrom:     fallbackFrom,
			FallbackTo:       fallbackTo,
			Stream:           true,
			ErrorType:        "stream_error",
		})
		return
	}

	writeOpenAIError(w, http.StatusBadGateway, "upstream_error", "all providers failed for stream")
}

// ---------- Anthropic /v1/messages handler (transparent proxy) ----------

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAnthropicError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
		return
	}

	clientID := clientIDFromContext(r.Context())
	requestID := newRequestID()
	start := time.Now()

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		status := http.StatusBadRequest
		msg := err.Error()
		if strings.Contains(msg, "http: request body too large") {
			status = http.StatusRequestEntityTooLarge
			msg = "request body too large"
		}
		writeAnthropicError(w, status, "invalid_request_error", msg)
		return
	}

	// Debug: log the full inbound request from Claude Code (headers + body).
	s.logger.Debug("anthropic client request",
		"request_id", requestID, "client_id", clientID,
		"client_method", r.Method, "client_path", r.URL.Path,
		"client_headers", redactHeaders(r.Header),
		"client_body", truncStr(string(body), maxLogBody))

	// Parse just enough to route: model and stream flag.
	parsed, err := api.ParseAnthropicMessagesRequest(body)
	if err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	snap := s.snap.Load()
	resolved, err := snap.router.Resolve(parsed.Model)
	if err != nil {
		writeAnthropicError(w, http.StatusNotFound, "not_found_error", err.Error())
		return
	}

	if parsed.Stream {
		s.handleAnthropicStream(w, r, requestID, clientID, resolved, body, start)
		return
	}
	s.handleAnthropicNonStream(w, r, requestID, clientID, resolved, body, start)
}

// handleAnthropicNonStream runs the fallback chain for Anthropic non-streaming
// requests. The request body is forwarded verbatim — no format conversion.
func (s *Server) handleAnthropicNonStream(
	w http.ResponseWriter, r *http.Request,
	requestID, clientID string,
	resolved *router.ResolvedRoute,
	body []byte,
	start time.Time,
) {
	targets := resolved.OrderedTargets()
	maxAttempts := resolved.Fallback.MaxAttempts
	if maxAttempts <= 0 || maxAttempts > len(targets) {
		maxAttempts = len(targets)
	}

	var lastErr *provider.Error
	var fallbackFrom, fallbackTo string

	for i := 0; i < maxAttempts; i++ {
		target := targets[i]
	snap := s.snap.Load()
		prov, ok := snap.providers[target.Provider]
		if !ok {
			continue
		}
		if !s.breaker.Allow(breakerKey(target.Provider, target.Model)) {
			lastErr = &provider.Error{Kind: provider.KindServerError, Message: "circuit open", StatusCode: 503}
			continue
		}

		// Rewrite the model name in the body so the upstream gets the right model.
		upstreamBody, err := api.ReplaceModel(body, target.Model)
		if err != nil {
			writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), s.providerTimeout(snap, target.Provider))
		chReq := &provider.ChatRequest{Body: upstreamBody, Header: r.Header, Path: r.URL.Path, Model: target.Model, Stream: false}
		resp, perr := prov.Chat(ctx, chReq)
		cancel()

		if i > 0 && fallbackFrom == "" {
			fallbackFrom = targets[0].Provider + ":" + targets[0].Model
		}

		if perr == nil && resp.Err == nil {
			fallbackTo = target.Provider + ":" + target.Model
			// Transparent: forward the upstream response as-is.
			for k, v := range resp.Header {
				if strings.EqualFold(k, "Content-Length") || strings.EqualFold(k, "Content-Encoding") {
					continue
				}
				w.Header()[k] = v
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Request-Id", requestID)
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(resp.Body)

			s.logger.Debug("anthropic proxy downstream response (non-stream)",
				"request_id", requestID, "client_id", clientID,
				"provider", target.Provider, "model", target.Model,
				"status", resp.StatusCode, "body_bytes", len(resp.Body),
				"resp_body", truncStr(string(resp.Body), maxLogBody))

			metrics.RecordRequest(metrics.RequestLog{
				RequestID:        requestID,
				ClientID:         clientID,
				Route:            resolved.Name,
				Provider:         target.Provider,
				Model:            target.Model,
				StatusCode:       resp.StatusCode,
				LatencyMs:        float64(time.Since(start).Milliseconds()),
				PromptTokens:     resp.Usage.PromptTokens,
				CompletionTokens: resp.Usage.CompletionTokens,
				TotalTokens:      resp.Usage.TotalTokens,
				FallbackCount:    i,
				FallbackFrom:     fallbackFrom,
				FallbackTo:       fallbackTo,
				Stream:           false,
			})
			s.recordEvent(storage.Event{
				RequestID:        requestID,
				ClientID:         clientID,
				Route:            resolved.Name,
				Provider:         target.Provider,
				Model:            target.Model,
				StatusCode:       resp.StatusCode,
				LatencyMs:        float64(time.Since(start).Milliseconds()),
				PromptTokens:     resp.Usage.PromptTokens,
				CompletionTokens: resp.Usage.CompletionTokens,
				TotalTokens:      resp.Usage.TotalTokens,
				FallbackCount:    i,
				FallbackFrom:     fallbackFrom,
				FallbackTo:       fallbackTo,
				Stream:           false,
			})
			return
		}

		if perr != nil {
			lastErr = &provider.Error{Kind: provider.KindConnectError, Message: perr.Error(), Cause: perr}
		} else {
			lastErr = resp.Err
		}

		if !fallback.ShouldFallback(resolved.Fallback, lastErr) {
			break
		}
	}

	statusCode := http.StatusBadGateway
	errType := "api_error"
	errMsg := "all providers failed"
	if lastErr != nil {
		if lastErr.StatusCode != 0 {
			statusCode = lastErr.StatusCode
		}
		errMsg = lastErr.Message
		switch lastErr.Kind {
		case provider.KindRateLimited:
			errType = "rate_limit_error"
		case provider.KindTimeout:
			errType = "timeout_error"
		case provider.KindConnectError:
			errType = "connection_error"
		}
	}
	writeAnthropicError(w, statusCode, errType, errMsg)
}

// handleAnthropicStream proxies a streaming Anthropic response.
// SSE chunks from the upstream Anthropic provider are forwarded verbatim —
// no OpenAI→Anthropic format conversion.
func (s *Server) handleAnthropicStream(
	w http.ResponseWriter, r *http.Request,
	requestID, clientID string,
	resolved *router.ResolvedRoute,
	body []byte,
	start time.Time,
) {
	targets := resolved.OrderedTargets()
	maxAttempts := resolved.Fallback.MaxAttempts
	if maxAttempts <= 0 || maxAttempts > len(targets) {
		maxAttempts = len(targets)
	}

	var firstTokenTime time.Time
	var fallbackFrom, fallbackTo string
	var inputTokens, outputTokens int

	for i := 0; i < maxAttempts; i++ {
		target := targets[i]
	snap := s.snap.Load()
		prov, ok := snap.providers[target.Provider]
		if !ok {
			continue
		}
		if !s.breaker.Allow(breakerKey(target.Provider, target.Model)) {
			continue
		}

		upstreamBody, err := api.ReplaceModel(body, target.Model)
		if err != nil {
			writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
		upstreamBody, err = api.InjectStreamUsageOptions(upstreamBody)
		if err != nil {
			writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), s.providerTimeout(snap, target.Provider))
		chReq := &provider.ChatRequest{Body: upstreamBody, Header: r.Header, Path: r.URL.Path, Model: target.Model, Stream: true}
		ch, perr := prov.ChatStream(ctx, chReq)

		if perr != nil {
			cancel()
			s.logger.Warn("stream open failed",
				"request_id", requestID, "provider", target.Provider, "err", perr.Error())
			if i == 0 {
				fallbackFrom = target.Provider + ":" + target.Model
			}
			continue
		}

		// Do not write the downstream 200 until the first upstream SSE chunk
		// arrives. Some broken gateways return HTTP 200 with an empty body; if we
		// forwarded that as-is, Claude Code reports "empty or malformed response".
		flusher, _ := w.(http.Flusher)

		fallbackTo = target.Provider + ":" + target.Model
		var streamFailed bool
		var wroteHeader bool

		for chunk := range ch {
			if chunk.Err != nil {
				streamFailed = true
				s.logger.Warn("stream chunk error",
					"request_id", requestID, "provider", target.Provider, "err", chunk.Err.Error())
				break
			}
			if !wroteHeader {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("Connection", "keep-alive")
				w.Header().Set("X-Request-Id", requestID)
				w.WriteHeader(http.StatusOK)
				wroteHeader = true
			}
			if firstTokenTime.IsZero() {
				firstTokenTime = time.Now()
			}
			// Extract usage from Anthropic SSE events (message_start / message_delta).
			inputTokens, outputTokens = maybeExtractUsageAnthropic(chunk.Data, inputTokens, outputTokens)
			_, _ = w.Write(chunk.Data)
			if flusher != nil {
				flusher.Flush()
			}
		}
		cancel()

		if !wroteHeader && !streamFailed {
			s.logger.Warn("anthropic upstream stream returned HTTP 200 with no SSE chunks",
				"request_id", requestID, "provider", target.Provider, "model", target.Model)
			lastErr := &provider.Error{Kind: provider.KindServerError, StatusCode: http.StatusBadGateway, Message: "upstream returned empty stream"}
			if !fallback.ShouldFallback(resolved.Fallback, lastErr) {
				writeAnthropicError(w, http.StatusBadGateway, "api_error", lastErr.Message)
				return
			}
			continue
		}

		if !streamFailed {
			latMs := float64(time.Since(start).Milliseconds())
			ftMs := float64(firstTokenTime.Sub(start).Milliseconds())
			metrics.RecordRequest(metrics.RequestLog{
				RequestID:        requestID,
				ClientID:         clientID,
				Route:            resolved.Name,
				Provider:         target.Provider,
				Model:            target.Model,
				StatusCode:       http.StatusOK,
				LatencyMs:        latMs,
				FirstTokenMs:     ftMs,
				PromptTokens:     inputTokens,
				CompletionTokens: outputTokens,
				FallbackCount:    i,
				FallbackFrom:     fallbackFrom,
				FallbackTo:       fallbackTo,
				Stream:           true,
			})
			s.recordEvent(storage.Event{
				RequestID:        requestID,
				ClientID:         clientID,
				Route:            resolved.Name,
				Provider:         target.Provider,
				Model:            target.Model,
				StatusCode:       http.StatusOK,
				LatencyMs:        latMs,
				FirstTokenMs:     ftMs,
				PromptTokens:     inputTokens,
				CompletionTokens: outputTokens,
				TotalTokens:      inputTokens + outputTokens,
				FallbackCount:    i,
				FallbackFrom:     fallbackFrom,
				FallbackTo:       fallbackTo,
				Stream:           true,
			})
			return
		}

		// Stream failed mid-way: we already wrote a 200, so we cannot fallback.
		latMs := float64(time.Since(start).Milliseconds())
		ftMs := float64(firstTokenTime.Sub(start).Milliseconds())
		metrics.RecordRequest(metrics.RequestLog{
			RequestID:        requestID,
			ClientID:         clientID,
			Route:            resolved.Name,
			Provider:         target.Provider,
			Model:            target.Model,
			StatusCode:       http.StatusOK,
			LatencyMs:        latMs,
			FirstTokenMs:     ftMs,
			PromptTokens:     inputTokens,
			CompletionTokens: outputTokens,
			FallbackCount:    i,
			FallbackFrom:     fallbackFrom,
			FallbackTo:       fallbackTo,
			Stream:           true,
			ErrorType:        "stream_error",
		})
		s.recordEvent(storage.Event{
			RequestID:        requestID,
			ClientID:         clientID,
			Route:            resolved.Name,
			Provider:         target.Provider,
			Model:            target.Model,
			StatusCode:       http.StatusOK,
			LatencyMs:        latMs,
			FirstTokenMs:     ftMs,
			PromptTokens:     inputTokens,
			CompletionTokens: outputTokens,
			TotalTokens:      inputTokens + outputTokens,
			FallbackCount:    i,
			FallbackFrom:     fallbackFrom,
			FallbackTo:       fallbackTo,
			Stream:           true,
			ErrorType:        "stream_error",
		})
		return
	}

	writeAnthropicError(w, http.StatusBadGateway, "api_error", "all providers failed for stream")
}

// writeAnthropicError writes an Anthropic-format error response.
func writeAnthropicError(w http.ResponseWriter, status int, errType, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(api.AnthropicErrorJSON(errType, msg))
}

// maybeExtractUsageAnthropic extracts usage from SSE chunks. It handles both
// Anthropic-style usage (input_tokens / output_tokens) and OpenAI-style usage
// (prompt_tokens / completion_tokens), and looks for the usage object both at
// the top level and nested under "message" — because a real Anthropic
// message_start event carries usage inside {"message":{"usage":{...}}} while
// message_delta and OpenAI-style chunks put it at the top level. This dual
// location/dual field-name handling is needed because a transparent proxy may
// forward to gateways that emit OpenAI-style SSE (e.g. DeepSeek via aipds) or
// to a real Anthropic-compatible upstream; without it the recorded token
// counts silently stay at zero.
func maybeExtractUsageAnthropic(chunk []byte, input, output int) (int, int) {
	if bytes.Index(chunk, []byte("\"usage\"")) < 0 {
		return input, output
	}
	// Find JSON data within the SSE chunk.
	dataStart := bytes.Index(chunk, []byte("data: "))
	if dataStart < 0 {
		return input, output
	}
	payload := chunk[dataStart+6:]
	if n := bytes.Index(payload, []byte("\n")); n >= 0 {
		payload = payload[:n]
	}
	payload = bytes.TrimSpace(payload)
	if bytes.Equal(payload, []byte("[DONE]")) {
		return input, output
	}
	// usageFields holds the four possible field names. The same object shape is
	// reused whether the usage sits at the top level or under "message".
	type usageFields struct {
		InputTokens      int `json:"input_tokens"`
		OutputTokens     int `json:"output_tokens"`
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	}
	var parsed struct {
		Usage   usageFields `json:"usage"`
		Message struct {
			Usage usageFields `json:"usage"`
		} `json:"message"`
	}
	if json.Unmarshal(payload, &parsed) != nil {
		return input, output
	}
	// A chunk may carry usage at either location. Prefer the first non-empty one.
	pick := func(u usageFields) (int, int) {
		in := u.InputTokens
		if in == 0 {
			in = u.PromptTokens
		}
		out := u.OutputTokens
		if out == 0 {
			out = u.CompletionTokens
		}
		return in, out
	}
	for _, u := range []usageFields{parsed.Usage, parsed.Message.Usage} {
		in, out := pick(u)
		if in == 0 && out == 0 {
			continue
		}
		if in > 0 {
			input = in
		}
		if out > 0 {
			output += out
		}
	}
	return input, output
}

// breakerKey is the circuit-breaker granularity key. Breaking on
// provider+model means a single downed model does not trip the breaker for
// other models served by the same provider (e.g. model A failing on a
// provider should still allow model B on that provider to serve traffic).
func breakerKey(provider, model string) string {
	return provider + "|" + model
}

func (s *Server) recordEvent(e storage.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.store.Record(ctx, e); err != nil {
		s.logger.Warn("storage record failed", "err", err)
	}
}

func (s *Server) providerTimeout(snap *snapshot, name string) time.Duration {
	p, ok := snap.cfg.Providers[name]
	if !ok || p.Timeout == 0 {
		return snap.cfg.Server.RequestTimeout
	}
	return p.Timeout
}

// ---------- helpers ----------

type contextKey string

const ctxKeyClientID contextKey = "client_id"

func withClientID(r *http.Request, id string) *http.Request {
	ctx := context.WithValue(r.Context(), ctxKeyClientID, id)
	return r.WithContext(ctx)
}

func clientIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyClientID).(string); ok {
		return v
	}
	return "anonymous"
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "req_" + hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeOpenAIError(w http.ResponseWriter, status int, errType, msg string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"type":    errType,
			"message": msg,
			"code":    status,
		},
	})
}

func maybeExtractUsage(chunk []byte, prompt, completion int) (int, int) {
	// SSE chunks look like "data: {...}\n\n". Try to find usage in the final chunk.
	idx := bytes.Index(chunk, []byte("\"usage\""))
	if idx < 0 {
		return prompt, completion
	}
	var parsed struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	// Try to extract just the JSON object.
	dataStart := bytes.Index(chunk, []byte("data: "))
	if dataStart < 0 {
		return prompt, completion
	}
	payload := chunk[dataStart+6:]
	if n := bytes.Index(payload, []byte("\n")); n >= 0 {
		payload = payload[:n]
	}
	payload = bytes.TrimSpace(payload)
	if bytes.Equal(payload, []byte("[DONE]")) {
		return prompt, completion
	}
	if json.Unmarshal(payload, &parsed) != nil {
		return prompt, completion
	}
	if parsed.Usage.PromptTokens > 0 {
		prompt = parsed.Usage.PromptTokens
	}
	if parsed.Usage.CompletionTokens > 0 {
		completion = parsed.Usage.CompletionTokens
	}
	return prompt, completion
}

// ---------- debug logging helpers ----------

const (
	maxLogBody = 4096
	// maxRequestBodyBytes caps inbound chat/messages bodies. LLM prompts can
	// be large (long context windows), so allow 32 MiB which comfortably
	// covers typical use while blocking unbounded memory growth.
	maxRequestBodyBytes = 32 << 20
)

func redactHeaders(h http.Header) map[string]string {
	const masked = "***REDACTED***"
	out := make(map[string]string, len(h))
	for k, vs := range h {
		switch strings.ToLower(k) {
		case "authorization", "x-api-key", "cookie", "set-cookie", "anthropic-auth-token":
			out[k] = masked
		default:
			out[k] = strings.Join(vs, ", ")
		}
	}
	return out
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("...(%d more bytes)", len(s)-n)
}

func buildAuthStore(cfg *config.Config) *auth.KeyStore {
	pairs := make([][2]string, 0, len(cfg.Auth.APIKeys))
	for _, k := range cfg.Auth.APIKeys {
		pairs = append(pairs, [2]string{k.Key, k.ClientID})
	}
	return auth.NewKeyStore(pairs)
}

var _ = errors.New
