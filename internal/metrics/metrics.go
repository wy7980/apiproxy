package metrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	RequestTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "apiproxy_request_total",
		Help: "Total number of LLM API requests.",
	}, []string{"provider", "model", "route", "client_id", "status_code", "fallback_from", "fallback_to"})

	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "apiproxy_request_duration_seconds",
		Help:    "Request duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"provider", "model", "route", "stream"})

	FirstTokenDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "apiproxy_first_token_duration_seconds",
		Help:    "Time to first token in seconds (streaming requests only).",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"provider", "model", "route"})

	TokenTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "apiproxy_token_total",
		Help: "Total tokens used.",
	}, []string{"provider", "model", "route", "client_id", "type"})

	ErrorTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "apiproxy_error_total",
		Help: "Total number of errors.",
	}, []string{"provider", "model", "route", "error_type"})

	FallbackTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "apiproxy_fallback_total",
		Help: "Total number of fallback invocations.",
	}, []string{"route", "from_provider", "from_model", "to_provider", "to_model", "reason"})

	CircuitBreakerState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "apiproxy_circuit_breaker_state",
		Help: "Circuit breaker state: 0=closed, 0.5=half-open, 1=open.",
	}, []string{"provider"})
)

type RequestLog struct {
	RequestID        string
	ClientID         string
	Route            string
	Provider         string
	Model            string
	StatusCode       int
	LatencyMs        float64
	FirstTokenMs     float64
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	FallbackCount    int
	FallbackFrom     string
	FallbackTo       string
	ErrorType        string
	Stream           bool
}

func Init() {
	RequestTotal.WithLabelValues("", "", "", "", "", "", "").Add(0)
	RequestDuration.WithLabelValues("", "", "", "").Observe(0)
	FirstTokenDuration.WithLabelValues("", "", "").Observe(0)
	TokenTotal.WithLabelValues("", "", "", "", "").Add(0)
	ErrorTotal.WithLabelValues("", "", "", "").Add(0)
	FallbackTotal.WithLabelValues("", "", "", "", "", "").Add(0)
	CircuitBreakerState.WithLabelValues("").Set(0)
}

func RecordRequest(l RequestLog) {
	RequestTotal.WithLabelValues(
		l.Provider, l.Model, l.Route, l.ClientID,
		statusStr(l.StatusCode), l.FallbackFrom, l.FallbackTo,
	).Inc()

	RequestDuration.WithLabelValues(
		l.Provider, l.Model, l.Route, streamStr(l.Stream),
	).Observe(l.LatencyMs / 1000)

	if l.Stream && l.FirstTokenMs > 0 {
		FirstTokenDuration.WithLabelValues(
			l.Provider, l.Model, l.Route,
		).Observe(l.FirstTokenMs / 1000)
	}

	TokenTotal.WithLabelValues(l.Provider, l.Model, l.Route, l.ClientID, "prompt").Add(float64(l.PromptTokens))
	TokenTotal.WithLabelValues(l.Provider, l.Model, l.Route, l.ClientID, "completion").Add(float64(l.CompletionTokens))

	if l.ErrorType != "" {
		ErrorTotal.WithLabelValues(l.Provider, l.Model, l.Route, l.ErrorType).Inc()
	}

	if l.FallbackFrom != "" && l.FallbackTo != "" {
		FallbackTotal.WithLabelValues(l.Route, l.FallbackFrom, "", l.FallbackTo, "", "").Inc()
	}
}

func statusStr(code int) string {
	return fmt.Sprintf("%d", code)
}

func streamStr(s bool) string {
	if s {
		return "true"
	}
	return "false"
}
