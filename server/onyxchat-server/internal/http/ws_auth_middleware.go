package http

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
)

func WSAuthMiddleware(jwtMgr *JWTManager, rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var user *AuthUser

			// Try ticket first
			if ticket := r.URL.Query().Get("ticket"); ticket != "" {
				key := ticketPrefix + ticket
				val, err := rdb.GetDel(context.Background(), key).Result()
				if err == nil {
					parts := strings.SplitN(val, ":", 2)
					if len(parts) == 2 {
						id, err := strconv.ParseInt(parts[0], 10, 64)
						if err == nil {
							user = &AuthUser{ID: id, Username: parts[1]}
						}
					}
				}
			}

			// Fall back to JWT Bearer token (dev / non-browser clients)
			if user == nil {
				authHeader := r.Header.Get("Authorization")
				if authHeader != "" {
					parts := strings.SplitN(authHeader, " ", 2)
					if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
						if claims, err := jwtMgr.Parse(parts[1]); err == nil {
							user = &AuthUser{ID: claims.UserID, Username: claims.Username}
						}
					}
				}
			}

			if user == nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
