package http

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cole/onyxchat-server/internal/store"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

const (
	maxWSMessageBytes = 16 * 1024

	wsMsgsPerSecond = 20
	wsBurst         = 40

	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = 45 * time.Second

	// Tune for your traffic. Bigger = more buffering, but more memory per conn.
	sendQueueSize = 64
)

// ---- Message types ----

type TypingIncoming struct {
	Type     string `json:"type"` // "typing"
	To       string `json:"to"`
	IsTyping bool   `json:"isTyping"`
}

type TypingOutgoing struct {
	Type     string `json:"type"` // "typing"
	From     string `json:"from"`
	To       string `json:"to"`
	IsTyping bool   `json:"isTyping"`
}

type PresenceEvent struct {
	Type       string `json:"type"` // "presence"
	UserID     int64  `json:"userId"`
	Username   string `json:"username"`
	Status     string `json:"status"` // "online"|"offline"
	IsSnapshot bool   `json:"isSnapshot,omitempty"`
}

// ---- Client wrapper (1 writer per conn) ----

type wsClient struct {
	userID   int64
	username string
	conn     *websocket.Conn
	send     chan []byte
	log      *zap.Logger

	closed atomic.Bool
}

func (c *wsClient) close() { c.closeWith(websocket.CloseNormalClosure, "") }
func (c *wsClient) closeWith(code int, reason string) {
	if c.closed.Swap(true) {
		return
	}

	// avoid panic if something else already closed send (belt & suspenders)
	func() {
		defer func() { _ = recover() }()
		close(c.send)
	}()

	if c.conn != nil {
		_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
		_ = c.conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(code, reason),
			time.Now().Add(writeWait),
		)
		_ = c.conn.Close()
	}
}

// ---- Hub ----
// Hub tracks locally connected WebSocket clients for message/typing routing.
// Presence state (online/offline) is managed by PresenceStore via Redis so it
// works correctly across multiple server instances.

type Hub struct {
	mu sync.RWMutex

	clientsByUser  map[int64]map[*wsClient]struct{}
	limitersByUser map[int64]*rate.Limiter // shared per user, not per connection
}

func NewHub() *Hub {
	return &Hub{
		clientsByUser:  make(map[int64]map[*wsClient]struct{}),
		limitersByUser: make(map[int64]*rate.Limiter),
	}
}

// limiterFor returns the shared rate limiter for a user, creating one if needed.
func (h *Hub) limiterFor(userID int64) *rate.Limiter {
	h.mu.Lock()
	defer h.mu.Unlock()
	if l, ok := h.limitersByUser[userID]; ok {
		return l
	}
	l := rate.NewLimiter(rate.Limit(wsMsgsPerSecond), wsBurst)
	h.limitersByUser[userID] = l
	return l
}

func (h *Hub) addClient(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()

	set := h.clientsByUser[c.userID]
	if set == nil {
		set = make(map[*wsClient]struct{})
		h.clientsByUser[c.userID] = set
	}
	set[c] = struct{}{}
}

func (h *Hub) removeClient(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()

	set := h.clientsByUser[c.userID]
	if set == nil {
		return
	}
	delete(set, c)
	if len(set) == 0 {
		delete(h.clientsByUser, c.userID)
		delete(h.limitersByUser, c.userID)
	}
}

