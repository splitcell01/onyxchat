package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/cole/onyxchat-server/internal/store"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// ─────────────────────────────────────────────────────────────
// DELETE /api/v1/account  — GDPR right to erasure
// ─────────────────────────────────────────────────────────────

// DeleteAccountHandler performs a GDPR-compliant account deletion.
// Requires the user to confirm their password to prevent accidental deletion.
func DeleteAccountHandler(userStore userStorer, log *zap.Logger) http.HandlerFunc {
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
		dbStart := time.Now()
		user, err := userStore.GetUserByUsername(cu.Username)
		ObserveDBQuery("user_get_by_username", dbStart)
		if err != nil {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		if err := bcrypt.CompareHashAndPassword(
			[]byte(user.PasswordHash),
			[]byte(req.Password),
		); err != nil {
			http.Error(w, "invalid password", http.StatusUnauthorized)
			return
		}

		dbStart = time.Now()
		record, err := userStore.DeleteAccountGDPR(cu.ID)
		ObserveDBQuery("account_delete", dbStart)
		if err != nil {
			switch {
			case errors.Is(err, store.ErrAlreadyDeleted):
				http.Error(w, "account already deleted", http.StatusGone)
			default:
				log.Error("[DeleteAccount] GDPR deletion failed", zap.Int64("user_id", cu.ID), zap.Error(err))
				writeJSONError(w, http.StatusInternalServerError, "failed to delete account")
			}
			return
		}

		log.Info("[DeleteAccount] GDPR deletion complete",
			zap.Int64("user_id", cu.ID),
			zap.Int("messages_purged", record.MessagesPurged),
			zap.Int("invites_expired", record.InvitesExpired),
		)

		w.WriteHeader(http.StatusNoContent) // 204 — success, no body
	}
}

// ─────────────────────────────────────────────────────────────
// GET /api/v1/contacts
// ─────────────────────────────────────────────────────────────

func ListContactsHandler(userStore userStorer, log *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cu := CurrentUser(r)
		if cu == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		dbStart := time.Now()
		contacts, err := userStore.ListContacts(cu.ID)
		ObserveDBQuery("contact_list", dbStart)
		if err != nil {
			log.Error("[ListContacts] error", zap.Int64("user_id", cu.ID), zap.Error(err))
			writeJSONError(w, http.StatusInternalServerError, "failed to list contacts")
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

func AddContactHandler(userStore userStorer, log *zap.Logger) http.HandlerFunc {
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

		dbStart := time.Now()
		err := userStore.AddContact(cu.ID, req.Username)
		ObserveDBQuery("contact_add", dbStart)
		if err != nil {
			switch {
			case errors.Is(err, store.ErrUserNotFound):
				http.Error(w, "user not found", http.StatusNotFound)
			case errors.Is(err, store.ErrContactExists):
				http.Error(w, "already in contacts", http.StatusConflict)
			default:
				log.Error("[AddContact] error", zap.Error(err))
				writeJSONError(w, http.StatusInternalServerError, "failed to add contact")
			}
			return
		}

		w.WriteHeader(http.StatusCreated)
	}
}

// ─────────────────────────────────────────────────────────────
// DELETE /api/v1/contacts/{username}
// ─────────────────────────────────────────────────────────────

func RemoveContactHandler(userStore userStorer, log *zap.Logger) http.HandlerFunc {
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

		dbStart := time.Now()
		err := userStore.RemoveContact(cu.ID, targetUsername)
		ObserveDBQuery("contact_remove", dbStart)
		if err != nil {
			if errors.Is(err, store.ErrContactNotFound) {
				http.Error(w, "contact not found", http.StatusNotFound)
				return
			}
			log.Error("[RemoveContact] error", zap.Error(err))
			writeJSONError(w, http.StatusInternalServerError, "failed to remove contact")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
