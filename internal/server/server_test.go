package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wangyong/apiproxy/internal/breaker"
	"github.com/wangyong/apiproxy/internal/config"
	"github.com/wangyong/apiproxy/internal/provider"
)

// mockProvider is a controllable provider for testing fallback.
type mockProvider struct {
	name     string
	response *provider.ChatResponse
	lastReq  *provider.ChatRequest
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) Chat(_ context.Context, req *provider.ChatRequest) (*provider.ChatResponse, error) {
	m.lastReq = req
	return m.response, nil
}
func (m *mockProvider) ChatStream(_ context.Context, _ *provider.ChatRequest) (<-chan provider.StreamChunk, error) {
	return nil, nil
}

func testConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{Listen: ":0", RequestTimeout: 30 * time.Second},
		Auth:   config.AuthConfig{Enabled: true, APIKeys: []config.APIKey{{Key: "test-key", ClientID: "tester"}}},
		Providers: map[string]config.Provider{
			"p1": {Type: "openai", BaseURL: "https://p1.example.com/v1", Timeout: 10 * time.Second, Tier: "advanced"},
			"p2": {Type: "openai", BaseURL: "https://p2.example.com/v1", Timeout: 10 * time.Second, Tier: "advanced"},
		},
		Routes: map[string]config.Route{
			"chat": {
				Strategy: "priority",
				Fallback: config.FallbackConfig{
					Enabled:         true,
					MaxAttempts:     2,
					OnStatus:        []int{429, 500, 502, 503, 504},
					OnTimeout:       true,
					OnConnectError:  true,
					AllowDowngrade:  false,
				},
				Providers: []config.RouteTarget{
					{Provider: "p1", Model: "m1", Tier: "advanced"},
					{Provider: "p2", Model: "m2", Tier: "advanced"},
				},
			},
		},
		Metrics:    config.MetricsConfig{},
		Logging:    config.LoggingConfig{Level: "info", Format: "json"},
	}
}

func TestHealthz(t *testing.T) {
	srv := NewWithProviders(testConfig(), slog.Default(), nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("healthz status = %d", w.Code)
	}
	var body map[string]string
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "ok" {
		t.Fatalf("healthz body = %v", body)
	}
}

func TestAuthRequired(t *testing.T) {
	srv := NewWithProviders(testConfig(), slog.Default(), nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("unauth status = %d", w.Code)
	}
}

func TestModels(t *testing.T) {
	srv := NewWithProviders(testConfig(), slog.Default(), nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("models status = %d", w.Code)
	}
}

