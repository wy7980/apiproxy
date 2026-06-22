package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wangyong/apiproxy/internal/admin"
	"github.com/wangyong/apiproxy/internal/config"
	"github.com/wangyong/apiproxy/internal/storage"
)

// noRedirectClient returns an HTTP client that does not follow redirects,
// so we can inspect 303 responses directly.
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// TestE2EFullStack exercises the full proxy+admin stack over real TCP connections
// using httptest.NewServer. It covers healthz, auth, chat forwarding, 413, 404,
// streaming, admin login/redirect/API, login throttle, and storage persistence.
func TestE2EFullStack(t *testing.T) {
	// Fake upstream: handles both /chat/completions (OpenAI-style, since the
	// openai provider appends "/chat/completions" to BaseURL) and returns
	// either SSE for streaming requests or a JSON chat completion otherwise.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Stream bool `json:"stream"`
			}
			json.Unmarshal(body, &req)
			if req.Stream {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Write([]byte("data: {\"id\":\"s1\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
				w.Write([]byte("data: [DONE]\n\n"))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"c1","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"hi"}}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"m1","object":"model","data":[{"id":"m1","object":"model","owned_by":"p1"}]}`))
	}))
	defer upstream.Close()

	// ── SQLite in temp dir ────────────────────────────────────────
	store, err := storage.Open(filepath.Join(t.TempDir(), "test.db"), 24*time.Hour)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()

	// ── Config ────────────────────────────────────────────────────
	cfg := &config.Config{
		Server: config.ServerConfig{Listen: ":0", RequestTimeout: 30 * time.Second},
		Auth:   config.AuthConfig{Enabled: true, APIKeys: []config.APIKey{{Key: "test-key", ClientID: "tester"}}},
		Providers: map[string]config.Provider{
			"p1": {BaseURL: upstream.URL, APIKey: "ignored", Timeout: 5 * time.Second, Tier: "advanced"},
		},
		Routes: map[string]config.Route{
			"chat": {Strategy: "priority", Providers: []config.RouteTarget{{Provider: "p1", Model: "m1"}}},
		},
		Metrics: config.MetricsConfig{},
		Logging: config.LoggingConfig{Level: "info", Format: "json"},
	}
	if err := cfg.ApplyDefaults(); err != nil {
		t.Fatalf("cfg.ApplyDefaults: %v", err)
	}

	// ── Proxy server ──────────────────────────────────────────────
	proxySrv, err := New(cfg, slog.Default())
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	proxySrv = proxySrv.WithStore(store)
	proxyTS := httptest.NewServer(proxySrv.Routes())
	defer proxyTS.Close()

	// ── Admin server ──────────────────────────────────────────────
	adminSrv := admin.New(store, slog.Default(), "", proxySrv, "admin", "pass123")
	adminTS := httptest.NewServer(adminSrv.Handler())
	defer adminTS.Close()

	cli := noRedirectClient()

	// ── 1. Health check ───────────────────────────────────────────
	t.Run("Healthz", func(t *testing.T) {
		resp, err := http.Get(proxyTS.URL + "/healthz")
		if err != nil {
			t.Fatalf("GET /healthz: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		var body map[string]string
		json.NewDecoder(resp.Body).Decode(&body)
		if body["status"] != "ok" {
			t.Fatalf("body = %v, want status=ok", body)
		}
	})

	// ── 2. Unauthenticated reject ─────────────────────────────────
	t.Run("AuthRequired", func(t *testing.T) {
		resp, err := http.Get(proxyTS.URL + "/v1/models")
		if err != nil {
			t.Fatalf("GET /v1/models: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
	})

	// ── 3. Authenticated models ───────────────────────────────────
	t.Run("AuthOKModels", func(t *testing.T) {
		req, _ := http.NewRequest("GET", proxyTS.URL+"/v1/models", nil)
		req.Header.Set("Authorization", "Bearer test-key")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /v1/models: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	})

	// ── 4. Real chat request ──────────────────────────────────────
	t.Run("ChatCompletion", func(t *testing.T) {
		body := `{"model":"chat","messages":[{"role":"user","content":"hello"}]}`
		req, _ := http.NewRequest("POST", proxyTS.URL+"/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-key")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST /v1/chat/completions: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		if result["id"] != "c1" {
			t.Fatalf("body id = %v, want c1", result["id"])
		}
	})

	// ── 5. Model not found ────────────────────────────────────────
	t.Run("ModelNotFound", func(t *testing.T) {
		body := `{"model":"missing","messages":[{"role":"user","content":"hello"}]}`
		req, _ := http.NewRequest("POST", proxyTS.URL+"/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-key")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST /v1/chat/completions: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})

	// ── 6. Request body too large ─────────────────────────────────
	t.Run("BodyTooLarge", func(t *testing.T) {
		oversized := int(maxRequestBodyBytes) + 1024
		padding := make([]byte, oversized)
		for i := range padding {
			padding[i] = 'a'
		}
		payload := `{"model":"m1","messages":[{"role":"user","content":"` + string(padding) + `"}]}`
		req, _ := http.NewRequest("POST", proxyTS.URL+"/v1/chat/completions", strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer test-key")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST /v1/chat/completions: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 413 {
			t.Fatalf("status = %d, want 413", resp.StatusCode)
		}
	})

	// ── 7. Streaming request ──────────────────────────────────────
	t.Run("StreamingChat", func(t *testing.T) {
		body := `{"model":"chat","messages":[{"role":"user","content":"hello"}],"stream":true}`
		req, _ := http.NewRequest("POST", proxyTS.URL+"/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-key")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST /v1/chat/completions stream: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "text/event-stream") {
			t.Fatalf("Content-Type = %q, want text/event-stream", ct)
		}
	})

	// ── 8. Admin unauthenticated redirect ─────────────────────────
	t.Run("AdminRedirectToLogin", func(t *testing.T) {
		resp, err := cli.Get(adminTS.URL + "/")
		if err != nil {
			t.Fatalf("GET /: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 303 {
			t.Fatalf("status = %d, want 303", resp.StatusCode)
		}
		loc := resp.Header.Get("Location")
		if !strings.HasPrefix(loc, "/login") {
			t.Fatalf("Location = %q, want /login", loc)
		}
	})

	// ── 9. Admin login ────────────────────────────────────────────
	var adminCookie *http.Cookie
	t.Run("AdminLogin", func(t *testing.T) {
		form := url.Values{"username": {"admin"}, "password": {"pass123"}}
		resp, err := cli.PostForm(adminTS.URL+"/login", form)
		if err != nil {
			t.Fatalf("POST /login: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 303 {
			t.Fatalf("status = %d, want 303", resp.StatusCode)
		}
		cookies := resp.Cookies()
		for _, c := range cookies {
			if c.Name == "apiproxy_admin" {
				adminCookie = c
				break
			}
		}
		if adminCookie == nil {
			t.Fatalf("no apiproxy_admin cookie in response")
		}
		if !adminCookie.HttpOnly {
			t.Error("cookie.HttpOnly = false, want true")
		}
		if adminCookie.SameSite != http.SameSiteLaxMode {
			t.Errorf("cookie.SameSite = %v, want Lax", adminCookie.SameSite)
		}
	})

	// ── 10. Admin API with auth ───────────────────────────────────
	t.Run("AdminAPIAuthenticated", func(t *testing.T) {
		if adminCookie == nil {
			t.Skip("no cookie from login")
		}
		req, _ := http.NewRequest("GET", adminTS.URL+"/api/summary", nil)
		req.AddCookie(adminCookie)
		resp, err := cli.Do(req)
		if err != nil {
			t.Fatalf("GET /api/summary: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	})

	// ── 11. Login throttle ────────────────────────────────────────
	t.Run("LoginThrottle", func(t *testing.T) {
		// Use a fresh client with a different source IP by going through
		// the test server's loopback; httptest.NewServer always uses 127.0.0.1.
		badForm := url.Values{"username": {"admin"}, "password": {"wrong"}}
		for i := 0; i < 5; i++ {
			resp, err := cli.PostForm(adminTS.URL+"/login", badForm)
			if err != nil {
				t.Fatalf("bad login %d: %v", i+1, err)
			}
			resp.Body.Close()
		}
		// 6th attempt should be throttled.
		resp, err := cli.PostForm(adminTS.URL+"/login", badForm)
		if err != nil {
			t.Fatalf("throttled login: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 429 {
			t.Fatalf("status = %d, want 429", resp.StatusCode)
		}
	})

	// ── 12. Storage persistence ───────────────────────────────────
	t.Run("StoragePersistence", func(t *testing.T) {
		if adminCookie == nil {
			t.Skip("no cookie from login")
		}
		// Send a chat request first (case 4 already did, but re-send to be safe).
		body := `{"model":"chat","messages":[{"role":"user","content":"hello"}]}`
		req, _ := http.NewRequest("POST", proxyTS.URL+"/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-key")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("chat request: %v", err)
		}
		resp.Body.Close()

		// Query admin summary to verify the request was recorded.
		sreq, _ := http.NewRequest("GET", adminTS.URL+"/api/summary", nil)
		sreq.AddCookie(adminCookie)
		sresp, err := cli.Do(sreq)
		if err != nil {
			t.Fatalf("GET /api/summary: %v", err)
		}
		defer sresp.Body.Close()
		if sresp.StatusCode != 200 {
			t.Fatalf("summary status = %d, want 200", sresp.StatusCode)
		}
		var summaries []map[string]any
		json.NewDecoder(sresp.Body).Decode(&summaries)
		totalRequests := 0
		for _, s := range summaries {
			if n, ok := s["requests"].(float64); ok {
				totalRequests += int(n)
			}
		}
		if totalRequests < 1 {
			t.Fatalf("total requests = %d, want >= 1 (storage not persisting)", totalRequests)
		}
	})
}
