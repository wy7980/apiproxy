package fallback

import (
	"testing"

	"github.com/wangyong/apiproxy/internal/config"
	"github.com/wangyong/apiproxy/internal/provider"
)

func TestShouldFallback(t *testing.T) {
	cfg := config.FallbackConfig{
		Enabled:        true,
		OnStatus:       []int{429, 500, 502, 503, 504},
		OnTimeout:      true,
		OnConnectError: true,
	}

	tests := []struct {
		name string
		err  *provider.Error
		want bool
	}{
		{"disabled", nil, false},
		{"timeout", &provider.Error{Kind: provider.KindTimeout}, true},
		{"connect", &provider.Error{Kind: provider.KindConnectError}, true},
		{"rate limit", &provider.Error{Kind: provider.KindRateLimited, StatusCode: 429}, true},
		{"server error", &provider.Error{Kind: provider.KindServerError, StatusCode: 503}, true},
		{"client bad request", &provider.Error{Kind: provider.KindClientError, StatusCode: 400}, false},
		{"client configured", &provider.Error{Kind: provider.KindClientError, StatusCode: 429}, true},
		{"unknown", &provider.Error{Kind: provider.KindUnknown}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldFallback(cfg, tt.err); got != tt.want {
				t.Fatalf("ShouldFallback() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldFallbackDisabled(t *testing.T) {
	cfg := config.FallbackConfig{Enabled: false, OnTimeout: true}
	if ShouldFallback(cfg, &provider.Error{Kind: provider.KindTimeout}) {
		t.Fatal("ShouldFallback() = true, want false when disabled")
	}
}
