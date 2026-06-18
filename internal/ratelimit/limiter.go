package ratelimit

import (
	"sync"

	"golang.org/x/time/rate"
)

type Limiter struct {
	mu      sync.Mutex
	limiters map[string]*rate.Limiter
	rps     rate.Limit
	burst   int
}

func New(rps float64, burst int) *Limiter {
	return &Limiter{
		limiters: make(map[string]*rate.Limiter),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	lim, ok := l.limiters[key]
	if !ok {
		lim = rate.NewLimiter(l.rps, l.burst)
		l.limiters[key] = lim
	}
	l.mu.Unlock()
	return lim.Allow()
}
