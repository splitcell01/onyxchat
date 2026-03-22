package http

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

type uploadKeyRequest struct {
	PublicKey string `json:"publicKey"`
}

func UploadKeyHandler(userStore userStorer) http.HandlerFunc {
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

		if err := userStore.SetPublicKey(user.ID, req.PublicKey); err != nil {
			http.Error(w, "failed to save key", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

type getKeyResponse struct {
	Username  string `json:"username"`
	PublicKey string `json:"publicKey"`
}

func GetKeyHandler(userStore userStorer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		username := vars["username"]
		if username == "" {
			http.Error(w, "username required", http.StatusBadRequest)
			return
		}

		key, err := userStore.GetPublicKeyByUsername(username)
		if err != nil || key == "" {
			http.Error(w, "key not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(getKeyResponse{
			Username:  username,
			PublicKey: key,
		})
	}
}