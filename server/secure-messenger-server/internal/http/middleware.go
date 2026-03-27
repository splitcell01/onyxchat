package http

import (
	"context"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

type authContextKey string

const userContextKey authContextKey = "authUser"

type AuthUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// AuthMiddleware validates Authorization: Bearer <token>
// and injects AuthUser into the request context.
func AuthMiddleware(jwtMgr *JWTManager, userStore userStorer, log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				log.Warn("auth failed: missing authorization header",
					zap.String("path", r.URL.Path),
					zap.String("method", r.Method),
					zap.String("remote_addr", r.RemoteAddr),
				)
				http.Error(w, "missing authorization header", http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				log.Warn("auth failed: invalid authorization header format",
					zap.String("path", r.URL.Path),
					zap.String("method", r.Method),
					zap.String("remote_addr", r.RemoteAddr),
				)
				http.Error(w, "invalid authorization header", http.StatusUnauthorized)
				return
			}

			claims, err := jwtMgr.Parse(parts[1])
			if err != nil {
				log.Warn("auth failed: invalid or expired token",
					zap.String("path", r.URL.Path),
					zap.String("method", r.Method),
					zap.String("remote_addr", r.RemoteAddr),
					zap.Error(err),
				)
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				return
			}

			_, err = userStore.GetUserByID(claims.UserID)
			if err != nil {
				log.Warn("auth failed: account not found or deleted",
					zap.Int64("user_id", claims.UserID),
					zap.String("username", claims.Username),
					zap.String("path", r.URL.Path),
					zap.String("method", r.Method),
					zap.Error(err),
				)
				http.Error(w, "account not found or deleted", http.StatusUnauthorized)
				return
			}

			user := &AuthUser{
				ID:       claims.UserID,
				Username: claims.Username,
			}

			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func CurrentUser(r *http.Request) *AuthUser {
	if v := r.Context().Value(userContextKey); v != nil {
		if u, ok := v.(*AuthUser); ok {
			return u
		}
	}
	return nil
}