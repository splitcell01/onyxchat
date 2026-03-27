package http

// internal/http/admin_handlers.go

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// ─────────────────────────────────────────────────────────────
// Wire-format types
// ─────────────────────────────────────────────────────────────

type inviteResponse struct {
	ID        int64   `json:"id"`
	Code      string  `json:"code"`
	CreatedBy string  `json:"created_by"`
	UsedBy    *string `json:"used_by"`
	UsedAt    *string `json:"used_at"`    // ISO 8601 or null
	ExpiresAt *string `json:"expires_at"` // ISO 8601 or null
	CreatedAt string  `json:"created_at"`
}

type createInviteRequest struct {
	// Optional: if omitted a random code is generated.
	Code string `json:"code"`
	// Days until expiry. 0 or omitted = no expiry.
	ExpiresDays int `json:"expires_days"`
}

// ─────────────────────────────────────────────────────────────
// GET /api/v1/admin/invites — list all codes
// ─────────────────────────────────────────────────────────────

func AdminListInvitesHandler(userStore userStorer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		codes, err := userStore.AdminListInvites()
		if err != nil {
			log.Printf("[AdminListInvites] %v", err)
			http.Error(w, "failed to list invite codes", http.StatusInternalServerError)
			return
		}

		resp := make([]inviteResponse, 0, len(codes))
		for _, c := range codes {
			resp = append(resp, inviteResponse{
				ID:        c.ID,
				Code:      c.Code,
				CreatedBy: c.CreatedBy,
				UsedBy:    c.UsedBy,
				UsedAt:    nullTimeToString(c.UsedAt),
				ExpiresAt: nullTimeToString(c.ExpiresAt),
				CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// ─────────────────────────────────────────────────────────────
// POST /api/v1/admin/invites — create a new code
// ─────────────────────────────────────────────────────────────

func AdminCreateInviteHandler(userStore userStorer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		caller := CurrentUser(r)

		var req createInviteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		code := strings.TrimSpace(req.Code)
		if code == "" {
			code = randomCode(strings.ToUpper(caller.Username))
		}

		var expiresAt *time.Time
		if req.ExpiresDays > 0 {
			t := time.Now().UTC().AddDate(0, 0, req.ExpiresDays)
			expiresAt = &t
		}

		created, err := userStore.AdminCreateInvite(code, caller.Username, expiresAt)
		if err != nil {
			if strings.Contains(err.Error(), "already exists") {
				http.Error(w, "invite code already exists", http.StatusConflict)
				return
			}
			log.Printf("[AdminCreateInvite] %v", err)
			http.Error(w, "failed to create invite code", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(inviteResponse{
			ID:        created.ID,
			Code:      created.Code,
			CreatedBy: created.CreatedBy,
			UsedBy:    created.UsedBy,
			UsedAt:    nullTimeToString(created.UsedAt),
			ExpiresAt: nullTimeToString(created.ExpiresAt),
			CreatedAt: created.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
}

// ─────────────────────────────────────────────────────────────
// POST /api/v1/admin/invites/{code}/reset — un-burn a code
// ─────────────────────────────────────────────────────────────

func AdminResetInviteHandler(userStore userStorer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := mux.Vars(r)["code"]
		if code == "" {
			http.Error(w, "code is required", http.StatusBadRequest)
			return
		}

		if err := userStore.AdminResetInvite(code); err != nil {
			if strings.Contains(err.Error(), "not found") {
				http.Error(w, "invite code not found", http.StatusNotFound)
				return
			}
			log.Printf("[AdminResetInvite] %v", err)
			http.Error(w, "failed to reset invite code", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "code": code})
	}
}

// ─────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────

// charset omits O/0 and I/1 to avoid visual confusion.
const inviteCharset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func randomCode(prefix string) string {
	b := make([]byte, 6)
	for i := range b {
		b[i] = inviteCharset[rand.Intn(len(inviteCharset))]
	}
	// e.g. ASHENSPELLBOOK-ABC-123
	return prefix + "-" + string(b[:3]) + "-" + string(b[3:])
}

func nullTimeToString(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func AdminOnly(allowedUsername string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := CurrentUser(r)
			if user == nil || user.Username != allowedUsername {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
