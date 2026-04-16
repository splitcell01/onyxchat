package http

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	presenceNamesKey = "presence:names"  // HASH: userID -> username, only online users
	presenceConnsKey = "presence:conns:" // + userID -> INCR/DECR connection counter
	presenceChannel  = "presence.events"

	// presenceConnsTTL is the expiry on each connection-counter key. It must be
	// longer than two ping cycles (2 × 45 s = 90 s) so a single slow pong does
	// not false-expire, while still clearing stale keys after a server crash.
	presenceConnsTTL = 2 * time.Minute
)

type PresenceStore struct {
	rdb *redis.Client
}

func NewPresenceStore(rdb *redis.Client) *PresenceStore {
	return &PresenceStore{rdb: rdb}
}

// Connect increments the global connection counter for userID. Returns true if this
// is the first connection across all instances (user just came online).
func (p *PresenceStore) Connect(ctx context.Context, userID int64, username string) (bool, error) {
	defer ObserveRedisOp("presence_connect", time.Now())
	key := presenceConnsKey + strconv.FormatInt(userID, 10)
	n, err := p.rdb.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}
	p.rdb.Expire(ctx, key, presenceConnsTTL)
	if n == 1 {
		p.rdb.HSet(ctx, presenceNamesKey, strconv.FormatInt(userID, 10), username)
		p.publish(ctx, PresenceEvent{Type: "presence", UserID: userID, Username: username, Status: "online"})
		return true, nil
	}
	return false, nil
}

// Disconnect decrements the global connection counter. Returns true if no connections
// remain across all instances (user just went offline).
func (p *PresenceStore) Disconnect(ctx context.Context, userID int64, username string) (bool, error) {
	defer ObserveRedisOp("presence_disconnect", time.Now())
	key := presenceConnsKey + strconv.FormatInt(userID, 10)
	n, err := p.rdb.Decr(ctx, key).Result()
	if err != nil {
		return false, err
	}
	if n <= 0 {
		p.rdb.Del(ctx, key)
		p.rdb.HDel(ctx, presenceNamesKey, strconv.FormatInt(userID, 10))
		p.publish(ctx, PresenceEvent{Type: "presence", UserID: userID, Username: username, Status: "offline"})
		return true, nil
	}
	return false, nil
}

// Refresh resets the TTL on the connection-counter key. Call this on each
// WebSocket ping so the key stays alive for connected clients and expires
// naturally after presenceConnsTTL if the server crashes without calling Disconnect.
func (p *PresenceStore) Refresh(ctx context.Context, userID int64) error {
	defer ObserveRedisOp("presence_refresh", time.Now())
	key := presenceConnsKey + strconv.FormatInt(userID, 10)
	return p.rdb.Expire(ctx, key, presenceConnsTTL).Err()
}

// GetSnapshot returns online status for the given contact IDs. Only users whose
// ID appears in contactIDs are included, so a newly connected client only learns
// about contacts, not every online user on the server.
func (p *PresenceStore) GetSnapshot(ctx context.Context, contactIDs []int64) ([]PresenceEvent, error) {
	if len(contactIDs) == 0 {
		return nil, nil
	}
	defer ObserveRedisOp("presence_snapshot", time.Now())

	allowed := make(map[int64]struct{}, len(contactIDs))
	for _, id := range contactIDs {
		allowed[id] = struct{}{}
	}

	result, err := p.rdb.HGetAll(ctx, presenceNamesKey).Result()
	if err != nil {
		return nil, err
	}
	events := make([]PresenceEvent, 0)
	for idStr, username := range result {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			continue
		}
		if _, ok := allowed[id]; !ok {
			continue
		}
		events = append(events, PresenceEvent{
			Type:       "presence",
			UserID:     id,
			Username:   username,
			Status:     "online",
			IsSnapshot: true,
		})
	}
	return events, nil
}

func (p *PresenceStore) publish(ctx context.Context, ev PresenceEvent) {
	b, _ := json.Marshal(ev)
	p.rdb.Publish(ctx, presenceChannel, string(b))
}

// StartPresenceSubscriber subscribes to presence.events and delivers each event
// only to users who have the subject in their contact list. This prevents leaking
// who is online to unrelated users.
func StartPresenceSubscriber(ctx context.Context, rdb *redis.Client, hub *Hub, userStore userStorer, log *zap.Logger) error {
	sub := rdb.Subscribe(ctx, presenceChannel)
	defer sub.Close()
	ch := sub.Channel()

	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-ch:
			if !ok {
				return fmt.Errorf("presence subscription channel closed")
			}
			var ev PresenceEvent
			if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
				log.Error("[PresenceSub] invalid payload", zap.Error(err))
				continue
			}
			followerIDs, err := userStore.GetContactFollowerIDs(ev.UserID)
			if err != nil {
				log.Error("[PresenceSub] follower lookup failed", zap.Int64("user", ev.UserID), zap.Error(err))
				continue
			}
			for _, followerID := range followerIDs {
				hub.sendToUser(followerID, ev)
			}
		}
	}
}