func (h *Hub) broadcastJSON(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}

	// Copy targets under lock, then write without holding lock.
	h.mu.RLock()
	var targets []*wsClient
	for _, set := range h.clientsByUser {
		for c := range set {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range targets {
		enqueueDropSlow(c, b)
	}
}

func (h *Hub) sendToUser(userID int64, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}

	h.mu.RLock()
	set := h.clientsByUser[userID]
	// copy to avoid holding lock during enqueue
	var targets []*wsClient
	for c := range set {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	for _, c := range targets {
		enqueueDropSlow(c, b)
	}
}

func (h *Hub) CloseAll(reason string) {
	h.mu.RLock()
	var targets []*wsClient
	for _, set := range h.clientsByUser {
		for c := range set {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range targets {
		c.closeWith(websocket.CloseGoingAway, reason)
	}
}

// DisconnectUser closes all WebSocket connections for the given user ID.
// Called after account deletion so the session doesn't linger.
func (h *Hub) DisconnectUser(userID int64) {
	h.mu.RLock()
	var targets []*wsClient
	for c := range h.clientsByUser[userID] {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	for _, c := range targets {
		c.closeWith(websocket.ClosePolicyViolation, "account deleted")
	}
}

// If a client is too slow and its buffer is full, drop it (or drop message).
// In production, you almost always want to disconnect slow consumers.
func enqueueDropSlow(c *wsClient, msg []byte) {
	if c.closed.Load() {
		return
	}
	select {
	case c.send <- msg:
	default:
		c.closeWith(websocket.ClosePolicyViolation, "slow consumer")
	}
}

// ---- Upgrader (production-safe origin check) ----

// Provide allowed origins via config/env in real prod.
func NewUpgrader(allowedOrigins []string, env string) websocket.Upgrader {
	allow := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		allow[o] = struct{}{}
	}

	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")

			// Dev convenience: allow non-browser clients
			if origin == "" {
				return env != "prod"
			}

			_, ok := allow[origin]
			return ok
		},
	}
}

// ---- Handler ----

func WebSocketHandler(
	userStore userStorer,
	msgStore messageStorer,
	hub *Hub,
	presenceStore *PresenceStore,
	upgrader websocket.Upgrader,
	log *zap.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sinceIDStr := r.URL.Query().Get("sinceId")
		sinceID, _ := strconv.ParseInt(sinceIDStr, 10, 64)

		log.Info("[WebSocket] request", zap.String("path", r.URL.Path), zap.String("query", r.URL.RawQuery))

		authUser := CurrentUser(r)
		if authUser == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		log.Info("[WebSocket] authorized", zap.Int64("user", authUser.ID), zap.String("username", authUser.Username))

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Error("[WebSocket] upgrade failed", zap.Error(err))
			return
		}
		log.Info("[WebSocket] upgraded", zap.Int64("user", authUser.ID))

		c := &wsClient{
			userID:   authUser.ID,
			username: authUser.Username,
			conn:     conn,
			send:     make(chan []byte, sendQueueSize),
			log:      log,
		}

		conn.SetReadLimit(maxWSMessageBytes)
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		conn.SetPongHandler(func(string) error {
			_ = conn.SetReadDeadline(time.Now().Add(pongWait))
			return nil
		})

		hub.addClient(c)
		ActiveWSConnections.Inc()

		// Register with distributed presence and send a snapshot of which contacts
		// are currently online. We fetch contacts first so the snapshot is scoped
		// to people the user actually cares about.
		presenceCtx := r.Context()
		if _, err := presenceStore.Connect(presenceCtx, c.userID, c.username); err != nil {
			log.Warn("[WebSocket] presence connect error", zap.Int64("user", c.userID), zap.Error(err))
		}

		contacts, err := userStore.ListContacts(c.userID)
		if err != nil {
			log.Warn("[WebSocket] contact list error", zap.Int64("user", c.userID), zap.Error(err))
		}
		contactIDs := make([]int64, len(contacts))
		for i, ct := range contacts {
			contactIDs[i] = ct.ID
		}

		snapshot, err := presenceStore.GetSnapshot(presenceCtx, contactIDs)
		if err != nil {
			log.Warn("[WebSocket] presence snapshot error", zap.Int64("user", c.userID), zap.Error(err))
		}
		for _, evt := range snapshot {
			b, _ := json.Marshal(evt)
			enqueueDropSlow(c, b)
		}

		// Deliver any messages the client missed while disconnected.
		if sinceID >= 0 {
			unread, err := msgStore.GetUnreadForUser(c.userID, sinceID)
			if err == nil {
				for _, msg := range unread {
					hub.SendMessageToUser(c.userID, &msg)
				}
			}
		}

		wsCtx, cancel := context.WithCancel(r.Context())
		go c.writePump(wsCtx, cancel, presenceStore)
		go c.readPump(wsCtx, cancel, userStore, hub, presenceStore)

		log.Info("[WebSocket] pumps started", zap.Int64("user", c.userID))
		<-wsCtx.Done()
		log.Info("[WebSocket] ctx done", zap.Int64("user", c.userID))
	}
}

func (c *wsClient) writePump(ctx context.Context, cancel context.CancelFunc, presenceStore *PresenceStore) {
	c.log.Info("[WebSocket] writePump start", zap.Int64("user", c.userID))
	defer c.log.Info("[WebSocket] writePump exit", zap.Int64("user", c.userID))
	defer func() {
		cancel()
		c.close()
	}()

	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Try to flush buffered messages quickly, then exit
			deadline := time.Now().Add(writeWait)
			for {
				select {
				case msg, ok := <-c.send:
					if !ok {
						return
					}
					_ = c.conn.SetWriteDeadline(deadline)
					if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
						return
					}
				default:
					return
				}
			}

		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				c.log.Error("[WebSocket] writePump write error", zap.Int64("user", c.userID), zap.Error(err))
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.log.Error("[WebSocket] writePump ping error", zap.Int64("user", c.userID), zap.Error(err))
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.log.Error("[WebSocket] ping error", zap.Int64("user", c.userID), zap.Error(err))
				return
			}
			if err := presenceStore.Refresh(context.Background(), c.userID); err != nil {
				c.log.Warn("[WebSocket] presence refresh error", zap.Int64("user", c.userID), zap.Error(err))
			}
		}
	}
}

