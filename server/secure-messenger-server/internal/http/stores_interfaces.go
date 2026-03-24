package http

import (
	"context"

	"github.com/cole/secure-messenger-server/internal/store"
)

// userStorer is the subset of *store.UserStore used by HTTP handlers.
// Defining it here lets tests inject a fake without touching the store package.
type userStorer interface {
	RegisterWithInvite(code, username, passwordHash string) (*store.User, error)
	CreateUser(username, passwordHash string) (*store.User, error)
	GetUserByUsername(username string) (*store.User, error)
	GetByUsername(username string) (*store.User, error)
	ListUsers() ([]*store.User, error)
	ConsumeInviteCode(code, username string) error
	SetPublicKey(userID int64, pubKey string) error
	GetPublicKeyByUsername(username string) (string, error)
	Ping(ctx context.Context) error
}

// messageStorer is the subset of *store.MessageStore used by HTTP handlers.
type messageStorer interface {
	CreateOrGetExisting(senderID, recipientID int64, body, iv string, encrypted bool, clientMessageID string) (*store.Message, bool, error)
	ListConversationSince(userID, peerID, sinceID int64) ([]store.Message, error)
	GetByID(id int64) (*store.Message, error)
	GetUnreadForUser(userID, sinceID int64) ([]store.Message, error)
}

// Compile-time checks: the real store types must satisfy the interfaces.
var _ userStorer = (*store.UserStore)(nil)
var _ messageStorer = (*store.MessageStore)(nil)


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