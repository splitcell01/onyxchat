package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cole/secure-messenger-server/internal/store"
	"golang.org/x/time/rate"
)

// ─────────────────────────────────────────────────────────────
// Fake stores — no DB required
// ─────────────────────────────────────────────────────────────

type fakeUserStore struct {
	users       map[string]*store.User
	nextID      int64
	inviteCodes map[string]bool // code → still available
	publicKeys  map[int64]string
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{
		users:       make(map[string]*store.User),
		inviteCodes: make(map[string]bool),
		publicKeys:  make(map[int64]string),
		nextID:      1,
	}
}

func (f *fakeUserStore) addInvite(code string) { f.inviteCodes[code] = true }

func (f *fakeUserStore) CreateUser(username, passwordHash string) (*store.User, error) {
	if _, exists := f.users[username]; exists {
		return nil, errors.New("username already taken")
	}
	u := &store.User{ID: f.nextID, Username: username, PasswordHash: passwordHash}
	f.nextID++
	f.users[username] = u
	return u, nil
}

func (f *fakeUserStore) GetUserByUsername(username string) (*store.User, error) {
	u, ok := f.users[username]
	if !ok {
		return nil, store.ErrUserNotFound
	}
	return u, nil
}

func (f *fakeUserStore) GetByUsername(username string) (*store.User, error) {
	return f.GetUserByUsername(username)
}

func (f *fakeUserStore) ListUsers() ([]*store.User, error) {
	out := make([]*store.User, 0, len(f.users))
	for _, u := range f.users {
		out = append(out, u)
	}
	return out, nil
}

func (f *fakeUserStore) ConsumeInviteCode(code, _ string) error {
	available, exists := f.inviteCodes[code]
	if !exists || !available {
		return errors.New("invalid or already used invite code")
	}
	f.inviteCodes[code] = false
	return nil
}

func (f *fakeUserStore) SetPublicKey(userID int64, pubKey string) error {
	f.publicKeys[userID] = pubKey
	return nil
}

func (f *fakeUserStore) GetPublicKeyByUsername(username string) (string, error) {
	u, ok := f.users[username]
	if !ok {
		return "", store.ErrUserNotFound
	}
	return f.publicKeys[u.ID], nil
}

func (f *fakeUserStore) Ping(_ context.Context) error { return nil }

// ─────────────────────────────────────────────────────────────

type fakeMessageStore struct {
	messages []*store.Message
	nextID   int64
}

func newFakeMessageStore() *fakeMessageStore { return &fakeMessageStore{nextID: 1} }

func (f *fakeMessageStore) CreateOrGetExisting(
	senderID, recipientID int64,
	body, iv string,
	encrypted bool,
	clientMessageID string,
) (*store.Message, bool, error) {
	for _, m := range f.messages {
		if m.SenderID == senderID && m.ClientMessageID == clientMessageID {
			return m, false, nil // deduplicated
		}
	}
	m := &store.Message{
		ID: f.nextID, SenderID: senderID, RecipientID: recipientID,
		Body: body, IV: iv, Encrypted: encrypted, ClientMessageID: clientMessageID,
		CreatedAt: time.Now(),
	}
	f.nextID++
	f.messages = append(f.messages, m)
	return m, true, nil
}

func (f *fakeMessageStore) ListConversationSince(userID, peerID, sinceID int64) ([]store.Message, error) {
	var out []store.Message
	for _, m := range f.messages {
		if m.ID > sinceID &&
			((m.SenderID == userID && m.RecipientID == peerID) ||
				(m.SenderID == peerID && m.RecipientID == userID)) {
			out = append(out, *m)
		}
	}
	return out, nil
}

