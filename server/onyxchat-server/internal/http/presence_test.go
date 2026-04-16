package http

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// newTestPresenceStore creates a PresenceStore backed by an in-process miniredis
// instance that is automatically closed when the test ends.
func newTestPresenceStore(t *testing.T) (*PresenceStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return NewPresenceStore(rdb, zap.NewNop()), mr
}

// ─── Presence TTL behaviour ──────────────────────────────────────────────────

func TestPresenceConnect_SetsTTL(t *testing.T) {
	p, mr := newTestPresenceStore(t)

	if _, err := p.Connect(context.Background(), 1, "alice"); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	ttl := mr.TTL("presence:conns:1")
	if ttl <= 0 {
		t.Fatalf("expected TTL > 0 after Connect, got %v", ttl)
	}
	if ttl != presenceConnsTTL {
		t.Fatalf("expected TTL=%v, got %v", presenceConnsTTL, ttl)
	}
}

func TestPresenceKey_ExpiresWithoutRefresh(t *testing.T) {
	p, mr := newTestPresenceStore(t)

	if _, err := p.Connect(context.Background(), 1, "alice"); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	mr.FastForward(presenceConnsTTL + time.Second)

	if mr.Exists("presence:conns:1") {
		t.Fatal("expected presence:conns:1 to have expired, but it still exists")
	}
}

func TestPresenceRefresh_ResetsTTL(t *testing.T) {
	p, mr := newTestPresenceStore(t)
	ctx := context.Background()

	if _, err := p.Connect(ctx, 1, "alice"); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Consume 90 s of the 2-minute window — TTL is now ~30 s remaining.
	mr.FastForward(90 * time.Second)

	if err := p.Refresh(ctx, 1); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// After Refresh the TTL should be back near presenceConnsTTL, not ~30 s.
	ttl := mr.TTL("presence:conns:1")
	if ttl < 90*time.Second {
		t.Fatalf("expected TTL to be reset near presenceConnsTTL after Refresh, got %v", ttl)
	}
}

func TestPresenceRefresh_PreventsExpiry(t *testing.T) {
	p, mr := newTestPresenceStore(t)
	ctx := context.Background()

	if _, err := p.Connect(ctx, 1, "alice"); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Advance to just before expiry, refresh, then advance the same distance again.
	// Without Refresh the key would have expired on the second advance.
	almostExpired := presenceConnsTTL - time.Second
	mr.FastForward(almostExpired)

	if err := p.Refresh(ctx, 1); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	mr.FastForward(almostExpired)

	if !mr.Exists("presence:conns:1") {
		t.Fatal("expected presence:conns:1 to still exist after Refresh extended the TTL")
	}
}

// ─── GetSnapshot contact filtering ───────────────────────────────────────────

func TestGetSnapshot_FiltersToContactIDs(t *testing.T) {
	p, _ := newTestPresenceStore(t)
	ctx := context.Background()

	p.Connect(ctx, 1, "alice")
	p.Connect(ctx, 2, "bob")

	// The requesting user only has alice (1) as a contact.
	snapshot, err := p.GetSnapshot(ctx, []int64{1})
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if len(snapshot) != 1 {
		t.Fatalf("expected 1 event, got %d", len(snapshot))
	}
	if snapshot[0].Username != "alice" || snapshot[0].UserID != 1 {
		t.Fatalf("expected alice/1, got %q/%d", snapshot[0].Username, snapshot[0].UserID)
	}
	if !snapshot[0].IsSnapshot {
		t.Fatal("expected IsSnapshot=true on snapshot events")
	}
}

func TestGetSnapshot_EmptyContactIDs(t *testing.T) {
	p, _ := newTestPresenceStore(t)
	ctx := context.Background()

	p.Connect(ctx, 1, "alice")

	snapshot, err := p.GetSnapshot(ctx, nil)
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if len(snapshot) != 0 {
		t.Fatalf("expected empty snapshot for nil contactIDs, got %d events", len(snapshot))
	}
}

func TestGetSnapshot_ContactOffline(t *testing.T) {
	p, _ := newTestPresenceStore(t)
	ctx := context.Background()

	// alice (1) is a contact but has never connected.
	snapshot, err := p.GetSnapshot(ctx, []int64{1})
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if len(snapshot) != 0 {
		t.Fatalf("expected 0 events for offline contact, got %d", len(snapshot))
	}
}

