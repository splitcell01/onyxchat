package http

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

type uploadKeyRequest struct {
	PublicKey string `json:"publicKey"`
}

func UploadKeyHandler(userStore userStorer, hub *Hub, log *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := CurrentUser(r)
		if user == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var req uploadKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if req.PublicKey == "" {
			http.Error(w, "publicKey required", http.StatusBadRequest)
			return
		}
		if len(req.PublicKey) < 80 || len(req.PublicKey) > 256 {
			http.Error(w, "publicKey length invalid", http.StatusBadRequest)
			return
		}
		if _, err := base64.StdEncoding.DecodeString(req.PublicKey); err != nil {
			if _, err2 := base64.RawStdEncoding.DecodeString(req.PublicKey); err2 != nil {
				http.Error(w, "publicKey must be base64-encoded", http.StatusBadRequest)
				return
			}
		}

		dbStart := time.Now()
		err := userStore.SetPublicKey(user.ID, req.PublicKey)
		ObserveDBQuery("key_set", dbStart)
		if err != nil {
			http.Error(w, "failed to save key", http.StatusInternalServerError)
			return
		}

		// Notify contacts who are currently connected so they re-fetch the key
		// and re-derive the shared secret before their next message. Fire-and-forget:
		// the DB lookup must not block or fail the response.
		go func(userID int64, username string) {
			followerIDs, err := userStore.GetContactFollowerIDs(userID)
			if err != nil {
				log.Error("[UploadKey] follower lookup failed", zap.Int64("user", userID), zap.Error(err))
				return
			}
			for _, fid := range followerIDs {
				hub.SendKeyChangedToUser(fid, username)
			}
		}(user.ID, user.Username)

		w.WriteHeader(http.StatusNoContent)
	}
}

type getKeyResponse struct {
	Username  string `json:"username"`
	PublicKey string `json:"publicKey"`
}

func GetKeyHandler(userStore userStorer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		currentUser := CurrentUser(r)
		if currentUser == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		vars := mux.Vars(r)
		username := vars["username"]
		if username == "" {
			http.Error(w, "username required", http.StatusBadRequest)
			return
		}

		dbStart := time.Now()
		target, err := userStore.GetByUsername(username)
		ObserveDBQuery("user_get_by_username", dbStart)
		if err != nil || target == nil {
			http.Error(w, "key not found", http.StatusNotFound)
			return
		}

		// Only contacts may fetch a public key. This prevents an authenticated
		// stranger from harvesting keys for offline MITM prep or user enumeration.
		dbStart = time.Now()
		ok, err := userStore.IsContact(currentUser.ID, target.ID)
		ObserveDBQuery("contact_is_contact", dbStart)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "key not found", http.StatusNotFound)
			return
		}

		if target.PublicKey == "" {
			http.Error(w, "key not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(getKeyResponse{
			Username:  username,
			PublicKey: target.PublicKey,
		})
	}
}
