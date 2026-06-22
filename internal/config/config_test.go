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
	if cfg.Providers["test"].AuthHeader != "both" {
		t.Fatalf("provider auth_header default = %q, want both", cfg.Providers["test"].AuthHeader)
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

func TestLogFileDirRequiredWhenEnabled(t *testing.T) {
	path := writeTempConfig(t, `
server:
  listen: ":9090"
providers:
  test:
    base_url: "https://example.com/v1"
routes:
  chat:
    providers:
      - provider: test
        model: test-model
logging:
  file:
    enabled: true
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "logging.file.dir is required") {
		t.Fatalf("Load() error = %v, want dir required error", err)
	}
}

func TestLogFileDefaultsWhenEnabled(t *testing.T) {
	path := writeTempConfig(t, `
server:
  listen: ":9090"
providers:
  test:
    base_url: "https://example.com/v1"
routes:
  chat:
    providers:
      - provider: test
        model: test-model
logging:
  file:
    enabled: true
    dir: "logs"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Logging.File.MaxDays != 7 {
		t.Fatalf("max_days default = %d, want 7", cfg.Logging.File.MaxDays)
	}
	if cfg.Logging.File.MaxSize != 100 {
		t.Fatalf("max_size default = %d, want 100", cfg.Logging.File.MaxSize)
	}
}

func TestLogFileNotSetWhenDisabled(t *testing.T) {
	path := writeTempConfig(t, `
server:
  listen: ":9090"
providers:
  test:
    base_url: "https://example.com/v1"
routes:
  chat:
    providers:
      - provider: test
        model: test-model
logging:
  file:
    enabled: false
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Logging.File.MaxDays != 0 || cfg.Logging.File.MaxSize != 0 {
		t.Fatalf("file logging disabled but got max_days=%d max_size=%d",
			cfg.Logging.File.MaxDays, cfg.Logging.File.MaxSize)
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
