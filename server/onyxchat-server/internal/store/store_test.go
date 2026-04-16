package store_test

import (
	"database/sql"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cole/onyxchat-server/internal/store"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// ─────────────────────────────────────────────────────────────
// Test infrastructure
// ─────────────────────────────────────────────────────────────

var testDB *sql.DB

func TestMain(m *testing.M) {
	dsn := os.Getenv("SM_DB_DSN")
	if dsn == "" {
		dsn = "postgres://sm:sm@localhost:5432/sm?sslmode=disable"
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil || db.Ping() != nil {
		fmt.Println("store tests: no DB available, skipping")
		os.Exit(0)
	}

	if err := store.RunMigrations(db); err != nil {
		fmt.Fprintf(os.Stderr, "store tests: migration failed: %v\n", err)
		os.Exit(1)
	}

	testDB = db
	code := m.Run()
	db.Close()
	os.Exit(code)
}

// counter generates unique names so parallel tests don't collide.
var counter atomic.Int64

func uid() string {
	return fmt.Sprintf("t%d", counter.Add(1))
}

// seed inserts an invite code and returns it.
func seedInvite(t *testing.T, code string) {
	t.Helper()
	_, err := testDB.Exec(`INSERT INTO invite_codes (code, created_by) VALUES ($1, 'test')`, code)
	if err != nil {
		t.Fatalf("seedInvite: %v", err)
	}
	t.Cleanup(func() {
		testDB.Exec(`DELETE FROM invite_codes WHERE code = $1`, code)
	})
}

// seedUser creates a user directly (bypassing invite flow) and returns it.
func seedUser(t *testing.T, username string) *store.User {
	t.Helper()
	us := store.NewUserStore(testDB)
	u, err := us.CreateUser(username, "hash")
	if err != nil {
		t.Fatalf("seedUser: %v", err)
	}
	t.Cleanup(func() {
		testDB.Exec(`DELETE FROM contacts WHERE user_id = $1 OR contact_id = $1`, u.ID)
		testDB.Exec(`DELETE FROM messages WHERE sender_id = $1 OR recipient_id = $1`, u.ID)
		testDB.Exec(`DELETE FROM users WHERE id = $1`, u.ID)
	})
	return u
}

// ─────────────────────────────────────────────────────────────
// UserStore — registration
// ─────────────────────────────────────────────────────────────

func TestRegisterWithInvite_Success(t *testing.T) {
	code := "REG-OK-" + uid()
	seedInvite(t, code)

	username := "u" + uid()
	us := store.NewUserStore(testDB)
	user, err := us.RegisterWithInvite(code, username, "hash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if user.Username != username {
		t.Fatalf("got username %q, want %q", user.Username, username)
	}

	t.Cleanup(func() { testDB.Exec(`DELETE FROM users WHERE id = $1`, user.ID) })

	// Invite should be consumed — second attempt must fail.
	_, err = us.RegisterWithInvite(code, "u"+uid(), "hash")
	if err != store.ErrInvalidInviteCode {
		t.Fatalf("expected store.ErrInvalidInviteCode on reuse, got %v", err)
	}
}

func TestRegisterWithInvite_InvalidCode(t *testing.T) {
	us := store.NewUserStore(testDB)
	_, err := us.RegisterWithInvite("DOES-NOT-EXIST", "u"+uid(), "hash")
	if err != store.ErrInvalidInviteCode {
		t.Fatalf("expected store.ErrInvalidInviteCode, got %v", err)
	}
}

func TestRegisterWithInvite_DuplicateUsername(t *testing.T) {
	// Create a user to occupy the username.
	existing := seedUser(t, "u"+uid())

	code := "DUP-" + uid()
	seedInvite(t, code)

	us := store.NewUserStore(testDB)
	_, err := us.RegisterWithInvite(code, existing.Username, "hash")
	if err != store.ErrUsernameTaken {
		t.Fatalf("expected store.ErrUsernameTaken, got %v", err)
	}

	// Invite must NOT have been burned — transaction should have rolled back.
	var usedBy sql.NullString
	testDB.QueryRow(`SELECT used_by FROM invite_codes WHERE code = $1`, code).Scan(&usedBy)
	if usedBy.Valid {
		t.Fatal("invite code was burned despite failed registration")
	}
}

// ─────────────────────────────────────────────────────────────
// UserStore — lookups
// ─────────────────────────────────────────────────────────────

func TestGetUserByUsername_NotFound(t *testing.T) {
	us := store.NewUserStore(testDB)
	_, err := us.GetUserByUsername("definitely-does-not-exist")
	if err != store.ErrUserNotFound {
		t.Fatalf("expected store.ErrUserNotFound, got %v", err)
	}
}

func TestGetUserByUsername_ExcludesDeleted(t *testing.T) {
	u := seedUser(t, "u"+uid())
	us := store.NewUserStore(testDB)

	// Soft-delete the user.
	testDB.Exec(`UPDATE users SET deleted_at = NOW() WHERE id = $1`, u.ID)

	_, err := us.GetUserByUsername(u.Username)
	if err != store.ErrUserNotFound {
		t.Fatalf("expected deleted user to be invisible, got %v", err)
	}
}

func TestGetUserByID_ExcludesDeleted(t *testing.T) {
	u := seedUser(t, "u"+uid())
	us := store.NewUserStore(testDB)

	testDB.Exec(`UPDATE users SET deleted_at = NOW() WHERE id = $1`, u.ID)

	_, err := us.GetUserByID(u.ID)
	if err != store.ErrUserNotFound {
		t.Fatalf("expected deleted user to be invisible, got %v", err)
	}
}

func TestSetAndGetPublicKey(t *testing.T) {
	u := seedUser(t, "u"+uid())
	us := store.NewUserStore(testDB)

	key := "base64pubkey==" + uid()
	if err := us.SetPublicKey(u.ID, key); err != nil {
		t.Fatalf("SetPublicKey: %v", err)
	}

	got, err := us.GetUserByUsername(u.Username)
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if got.PublicKey != key {
		t.Fatalf("got public key %q, want %q", got.PublicKey, key)
	}
}

func TestSearchUsers(t *testing.T) {
	prefix := "srch" + uid()
	u1 := seedUser(t, prefix+"_alice")
	u2 := seedUser(t, prefix+"_bob")
	_ = u1
	_ = u2

	us := store.NewUserStore(testDB)
	results, err := us.SearchUsers(prefix)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results for prefix %q, got %d", prefix, len(results))
	}
}

func TestUpdatePassword(t *testing.T) {
	u := seedUser(t, "u"+uid())
	us := store.NewUserStore(testDB)

	newHash := "newhash-" + uid()
	if err := us.UpdatePassword(u.ID, newHash); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}

	got, err := us.GetUserByUsername(u.Username)
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if got.PasswordHash != newHash {
		t.Fatalf("password hash not updated: got %q", got.PasswordHash)
	}
}

