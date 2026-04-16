package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/cole/onyxchat-server/internal/store"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// ─────────────────────────────────────────────────────────────
// Health check
// ─────────────────────────────────────────────────────────────

func HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// ─────────────────────────────────────────────────────────────
// Register
// ─────────────────────────────────────────────────────────────

type RegisterRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	InviteCode string `json:"invite_code"`
}

type RegisterResponse struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

func RegisterHandler(userStore userStorer, jwtMgr *JWTManager, rdb *redis.Client, log *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Warn("[Register] invalid JSON", zap.Error(err))
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}

		req.Username = strings.TrimSpace(req.Username)
		if req.Username == "" || req.Password == "" {
			writeJSONError(w, http.StatusBadRequest, "username and password required")
			return
		}

		if req.InviteCode == "" {
			writeJSONError(w, http.StatusForbidden, "invite code required")
			return
		}

		hashedBytes, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			log.Error("[Register] could not hash password", zap.Error(err))
			writeJSONError(w, http.StatusInternalServerError, "could not hash password")
			return
		}

		// Consume invite code and create user atomically — if user creation
		// fails (e.g. duplicate username) the invite code is not burned.
		dbStart := time.Now()
		user, err := userStore.RegisterWithInvite(req.InviteCode, req.Username, string(hashedBytes))
		ObserveDBQuery("user_register", dbStart)
		if err != nil {
			log.Warn("[Register] could not create user", zap.String("username", req.Username), zap.Error(err))
			if strings.Contains(err.Error(), "invalid or already used invite code") {
				writeJSONError(w, http.StatusForbidden, "invalid or already used invite code")
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "failed to create user")
			return
		}

		token, err := jwtMgr.Generate(user)
		if err != nil {
			log.Error("[Register] failed to generate JWT", zap.Error(err))
			writeJSONError(w, http.StatusInternalServerError, "failed to generate token")
			return
		}

		rt, err := newRefreshToken()
		if err != nil {
			log.Error("[Register] failed to generate refresh token", zap.Error(err))
			writeJSONError(w, http.StatusInternalServerError, "failed to generate token")
			return
		}
		if err := storeRefreshToken(r.Context(), rdb, rt, user.ID); err != nil {
			log.Error("[Register] failed to store refresh token", zap.Error(err))
			writeJSONError(w, http.StatusInternalServerError, "failed to generate token")
			return
		}

		writeJSON(w, http.StatusOK, RegisterResponse{
			ID:           user.ID,
			Username:     user.Username,
			Token:        token,
			RefreshToken: rt,
		})
	}
}

// ─────────────────────────────────────────────────────────────
// Login
// ─────────────────────────────────────────────────────────────

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

func LoginHandler(userStore userStorer, jwtMgr *JWTManager, idLimiter *KeyedLimiter, rdb *redis.Client, log *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}

		req.Username = strings.TrimSpace(req.Username)
		if req.Username == "" || req.Password == "" {
			writeJSONError(w, http.StatusBadRequest, "username and password required")
			return
		}

		// rate limit by identifier BEFORE lookup/hash work
		if !idLimiter.Allow(strings.ToLower(req.Username)) {
			writeJSONError(w, http.StatusTooManyRequests, "too many requests")
			return
		}

		dbStart := time.Now()
		user, err := userStore.GetUserByUsername(req.Username)
		ObserveDBQuery("user_get_by_username", dbStart)
		if err != nil {
			if err == store.ErrUserNotFound {
				writeJSONError(w, http.StatusUnauthorized, "invalid credentials")
			} else {
				log.Error("[Login] database error", zap.String("username", req.Username), zap.Error(err))
				writeJSONError(w, http.StatusInternalServerError, "internal server error")
			}
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
			writeJSONError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}

		token, err := jwtMgr.Generate(user)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to generate token")
			return
		}

		rt, err := newRefreshToken()
		if err != nil {
			log.Error("[Login] failed to generate refresh token", zap.Error(err))
			writeJSONError(w, http.StatusInternalServerError, "failed to generate token")
			return
		}
		if err := storeRefreshToken(r.Context(), rdb, rt, user.ID); err != nil {
			log.Error("[Login] failed to store refresh token", zap.Error(err))
			writeJSONError(w, http.StatusInternalServerError, "failed to generate token")
			return
		}

		writeJSON(w, http.StatusOK, LoginResponse{
			ID:           user.ID,
			Username:     user.Username,
			Token:        token,
			RefreshToken: rt,
		})
	}
}

// ─────────────────────────────────────────────────────────────
// Refresh
// ─────────────────────────────────────────────────────────────

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type refreshResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

// RefreshHandler exchanges a valid refresh token for a new access token + rotated refresh token.
func RefreshHandler(userStore userStorer, jwtMgr *JWTManager, rdb *redis.Client, log *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req refreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.RefreshToken == "" {
			writeJSONError(w, http.StatusUnauthorized, "refresh_token required")
			return
		}

		userID, err := lookupRefreshToken(r.Context(), rdb, req.RefreshToken)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "invalid or expired refresh token")
			return
		}

		dbStart := time.Now()
		user, err := userStore.GetUserByID(userID)
		ObserveDBQuery("user_get_by_id", dbStart)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "account not found")
			return
		}

		// Rotate: delete old, issue new
		_ = deleteRefreshToken(r.Context(), rdb, req.RefreshToken)

		newRT, err := newRefreshToken()
		if err != nil {
			log.Error("[Refresh] failed to generate refresh token", zap.Error(err))
			writeJSONError(w, http.StatusInternalServerError, "failed to generate token")
			return
		}
		if err := storeRefreshToken(r.Context(), rdb, newRT, user.ID); err != nil {
			log.Error("[Refresh] failed to store refresh token", zap.Error(err))
			writeJSONError(w, http.StatusInternalServerError, "failed to generate token")
			return
		}

		token, err := jwtMgr.Generate(user)
		if err != nil {
			log.Error("[Refresh] failed to generate access token", zap.Error(err))
			writeJSONError(w, http.StatusInternalServerError, "failed to generate token")
			return
		}

		writeJSON(w, http.StatusOK, refreshResponse{Token: token, RefreshToken: newRT})
	}
}

// ─────────────────────────────────────────────────────────────
// Logout
// ─────────────────────────────────────────────────────────────

// LogoutHandler revokes the supplied refresh token so it can't be used again.
func LogoutHandler(rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req refreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.RefreshToken != "" {
			_ = deleteRefreshToken(r.Context(), rdb, req.RefreshToken)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ─────────────────────────────────────────────────────────────
// List users
// ─────────────────────────────────────────────────────────────

type UserResponse struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

func ListUsersHandler(userStore userStorer, log *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var users []*store.User
		var err error

		dbStart := time.Now()
		if q := strings.TrimSpace(r.URL.Query().Get("search")); q != "" {
			users, err = userStore.SearchUsers(q)
			ObserveDBQuery("user_search", dbStart)
		} else {
			users, err = userStore.ListUsers()
			ObserveDBQuery("user_list", dbStart)
		}
		if err != nil {
			log.Error("[ListUsers] failed to list users", zap.Error(err))
			writeJSONError(w, http.StatusInternalServerError, "failed to list users")
			return
		}

		resp := make([]UserResponse, 0, len(users))
		for _, u := range users {
			resp = append(resp, UserResponse{ID: u.ID, Username: u.Username})
		}

		writeJSON(w, http.StatusOK, resp)
	}
}
