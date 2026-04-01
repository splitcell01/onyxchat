package http

import (
	"net/http"
)

func LoginIPRateLimit(ipLimiter *KeyedLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ClientIP(r)
			if !ipLimiter.Allow(ip) {
				w.Header().Set("Retry-After", "1") // seconds
				http.Error(w, "too many requests", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
