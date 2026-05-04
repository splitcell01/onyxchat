package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/cole/onyxchat-server/internal/store"
	"github.com/gorilla/mux"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// validTestKey is a 65-byte value base64-encoded to 88 chars (valid P-256 key length range).
var validTestKey = base64.StdEncoding.EncodeToString(make([]byte, 65))

func newTestRDB(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

// ─────────────────────────────────────────────────────────────
// Fake stores — no DB required
// ─────────────────────────────────────────────────────────────

type fakeUserStore struct {
	users            map[string]*store.User
	nextID           int64
	inviteCodes      map[string]bool // code → still available
	publicKeys       map[int64]string
	contactFollowers map[int64][]int64 // userID → IDs of users who have them as a contact

	// Configurable per-test fields
	contacts          map[int64][]*store.Contact
	isContactMap      map[[2]int64]bool
	addContactErr     error
	removeContactErr  error
	deleteGDPRErr     error
	pingErr           error
	adminInvites      []store.InviteCodeFull
	adminListErr      error
	adminCreateResult *store.InviteCodeFull
	adminCreateErr    error
	adminResetErr     error
	updatePasswordErr error
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{
		users:            make(map[string]*store.User),
		inviteCodes:      make(map[string]bool),
		publicKeys:       make(map[int64]string),
		contactFollowers: make(map[int64][]int64),
		contacts:         make(map[int64][]*store.Contact),
		isContactMap:     make(map[[2]int64]bool),
		nextID:           1,
	}
}

func (f *fakeUserStore) RegisterWithInvite(code, username, passwordHash string) (*store.User, error) {
	if err := f.ConsumeInviteCode(code, username); err != nil {
		return nil, err
	}
	return f.CreateUser(username, passwordHash)
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

func (f *fakeUserStore) SearchUsers(query string) ([]*store.User, error) {
	out := make([]*store.User, 0)
	for _, u := range f.users {
		if strings.Contains(strings.ToLower(u.Username), strings.ToLower(query)) {
			out = append(out, u)
		}
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

func (f *fakeUserStore) Ping(_ context.Context) error { return f.pingErr }

func (f *fakeUserStore) GetUserByID(userID int64) (*store.User, error) {
	for _, u := range f.users {
		if u.ID == userID {
			return u, nil
		}
	}
	return nil, store.ErrUserNotFound
}

func (f *fakeUserStore) ListContacts(userID int64) ([]*store.Contact, error) {
	if cs, ok := f.contacts[userID]; ok {
		return cs, nil
	}
	return []*store.Contact{}, nil
}

func (f *fakeUserStore) GetContactFollowerIDs(userID int64) ([]int64, error) {
	return f.contactFollowers[userID], nil
}

func (f *fakeUserStore) IsContact(userID, peerID int64) (bool, error) {
	return f.isContactMap[[2]int64{userID, peerID}], nil
}

func (f *fakeUserStore) AddContact(userID int64, targetUsername string) error {
	return f.addContactErr
}

func (f *fakeUserStore) RemoveContact(userID int64, targetUsername string) error {
	return f.removeContactErr
}

func (f *fakeUserStore) DeleteAccountGDPR(userID int64) (*store.GDPRDeletionRecord, error) {
	if f.deleteGDPRErr != nil {
		return nil, f.deleteGDPRErr
	}
	return &store.GDPRDeletionRecord{
		UserID:           userID,
		MessagesPurged:   0,
		PublicKeyCleared: true,
		InvitesExpired:   0,
	}, nil
}

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

func (f *fakeMessageStore) ListConversationSince(userID, peerID, sinceID int64, limit int) ([]store.Message, bool, error) {
	var out []store.Message
	for _, m := range f.messages {
		if m.ID > sinceID &&
			((m.SenderID == userID && m.RecipientID == peerID) ||
				(m.SenderID == peerID && m.RecipientID == userID)) {
			out = append(out, *m)
		}
	}
	if len(out) > limit {
		return out[:limit], true, nil
	}
	return out, false, nil
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

func (f *fakeMessageStore) DeleteMessage(id, senderID int64) error {
	for i, m := range f.messages {
		if m.ID == id && m.SenderID == senderID {
			f.messages = append(f.messages[:i], f.messages[i+1:]...)
			return nil
		}
	}
	return sql.ErrNoRows
}

// fakePublisher satisfies EventPublisher without Redis.
type fakePublisher struct{}

func (p *fakePublisher) PublishMessageCreated(_ context.Context, _ MessageCreatedEvent) error {
	return nil
}

func (p *fakePublisher) PublishMessageDeleted(_ context.Context, _ MessageDeletedEvent) error {
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
	RegisterHandler(us, jwtMgr, newTestRDB(t), zap.NewNop())(rr, httptest.NewRequest(http.MethodPost, "/register", body))
	if rr.Code != http.StatusOK {
		t.Fatalf("registerUser setup failed: %d %s", rr.Code, rr.Body.String())
	}
}

func (f *fakeUserStore) AdminListInvites() ([]store.InviteCodeFull, error) {
	return f.adminInvites, f.adminListErr
}

func (f *fakeUserStore) AdminCreateInvite(code, createdBy string, expiresAt *time.Time) (*store.InviteCodeFull, error) {
	if f.adminCreateErr != nil {
		return nil, f.adminCreateErr
	}
	if f.adminCreateResult != nil {
		return f.adminCreateResult, nil
	}
	return &store.InviteCodeFull{ID: 1, Code: code, CreatedBy: createdBy, CreatedAt: time.Now()}, nil
}

func (f *fakeUserStore) AdminResetInvite(code string) error {
	return f.adminResetErr
}

func (f *fakeUserStore) UpdatePassword(userID int64, newHash string) error {
	return f.updatePasswordErr
}

// ─────────────────────────────────────────────────────────────
// RegisterHandler
// ─────────────────────────────────────────────────────────────

func TestRegisterHandler_Success(t *testing.T) {
	us := newFakeUserStore()
	us.addInvite("VALID-CODE")
	h := RegisterHandler(us, newTestJWT(), newTestRDB(t), zap.NewNop())

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
	h := RegisterHandler(newFakeUserStore(), newTestJWT(), newTestRDB(t), zap.NewNop())
	body := mustMarshal(t, map[string]string{"username": "alice", "password": "secret123"})
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/register", body))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestRegisterHandler_InvalidInviteCode(t *testing.T) {
	h := RegisterHandler(newFakeUserStore(), newTestJWT(), newTestRDB(t), zap.NewNop())
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
	h := RegisterHandler(us, newTestJWT(), newTestRDB(t), zap.NewNop())

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
	h := RegisterHandler(us, newTestJWT(), newTestRDB(t), zap.NewNop())
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
	h := RegisterHandler(us, newTestJWT(), newTestRDB(t), zap.NewNop())
	body := mustMarshal(t, map[string]string{"username": "alice", "invite_code": "CODE"})
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/register", body))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestRegisterHandler_InvalidJSON(t *testing.T) {
	h := RegisterHandler(newFakeUserStore(), newTestJWT(), newTestRDB(t), zap.NewNop())
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

	h := LoginHandler(us, jwtMgr, openLimiter(), newTestRDB(t), zap.NewNop())
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

	h := LoginHandler(us, jwtMgr, openLimiter(), newTestRDB(t), zap.NewNop())
	body := mustMarshal(t, map[string]string{"username": "alice", "password": "wrong"})
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/login", body))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestLoginHandler_UnknownUser(t *testing.T) {
	h := LoginHandler(newFakeUserStore(), newTestJWT(), openLimiter(), newTestRDB(t), zap.NewNop())
	body := mustMarshal(t, map[string]string{"username": "nobody", "password": "pass"})
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/login", body))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestLoginHandler_MissingFields(t *testing.T) {
	h := LoginHandler(newFakeUserStore(), newTestJWT(), openLimiter(), newTestRDB(t), zap.NewNop())
	body := mustMarshal(t, map[string]string{"username": "alice"}) // no password
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/login", body))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLoginHandler_InvalidJSON(t *testing.T) {
	h := LoginHandler(newFakeUserStore(), newTestJWT(), openLimiter(), newTestRDB(t), zap.NewNop())
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

	h := injectUser(http.HandlerFunc(ListUsersHandler(us, zap.NewNop())), &AuthUser{ID: 1, Username: "alice"})
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
	h := injectUser(http.HandlerFunc(ListUsersHandler(newFakeUserStore(), zap.NewNop())), &AuthUser{ID: 1, Username: "alice"})
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
	return injectUser(http.HandlerFunc(SendMessageHandler(us, ms, hub, pub, zap.NewNop())), asUser)
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
	h := http.HandlerFunc(SendMessageHandler(us, ms, hub, pub, zap.NewNop()))

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

	h := injectUser(http.HandlerFunc(ListMessagesHandler(us, ms, zap.NewNop())), &AuthUser{ID: 1, Username: "alice"})
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

	h := injectUser(http.HandlerFunc(ListMessagesHandler(us, ms, zap.NewNop())), &AuthUser{ID: 1, Username: "alice"})
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
		http.HandlerFunc(ListMessagesHandler(newFakeUserStore(), newFakeMessageStore(), zap.NewNop())),
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
		http.HandlerFunc(ListMessagesHandler(newFakeUserStore(), newFakeMessageStore(), zap.NewNop())),
		&AuthUser{ID: 1, Username: "alice"},
	)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/messages?peer=ghost", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestListMessagesHandler_Unauthenticated(t *testing.T) {
	h := http.HandlerFunc(ListMessagesHandler(newFakeUserStore(), newFakeMessageStore(), zap.NewNop()))
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
		// AAAAAAAAAAAAAAAA = base64(12 zero bytes) — correct AES-GCM nonce size
		{"encrypted with valid 12-byte iv", func(r *SendMessageRequest) { r.Encrypted = true; r.IV = "AAAAAAAAAAAAAAAA" }, false},
		// AAAAAAAAAAAA = base64(9 zero bytes) — too short
		{"encrypted with iv too short", func(r *SendMessageRequest) { r.Encrypted = true; r.IV = "AAAAAAAAAAAA" }, true},
		// AAAAAAAAAAAAAAAAAAAA = base64(15 zero bytes) — too long
		{"encrypted with iv too long", func(r *SendMessageRequest) { r.Encrypted = true; r.IV = "AAAAAAAAAAAAAAAAAAAA" }, true},
		{"encrypted with invalid base64 iv", func(r *SendMessageRequest) { r.Encrypted = true; r.IV = "not-valid-b64!!!" }, true},
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

// ─────────────────────────────────────────────────────────────
// RefreshHandler
// ─────────────────────────────────────────────────────────────

func storeTestRT(t *testing.T, rdb *redis.Client, userID int64) string {
	t.Helper()
	rt := "test-rt-" + t.Name()
	if err := rdb.Set(context.Background(), "rt:"+rt, userID, 30*24*time.Hour).Err(); err != nil {
		t.Fatalf("storeTestRT: %v", err)
	}
	return rt
}

func TestRefreshHandler_Success(t *testing.T) {
	us := newFakeUserStore()
	us.users["alice"] = &store.User{ID: 1, Username: "alice"}
	rdb := newTestRDB(t)
	rt := storeTestRT(t, rdb, 1)

	h := RefreshHandler(us, newTestJWT(), rdb, zap.NewNop())
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/refresh", mustMarshal(t, map[string]string{"refresh_token": rt})))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp refreshResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token == "" || resp.RefreshToken == "" {
		t.Fatal("expected non-empty token and refresh_token")
	}
	// Old token should be gone
	if rdb.Exists(context.Background(), "rt:"+rt).Val() != 0 {
		t.Fatal("old refresh token should be deleted after rotation")
	}
}

func TestRefreshHandler_MissingToken(t *testing.T) {
	h := RefreshHandler(newFakeUserStore(), newTestJWT(), newTestRDB(t), zap.NewNop())
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/refresh", mustMarshal(t, map[string]string{})))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestRefreshHandler_InvalidToken(t *testing.T) {
	h := RefreshHandler(newFakeUserStore(), newTestJWT(), newTestRDB(t), zap.NewNop())
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/refresh", mustMarshal(t, map[string]string{"refresh_token": "bogus"})))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestRefreshHandler_DeletedUser(t *testing.T) {
	us := newFakeUserStore() // user NOT in store
	rdb := newTestRDB(t)
	rt := storeTestRT(t, rdb, 99) // valid token but user 99 doesn't exist

	h := RefreshHandler(us, newTestJWT(), rdb, zap.NewNop())
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/refresh", mustMarshal(t, map[string]string{"refresh_token": rt})))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestRefreshHandler_InvalidJSON(t *testing.T) {
	h := RefreshHandler(newFakeUserStore(), newTestJWT(), newTestRDB(t), zap.NewNop())
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/refresh", bytes.NewBufferString("{bad")))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ─────────────────────────────────────────────────────────────
// LogoutHandler
// ─────────────────────────────────────────────────────────────

func TestLogoutHandler_RevokesToken(t *testing.T) {
	rdb := newTestRDB(t)
	rt := storeTestRT(t, rdb, 1)

	h := LogoutHandler(rdb)
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/logout", mustMarshal(t, map[string]string{"refresh_token": rt})))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if rdb.Exists(context.Background(), "rt:"+rt).Val() != 0 {
		t.Fatal("refresh token should be deleted after logout")
	}
}

func TestLogoutHandler_NoToken(t *testing.T) {
	h := LogoutHandler(newTestRDB(t))
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/logout", mustMarshal(t, map[string]string{})))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestLogoutHandler_InvalidJSON(t *testing.T) {
	h := LogoutHandler(newTestRDB(t))
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/logout", bytes.NewBufferString("{bad")))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ─────────────────────────────────────────────────────────────
// ListUsersHandler — search
// ─────────────────────────────────────────────────────────────

func TestListUsersHandler_Search(t *testing.T) {
	us := newFakeUserStore()
	us.users["alice"] = &store.User{ID: 1, Username: "alice"}
	us.users["bob"] = &store.User{ID: 2, Username: "bob"}

	h := injectUser(http.HandlerFunc(ListUsersHandler(us, zap.NewNop())), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/users?search=ali", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var users []UserResponse
	json.NewDecoder(rr.Body).Decode(&users)
	if len(users) != 1 || users[0].Username != "alice" {
		t.Fatalf("expected only alice, got %v", users)
	}
}

// ─────────────────────────────────────────────────────────────
// DeleteAccountHandler
// ─────────────────────────────────────────────────────────────

func TestDeleteAccountHandler_Success(t *testing.T) {
	us := newFakeUserStore()
	jwtMgr := newTestJWT()
	registerUser(t, us, jwtMgr, "alice", "mypassword", "CODE")
	alice := us.users["alice"]

	h := injectUser(
		http.HandlerFunc(DeleteAccountHandler(us, NewHub(), zap.NewNop())),
		&AuthUser{ID: alice.ID, Username: "alice"},
	)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/api/v1/account",
		mustMarshal(t, map[string]string{"password": "mypassword"})))

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDeleteAccountHandler_Unauthenticated(t *testing.T) {
	h := http.HandlerFunc(DeleteAccountHandler(newFakeUserStore(), NewHub(), zap.NewNop()))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/api/v1/account",
		mustMarshal(t, map[string]string{"password": "pass"})))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestDeleteAccountHandler_MissingPassword(t *testing.T) {
	h := injectUser(
		http.HandlerFunc(DeleteAccountHandler(newFakeUserStore(), NewHub(), zap.NewNop())),
		&AuthUser{ID: 1, Username: "alice"},
	)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/api/v1/account",
		mustMarshal(t, map[string]string{})))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDeleteAccountHandler_WrongPassword(t *testing.T) {
	us := newFakeUserStore()
	jwtMgr := newTestJWT()
	registerUser(t, us, jwtMgr, "alice", "realpassword", "CODE")
	alice := us.users["alice"]

	h := injectUser(
		http.HandlerFunc(DeleteAccountHandler(us, NewHub(), zap.NewNop())),
		&AuthUser{ID: alice.ID, Username: "alice"},
	)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/api/v1/account",
		mustMarshal(t, map[string]string{"password": "wrongpassword"})))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestDeleteAccountHandler_AlreadyDeleted(t *testing.T) {
	us := newFakeUserStore()
	jwtMgr := newTestJWT()
	registerUser(t, us, jwtMgr, "alice", "mypassword", "CODE")
	alice := us.users["alice"]
	us.deleteGDPRErr = store.ErrAlreadyDeleted

	h := injectUser(
		http.HandlerFunc(DeleteAccountHandler(us, NewHub(), zap.NewNop())),
		&AuthUser{ID: alice.ID, Username: "alice"},
	)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/api/v1/account",
		mustMarshal(t, map[string]string{"password": "mypassword"})))
	if rr.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d", rr.Code)
	}
}

// ─────────────────────────────────────────────────────────────
// ListContactsHandler
// ─────────────────────────────────────────────────────────────

func TestListContactsHandler_ReturnsContacts(t *testing.T) {
	us := newFakeUserStore()
	us.contacts[1] = []*store.Contact{
		{ID: 2, Username: "bob"},
		{ID: 3, Username: "carol"},
	}

	h := injectUser(http.HandlerFunc(ListContactsHandler(us, zap.NewNop())), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/contacts", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp []struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp) != 2 {
		t.Fatalf("expected 2 contacts, got %d", len(resp))
	}
}

func TestListContactsHandler_Empty(t *testing.T) {
	h := injectUser(http.HandlerFunc(ListContactsHandler(newFakeUserStore(), zap.NewNop())), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/contacts", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestListContactsHandler_Unauthenticated(t *testing.T) {
	h := http.HandlerFunc(ListContactsHandler(newFakeUserStore(), zap.NewNop()))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/contacts", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

// ─────────────────────────────────────────────────────────────
// AddContactHandler
// ─────────────────────────────────────────────────────────────

func TestAddContactHandler_Success(t *testing.T) {
	h := injectUser(http.HandlerFunc(AddContactHandler(newFakeUserStore(), zap.NewNop())), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/contacts",
		mustMarshal(t, map[string]string{"username": "bob"})))
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAddContactHandler_Unauthenticated(t *testing.T) {
	h := http.HandlerFunc(AddContactHandler(newFakeUserStore(), zap.NewNop()))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/contacts",
		mustMarshal(t, map[string]string{"username": "bob"})))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAddContactHandler_MissingUsername(t *testing.T) {
	h := injectUser(http.HandlerFunc(AddContactHandler(newFakeUserStore(), zap.NewNop())), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/contacts",
		mustMarshal(t, map[string]string{})))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestAddContactHandler_AddSelf(t *testing.T) {
	h := injectUser(http.HandlerFunc(AddContactHandler(newFakeUserStore(), zap.NewNop())), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/contacts",
		mustMarshal(t, map[string]string{"username": "alice"})))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestAddContactHandler_UserNotFound(t *testing.T) {
	us := newFakeUserStore()
	us.addContactErr = store.ErrUserNotFound

	h := injectUser(http.HandlerFunc(AddContactHandler(us, zap.NewNop())), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/contacts",
		mustMarshal(t, map[string]string{"username": "ghost"})))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestAddContactHandler_AlreadyExists(t *testing.T) {
	us := newFakeUserStore()
	us.addContactErr = store.ErrContactExists

	h := injectUser(http.HandlerFunc(AddContactHandler(us, zap.NewNop())), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/contacts",
		mustMarshal(t, map[string]string{"username": "bob"})))
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rr.Code)
	}
}

