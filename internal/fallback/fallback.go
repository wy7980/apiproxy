package fallback

import (
	"github.com/wangyong/apiproxy/internal/config"
	"github.com/wangyong/apiproxy/internal/provider"
)

func ShouldFallback(cfg config.FallbackConfig, err *provider.Error) bool {
	if !cfg.Enabled || err == nil {
		return false
	}

	switch err.Kind {
	case provider.KindTimeout:
		return cfg.OnTimeout
	case provider.KindConnectError:
		return cfg.OnConnectError
	case provider.KindRateLimited, provider.KindServerError:
		return statusAllowed(cfg, err.StatusCode)
	case provider.KindClientError:
		return statusAllowed(cfg, err.StatusCode)
	default:
		return false
	}
}

func statusAllowed(cfg config.FallbackConfig, code int) bool {
	for _, allowed := range cfg.OnStatus {
		if code == allowed {
			return true
		}
	}
	return false
}
