package http

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type limiterEntry struct {
	lim  *rate.Limiter
	last time.Time
}

type KeyedLimiter struct {
	mu    sync.Mutex
	byKey map[string]*limiterEntry
	r     rate.Limit
	burst int
	ttl   time.Duration
}

func NewKeyedLimiter(r rate.Limit, burst int, ttl time.Duration) *KeyedLimiter {
	return &KeyedLimiter{
		byKey: make(map[string]*limiterEntry),
		r:     r,
		burst: burst,
		ttl:   ttl,
	}
}

func (kl *KeyedLimiter) Allow(key string) bool {
	now := time.Now()

	kl.mu.Lock()
	defer kl.mu.Unlock()

	for k, e := range kl.byKey {
		if now.Sub(e.last) > kl.ttl {
			delete(kl.byKey, k)
		}
	}

	e, ok := kl.byKey[key]
	if !ok {
		e = &limiterEntry{lim: rate.NewLimiter(kl.r, kl.burst), last: now}
		kl.byKey[key] = e
	}
	e.last = now
	return e.lim.Allow()
}
