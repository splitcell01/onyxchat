package http

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/cole/secure-messenger-server/internal/store"
)

// ─────────────────────────────────────────────────────────────
// DELETE /api/v1/account  — GDPR right to erasure
// ─────────────────────────────────────────────────────────────

// DeleteAccountHandler performs a GDPR-compliant account deletion.
// Requires the user to confirm their password to prevent accidental deletion.
func DeleteAccountHandler(userStore userStorer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cu := CurrentUser(r)
		if cu == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Require password confirmation — prevents accidents and CSRF
		var req struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Password) == "" {
			http.Error(w, "password confirmation required", http.StatusBadRequest)
			return
		}

		// Verify password before deleting
		user, err := userStore.GetUserByUsername(cu.Username)
		if err != nil {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		if err := bcryptCompare(user.PasswordHash, req.Password); err != nil {
			http.Error(w, "invalid password", http.StatusUnauthorized)
			return
		}

		record, err := userStore.DeleteAccountGDPR(cu.UserID)
		if err != nil {
			switch {
			case errors.Is(err, store.ErrAlreadyDeleted):
				http.Error(w, "account already deleted", http.StatusGone)
			default:
				log.Printf("[DeleteAccount] GDPR deletion failed for user %d: %v", cu.ID, err)
				http.Error(w, "failed to delete account", http.StatusInternalServerError)
			}
			return
		}

		log.Printf("[DeleteAccount] GDPR deletion complete: user=%d messages_purged=%d invites_expired=%d",
			cu.ID, record.MessagesPurged, record.InvitesExpired)

		w.WriteHeader(http.StatusNoContent) // 204 — success, no body
	}
}

// ─────────────────────────────────────────────────────────────
// GET /api/v1/contacts
// ─────────────────────────────────────────────────────────────

func ListContactsHandler(userStore userStorer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cu := CurrentUser(r)
		if cu == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		contacts, err := userStore.ListContacts(cu.ID)
		if err != nil {
			log.Printf("[ListContacts] error for user %d: %v", cu.ID, err)
			http.Error(w, "failed to list contacts", http.StatusInternalServerError)
			return
		}

		type contactResp struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
			Online   bool   `json:"online"`
		}

		resp := make([]contactResp, 0, len(contacts))
		for _, c := range contacts {
			resp = append(resp, contactResp{
				ID:       c.ID,
				Username: c.Username,
				Online:   false, // frontend merges with WS presence events
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// ─────────────────────────────────────────────────────────────
// POST /api/v1/contacts
// Body: { "username": "alice" }
// ─────────────────────────────────────────────────────────────

func AddContactHandler(userStore userStorer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cu := CurrentUser(r)
		if cu == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var req struct {
			Username string `json:"username"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		req.Username = strings.TrimSpace(req.Username)
		if req.Username == "" {
			http.Error(w, "username is required", http.StatusBadRequest)
			return
		}
		if strings.EqualFold(req.Username, cu.Username) {
			http.Error(w, "cannot add yourself", http.StatusBadRequest)
			return
		}

		if err := userStore.AddContact(cu.ID, req.Username); err != nil {
			switch {
			case errors.Is(err, store.ErrUserNotFound):
				http.Error(w, "user not found", http.StatusNotFound)
			case errors.Is(err, store.ErrContactExists):
				http.Error(w, "already in contacts", http.StatusConflict)
			default:
				log.Printf("[AddContact] error: %v", err)
				http.Error(w, "failed to add contact", http.StatusInternalServerError)
			}
			return
		}

		w.WriteHeader(http.StatusCreated)
	}
}

// ─────────────────────────────────────────────────────────────
// DELETE /api/v1/contacts/{username}
// ─────────────────────────────────────────────────────────────

func RemoveContactHandler(userStore userStorer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cu := CurrentUser(r)
		if cu == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		targetUsername := mux.Vars(r)["username"]
		if targetUsername == "" {
			http.Error(w, "username is required", http.StatusBadRequest)
			return
		}

		if err := userStore.RemoveContact(cu.ID, targetUsername); err != nil {
			if errors.Is(err, store.ErrContactNotFound) {
				http.Error(w, "contact not found", http.StatusNotFound)
				return
			}
			log.Printf("[RemoveContact] error: %v", err)
			http.Error(w, "failed to remove contact", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}