func TestModelNotFound(t *testing.T) {
	srv := NewWithProviders(testConfig(), slog.Default(), nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"missing","messages":[]}`)))
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("missing model status = %d", w.Code)
	}
}

func TestChatSuccess(t *testing.T) {
	cfg := testConfig()
	prov := &mockProvider{
		name: "p1",
		response: &provider.ChatResponse{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       []byte(`{"id":"c1","choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`),
			Usage:      provider.Usage{PromptTokens: 5, CompletionTokens: 1, TotalTokens: 6},
		},
	}
	srv := NewWithProviders(cfg, slog.Default(), map[string]provider.Provider{"p1": prov})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"chat","messages":[{"role":"user","content":"hi"}]}`)))
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("chat status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestChatFallbackOnServerError(t *testing.T) {
	cfg := testConfig()
	p1 := &mockProvider{
		name: "p1",
		response: &provider.ChatResponse{
			StatusCode: 503,
			Body:       []byte(`{"error":{"message":"overloaded"}}`),
			Err:        &provider.Error{Kind: provider.KindServerError, StatusCode: 503, Message: "overloaded"},
		},
	}
	p2 := &mockProvider{
		name: "p2",
		response: &provider.ChatResponse{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       []byte(`{"id":"c2","choices":[{"message":{"content":"fallback"}}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`),
			Usage:      provider.Usage{PromptTokens: 5, CompletionTokens: 1, TotalTokens: 6},
		},
	}
	srv := NewWithProviders(cfg, slog.Default(), map[string]provider.Provider{"p1": p1, "p2": p2})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"chat","messages":[{"role":"user","content":"hi"}]}`)))
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("fallback status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestChatFallbackOnTimeout(t *testing.T) {
	cfg := testConfig()
	p1 := &mockProvider{
		name: "p1",
		response: &provider.ChatResponse{
			Err: &provider.Error{Kind: provider.KindTimeout, Message: "timeout"},
		},
	}
	p2 := &mockProvider{
		name: "p2",
		response: &provider.ChatResponse{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       []byte(`{"id":"c2","choices":[{"message":{"content":"ok"}}]}`),
		},
	}
	srv := NewWithProviders(cfg, slog.Default(), map[string]provider.Provider{"p1": p1, "p2": p2})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"chat","messages":[]}`)))
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("timeout fallback status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestChatAllProvidersFail(t *testing.T) {
	cfg := testConfig()
	p1 := &mockProvider{
		name: "p1",
		response: &provider.ChatResponse{
			Err: &provider.Error{Kind: provider.KindServerError, StatusCode: 500, Message: "fail"},
		},
	}
	p2 := &mockProvider{
		name: "p2",
		response: &provider.ChatResponse{
			Err: &provider.Error{Kind: provider.KindRateLimited, StatusCode: 429, Message: "rate limited"},
		},
	}
	srv := NewWithProviders(cfg, slog.Default(), map[string]provider.Provider{"p1": p1, "p2": p2})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"chat","messages":[]}`)))
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)
	if w.Code != 429 && w.Code != 500 {
		t.Fatalf("all fail status = %d, body = %s", w.Code, w.Body.String())
	}
}

// TestCircuitBreakerPerModel confirms the breaker is keyed by provider+model:
// with p1's model m1 open, a second target using the SAME provider p1 but a
// different model m1b must still be allowed to serve the request.
func TestCircuitBreakerPerModel(t *testing.T) {
	cfg := testConfig()
	cfg.Routes["chat"] = config.Route{
		Strategy: "priority",
		Fallback: config.FallbackConfig{
			Enabled:        true,
			MaxAttempts:    2,
			OnStatus:       []int{429, 500, 502, 503, 504},
			OnTimeout:      true,
			OnConnectError: true,
		},
		Providers: []config.RouteTarget{
			{Provider: "p1", Model: "m1", Tier: "advanced"},
			{Provider: "p1", Model: "m1b", Tier: "advanced"},
		},
	}
	p1b := &mockProvider{
		name: "p1",
		response: &provider.ChatResponse{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       []byte(`{"id":"c2","choices":[{"message":{"content":"ok"}}]}`),
		},
	}
	srv := NewWithProviders(cfg, slog.Default(), map[string]provider.Provider{"p1": p1b})
	srv.breaker.Set("p1|m1", breaker.Open) // m1 tripped, but p1|m1b must remain usable

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"chat","messages":[]}`)))
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("same-provider other-model status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestCircuitBreakerBlocksProvider(t *testing.T) {
	cfg := testConfig()
	p1 := &mockProvider{name: "p1"} // won't be called
	p2 := &mockProvider{
		name: "p2",
		response: &provider.ChatResponse{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       []byte(`{"id":"c2","choices":[{"message":{"content":"ok"}}]}`),
		},
	}
	srv := NewWithProviders(cfg, slog.Default(), map[string]provider.Provider{"p1": p1, "p2": p2})
	srv.breaker.Set("p1|m1", breaker.Open)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"chat","messages":[]}`)))
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("circuit breaker fallback status = %d, body = %s", w.Code, w.Body.String())
	}
}

// ---------- Anthropic /v1/messages tests ----------

func TestAnthropicAuthXApiKey(t *testing.T) {
	cfg := testConfig()
	srv := NewWithProviders(cfg, slog.Default(), nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("x-api-key", "test-key")
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("x-api-key auth status = %d, want 200", w.Code)
	}
}