// ─────────────────────────────────────────────────────────────
// Contacts
// ─────────────────────────────────────────────────────────────

func TestAddAndListContacts(t *testing.T) {
	alice := seedUser(t, "u"+uid())
	bob := seedUser(t, "u"+uid())
	us := store.NewUserStore(testDB)

	if err := us.AddContact(alice.ID, bob.Username); err != nil {
		t.Fatalf("AddContact: %v", err)
	}
	t.Cleanup(func() {
		testDB.Exec(`DELETE FROM contacts WHERE user_id = $1 AND contact_id = $2`, alice.ID, bob.ID)
	})

	contacts, err := us.ListContacts(alice.ID)
	if err != nil {
		t.Fatalf("ListContacts: %v", err)
	}
	found := false
	for _, c := range contacts {
		if c.ID == bob.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("bob not in alice's contacts after AddContact")
	}
}

func TestAddContact_UserNotFound(t *testing.T) {
	alice := seedUser(t, "u"+uid())
	us := store.NewUserStore(testDB)

	err := us.AddContact(alice.ID, "ghost-"+uid())
	if err != store.ErrUserNotFound {
		t.Fatalf("expected store.ErrUserNotFound, got %v", err)
	}
}

func TestRemoveContact(t *testing.T) {
	alice := seedUser(t, "u"+uid())
	bob := seedUser(t, "u"+uid())
	us := store.NewUserStore(testDB)

	_ = us.AddContact(alice.ID, bob.Username)

	if err := us.RemoveContact(alice.ID, bob.Username); err != nil {
		t.Fatalf("RemoveContact: %v", err)
	}

	ok, err := us.IsContact(alice.ID, bob.ID)
	if err != nil {
		t.Fatalf("IsContact: %v", err)
	}
	if ok {
		t.Fatal("bob still in alice's contacts after removal")
	}
}

func TestIsContact(t *testing.T) {
	alice := seedUser(t, "u"+uid())
	bob := seedUser(t, "u"+uid())
	us := store.NewUserStore(testDB)

	ok, _ := us.IsContact(alice.ID, bob.ID)
	if ok {
		t.Fatal("expected not a contact before adding")
	}

	_ = us.AddContact(alice.ID, bob.Username)
	t.Cleanup(func() {
		testDB.Exec(`DELETE FROM contacts WHERE user_id = $1 AND contact_id = $2`, alice.ID, bob.ID)
	})

	ok, err := us.IsContact(alice.ID, bob.ID)
	if err != nil || !ok {
		t.Fatalf("expected contact after adding, err=%v ok=%v", err, ok)
	}
}

