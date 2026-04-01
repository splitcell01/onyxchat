package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

const ticketTTL = 30 * time.Second
const ticketPrefix = "ws:ticket:"

func generateTicket() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func WSTicketHandler(rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := CurrentUser(r)
		if user == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ticket, err := generateTicket()
		if err != nil {
			http.Error(w, "failed to generate ticket", http.StatusInternalServerError)
			return
		}

		// Store "userID:username" under the ticket key, expires in 30s
		val := fmt.Sprintf("%d:%s", user.ID, user.Username)
		if err := rdb.Set(context.Background(), ticketPrefix+ticket, val, ticketTTL).Err(); err != nil {
			http.Error(w, "failed to store ticket", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"ticket": ticket})
	}
}