func (c *wsClient) readPump(ctx context.Context, cancel context.CancelFunc, userStore userStorer, hub *Hub, presenceStore *PresenceStore) {
	c.log.Info("[WebSocket] readPump start", zap.Int64("user", c.userID))
	defer c.log.Info("[WebSocket] readPump exit", zap.Int64("user", c.userID))

	limiter := hub.limiterFor(c.userID)

	defer func() {
		ActiveWSConnections.Dec()
		hub.removeClient(c)
		disconnectCtx := context.Background()
		if _, err := presenceStore.Disconnect(disconnectCtx, c.userID, c.username); err != nil {
			c.log.Warn("[WebSocket] presence disconnect error", zap.Int64("user", c.userID), zap.Error(err))
		}
		cancel()
		c.close()
		c.log.Info("[WebSocket] user disconnected", zap.Int64("user", c.userID))
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !limiter.Allow() {
			c.log.Warn("[WebSocket] rate limit", zap.Int64("user", c.userID))
			WSRateLimitRejections.Inc()
			c.closeWith(websocket.ClosePolicyViolation, "rate limit")
			return
		}

		mt, data, err := c.conn.ReadMessage()
		if err != nil {
			if ce, ok := err.(*websocket.CloseError); ok {
				c.log.Info("[WebSocket] close", zap.Int64("user", c.userID), zap.Int("code", ce.Code), zap.String("text", ce.Text))
				return
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				c.log.Warn("[WebSocket] timeout", zap.Int64("user", c.userID), zap.Error(err))
				return
			}
			c.log.Error("[WebSocket] read error", zap.Int64("user", c.userID), zap.Error(err))
			return
		}

		if mt != websocket.TextMessage {
			c.log.Warn("[WebSocket] unsupported message type", zap.Int64("user", c.userID), zap.Int("mt", mt))
			continue
		}

		var base struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &base); err != nil {
			c.log.Warn("[WebSocket] invalid JSON", zap.Int64("user", c.userID), zap.Error(err))
			continue
		}

		WSMessagesReceived.WithLabelValues(base.Type).Inc()

		switch base.Type {
		case "typing":
			var ti TypingIncoming
			if err := json.Unmarshal(data, &ti); err != nil {
				c.log.Warn("[WebSocket] invalid typing JSON", zap.Int64("user", c.userID), zap.Error(err))
				continue
			}

			toUser, err := userStore.GetUserByUsername(ti.To)
			if err != nil {
				// Don't reveal whether the username exists — just drop silently.
				if !errors.Is(err, store.ErrUserNotFound) {
					c.log.Error("[WebSocket] typing lookup error", zap.Int64("user", c.userID), zap.String("to", ti.To), zap.Error(err))
				}
				continue
			}

			// Only forward typing indicators between mutual contacts.
			// Without this check any user could probe for valid usernames or
			// spam typing notifications to arbitrary accounts.
			ok, err := userStore.IsContact(c.userID, toUser.ID)
			if err != nil {
				c.log.Error("[WebSocket] typing contact check error", zap.Int64("user", c.userID), zap.Error(err))
				continue
			}
			if !ok {
				continue
			}

			hub.SendTypingToUser(c.username, toUser.ID, toUser.Username, ti.IsTyping)

		default:
			c.log.Warn("[WebSocket] unknown message type", zap.Int64("user", c.userID), zap.String("type", base.Type))
		}
	}
}

// ---- Push APIs you already had ----

func (h *Hub) SendMessageToUser(userID int64, msg *store.Message) {
	type payload struct {
		Type    string         `json:"type"`
		Message *store.Message `json:"message"`
	}
	h.sendToUser(userID, payload{Type: "message", Message: msg})
}

func (h *Hub) SendTypingToUser(fromUsername string, toUserID int64, toUsername string, isTyping bool) {
	h.sendToUser(toUserID, TypingOutgoing{
		Type:     "typing",
		From:     fromUsername,
		To:       toUsername,
		IsTyping: isTyping,
	})
}

func (h *Hub) SendMessageDeletedToUser(userID int64, messageID int64) {
	type payload struct {
		Type      string `json:"type"`
		MessageID int64  `json:"messageId"`
	}
	h.sendToUser(userID, payload{Type: "message_deleted", MessageID: messageID})
}

// SendKeyChangedToUser notifies a single connected user that the named peer has
// uploaded a new public key. The recipient should re-fetch the key and re-derive
// the shared secret before sending their next message.
func (h *Hub) SendKeyChangedToUser(userID int64, changedUsername string) {
	type keyChangedEvent struct {
		Type     string `json:"type"`
		Username string `json:"username"`
	}
	h.sendToUser(userID, keyChangedEvent{Type: "key_changed", Username: changedUsername})
}

// Optional: clean close on server shutdown if you have a global cancel.
var ErrClientClosed = errors.New("client closed")
