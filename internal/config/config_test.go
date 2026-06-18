package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadAppliesDefaultsAndExpandsEnv(t *testing.T) {
	t.Setenv("TEST_PROVIDER_KEY", "secret-key")
	path := writeTempConfig(t, `
server:
  listen: ":9090"
providers:
  test:
    base_url: "https://example.com/v1"
    api_key: "$TEST_PROVIDER_KEY"
routes:
  chat:
    providers:
      - provider: test
        model: test-model
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Listen != ":9090" {
		t.Fatalf("listen = %q", cfg.Server.Listen)
	}
	if cfg.Server.RequestTimeout != 120*time.Second {
		t.Fatalf("request timeout = %s", cfg.Server.RequestTimeout)
	}
	if cfg.Providers["test"].Type != "openai" {
		t.Fatalf("provider type = %q", cfg.Providers["test"].Type)
	}
	if cfg.Providers["test"].APIKey != "secret-key" {
		t.Fatalf("api key = %q", cfg.Providers["test"].APIKey)
	}
	if cfg.Routes["chat"].Strategy != "priority" {
		t.Fatalf("strategy = %q", cfg.Routes["chat"].Strategy)
	}
	if cfg.Routes["chat"].Fallback.MaxAttempts != 1 {
		t.Fatalf("max attempts = %d", cfg.Routes["chat"].Fallback.MaxAttempts)
	}
}

func TestLoadRejectsUnknownProviderInRoute(t *testing.T) {
	path := writeTempConfig(t, `
server:
  listen: ":9090"
providers:
  test:
    base_url: "https://example.com/v1"
routes:
  chat:
    providers:
      - provider: missing
        model: test-model
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "not defined") {
		t.Fatalf("Load() error = %v, want unknown provider error", err)
	}
}

func TestStorageRetentionDefault(t *testing.T) {
	path := writeTempConfig(t, `
server:
  listen: ":9090"
providers:
  test:
    base_url: "https://example.com/v1"
storage:
  enabled: true
routes:
  chat:
    providers:
      - provider: test
        model: test-model
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Storage.Retention != 7*24*time.Hour {
		t.Fatalf("retention = %v, want 7d", cfg.Storage.Retention)
	}
}

func TestStorageRetentionExplicit(t *testing.T) {
	path := writeTempConfig(t, `
server:
  listen: ":9090"
providers:
  test:
    base_url: "https://example.com/v1"
storage:
  enabled: true
  retention: 72h
routes:
  chat:
    providers:
      - provider: test
        model: test-model
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Storage.Retention != 72*time.Hour {
		t.Fatalf("retention = %v, want 72h", cfg.Storage.Retention)
	}
}

func TestStorageRetentionNotSetWhenDisabled(t *testing.T) {
	path := writeTempConfig(t, `
server:
  listen: ":9090"
providers:
  test:
    base_url: "https://example.com/v1"
storage:
  enabled: false
routes:
  chat:
    providers:
      - provider: test
        model: test-model
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Storage.Retention != 0 {
		t.Fatalf("retention should be 0 when storage disabled, got %v", cfg.Storage.Retention)
	}
}

func TestProviderAPIKeyUsesEnv(t *testing.T) {
	t.Setenv("TEST_PROVIDER_KEY", "from-env")
	cfg := &Config{Providers: map[string]Provider{
		"test": {APIKeyEnv: "TEST_PROVIDER_KEY"},
	}}

	if got := cfg.ProviderAPIKey("test"); got != "from-env" {
		t.Fatalf("ProviderAPIKey() = %q", got)
	}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