func TestAnthropicAuthInvalidKey(t *testing.T) {
	cfg := testConfig()
	srv := NewWithProviders(cfg, slog.Default(), nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"chat","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`)))
	req.Header.Set("x-api-key", "bad-key")
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("bad x-api-key status = %d, want 401", w.Code)
	}
}

func TestAnthropicMessagesNonStream(t *testing.T) {
	cfg := testConfig()
	// The mock provider returns a raw Anthropic-format response body.
	// In transparent proxy mode, this is forwarded verbatim.
	anthropicBody := `{"id":"msg_123","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":"Hello!"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":2}}`
	p1 := &mockProvider{
		name: "p1",
		response: &provider.ChatResponse{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       []byte(anthropicBody),
			Usage:      provider.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12},
		},
	}
	srv := NewWithProviders(cfg, slog.Default(), map[string]provider.Provider{"p1": p1})
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"chat","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`)))
	req.Header.Set("x-api-key", "test-key")
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("messages non-stream status = %d, body = %s", w.Code, w.Body.String())
	}

	// Verify the response is the raw Anthropic body forwarded verbatim.
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	if resp["type"] != "message" {
		t.Fatalf("type = %v", resp["type"])
	}
	if resp["role"] != "assistant" {
		t.Fatalf("role = %v", resp["role"])
	}
	content := resp["content"].([]any)
	block := content[0].(map[string]any)
	if block["type"] != "text" || block["text"] != "Hello!" {
		t.Fatalf("content block = %v", block)
	}
	usage := resp["usage"].(map[string]any)
	if usage["input_tokens"].(float64) != 10 || usage["output_tokens"].(float64) != 2 {
		t.Fatalf("usage = %v", usage)
	}

	// Verify the request body was forwarded (model rewritten to upstream target).
	if p1.lastReq == nil {
		t.Fatal("provider did not receive request")
	}
	var sentBody map[string]any
	json.Unmarshal(p1.lastReq.Body, &sentBody)
	if sentBody["model"] != "m1" {
		t.Fatalf("upstream model = %v, want m1", sentBody["model"])
	}
}

func TestAnthropicMessagesFallback(t *testing.T) {
	cfg := testConfig()
	p1 := &mockProvider{
		name: "p1",
		response: &provider.ChatResponse{
			StatusCode: 503,
			Body:       []byte(`{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`),
			Err:        &provider.Error{Kind: provider.KindServerError, StatusCode: 503, Message: "overloaded"},
		},
	}
	anthropicFallback := `{"id":"msg_456","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":"fallback"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":1}}`
	p2 := &mockProvider{
		name: "p2",
		response: &provider.ChatResponse{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       []byte(anthropicFallback),
			Usage:      provider.Usage{PromptTokens: 5, CompletionTokens: 1, TotalTokens: 6},
		},
	}
	srv := NewWithProviders(cfg, slog.Default(), map[string]provider.Provider{"p1": p1, "p2": p2})
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"chat","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`)))
	req.Header.Set("x-api-key", "test-key")
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("messages fallback status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	content := resp["content"].([]any)[0].(map[string]any)
	if content["text"] != "fallback" {
		t.Fatalf("fallback content = %v", content)
	}
}

func TestAnthropicMessagesModelNotFound(t *testing.T) {
	cfg := testConfig()
	srv := NewWithProviders(cfg, slog.Default(), nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"missing","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`)))
	req.Header.Set("x-api-key", "test-key")
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)

	if w.Code != 404 {
		t.Fatalf("model not found status = %d", w.Code)
	}
	// Should return Anthropic-format error.
	var errResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &errResp)
	if errResp["type"] != "error" {
		t.Fatalf("error type = %v", errResp["type"])
	}
}

// ---------- Reload tests ----------

