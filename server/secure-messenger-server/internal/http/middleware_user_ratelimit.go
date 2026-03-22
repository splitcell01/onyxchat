package http

import (
	"net/http"
	"strconv"
)

func PerUserRateLimit(userLimiter *KeyedLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := CurrentUser(r)
			if u == nil {
				// Let AuthMiddleware handle unauthorized
				next.ServeHTTP(w, r)
				return
			}

			key := strconv.FormatInt(u.ID, 10)
			if !userLimiter.Allow(key) {
				w.Header().Set("Retry-After", "1") // seconds
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
