package http

import (
	"context"
	"net/http"
	"strings"
)

type authContextKey string

const userContextKey authContextKey = "authUser"

type AuthUser struct {
	ID       int64
	Username string
}

// AuthMiddleware validates Authorization: Bearer <token>
// and injects AuthUser into the request context.
func AuthMiddleware(jwtMgr *JWTManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "missing Authorization header", http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				http.Error(w, "invalid Authorization header", http.StatusUnauthorized)
				return
			}

			claims, err := jwtMgr.Parse(parts[1])
			if err != nil {
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				return
			}

			user := &AuthUser{
				ID:       claims.UserID,
				Username: claims.Username,
			}

			_, err = userStore.GetUserByID(claims.UserID)
			if err != nil {
				http.Error(w, "account not found or deleted", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CurrentUser pulls the AuthUser out of context.
func CurrentUser(r *http.Request) *AuthUser {
	if v := r.Context().Value(userContextKey); v != nil {
		if u, ok := v.(*AuthUser); ok {
			return u
		}
	}
	return nil
}