// TestReload_SwapsProviders verifies that calling Reload with a new config
// causes subsequent requests to be served by the newly-configured providers.
// The config goes through the real buildSnapshot path (constructs openai/
// anthropic providers via HTTP), so we cannot inject mocks — instead we
// verify the proxy stops talking to the old provider. We do this by pointing
// the new config at a provider whose HTTP server returns a distinctive body
// and asserting the response matches.
func TestReload_SwapsProviders(t *testing.T) {
	// Upstream fake: counts requests and returns a stable body.
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"message":{"content":"v2"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer upstream.Close()

	cfg := testConfig()
	// Initial server has no real providers wired (NewWithProviders with nil),
	// so a chat request returns 4xx/5xx. After Reload with a config pointing
	// at the test upstream, the same request should succeed.
	srv := NewWithProviders(cfg, slog.Default(), nil)

	// New config: single openai provider backed by the test upstream.
	newCfg := &config.Config{
		Server: config.ServerConfig{Listen: ":0", RequestTimeout: 30 * time.Second},
		Auth:   config.AuthConfig{Enabled: true, APIKeys: []config.APIKey{{Key: "test-key", ClientID: "tester"}}},
		Providers: map[string]config.Provider{
			"p1": {Type: "openai", BaseURL: upstream.URL, APIKey: "ignored", Timeout: 5 * time.Second, Tier: "advanced"},
		},
		Routes: map[string]config.Route{
			"chat": {
				Strategy:  "priority",
				Providers: []config.RouteTarget{{Provider: "p1", Model: "any"}},
			},
		},
	}

	if err := srv.Reload(newCfg); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	// CurrentConfig must reflect the reloaded config.
	if got := srv.CurrentConfig(); got != newCfg {
		t.Fatalf("CurrentConfig did not update after Reload")
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"chat","messages":[{"role":"user","content":"hi"}]}`)))
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("post-reload chat status = %d, body = %s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(&hits) == 0 {
		t.Fatal("upstream was not hit after Reload — old provider snapshot may still be in use")
	}
}

// TestReload_ConcurrentSafe fires many concurrent requests while calling
// Reload midway. The snapshot is stored in an atomic.Pointer, so in-flight
// requests should never observe a torn state and the server must not panic.
func TestReload_ConcurrentSafe(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer upstream.Close()

	cfg := testConfig()
	srv := NewWithProviders(cfg, slog.Default(), nil)

	// Reload to a valid config first so requests can succeed.
	initial := &config.Config{
		Server: config.ServerConfig{Listen: ":0", RequestTimeout: 30 * time.Second},
		Auth:   config.AuthConfig{Enabled: true, APIKeys: []config.APIKey{{Key: "test-key", ClientID: "tester"}}},
		Providers: map[string]config.Provider{
			"p1": {Type: "openai", BaseURL: upstream.URL, APIKey: "x", Timeout: 5 * time.Second, Tier: "advanced"},
		},
		Routes: map[string]config.Route{
			"chat": {Strategy: "priority", Providers: []config.RouteTarget{{Provider: "p1", Model: "m1"}}},
		},
	}
	if err := srv.Reload(initial); err != nil {
		t.Fatalf("initial Reload: %v", err)
	}

	body := []byte(`{"model":"chat","messages":[{"role":"user","content":"hi"}]}`)
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// 50 goroutines hammering the server.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
				req.Header.Set("Authorization", "Bearer test-key")
				w := httptest.NewRecorder()
				srv.Routes().ServeHTTP(w, req)
				// We don't assert on status — some requests may land during
				// a reload window and that's fine. The point is no panic.
				_ = w.Code
			}
		}()
	}

	// Reload repeatedly while requests are in flight.
	for i := 0; i < 20; i++ {
		next := &config.Config{
			Server:    initial.Server,
			Auth:      initial.Auth,
			Providers: map[string]config.Provider{"p1": {Type: "openai", BaseURL: upstream.URL, APIKey: "x", Timeout: 5 * time.Second, Tier: "advanced"}},
			Routes:    map[string]config.Route{"chat": {Strategy: "priority", Providers: []config.RouteTarget{{Provider: "p1", Model: "m1"}}}},
		}
		if err := srv.Reload(next); err != nil {
			t.Fatalf("Reload %d: %v", i, err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	close(stop)
	wg.Wait()
	// If we got here without panicking, the test passes.
}

