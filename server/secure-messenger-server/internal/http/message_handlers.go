package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/cole/secure-messenger-server/internal/store"
)

const (
	maxUsernameLen = 32
	maxMessageLen  = 8192 // bumped: ciphertext is larger than plaintext
)

// ─────────────────────────────────────────────────────────────
// POST /api/v1/messages
// Body (plaintext):  {"recipientUsername":"bob","body":"hello"}
// Body (encrypted):  {"recipientUsername":"bob","body":"<ciphertext>","iv":"<nonce>","encrypted":true}
// ─────────────────────────────────────────────────────────────

type SendMessageRequest struct {
	RecipientUsername string `json:"recipientUsername"`
	Body              string `json:"body"`
	IV                string `json:"iv"`
	Encrypted         bool   `json:"encrypted"`
	ClientMessageID   string `json:"clientMessageId"`
}

type MessageCreatedEvent struct {
	MessageID       int64  `json:"messageId"`
	SenderID        int64  `json:"senderId"`
	RecipientID     int64  `json:"recipientId"`
	ClientMessageID string `json:"clientMessageId"`
}

type RedisPublisher struct {
	Client *redis.Client
}

func (p *RedisPublisher) PublishMessageCreated(ctx context.Context, event MessageCreatedEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return p.Client.Publish(ctx, "message.created", payload).Err()
}

func (r *SendMessageRequest) Validate() string {
	if r.RecipientUsername == "" || r.Body == "" || r.ClientMessageID == "" {
		return "recipientUsername, body, and clientMessageId required"
	}
	if len(r.RecipientUsername) > maxUsernameLen {
		return "recipientUsername too long"
	}
	if len(r.Body) > maxMessageLen {
		return "message body too long"
	}
	if len(r.ClientMessageID) > 128 {
		return "clientMessageId too long"
	}
	if r.Encrypted && r.IV == "" {
		return "iv required when encrypted is true"
	}
	return ""
}

func StartMessageSubscriber(
	ctx context.Context,
	rdb *redis.Client,
	msgStore messageStorer,
	hub *Hub,
) error {
	sub := rdb.Subscribe(ctx, "message.created")
	defer sub.Close()
	ch := sub.Channel()

	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-ch:
			if !ok {
				return fmt.Errorf("subscription channel closed")
			}
			var ev MessageCreatedEvent
			if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
				log.Printf("[RedisSub] invalid payload: %v", err)
				continue
			}

			fullMsg, err := msgStore.GetByID(ev.MessageID)
			if err != nil {
				log.Printf("[RedisSub] failed to load message %d: %v", ev.MessageID, err)
				continue
			}

			hub.SendMessageToUser(fullMsg.SenderID, fullMsg)
			hub.SendMessageToUser(fullMsg.RecipientID, fullMsg)
		}
	}
}

type EventPublisher interface {
	PublishMessageCreated(ctx context.Context, event MessageCreatedEvent) error
}

func SendMessageHandler(
	userStore userStorer,
	msgStore messageStorer,
	hub *Hub,
	publisher EventPublisher,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		currentUser := CurrentUser(r)
		if currentUser == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var req SendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if msg := req.Validate(); msg != "" {
			http.Error(w, msg, http.StatusBadRequest)
			return
		}

		recipient, err := userStore.GetByUsername(req.RecipientUsername)
		if err != nil || recipient == nil {
			http.Error(w, "recipient not found", http.StatusBadRequest)
			return
		}

		dbStart := time.Now()
		saved, inserted, err := msgStore.CreateOrGetExisting(
			currentUser.ID,
			recipient.ID,
			req.Body,
			req.IV,
			req.Encrypted,
			req.ClientMessageID,
		)
		ObserveDBQuery("message_create", dbStart)
		if err != nil {
			log.Printf("[SendMessage] failed to save message: %v", err)
			http.Error(w, "failed to save message", http.StatusInternalServerError)
			return
		}

		if inserted {
			MessagesSent.Inc()
		} else {
			MessageDeduplicated.Inc()
		}

		w.Header().Set("Content-Type", "application/json")
		if inserted {
			w.WriteHeader(http.StatusCreated)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		if err := json.NewEncoder(w).Encode(saved); err != nil {
			log.Printf("[SendMessage] failed to write response: %v", err)
			return
		}

		event := MessageCreatedEvent{
			MessageID:       saved.ID,
			SenderID:        saved.SenderID,
			RecipientID:     saved.RecipientID,
			ClientMessageID: req.ClientMessageID,
		}

		go func(ev MessageCreatedEvent) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			if err := publisher.PublishMessageCreated(ctx, ev); err != nil {
				log.Printf("[SendMessage] failed to publish event: %v", err)
				MessagePublishFailures.Inc()
			}
		}(event)
	}
}

// ─────────────────────────────────────────────────────────────
// GET /api/v1/messages?peer=<username>&sinceId=<id>
// ─────────────────────────────────────────────────────────────

type ListMessagesResponse struct {
	Messages []store.Message `json:"messages"`
}

func ListMessagesHandler(userStore userStorer, msgStore messageStorer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		currentUser := CurrentUser(r)
		if currentUser == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		peer := r.URL.Query().Get("peer")
		if peer == "" {
			http.Error(w, "missing peer", http.StatusBadRequest)
			return
		}

		sinceStr := r.URL.Query().Get("sinceId")
		var sinceID int64
		if sinceStr != "" {
			if v, err := strconv.ParseInt(sinceStr, 10, 64); err == nil {
				sinceID = v
			}
		}

		peerUser, err := userStore.GetByUsername(peer)
		if err != nil || peerUser == nil {
			http.Error(w, "peer not found", http.StatusNotFound)
			return
		}

		dbStart := time.Now()
		msgs, err := msgStore.ListConversationSince(currentUser.ID, peerUser.ID, sinceID)
		ObserveDBQuery("message_list", dbStart)
		if err != nil {
			log.Printf("[ListMessages] failed to fetch messages: %v", err)
			http.Error(w, "failed to fetch messages", http.StatusInternalServerError)
			return
		}

		log.Printf("[ListMessages] returning %d messages", len(msgs))

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(ListMessagesResponse{Messages: msgs}); err != nil {
			log.Printf("[ListMessages] failed to write response: %v", err)
		}
	}
}
