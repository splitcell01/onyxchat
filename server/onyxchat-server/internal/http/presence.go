package http

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	presenceNamesKey = "presence:names"   // HASH: userID -> username, only online users
	presenceConnsKey = "presence:conns:"  // + userID -> INCR/DECR connection counter
	presenceChannel  = "presence.events"
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
	key := presenceConnsKey + strconv.FormatInt(userID, 10)
	n, err := p.rdb.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}
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

// GetSnapshot returns all currently online users for the initial presence snapshot
// sent to a newly connected client.
func (p *PresenceStore) GetSnapshot(ctx context.Context) ([]PresenceEvent, error) {
	result, err := p.rdb.HGetAll(ctx, presenceNamesKey).Result()
	if err != nil {
		return nil, err
	}
	events := make([]PresenceEvent, 0, len(result))
	for idStr, username := range result {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
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

// StartPresenceSubscriber subscribes to presence.events and broadcasts each event
// to all locally connected clients. Mirrors the StartMessageSubscriber pattern.
func StartPresenceSubscriber(ctx context.Context, rdb *redis.Client, hub *Hub, log *zap.Logger) error {
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
			hub.broadcastJSON(ev)
		}
	}
}
