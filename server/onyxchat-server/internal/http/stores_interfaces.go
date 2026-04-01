package http

import (
	"context"
	"time"

	"github.com/cole/onyxchat-server/internal/store"
)

// userStorer is the subset of *store.UserStore used by HTTP handlers.
// Defining it here lets tests inject a fake without touching the store package.
type userStorer interface {
	RegisterWithInvite(code, username, passwordHash string) (*store.User, error)
	CreateUser(username, passwordHash string) (*store.User, error)
	GetUserByUsername(username string) (*store.User, error)
	GetByUsername(username string) (*store.User, error)
	GetUserByID(userID int64) (*store.User, error)
	ListUsers() ([]*store.User, error)
	ConsumeInviteCode(code, username string) error
	SetPublicKey(userID int64, pubKey string) error
	GetPublicKeyByUsername(username string) (string, error)
	Ping(ctx context.Context) error
	AdminListInvites() ([]store.InviteCodeFull, error)
	AdminCreateInvite(code, createdBy string, expiresAt *time.Time) (*store.InviteCodeFull, error)
	AdminResetInvite(code string) error
	UpdatePassword(userID int64, newHash string) error

	ListContacts(userID int64) ([]*store.Contact, error)
	AddContact(userID int64, username string) error
	RemoveContact(userID int64, username string) error
	DeleteAccountGDPR(userID int64) (*store.GDPRDeletionRecord, error)
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