func TestListContacts_ExcludesDeleted(t *testing.T) {
	alice := seedUser(t, "u"+uid())
	bob := seedUser(t, "u"+uid())
	us := store.NewUserStore(testDB)

	_ = us.AddContact(alice.ID, bob.Username)

	// Delete bob.
	testDB.Exec(`UPDATE users SET deleted_at = NOW() WHERE id = $1`, bob.ID)

	contacts, err := us.ListContacts(alice.ID)
	if err != nil {
		t.Fatalf("ListContacts: %v", err)
	}
	for _, c := range contacts {
		if c.ID == bob.ID {
			t.Fatal("deleted user appeared in contacts list")
		}
	}
}

func TestGetContactFollowerIDs(t *testing.T) {
	alice := seedUser(t, "u"+uid())
	bob := seedUser(t, "u"+uid())
	us := store.NewUserStore(testDB)

	// Bob follows Alice (has Alice in his contacts).
	_ = us.AddContact(bob.ID, alice.Username)
	t.Cleanup(func() {
		testDB.Exec(`DELETE FROM contacts WHERE user_id = $1 AND contact_id = $2`, bob.ID, alice.ID)
	})

	ids, err := us.GetContactFollowerIDs(alice.ID)
	if err != nil {
		t.Fatalf("GetContactFollowerIDs: %v", err)
	}
	found := false
	for _, id := range ids {
		if id == bob.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("bob not in alice's follower set")
	}
}

// ─────────────────────────────────────────────────────────────
// MessageStore
// ─────────────────────────────────────────────────────────────

func TestCreateOrGetExisting_Dedup(t *testing.T) {
	alice := seedUser(t, "u"+uid())
	bob := seedUser(t, "u"+uid())
	ms := store.NewMessageStore(testDB)

	clientID := "client-" + uid()

	m1, inserted1, err := ms.CreateOrGetExisting(alice.ID, bob.ID, "hello", "", false, clientID)
	if err != nil {
		t.Fatalf("first CreateOrGetExisting: %v", err)
	}
	if !inserted1 {
		t.Fatal("expected first call to insert")
	}

	m2, inserted2, err := ms.CreateOrGetExisting(alice.ID, bob.ID, "hello", "", false, clientID)
	if err != nil {
		t.Fatalf("second CreateOrGetExisting: %v", err)
	}
	if inserted2 {
		t.Fatal("expected second call to return existing, not insert")
	}
	if m1.ID != m2.ID {
		t.Fatalf("IDs differ: first=%d second=%d", m1.ID, m2.ID)
	}
}