func (f *fakeMessageStore) GetByID(id int64) (*store.Message, error) {
	for _, m := range f.messages {
		if m.ID == id {
			return m, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (f *fakeMessageStore) GetUnreadForUser(userID, sinceID int64) ([]store.Message, error) {
	var out []store.Message
	for _, m := range f.messages {
		if m.RecipientID == userID && m.ID > sinceID {
			out = append(out, *m)
		}
	}
	return out, nil
}

// fakePublisher satisfies EventPublisher without Redis.
type fakePublisher struct{}

func (p *fakePublisher) PublishMessageCreated(_ context.Context, _ MessageCreatedEvent) error {
	return nil
}

// ─────────────────────────────────────────────────────────────
// Test helpers
// ─────────────────────────────────────────────────────────────

func newTestJWT() *JWTManager { return NewJWTManager("test-secret") }

// openLimiter returns a KeyedLimiter that always allows requests.
func openLimiter() *KeyedLimiter {
	return NewKeyedLimiter(rate.Limit(1000), 1000, time.Minute)
}

func mustMarshal(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return bytes.NewBuffer(b)
}

// injectUser wraps a handler and injects an authenticated user into ctx.
func injectUser(next http.Handler, u *AuthUser) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), userContextKey, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// registerUser is a test helper that runs a full registration via the handler
// so the password is properly bcrypt-hashed in the fake store.
func registerUser(t *testing.T, us *fakeUserStore, jwtMgr *JWTManager, username, password, inviteCode string) {
	t.Helper()
	us.addInvite(inviteCode)
	body := mustMarshal(t, map[string]string{
		"username":    username,
		"password":    password,
		"invite_code": inviteCode,
	})
	rr := httptest.NewRecorder()
	RegisterHandler(us, jwtMgr)(rr, httptest.NewRequest(http.MethodPost, "/register", body))
	if rr.Code != http.StatusOK {
		t.Fatalf("registerUser setup failed: %d %s", rr.Code, rr.Body.String())
	}
}

func (f *fakeUserStore) AdminListInvites() ([]store.InviteCodeFull, error) {
    return nil, nil
}

func (f *fakeUserStore) AdminCreateInvite(code, createdBy string, expiresAt *time.Time) (*store.InviteCodeFull, error) {
    return nil, nil
}

func (f *fakeUserStore) AdminResetInvite(code string) error {
    return nil
}

func (f *fakeUserStore) UpdatePassword(userID int64, newHash string) error {
    return nil
}

// ─────────────────────────────────────────────────────────────
// RegisterHandler
// ─────────────────────────────────────────────────────────────

func TestRegisterHandler_Success(t *testing.T) {
	us := newFakeUserStore()
	us.addInvite("VALID-CODE")
	h := RegisterHandler(us, newTestJWT())

	body := mustMarshal(t, map[string]string{
		"username": "alice", "password": "secret123", "invite_code": "VALID-CODE",
	})
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/register", body))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp RegisterResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Username != "alice" {
		t.Fatalf("expected username=alice, got %q", resp.Username)
	}
	if resp.Token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestRegisterHandler_MissingInviteCode(t *testing.T) {
	h := RegisterHandler(newFakeUserStore(), newTestJWT())
	body := mustMarshal(t, map[string]string{"username": "alice", "password": "secret123"})
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/register", body))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestRegisterHandler_InvalidInviteCode(t *testing.T) {
	h := RegisterHandler(newFakeUserStore(), newTestJWT())
	body := mustMarshal(t, map[string]string{
		"username": "alice", "password": "secret123", "invite_code": "BOGUS",
	})
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/register", body))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestRegisterHandler_InviteCodeSingleUse(t *testing.T) {
	us := newFakeUserStore()
	us.addInvite("ONCE")
	h := RegisterHandler(us, newTestJWT())

	wantCodes := []int{http.StatusOK, http.StatusForbidden}
	for i, want := range wantCodes {
		body := mustMarshal(t, map[string]string{
			"username":    "user" + string(rune('a'+i)),
			"password":    "secret123",
			"invite_code": "ONCE",
		})
		rr := httptest.NewRecorder()
		h(rr, httptest.NewRequest(http.MethodPost, "/register", body))
		if rr.Code != want {
			t.Fatalf("attempt %d: expected %d, got %d", i+1, want, rr.Code)
		}
	}
}

func TestRegisterHandler_MissingUsername(t *testing.T) {
	us := newFakeUserStore()
	us.addInvite("CODE")
	h := RegisterHandler(us, newTestJWT())
	body := mustMarshal(t, map[string]string{"password": "secret123", "invite_code": "CODE"})
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/register", body))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestRegisterHandler_MissingPassword(t *testing.T) {
	us := newFakeUserStore()
	us.addInvite("CODE")
	h := RegisterHandler(us, newTestJWT())
	body := mustMarshal(t, map[string]string{"username": "alice", "invite_code": "CODE"})
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/register", body))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestRegisterHandler_InvalidJSON(t *testing.T) {
	h := RegisterHandler(newFakeUserStore(), newTestJWT())
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString("not-json")))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ─────────────────────────────────────────────────────────────
// LoginHandler
// ─────────────────────────────────────────────────────────────

func TestLoginHandler_Success(t *testing.T) {
	us := newFakeUserStore()
	jwtMgr := newTestJWT()
	registerUser(t, us, jwtMgr, "alice", "correct-password", "CODE1")

	h := LoginHandler(us, jwtMgr, openLimiter())
	body := mustMarshal(t, map[string]string{"username": "alice", "password": "correct-password"})
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/login", body))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp LoginResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("expected non-empty token")
	}
	if resp.Username != "alice" {
		t.Fatalf("expected username=alice, got %q", resp.Username)
	}
}

