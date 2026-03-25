package http

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/cole/secure-messenger-server/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// ─────────────────────────────────────────────────────────────
// Health check
// ─────────────────────────────────────────────────────────────

func HealthHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("[Health] /health hit")

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	}); err != nil {
		log.Printf("[Health] failed to write response: %v", err)
	}
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
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Token    string `json:"token"`
}

func RegisterHandler(userStore userStorer, jwtMgr *JWTManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Println("[Register] incoming request")

		var req RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("[Register] invalid JSON: %v", err)
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		req.Username = strings.TrimSpace(req.Username)
		if req.Username == "" || req.Password == "" {
			log.Println("[Register] missing username or password")
			http.Error(w, "username and password required", http.StatusBadRequest)
			return
		}

		if req.InviteCode == "" {
			http.Error(w, "invite code required", http.StatusForbidden)
			return
		}

		hashedBytes, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("[Register] could not hash password: %v", err)
			http.Error(w, "could not hash password", http.StatusInternalServerError)
			return
		}

		// Consume invite code and create user atomically — if user creation
		// fails (e.g. duplicate username) the invite code is not burned.
		user, err := userStore.RegisterWithInvite(req.InviteCode, req.Username, string(hashedBytes))
		if err != nil {
			switch err {
			case store.ErrInvalidInviteCode:
				http.Error(w, "invalid or expired invite code", http.StatusForbidden)
			case store.ErrUsernameTaken:
				http.Error(w, "username already taken", http.StatusConflict)
			default:
				log.Printf("[Register] could not create user: %v", err)
				http.Error(w, "could not create user", http.StatusInternalServerError)
			}
			return
		}

		token, err := jwtMgr.Generate(user)
		if err != nil {
			log.Printf("[Register] failed to generate JWT: %v", err)
			http.Error(w, "failed to generate token", http.StatusInternalServerError)
			return
		}

		resp := RegisterResponse{
			ID:       user.ID,
			Username: user.Username,
			Token:    token,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("[Register] failed to write response: %v", err)
		}
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
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Token    string `json:"token"`
}

func LoginHandler(userStore userStorer, jwtMgr *JWTManager, idLimiter *KeyedLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		req.Username = strings.TrimSpace(req.Username)
		if req.Username == "" || req.Password == "" {
			http.Error(w, "username and password required", http.StatusBadRequest)
			return
		}

		// ✅ rate limit by identifier BEFORE lookup/hash work
		if !idLimiter.Allow(strings.ToLower(req.Username)) {
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}

		user, err := userStore.GetUserByUsername(req.Username)
		if err != nil {
			if err == store.ErrUserNotFound {
				http.Error(w, "invalid credentials", http.StatusUnauthorized)
			} else {
				log.Printf("[Login] database error looking up user: %v", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		token, err := jwtMgr.Generate(user)
		if err != nil {
			http.Error(w, "failed to generate token", http.StatusInternalServerError)
			return
		}

		resp := LoginResponse{ID: user.ID, Username: user.Username, Token: token}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// ─────────────────────────────────────────────────────────────
// List users (contacts)
// ─────────────────────────────────────────────────────────────

type UserResponse struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

func ListUsersHandler(userStore userStorer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, err := userStore.ListUsers()
		if err != nil {
			log.Printf("[ListUsers] failed to list users: %v", err)
			http.Error(w, "failed to list users", http.StatusInternalServerError)
			return
		}

		resp := make([]UserResponse, 0, len(users))
		for _, u := range users {
			resp = append(resp, UserResponse{
				ID:       u.ID,
				Username: u.Username,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("[ListUsers] failed to write response: %v", err)
		}
	}
}