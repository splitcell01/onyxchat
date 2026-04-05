package http

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cole/onyxchat-server/internal/store"
	"github.com/redis/go-redis/v9"
)

// okHandler records whether it was reached and always responds 200.
func okHandler(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
}

func storeTicket(t *testing.T, rdb *redis.Client, ticket string, userID int64, username string) {
	t.Helper()
	val := fmt.Sprintf("%d:%s", userID, username)
	if err := rdb.Set(context.Background(), ticketPrefix+ticket, val, 30*time.Second).Err(); err != nil {
		t.Fatalf("storeTicket: %v", err)
	}
}

// ─── ticket-based auth ────────────────────────────────────────────────────────

func TestWSAuthMiddleware_ValidTicket_ExistingUser(t *testing.T) {
	us := newFakeUserStore()
	us.users["alice"] = &store.User{ID: 1, Username: "alice"}
	rdb := newTestRDB(t)
	storeTicket(t, rdb, "ticket-1", 1, "alice")

	var reached bool
	h := WSAuthMiddleware(newTestJWT(), rdb, us)(okHandler(&reached))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/ws?ticket=ticket-1", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !reached {
		t.Fatal("handler should have been called")
	}
}

func TestWSAuthMiddleware_ValidTicket_DeletedUser(t *testing.T) {
	// Ticket is cryptographically valid but the user was deleted from the DB.
	us := newFakeUserStore() // empty — no users
	rdb := newTestRDB(t)
	storeTicket(t, rdb, "ticket-deleted", 99, "deleted_99")

	var reached bool
	h := WSAuthMiddleware(newTestJWT(), rdb, us)(okHandler(&reached))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/ws?ticket=ticket-deleted", nil))

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if reached {
		t.Fatal("handler must not be called for deleted user")
	}
}

func TestWSAuthMiddleware_ExpiredTicket(t *testing.T) {
	us := newFakeUserStore()
	us.users["alice"] = &store.User{ID: 1, Username: "alice"}
	rdb := newTestRDB(t)
	// No ticket stored — as if it expired and was evicted from Redis.

	var reached bool
	h := WSAuthMiddleware(newTestJWT(), rdb, us)(okHandler(&reached))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/ws?ticket=no-such-ticket", nil))

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if reached {
		t.Fatal("handler must not be called with unknown ticket")
	}
}

func TestWSAuthMiddleware_TicketSingleUse(t *testing.T) {
	// Ticket must be consumed (GETDEL) so it cannot be replayed.
	us := newFakeUserStore()
	us.users["alice"] = &store.User{ID: 1, Username: "alice"}
	rdb := newTestRDB(t)
	storeTicket(t, rdb, "ticket-once", 1, "alice")

	jwtMgr := newTestJWT()

	// First use — should pass.
	rr1 := httptest.NewRecorder()
	WSAuthMiddleware(jwtMgr, rdb, us)(okHandler(new(bool))).ServeHTTP(
		rr1, httptest.NewRequest(http.MethodGet, "/ws?ticket=ticket-once", nil),
	)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first use: expected 200, got %d", rr1.Code)
	}

	// Second use of the same ticket — must be rejected.
	rr2 := httptest.NewRecorder()
	WSAuthMiddleware(jwtMgr, rdb, us)(okHandler(new(bool))).ServeHTTP(
		rr2, httptest.NewRequest(http.MethodGet, "/ws?ticket=ticket-once", nil),
	)
	if rr2.Code != http.StatusUnauthorized {
		t.Fatalf("replay: expected 401, got %d", rr2.Code)
	}
}

// ─── JWT Bearer fallback ──────────────────────────────────────────────────────

func TestWSAuthMiddleware_ValidJWT_ExistingUser(t *testing.T) {
	us := newFakeUserStore()
	alice := &store.User{ID: 1, Username: "alice"}
	us.users["alice"] = alice
	rdb := newTestRDB(t)
	jwtMgr := newTestJWT()

	token, err := jwtMgr.Generate(alice)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	var reached bool
	h := WSAuthMiddleware(jwtMgr, rdb, us)(okHandler(&reached))

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !reached {
		t.Fatal("handler should have been called")
	}
}

func TestWSAuthMiddleware_ValidJWT_DeletedUser(t *testing.T) {
	// JWT is cryptographically valid but the user no longer exists in the DB.
	us := newFakeUserStore() // empty
	rdb := newTestRDB(t)
	jwtMgr := newTestJWT()

	token, _ := jwtMgr.Generate(&store.User{ID: 99, Username: "ghost"})

	var reached bool
	h := WSAuthMiddleware(jwtMgr, rdb, us)(okHandler(&reached))

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if reached {
		t.Fatal("handler must not be called for deleted user")
	}
}

func TestWSAuthMiddleware_InvalidJWT(t *testing.T) {
	us := newFakeUserStore()
	rdb := newTestRDB(t)

	var reached bool
	h := WSAuthMiddleware(newTestJWT(), rdb, us)(okHandler(&reached))

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-token")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if reached {
		t.Fatal("handler must not be called with invalid JWT")
	}
}

// ─── no credentials ───────────────────────────────────────────────────────────

func TestWSAuthMiddleware_NoCredentials(t *testing.T) {
	us := newFakeUserStore()
	rdb := newTestRDB(t)

	var reached bool
	h := WSAuthMiddleware(newTestJWT(), rdb, us)(okHandler(&reached))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/ws", nil))

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if reached {
		t.Fatal("handler must not be called without credentials")
	}
}
