package http

import (
	"encoding/json"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func ChangePasswordHandler(userStore userStorer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := CurrentUser(r)
		if user == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var req ChangePasswordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		if req.CurrentPassword == "" || req.NewPassword == "" {
			http.Error(w, "current_password and new_password required", http.StatusBadRequest)
			return
		}

		if len(req.NewPassword) < 8 {
			http.Error(w, "new password must be at least 8 characters", http.StatusBadRequest)
			return
		}

		// Fetch the full user record (with password hash) from DB
		existing, err := userStore.GetUserByUsername(user.Username)
		if err != nil {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}

		// Verify current password
		if err := bcrypt.CompareHashAndPassword([]byte(existing.PasswordHash), []byte(req.CurrentPassword)); err != nil {
			http.Error(w, "current password is incorrect", http.StatusUnauthorized)
			return
		}

		// Hash new password
		newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "failed to hash password", http.StatusInternalServerError)
			return
		}

		if err := userStore.UpdatePassword(user.ID, string(newHash)); err != nil {
			http.Error(w, "failed to update password", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
