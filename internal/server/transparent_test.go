package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wangyong/apiproxy/internal/config"
)

// TestTransparentPathMirroring verifies the merged provider forwards the
// CLIENT's request path to the upstream verbatim. One provider serves both
// /v1/chat/completions (OpenAI shape) and /v1/messages (Anthropic shape), with
// the response forwarded verbatim in each case. This is the regression guard
// for the original bug where glm-5.1 via an "openai" provider turned a
// /v1/messages request into /chat/completions and Claude Code got a malformed
// OpenAI response.
func TestTransparentPathMirroring(t *testing.T) {
	var lastUpstreamPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastUpstreamPath = r.URL.Path
		switch r.URL.Path {
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","choices":[{"message":{"content":"hi"}}]}`))
		case "/v1/messages":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{Listen: ":0", RequestTimeout: 30 * time.Second},
		Auth:   config.AuthConfig{Enabled: true, APIKeys: []config.APIKey{{Key: "test-key", ClientID: "tester"}}},
		Providers: map[string]config.Provider{
			"up": {BaseURL: upstream.URL, APIKey: "ignored", Timeout: 5 * time.Second, Tier: "advanced"},
		},
		Routes: map[string]config.Route{
			"m": {Strategy: "priority", Providers: []config.RouteTarget{{Provider: "up", Model: "m"}}},
		},
		Metrics:  config.MetricsConfig{},
		Logging:  config.LoggingConfig{Level: "info", Format: "json"},
	}
	if err := cfg.ApplyDefaults(); err != nil {
		t.Fatalf("ApplyDefaults: %v", err)
	}
	srv, err := New(cfg, slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	// Case 1: client hits /v1/chat/completions -> upstream gets /v1/chat/completions,
	// OpenAI response forwarded verbatim.
	{
		req, _ := http.NewRequest("POST", ts.URL+"/v1/chat/completions", strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Authorization", "Bearer test-key")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("chat request: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("chat status = %d, body = %s", resp.StatusCode, body)
		}
		if lastUpstreamPath != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q, want /v1/chat/completions", lastUpstreamPath)
		}
		if !strings.Contains(string(body), `"object":"chat.completion"`) {
			t.Fatalf("chat response not OpenAI shape: %s", body)
		}
	}

	// Case 2: client hits /v1/messages -> upstream gets /v1/messages,
	// Anthropic response forwarded verbatim. This is the Claude Code case.
	{
		req, _ := http.NewRequest("POST", ts.URL+"/v1/messages", strings.NewReader(`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Authorization", "Bearer test-key")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("messages request: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("messages status = %d, body = %s", resp.StatusCode, body)
		}
		if lastUpstreamPath != "/v1/messages" {
			t.Fatalf("upstream path = %q, want /v1/messages", lastUpstreamPath)
		}
		if !strings.Contains(string(body), `"type":"message"`) {
			t.Fatalf("messages response not Anthropic shape: %s", body)
		}
	}
}

// TestClientHeadersForwarded verifies the Anthropic protocol headers a Claude
// Code client sends (anthropic-version, anthropic-beta) ride through to the
// upstream untouched when hitting /v1/messages.
func TestClientHeadersForwarded(t *testing.T) {
	var gotVersion, gotBeta, gotAuth, gotXKey string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVersion = r.Header.Get("Anthropic-Version")
		gotBeta = r.Header.Get("Anthropic-Beta")
		gotAuth = r.Header.Get("Authorization")
		gotXKey = r.Header.Get("x-api-key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn"}`))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{Listen: ":0", RequestTimeout: 30 * time.Second},
		Auth:   config.AuthConfig{Enabled: true, APIKeys: []config.APIKey{{Key: "test-key", ClientID: "tester"}}},
		Providers: map[string]config.Provider{
			"up": {BaseURL: upstream.URL, APIKey: "sk-upstream-real", Timeout: 5 * time.Second, AuthHeader: "both"},
		},
		Routes: map[string]config.Route{
			"m": {Strategy: "priority", Providers: []config.RouteTarget{{Provider: "up", Model: "m"}}},
		},
		Metrics: config.MetricsConfig{},
		Logging: config.LoggingConfig{Level: "info", Format: "json"},
	}
	if err := cfg.ApplyDefaults(); err != nil {
		t.Fatalf("ApplyDefaults: %v", err)
	}
	srv, err := New(cfg, slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	// Client (Claude Code-like) sends anthropic-* headers and its own bearer
	// key; proxy auth uses the inbound key, and must overwrite upstream auth
	// with the provider's real key.
	req, _ := http.NewRequest("POST", ts.URL+"/v1/messages", strings.NewReader(`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Anthropic-Version", "2023-06-01")
	req.Header.Set("Anthropic-Beta", "prompt-caching-2024-07-31")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()

	if gotVersion != "2023-06-01" {
		t.Fatalf("upstream Anthropic-Version = %q, want 2023-06-01", gotVersion)
	}
	if gotBeta != "prompt-caching-2024-07-31" {
		t.Fatalf("upstream Anthropic-Beta = %q", gotBeta)
	}
	// Provider key (not the client's "test-key") must reach the upstream.
	if gotAuth != "Bearer sk-upstream-real" {
		t.Fatalf("upstream Authorization = %q, want Bearer sk-upstream-real", gotAuth)
	}
	if gotXKey != "sk-upstream-real" {
		t.Fatalf("upstream x-api-key = %q, want sk-upstream-real", gotXKey)
	}
}