func TestGetSnapshot_OnlyOnlineContactsIncluded(t *testing.T) {
	p, _ := newTestPresenceStore(t)
	ctx := context.Background()

	// alice online, bob offline; requesting user has both as contacts.
	p.Connect(ctx, 1, "alice")

	snapshot, err := p.GetSnapshot(ctx, []int64{1, 2})
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if len(snapshot) != 1 {
		t.Fatalf("expected 1 event (only alice online), got %d", len(snapshot))
	}
	if snapshot[0].Username != "alice" {
		t.Fatalf("expected alice, got %q", snapshot[0].Username)
	}
}

// ─── StartPresenceSubscriber fan-out ─────────────────────────────────────────

func TestPresenceSubscriber_DeliverToFollowerOnly(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	hub := NewHub()

	// follower (100) has alice (1) in their contact list — should receive events.
	followerCh := make(chan []byte, 4)
	follower := &wsClient{userID: 100, username: "follower", send: followerCh, log: zap.NewNop()}
	hub.addClient(follower)

	// stranger (200) has no relationship with alice — must not receive events.
	strangerCh := make(chan []byte, 4)
	stranger := &wsClient{userID: 200, username: "stranger", send: strangerCh, log: zap.NewNop()}
	hub.addClient(stranger)

	us := newFakeUserStore()
	us.contactFollowers[1] = []int64{100} // only user 100 follows user 1

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	subDone := make(chan error, 1)
	go func() { subDone <- StartPresenceSubscriber(ctx, rdb, hub, us, zap.NewNop()) }()

	// Wait for the subscriber goroutine to subscribe before publishing.
	time.Sleep(50 * time.Millisecond)

	ev := PresenceEvent{Type: "presence", UserID: 1, Username: "alice", Status: "online"}
	b, _ := json.Marshal(ev)
	rdb.Publish(ctx, presenceChannel, string(b))

	// Follower must receive the event.
	select {
	case msg := <-followerCh:
		var got PresenceEvent
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal follower event: %v", err)
		}
		if got.UserID != 1 || got.Username != "alice" || got.Status != "online" {
			t.Fatalf("unexpected event content: %+v", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for follower to receive presence event")
	}

	// Stranger's channel must remain empty.
	select {
	case msg := <-strangerCh:
		t.Fatalf("stranger received unexpected presence event: %s", msg)
	default:
	}

	cancel()
	select {
	case <-subDone:
	case <-time.After(time.Second):
		t.Fatal("subscriber did not stop after context cancellation")
	}
}

func TestPresenceSubscriber_MultipleFollowers(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	hub := NewHub()

	ch1 := make(chan []byte, 4)
	hub.addClient(&wsClient{userID: 10, username: "follower1", send: ch1, log: zap.NewNop()})
	ch2 := make(chan []byte, 4)
	hub.addClient(&wsClient{userID: 20, username: "follower2", send: ch2, log: zap.NewNop()})

	us := newFakeUserStore()
	us.contactFollowers[1] = []int64{10, 20} // both follow user 1

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go StartPresenceSubscriber(ctx, rdb, hub, us, zap.NewNop())
	time.Sleep(50 * time.Millisecond)

	ev := PresenceEvent{Type: "presence", UserID: 1, Username: "alice", Status: "online"}
	b, _ := json.Marshal(ev)
	rdb.Publish(ctx, presenceChannel, string(b))

	for _, ch := range []chan []byte{ch1, ch2} {
		select {
		case msg := <-ch:
			var got PresenceEvent
			if err := json.Unmarshal(msg, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.UserID != 1 {
				t.Fatalf("expected UserID=1, got %d", got.UserID)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for a follower to receive the event")
		}
	}
}

func TestPresenceSubscriber_OfflineEvent(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	hub := NewHub()
	ch := make(chan []byte, 4)
	hub.addClient(&wsClient{userID: 100, username: "follower", send: ch, log: zap.NewNop()})

	us := newFakeUserStore()
	us.contactFollowers[1] = []int64{100}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go StartPresenceSubscriber(ctx, rdb, hub, us, zap.NewNop())
	time.Sleep(50 * time.Millisecond)

	ev := PresenceEvent{Type: "presence", UserID: 1, Username: "alice", Status: "offline"}
	b, _ := json.Marshal(ev)
	rdb.Publish(ctx, presenceChannel, string(b))

	select {
	case msg := <-ch:
		var got PresenceEvent
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Status != "offline" {
			t.Fatalf("expected status=offline, got %q", got.Status)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for offline event")
	}
}
