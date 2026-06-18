package breaker

import "sync"

type State int

const (
	Closed State = iota
	HalfOpen
	Open
)

type Breaker struct {
	mu     sync.RWMutex
	states map[string]State
}

func New() *Breaker {
	return &Breaker{states: make(map[string]State)}
}

func (b *Breaker) Allow(name string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.states[name] != Open
}

func (b *Breaker) Set(name string, state State) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.states[name] = state
}

func (b *Breaker) State(name string) State {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.states[name]
}