func TestListConversationSince_Bidirectional(t *testing.T) {
	alice := seedUser(t, "u"+uid())
	bob := seedUser(t, "u"+uid())
	ms := store.NewMessageStore(testDB)

	// Alice → Bob
	ms.CreateOrGetExisting(alice.ID, bob.ID, "hi from alice", "", false, "c"+uid())
	// Bob → Alice
	ms.CreateOrGetExisting(bob.ID, alice.ID, "hi from bob", "", false, "c"+uid())

	msgs, _, err := ms.ListConversationSince(alice.ID, bob.ID, 0, 50)
	if err != nil {
		t.Fatalf("ListConversationSince: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}
}

func TestListConversationSince_Pagination(t *testing.T) {
	alice := seedUser(t, "u"+uid())
	bob := seedUser(t, "u"+uid())
	ms := store.NewMessageStore(testDB)

	// Insert 5 messages.
	for i := 0; i < 5; i++ {
		ms.CreateOrGetExisting(alice.ID, bob.ID, fmt.Sprintf("msg %d", i), "", false, "c"+uid())
	}

	msgs, hasMore, err := ms.ListConversationSince(alice.ID, bob.ID, 0, 3)
	if err != nil {
		t.Fatalf("ListConversationSince: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if !hasMore {
		t.Fatal("expected hasMore=true")
	}

	// Fetch next page using sinceID.
	msgs2, hasMore2, err := ms.ListConversationSince(alice.ID, bob.ID, msgs[2].ID, 3)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(msgs2) < 2 {
		t.Fatalf("expected at least 2 messages on second page, got %d", len(msgs2))
	}
	_ = hasMore2
}

func TestGetUnreadForUser(t *testing.T) {
	alice := seedUser(t, "u"+uid())
	bob := seedUser(t, "u"+uid())
	ms := store.NewMessageStore(testDB)

	m, _, _ := ms.CreateOrGetExisting(alice.ID, bob.ID, "unread", "", false, "c"+uid())

	msgs, err := ms.GetUnreadForUser(bob.ID, m.ID-1)
	if err != nil {
		t.Fatalf("GetUnreadForUser: %v", err)
	}
	found := false
	for _, msg := range msgs {
		if msg.ID == m.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("message not found in unread list")
	}
}

// ─────────────────────────────────────────────────────────────
// GDPR deletion
// ─────────────────────────────────────────────────────────────

func TestDeleteAccountGDPR(t *testing.T) {
	alice := seedUser(t, "u"+uid())
	bob := seedUser(t, "u"+uid())
	us := store.NewUserStore(testDB)
	ms := store.NewMessageStore(testDB)

	// Alice sends a message.
	m, _, _ := ms.CreateOrGetExisting(alice.ID, bob.ID, "secret", "", false, "c"+uid())

	// Alice has an invite code.
	code := "GDPR-" + uid()
	testDB.Exec(`INSERT INTO invite_codes (code, created_by) VALUES ($1, $2)`, code, alice.Username)

	record, err := us.DeleteAccountGDPR(alice.ID)
	if err != nil {
		t.Fatalf("DeleteAccountGDPR: %v", err)
	}
	if record.MessagesPurged == 0 {
		t.Fatal("expected at least 1 message purged")
	}

	// User should be invisible by username and ID.
	_, err = us.GetUserByUsername(alice.Username)
	if err != store.ErrUserNotFound {
		t.Fatalf("deleted user still findable by username: %v", err)
	}
	_, err = us.GetUserByID(alice.ID)
	if err != store.ErrUserNotFound {
		t.Fatalf("deleted user still findable by ID: %v", err)
	}

	// Message body should be cleared.
	var body string
	testDB.QueryRow(`SELECT body FROM messages WHERE id = $1`, m.ID).Scan(&body)
	if body != "" {
		t.Fatalf("message body not cleared after GDPR deletion, got %q", body)
	}

	// Invite code should be expired.
	var expiresAt *time.Time
	testDB.QueryRow(`SELECT expires_at FROM invite_codes WHERE code = $1`, code).Scan(&expiresAt)
	if expiresAt == nil || expiresAt.After(time.Now()) {
		t.Fatal("invite code not expired after GDPR deletion")
	}

	t.Cleanup(func() {
		testDB.Exec(`DELETE FROM invite_codes WHERE code = $1`, code)
		testDB.Exec(`DELETE FROM gdpr_deletion_log WHERE user_id = $1`, alice.ID)
	})
}

func TestDeleteAccountGDPR_AlreadyDeleted(t *testing.T) {
	u := seedUser(t, "u"+uid())
	us := store.NewUserStore(testDB)

	testDB.Exec(`UPDATE users SET deleted_at = NOW() WHERE id = $1`, u.ID)

	_, err := us.DeleteAccountGDPR(u.ID)
	if err != store.ErrAlreadyDeleted {
		t.Fatalf("expected store.ErrAlreadyDeleted, got %v", err)
	}
}

// ─────────────────────────────────────────────────────────────
// Admin invite management
// ─────────────────────────────────────────────────────────────

func TestAdminCreateAndListInvites(t *testing.T) {
	code := "ADMIN-" + uid()
	us := store.NewUserStore(testDB)

	invite, err := us.AdminCreateInvite(code, "admin", nil)
	if err != nil {
		t.Fatalf("AdminCreateInvite: %v", err)
	}
	if invite.Code != code {
		t.Fatalf("got code %q, want %q", invite.Code, code)
	}
	t.Cleanup(func() { testDB.Exec(`DELETE FROM invite_codes WHERE code = $1`, code) })

	// Duplicate code must fail.
	_, err = us.AdminCreateInvite(code, "admin", nil)
	if err == nil {
		t.Fatal("expected error on duplicate invite code")
	}

	invites, err := us.AdminListInvites()
	if err != nil {
		t.Fatalf("AdminListInvites: %v", err)
	}
	found := false
	for _, inv := range invites {
		if inv.Code == code {
			found = true
		}
	}
	if !found {
		t.Fatal("newly created invite not in list")
	}
}

func TestAdminResetInvite(t *testing.T) {
	code := "RESET-" + uid()
	seedInvite(t, code)

	// Mark it as used.
	testDB.Exec(`UPDATE invite_codes SET used_by = 'someone', used_at = NOW() WHERE code = $1`, code)

	us := store.NewUserStore(testDB)
	if err := us.AdminResetInvite(code); err != nil {
		t.Fatalf("AdminResetInvite: %v", err)
	}

	// Should be usable again.
	var usedBy sql.NullString
	testDB.QueryRow(`SELECT used_by FROM invite_codes WHERE code = $1`, code).Scan(&usedBy)
	if usedBy.Valid {
		t.Fatal("invite code still marked as used after reset")
	}
}