func TestAddContactHandler_InvalidJSON(t *testing.T) {
	h := injectUser(http.HandlerFunc(AddContactHandler(newFakeUserStore(), zap.NewNop())), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/contacts", bytes.NewBufferString("{bad")))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ─────────────────────────────────────────────────────────────
// RemoveContactHandler
// ─────────────────────────────────────────────────────────────

func removeContactRouter(us *fakeUserStore, cu *AuthUser) *mux.Router {
	r := mux.NewRouter()
	h := injectUser(http.HandlerFunc(RemoveContactHandler(us, zap.NewNop())), cu)
	r.Handle("/contacts/{username}", h).Methods(http.MethodDelete)
	return r
}

func TestRemoveContactHandler_Success(t *testing.T) {
	router := removeContactRouter(newFakeUserStore(), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/contacts/bob", nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestRemoveContactHandler_Unauthenticated(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/contacts/{username}", RemoveContactHandler(newFakeUserStore(), zap.NewNop()))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/contacts/bob", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestRemoveContactHandler_NotFound(t *testing.T) {
	us := newFakeUserStore()
	us.removeContactErr = store.ErrContactNotFound

	router := removeContactRouter(us, &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/contacts/ghost", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// ─────────────────────────────────────────────────────────────
// UploadKeyHandler
// ─────────────────────────────────────────────────────────────

func TestUploadKeyHandler_Success(t *testing.T) {
	h := injectUser(http.HandlerFunc(UploadKeyHandler(newFakeUserStore(), NewHub(), zap.NewNop())), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/api/v1/keys",
		mustMarshal(t, map[string]string{"publicKey": validTestKey})))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUploadKeyHandler_Unauthenticated(t *testing.T) {
	h := http.HandlerFunc(UploadKeyHandler(newFakeUserStore(), NewHub(), zap.NewNop()))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/api/v1/keys",
		mustMarshal(t, map[string]string{"publicKey": validTestKey})))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestUploadKeyHandler_MissingKey(t *testing.T) {
	h := injectUser(http.HandlerFunc(UploadKeyHandler(newFakeUserStore(), NewHub(), zap.NewNop())), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/api/v1/keys",
		mustMarshal(t, map[string]string{})))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestUploadKeyHandler_KeyTooShort(t *testing.T) {
	h := injectUser(http.HandlerFunc(UploadKeyHandler(newFakeUserStore(), NewHub(), zap.NewNop())), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/api/v1/keys",
		mustMarshal(t, map[string]string{"publicKey": strings.Repeat("A", 79)})))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestUploadKeyHandler_KeyTooLong(t *testing.T) {
	h := injectUser(http.HandlerFunc(UploadKeyHandler(newFakeUserStore(), NewHub(), zap.NewNop())), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/api/v1/keys",
		mustMarshal(t, map[string]string{"publicKey": strings.Repeat("A", 257)})))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestUploadKeyHandler_InvalidBase64(t *testing.T) {
	h := injectUser(http.HandlerFunc(UploadKeyHandler(newFakeUserStore(), NewHub(), zap.NewNop())), &AuthUser{ID: 1, Username: "alice"})
	// 96 chars of invalid base64 — length is valid but content is not
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/api/v1/keys",
		mustMarshal(t, map[string]string{"publicKey": strings.Repeat("!", 96)})))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestUploadKeyHandler_InvalidJSON(t *testing.T) {
	h := injectUser(http.HandlerFunc(UploadKeyHandler(newFakeUserStore(), NewHub(), zap.NewNop())), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/api/v1/keys", bytes.NewBufferString("{bad")))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ─────────────────────────────────────────────────────────────
// GetKeyHandler
// ─────────────────────────────────────────────────────────────

func getKeyRouter(us *fakeUserStore, cu *AuthUser) *mux.Router {
	r := mux.NewRouter()
	h := injectUser(http.HandlerFunc(GetKeyHandler(us)), cu)
	r.Handle("/keys/{username}", h).Methods(http.MethodGet)
	return r
}

func TestGetKeyHandler_Success(t *testing.T) {
	us := newFakeUserStore()
	us.users["alice"] = &store.User{ID: 1, Username: "alice"}
	us.users["bob"] = &store.User{ID: 2, Username: "bob", PublicKey: validTestKey}
	us.isContactMap[[2]int64{1, 2}] = true

	router := getKeyRouter(us, &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/keys/bob", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp getKeyResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.PublicKey != validTestKey {
		t.Fatalf("expected validTestKey, got %q", resp.PublicKey)
	}
}

func TestGetKeyHandler_Unauthenticated(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/keys/{username}", GetKeyHandler(newFakeUserStore()))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/keys/bob", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestGetKeyHandler_UserNotFound(t *testing.T) {
	us := newFakeUserStore()
	us.users["alice"] = &store.User{ID: 1, Username: "alice"}

	router := getKeyRouter(us, &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/keys/ghost", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestGetKeyHandler_NotContact(t *testing.T) {
	us := newFakeUserStore()
	us.users["alice"] = &store.User{ID: 1, Username: "alice"}
	us.users["bob"] = &store.User{ID: 2, Username: "bob", PublicKey: validTestKey}
	// isContactMap not set — IsContact returns false

	router := getKeyRouter(us, &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/keys/bob", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (key hidden from non-contacts), got %d", rr.Code)
	}
}

func TestGetKeyHandler_NoKeyUploaded(t *testing.T) {
	us := newFakeUserStore()
	us.users["alice"] = &store.User{ID: 1, Username: "alice"}
	us.users["bob"] = &store.User{ID: 2, Username: "bob"} // PublicKey empty
	us.isContactMap[[2]int64{1, 2}] = true

	router := getKeyRouter(us, &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/keys/bob", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// ─────────────────────────────────────────────────────────────
// ChangePasswordHandler
// ─────────────────────────────────────────────────────────────

func TestChangePasswordHandler_Success(t *testing.T) {
	us := newFakeUserStore()
	jwtMgr := newTestJWT()
	registerUser(t, us, jwtMgr, "alice", "oldpass123", "CODE")
	alice := us.users["alice"]

	h := injectUser(http.HandlerFunc(ChangePasswordHandler(us)), &AuthUser{ID: alice.ID, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPatch, "/api/v1/users/me/password",
		mustMarshal(t, map[string]string{"current_password": "oldpass123", "new_password": "newpass456"})))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestChangePasswordHandler_Unauthenticated(t *testing.T) {
	h := http.HandlerFunc(ChangePasswordHandler(newFakeUserStore()))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPatch, "/api/v1/users/me/password",
		mustMarshal(t, map[string]string{"current_password": "old", "new_password": "newpass456"})))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestChangePasswordHandler_MissingFields(t *testing.T) {
	h := injectUser(http.HandlerFunc(ChangePasswordHandler(newFakeUserStore())), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPatch, "/api/v1/users/me/password",
		mustMarshal(t, map[string]string{"current_password": "old"})))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestChangePasswordHandler_NewPasswordTooShort(t *testing.T) {
	us := newFakeUserStore()
	jwtMgr := newTestJWT()
	registerUser(t, us, jwtMgr, "alice", "oldpass123", "CODE")
	alice := us.users["alice"]

	h := injectUser(http.HandlerFunc(ChangePasswordHandler(us)), &AuthUser{ID: alice.ID, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPatch, "/api/v1/users/me/password",
		mustMarshal(t, map[string]string{"current_password": "oldpass123", "new_password": "short"})))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestChangePasswordHandler_WrongCurrentPassword(t *testing.T) {
	us := newFakeUserStore()
	jwtMgr := newTestJWT()
	registerUser(t, us, jwtMgr, "alice", "realpass123", "CODE")
	alice := us.users["alice"]

	h := injectUser(http.HandlerFunc(ChangePasswordHandler(us)), &AuthUser{ID: alice.ID, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPatch, "/api/v1/users/me/password",
		mustMarshal(t, map[string]string{"current_password": "wrongpass", "new_password": "newpass456"})))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestChangePasswordHandler_InvalidJSON(t *testing.T) {
	h := injectUser(http.HandlerFunc(ChangePasswordHandler(newFakeUserStore())), &AuthUser{ID: 1, Username: "alice"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPatch, "/api/v1/users/me/password", bytes.NewBufferString("{bad")))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ─────────────────────────────────────────────────────────────
// AdminListInvitesHandler
// ─────────────────────────────────────────────────────────────

func TestAdminListInvitesHandler_ReturnsList(t *testing.T) {
	us := newFakeUserStore()
	us.adminInvites = []store.InviteCodeFull{
		{ID: 1, Code: "AAA-BBB", CreatedBy: "admin", CreatedAt: time.Now()},
		{ID: 2, Code: "CCC-DDD", CreatedBy: "admin", CreatedAt: time.Now()},
	}

	h := http.HandlerFunc(AdminListInvitesHandler(us, zap.NewNop()))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/admin/invites", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp []inviteResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp) != 2 {
		t.Fatalf("expected 2 invites, got %d", len(resp))
	}
}

func TestAdminListInvitesHandler_Empty(t *testing.T) {
	h := http.HandlerFunc(AdminListInvitesHandler(newFakeUserStore(), zap.NewNop()))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/admin/invites", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ─────────────────────────────────────────────────────────────
// AdminCreateInviteHandler
// ─────────────────────────────────────────────────────────────

func TestAdminCreateInviteHandler_CustomCode(t *testing.T) {
	h := injectUser(
		http.HandlerFunc(AdminCreateInviteHandler(newFakeUserStore(), zap.NewNop())),
		&AuthUser{ID: 1, Username: "admin"},
	)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/admin/invites",
		mustMarshal(t, map[string]string{"code": "MY-CUSTOM-CODE"})))

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp inviteResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Code != "MY-CUSTOM-CODE" {
		t.Fatalf("expected MY-CUSTOM-CODE, got %q", resp.Code)
	}
}

func TestAdminCreateInviteHandler_AutoGeneratedCode(t *testing.T) {
	h := injectUser(
		http.HandlerFunc(AdminCreateInviteHandler(newFakeUserStore(), zap.NewNop())),
		&AuthUser{ID: 1, Username: "admin"},
	)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/admin/invites",
		mustMarshal(t, map[string]any{})))

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp inviteResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Code == "" {
		t.Fatal("expected non-empty auto-generated code")
	}
}

func TestAdminCreateInviteHandler_DuplicateCode(t *testing.T) {
	us := newFakeUserStore()
	us.adminCreateErr = errors.New("invite code already exists")

	h := injectUser(
		http.HandlerFunc(AdminCreateInviteHandler(us, zap.NewNop())),
		&AuthUser{ID: 1, Username: "admin"},
	)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/admin/invites",
		mustMarshal(t, map[string]string{"code": "DUP"})))
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rr.Code)
	}
}

func TestAdminCreateInviteHandler_InvalidJSON(t *testing.T) {
	h := injectUser(
		http.HandlerFunc(AdminCreateInviteHandler(newFakeUserStore(), zap.NewNop())),
		&AuthUser{ID: 1, Username: "admin"},
	)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/admin/invites", bytes.NewBufferString("{bad")))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ─────────────────────────────────────────────────────────────
// AdminResetInviteHandler
// ─────────────────────────────────────────────────────────────

func adminResetRouter(us *fakeUserStore) *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/admin/invites/{code}/reset", AdminResetInviteHandler(us, zap.NewNop()))
	return r
}

func TestAdminResetInviteHandler_Success(t *testing.T) {
	router := adminResetRouter(newFakeUserStore())
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/admin/invites/ABC-123/reset", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminResetInviteHandler_NotFound(t *testing.T) {
	us := newFakeUserStore()
	us.adminResetErr = errors.New("invite code not found")

	router := adminResetRouter(us)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/admin/invites/GHOST/reset", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// ─────────────────────────────────────────────────────────────
// AdminOnly middleware
// ─────────────────────────────────────────────────────────────

func TestAdminOnly_AllowsAdmin(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	h := injectUser(AdminOnly("admin")(inner), &AuthUser{ID: 1, Username: "admin"})

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if !called {
		t.Fatal("expected inner handler to be called for admin user")
	}
}

func TestAdminOnly_BlocksNonAdmin(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	h := injectUser(AdminOnly("admin")(inner), &AuthUser{ID: 2, Username: "alice"})

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminOnly_BlocksUnauthenticated(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	h := AdminOnly("admin")(inner) // no injectUser

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

// ─────────────────────────────────────────────────────────────
// AuthMiddleware
// ─────────────────────────────────────────────────────────────

func authMiddlewareHandler(us *fakeUserStore) http.Handler {
	jwtMgr := newTestJWT()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return AuthMiddleware(jwtMgr, us, zap.NewNop())(inner)
}

func validBearerToken(t *testing.T, u *store.User) string {
	t.Helper()
	tok, err := newTestJWT().Generate(u)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return "Bearer " + tok
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	us := newFakeUserStore()
	us.users["alice"] = &store.User{ID: 1, Username: "alice"}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", validBearerToken(t, us.users["alice"]))
	authMiddlewareHandler(us).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	rr := httptest.NewRecorder()
	authMiddlewareHandler(newFakeUserStore()).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAuthMiddleware_MalformedHeader(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "NotBearer sometoken")
	authMiddlewareHandler(newFakeUserStore()).ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer this.is.not.valid")
	authMiddlewareHandler(newFakeUserStore()).ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAuthMiddleware_DeletedUser(t *testing.T) {
	// Token is valid but the user no longer exists in the store
	us := newFakeUserStore()
	ghost := &store.User{ID: 99, Username: "ghost"}
	token := validBearerToken(t, ghost)
	// ghost is NOT in us.users

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", token)
	authMiddlewareHandler(us).ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAuthMiddleware_OptionsPassthrough(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	h := AuthMiddleware(newTestJWT(), newFakeUserStore(), zap.NewNop())(inner)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodOptions, "/", nil))
	if !called {
		t.Fatal("expected OPTIONS to pass through AuthMiddleware without auth check")
	}
}

// ─────────────────────────────────────────────────────────────
// Health handlers
// ─────────────────────────────────────────────────────────────

func TestHealthHandler(t *testing.T) {
	rr := httptest.NewRecorder()
	HealthHandler(rr, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Fatalf("expected status=ok, got %q", resp["status"])
	}
}

func TestLiveHandler(t *testing.T) {
	rr := httptest.NewRecorder()
	LiveHandler(rr, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["status"] != "live" {
		t.Fatalf("expected status=live, got %q", resp["status"])
	}
}

func TestReadyHandler_Ready(t *testing.T) {
	h := ReadyHandler(newFakeUserStore()) // pingErr is nil → DB ok
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestReadyHandler_NotReady(t *testing.T) {
	us := newFakeUserStore()
	us.pingErr = errors.New("connection refused")

	h := ReadyHandler(us)
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}