func TestLoginHandler_WrongPassword(t *testing.T) {
	us := newFakeUserStore()
	jwtMgr := newTestJWT()
	registerUser(t, us, jwtMgr, "alice", "correct-password", "CODE2")

	h := LoginHandler(us, jwtMgr, openLimiter())
	body := mustMarshal(t, map[string]string{"username": "alice", "password": "wrong"})
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/login", body))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestLoginHandler_UnknownUser(t *testing.T) {
	h := LoginHandler(newFakeUserStore(), newTestJWT(), openLimiter())
	body := mustMarshal(t, map[string]string{"username": "nobody", "password": "pass"})
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/login", body))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestLoginHandler_MissingFields(t *testing.T) {
	h := LoginHandler(newFakeUserStore(), newTestJWT(), openLimiter())
	body := mustMarshal(t, map[string]string{"username": "alice"}) // no password
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/login", body))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLoginHandler_InvalidJSON(t *testing.T) {
	h := LoginHandler(newFakeUserStore(), newTestJWT(), openLimiter())
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString("{bad")))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ─────────────────────────────────────────────────────────────
// ListUsersHandler
// ─────────────────────────────────────────────────────────────

func TestListUsersHandler_ReturnsAllUsers(t *testing.T) {
	us := newFakeUserStore()
	us.users["alice"] = &store.User{ID: 1, Username: "alice"}
	us.users["bob"] = &store.User{ID: 2, Username: "bob"}

	h := injectUser(http.HandlerFunc(ListUsersHandler(us)), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/users", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var users []UserResponse
	if err := json.NewDecoder(rr.Body).Decode(&users); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
}

func TestListUsersHandler_EmptyStore(t *testing.T) {
	h := injectUser(http.HandlerFunc(ListUsersHandler(newFakeUserStore())), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/users", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var users []UserResponse
	if err := json.NewDecoder(rr.Body).Decode(&users); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("expected 0 users, got %d", len(users))
	}
}

// ─────────────────────────────────────────────────────────────
// SendMessageHandler
// ─────────────────────────────────────────────────────────────

func setupSend(t *testing.T) (*fakeUserStore, *fakeMessageStore, *Hub, *fakePublisher) {
	t.Helper()
	us := newFakeUserStore()
	us.users["alice"] = &store.User{ID: 1, Username: "alice"}
	us.users["bob"] = &store.User{ID: 2, Username: "bob"}
	return us, newFakeMessageStore(), NewHub(), &fakePublisher{}
}

func sendHandler(us *fakeUserStore, ms *fakeMessageStore, hub *Hub, pub *fakePublisher, asUser *AuthUser) http.Handler {
	return injectUser(http.HandlerFunc(SendMessageHandler(us, ms, hub, pub)), asUser)
}

func TestSendMessageHandler_Success(t *testing.T) {
	us, ms, hub, pub := setupSend(t)
	h := sendHandler(us, ms, hub, pub, &AuthUser{ID: 1, Username: "alice"})

	body := mustMarshal(t, map[string]string{
		"recipientUsername": "bob", "body": "hello", "clientMessageId": "msg-001",
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/messages", body))
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSendMessageHandler_Deduplication(t *testing.T) {
	us, ms, hub, pub := setupSend(t)
	h := sendHandler(us, ms, hub, pub, &AuthUser{ID: 1, Username: "alice"})

	payload := map[string]string{
		"recipientUsername": "bob", "body": "hello", "clientMessageId": "msg-dup",
	}
	// First send → 201 Created
	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, httptest.NewRequest(http.MethodPost, "/api/v1/messages", mustMarshal(t, payload)))
	if rr1.Code != http.StatusCreated {
		t.Fatalf("first: expected 201, got %d", rr1.Code)
	}
	// Duplicate → 200 OK (idempotent)
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, httptest.NewRequest(http.MethodPost, "/api/v1/messages", mustMarshal(t, payload)))
	if rr2.Code != http.StatusOK {
		t.Fatalf("duplicate: expected 200, got %d", rr2.Code)
	}
}

func TestSendMessageHandler_UnknownRecipient(t *testing.T) {
	us, ms, hub, pub := setupSend(t)
	h := sendHandler(us, ms, hub, pub, &AuthUser{ID: 1, Username: "alice"})

	body := mustMarshal(t, map[string]string{
		"recipientUsername": "nobody", "body": "hello", "clientMessageId": "msg-002",
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/messages", body))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestSendMessageHandler_Unauthenticated(t *testing.T) {
	us, ms, hub, pub := setupSend(t)
	// No injectUser wrapper — no auth in context
	h := http.HandlerFunc(SendMessageHandler(us, ms, hub, pub))

	body := mustMarshal(t, map[string]string{
		"recipientUsername": "bob", "body": "hello", "clientMessageId": "msg-003",
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/messages", body))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestSendMessageHandler_MissingClientMessageID(t *testing.T) {
	us, ms, hub, pub := setupSend(t)
	h := sendHandler(us, ms, hub, pub, &AuthUser{ID: 1, Username: "alice"})

	body := mustMarshal(t, map[string]string{
		"recipientUsername": "bob", "body": "hello",
		// clientMessageId intentionally omitted
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/messages", body))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestSendMessageHandler_EncryptedWithoutIV(t *testing.T) {
	us, ms, hub, pub := setupSend(t)
	h := sendHandler(us, ms, hub, pub, &AuthUser{ID: 1, Username: "alice"})

	body := mustMarshal(t, map[string]any{
		"recipientUsername": "bob",
		"body":              "ciphertext",
		"clientMessageId":   "msg-004",
		"encrypted":         true,
		// iv intentionally omitted
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/messages", body))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestSendMessageHandler_BodyTooLong(t *testing.T) {
	us, ms, hub, pub := setupSend(t)
	h := sendHandler(us, ms, hub, pub, &AuthUser{ID: 1, Username: "alice"})

	longBody := string(make([]byte, maxMessageLen+1))
	body := mustMarshal(t, map[string]string{
		"recipientUsername": "bob", "body": longBody, "clientMessageId": "msg-005",
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/messages", body))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestSendMessageHandler_InvalidJSON(t *testing.T) {
	us, ms, hub, pub := setupSend(t)
	h := sendHandler(us, ms, hub, pub, &AuthUser{ID: 1, Username: "alice"})

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/messages", bytes.NewBufferString("{bad")))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ─────────────────────────────────────────────────────────────
// ListMessagesHandler
// ─────────────────────────────────────────────────────────────

func TestListMessagesHandler_ReturnsPeerMessages(t *testing.T) {
	us := newFakeUserStore()
	us.users["alice"] = &store.User{ID: 1, Username: "alice"}
	us.users["bob"] = &store.User{ID: 2, Username: "bob"}
	ms := newFakeMessageStore()
	ms.messages = []*store.Message{
		{ID: 1, SenderID: 1, RecipientID: 2, Body: "hey bob"},
		{ID: 2, SenderID: 2, RecipientID: 1, Body: "hey alice"},
	}

	h := injectUser(http.HandlerFunc(ListMessagesHandler(us, ms)), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/messages?peer=bob", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp ListMessagesResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(resp.Messages))
	}
}

func TestListMessagesHandler_SinceIDFilters(t *testing.T) {
	us := newFakeUserStore()
	us.users["alice"] = &store.User{ID: 1, Username: "alice"}
	us.users["bob"] = &store.User{ID: 2, Username: "bob"}
	ms := newFakeMessageStore()
	ms.messages = []*store.Message{
		{ID: 1, SenderID: 1, RecipientID: 2, Body: "old"},
		{ID: 2, SenderID: 1, RecipientID: 2, Body: "new"},
	}

	h := injectUser(http.HandlerFunc(ListMessagesHandler(us, ms)), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/messages?peer=bob&sinceId=1", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp ListMessagesResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp.Messages) != 1 {
		t.Fatalf("expected 1 message after sinceId=1, got %d", len(resp.Messages))
	}
	if resp.Messages[0].Body != "new" {
		t.Fatalf("expected 'new', got %q", resp.Messages[0].Body)
	}
}

func TestListMessagesHandler_MissingPeer(t *testing.T) {
	h := injectUser(
		http.HandlerFunc(ListMessagesHandler(newFakeUserStore(), newFakeMessageStore())),
		&AuthUser{ID: 1, Username: "alice"},
	)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/messages", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestListMessagesHandler_UnknownPeer(t *testing.T) {
	h := injectUser(
		http.HandlerFunc(ListMessagesHandler(newFakeUserStore(), newFakeMessageStore())),
		&AuthUser{ID: 1, Username: "alice"},
	)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/messages?peer=ghost", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestListMessagesHandler_Unauthenticated(t *testing.T) {
	h := http.HandlerFunc(ListMessagesHandler(newFakeUserStore(), newFakeMessageStore()))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/messages?peer=bob", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

// ─────────────────────────────────────────────────────────────
// SendMessageRequest.Validate (pure function — no fakes needed)
// ─────────────────────────────────────────────────────────────

func TestSendMessageRequest_Validate(t *testing.T) {
	base := SendMessageRequest{
		RecipientUsername: "bob",
		Body:              "hello",
		ClientMessageID:   "abc-123",
	}

	cases := []struct {
		name    string
		mutate  func(*SendMessageRequest)
		wantErr bool
	}{
		{"valid plaintext", func(_ *SendMessageRequest) {}, false},
		{"missing recipient", func(r *SendMessageRequest) { r.RecipientUsername = "" }, true},
		{"missing body", func(r *SendMessageRequest) { r.Body = "" }, true},
		{"missing clientMessageId", func(r *SendMessageRequest) { r.ClientMessageID = "" }, true},
		{"recipient too long", func(r *SendMessageRequest) { r.RecipientUsername = string(make([]byte, maxUsernameLen+1)) }, true},
		{"body too long", func(r *SendMessageRequest) { r.Body = string(make([]byte, maxMessageLen+1)) }, true},
		{"clientMessageId too long", func(r *SendMessageRequest) { r.ClientMessageID = string(make([]byte, 129)) }, true},
		{"encrypted missing iv", func(r *SendMessageRequest) { r.Encrypted = true }, true},
		{"encrypted with iv", func(r *SendMessageRequest) { r.Encrypted = true; r.IV = "nonce" }, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := base // copy
			tc.mutate(&req)
			got := req.Validate()
			if tc.wantErr && got == "" {
				t.Fatal("expected validation error, got none")
			}
			if !tc.wantErr && got != "" {
				t.Fatalf("expected no error, got %q", got)
			}
		})
	}
}