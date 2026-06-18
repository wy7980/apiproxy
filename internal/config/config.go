package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server         ServerConfig         `yaml:"server"`
	Admin          AdminServerConfig    `yaml:"admin"`
	Auth           AuthConfig           `yaml:"auth"`
	Providers      map[string]Provider  `yaml:"providers"`
	Routes         map[string]Route     `yaml:"routes"`
	CircuitBreaker CircuitBreakerConfig `yaml:"circuit_breaker"`
	Metrics        MetricsConfig        `yaml:"metrics"`
	Logging        LoggingConfig        `yaml:"logging"`
	Storage        StorageConfig        `yaml:"storage"`
}

type ServerConfig struct {
	Listen         string        `yaml:"listen"`
	ReadTimeout    time.Duration `yaml:"read_timeout"`
	WriteTimeout   time.Duration `yaml:"write_timeout"`
	RequestTimeout time.Duration `yaml:"request_timeout"`
}

type AuthConfig struct {
	Enabled bool      `yaml:"enabled"`
	APIKeys []APIKey  `yaml:"api_keys"`
}

type APIKey struct {
	Key      string `yaml:"key"`
	ClientID string `yaml:"client_id"`
}

type Provider struct {
	Type      string        `yaml:"type"`
	BaseURL   string        `yaml:"base_url"`
	APIKey    string        `yaml:"api_key"`
	APIKeyEnv string        `yaml:"api_key_env"`
	Timeout   time.Duration `yaml:"timeout"`
	Tier      string        `yaml:"tier"`
}

type Route struct {
	Strategy  string         `yaml:"strategy"`
	Fallback  FallbackConfig `yaml:"fallback"`
	Providers []RouteTarget  `yaml:"providers"`
}

type FallbackConfig struct {
	Enabled         bool    `yaml:"enabled"`
	MaxAttempts     int     `yaml:"max_attempts"`
	OnStatus        []int   `yaml:"on_status"`
	OnTimeout       bool    `yaml:"on_timeout"`
	OnConnectError  bool    `yaml:"on_connect_error"`
	AllowDowngrade  bool    `yaml:"allow_downgrade"`
}

type RouteTarget struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	Tier     string `yaml:"tier"`
	Weight   int    `yaml:"weight"`
}

type CircuitBreakerConfig struct {
	Enabled            bool          `yaml:"enabled"`
	Window             time.Duration `yaml:"window"`
	MinRequests        int           `yaml:"min_requests"`
	ErrorRateThreshold float64       `yaml:"error_rate_threshold"`
	OpenDuration       time.Duration `yaml:"open_duration"`
	HalfOpenRequests   int           `yaml:"half_open_requests"`
}

type MetricsConfig struct {
	Prometheus struct {
		Enabled bool   `yaml:"enabled"`
		Path    string `yaml:"path"`
	} `yaml:"prometheus"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type StorageConfig struct {
	Path      string        `yaml:"path"`
	Enabled   bool          `yaml:"enabled"`
	// Retention is how long to keep per-day event shards. Events whose
	// shard day is older than (now - Retention) are dropped via DROP TABLE.
	// Default is 7 * 24h. Events are stored in daily shards named
	// request_events_YYYYMMDD; DROP reclaims disk immediately.
	Retention time.Duration `yaml:"retention"`
}

type AdminServerConfig struct {
	Listen string `yaml:"listen"`
	Enabled bool  `yaml:"enabled"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	// Expand $VAR / ${VAR} in YAML before parsing.
	expanded := os.ExpandEnv(string(data))

	var cfg Config
	dec := yaml.NewDecoder(stringReader(expanded))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	return &cfg, nil
}

func (c *Config) validate() error {
	if c.Server.Listen == "" {
		return fmt.Errorf("server.listen is required")
	}
	if len(c.Providers) == 0 {
		return fmt.Errorf("at least one provider must be configured")
	}
	for name, p := range c.Providers {
		if p.BaseURL == "" {
			return fmt.Errorf("provider %q: base_url is required", name)
		}
	}
	for name, r := range c.Routes {
		if len(r.Providers) == 0 {
			return fmt.Errorf("route %q: at least one provider required", name)
		}
		for i, t := range r.Providers {
			if _, ok := c.Providers[t.Provider]; !ok {
				return fmt.Errorf("route %q: provider %q at index %d is not defined", name, t.Provider, i)
			}
		}
	}
	return nil
}

func (c *Config) applyDefaults() {
	if c.Server.RequestTimeout == 0 {
		c.Server.RequestTimeout = 120 * time.Second
	}
	if c.Server.ReadTimeout == 0 {
		c.Server.ReadTimeout = 60 * time.Second
	}
	if c.Server.WriteTimeout == 0 {
		c.Server.WriteTimeout = 120 * time.Second
	}
	for name, p := range c.Providers {
		if p.Timeout == 0 {
			p.Timeout = 60 * time.Second
			c.Providers[name] = p
		}
		if p.Type == "" {
			p.Type = "openai"
			c.Providers[name] = p
		}
	}
	for name, r := range c.Routes {
		if r.Strategy == "" {
			r.Strategy = "priority"
			c.Routes[name] = r
		}
		if r.Fallback.MaxAttempts == 0 {
			r.Fallback.MaxAttempts = len(r.Providers)
			c.Routes[name] = r
		}
	}
	if c.Metrics.Prometheus.Path == "" {
		c.Metrics.Prometheus.Path = "/metrics"
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Logging.Format == "" {
		c.Logging.Format = "json"
	}
	if c.Storage.Enabled {
		if c.Storage.Path == "" {
			c.Storage.Path = "data/apiproxy.db"
		}
		if c.Storage.Retention <= 0 {
			c.Storage.Retention = 7 * 24 * time.Hour
		}
	}
	if c.Admin.Enabled {
		if c.Admin.Listen == "" {
			c.Admin.Listen = ":8081"
		}
	}
}

// ProviderAPIKey resolves a provider's API key from inline config or env var.
func (c *Config) ProviderAPIKey(name string) string {
	p, ok := c.Providers[name]
	if !ok {
		return ""
	}
	if p.APIKey != "" {
		return p.APIKey
	}
	if p.APIKeyEnv != "" {
		return os.Getenv(p.APIKeyEnv)
	}
	return ""
}

func stringReader(s string) *strings.Reader {
	return strings.NewReader(s)
}
