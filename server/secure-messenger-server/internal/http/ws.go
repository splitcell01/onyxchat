package http

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv" // add this
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cole/secure-messenger-server/internal/store"
	"github.com/gorilla/websocket"
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

type Hub struct {
	mu sync.RWMutex

	clientsByUser map[int64]map[*wsClient]struct{}
	onlineCount   map[int64]int
	usernames     map[int64]string
}

func NewHub() *Hub {
	return &Hub{
		clientsByUser: make(map[int64]map[*wsClient]struct{}),
		onlineCount:   make(map[int64]int),
		usernames:     make(map[int64]string),
	}
}

func (h *Hub) addClient(c *wsClient) (snapshot []PresenceEvent, becameOnline bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	set := h.clientsByUser[c.userID]
	if set == nil {
		set = make(map[*wsClient]struct{})
		h.clientsByUser[c.userID] = set
	}
	set[c] = struct{}{}

	prev := h.onlineCount[c.userID]
	h.onlineCount[c.userID] = prev + 1
	h.usernames[c.userID] = c.username

	// build snapshot while locked (fast, memory only)
	for uid, cnt := range h.onlineCount {
		if cnt <= 0 {
			continue
		}
		un := h.usernames[uid]
		snapshot = append(snapshot, PresenceEvent{
			Type:       "presence",
			UserID:     uid,
			Username:   un,
			Status:     "online",
			IsSnapshot: true,
		})
	}

	becameOnline = (prev == 0)
	return snapshot, becameOnline
}

func (h *Hub) removeClient(c *wsClient) (becameOffline bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	set := h.clientsByUser[c.userID]
	if set == nil {
		return false
	}

	delete(set, c)
	if len(set) == 0 {
		delete(h.clientsByUser, c.userID)
	}

	prev := h.onlineCount[c.userID]
	if prev <= 1 {
		delete(h.onlineCount, c.userID)
		delete(h.usernames, c.userID)
		return true
	}

	h.onlineCount[c.userID] = prev - 1
	return false
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
	upgrader websocket.Upgrader,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sinceIDStr := r.URL.Query().Get("sinceId")
		sinceID, _ := strconv.ParseInt(sinceIDStr, 10, 64)

		log.Printf("WS request: path=%s query=%q", r.URL.Path, r.URL.RawQuery)

		authUser := CurrentUser(r)
		if authUser == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		log.Printf("WS authorized user=%d username=%s", authUser.ID, authUser.Username)

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[WebSocket] upgrade failed: %v", err)
			return
		}
		log.Printf("WS upgraded user=%d", authUser.ID)

		c := &wsClient{
			userID:   authUser.ID,
			username: authUser.Username,
			conn:     conn,
			send:     make(chan []byte, sendQueueSize),
		}

		conn.SetReadLimit(maxWSMessageBytes)
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		conn.SetPongHandler(func(string) error {
			_ = conn.SetReadDeadline(time.Now().Add(pongWait))
			return nil
		})

		snapshot, becameOnline := hub.addClient(c)

		if becameOnline && sinceID >= 0 {
			unread, err := msgStore.GetUnreadForUser(c.userID, sinceID)
			if err == nil {
				for _, msg := range unread {
					hub.SendMessageToUser(c.userID, &msg)
				}
			}
		}

		ActiveWSConnections.Inc()

		for _, evt := range snapshot {
			b, _ := json.Marshal(evt)
			enqueueDropSlow(c, b)
		}

		if becameOnline {
			hub.broadcastJSON(PresenceEvent{
				Type:     "presence",
				UserID:   c.userID,
				Username: c.username,
				Status:   "online",
			})
		}

		wsCtx, cancel := context.WithCancel(r.Context())
		go c.writePump(wsCtx, cancel)
		go c.readPump(wsCtx, cancel, userStore, hub)

		log.Printf("[WebSocket] pumps started user=%d", c.userID)
		<-wsCtx.Done()
		log.Printf("[WebSocket] ctx done user=%d", c.userID)
	}
}

func (c *wsClient) writePump(ctx context.Context, cancel context.CancelFunc) {
	log.Printf("[WebSocket] writePump start user=%d", c.userID)
	defer log.Printf("[WebSocket] writePump exit user=%d", c.userID)
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
				log.Printf("[WebSocket] writePump write error user=%d: %v", c.userID, err)
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("[WebSocket] writePump ping error user=%d: %v", c.userID, err)
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("[WebSocket] ping error user=%d: %v", c.userID, err)
				return
			}
		}
	}
}

func (c *wsClient) readPump(ctx context.Context, cancel context.CancelFunc, userStore userStorer, hub *Hub) {
	log.Printf("[WebSocket] readPump start user=%d", c.userID)
	defer log.Printf("[WebSocket] readPump exit user=%d", c.userID)

	limiter := rate.NewLimiter(rate.Limit(wsMsgsPerSecond), wsBurst)

	defer func() {
		ActiveWSConnections.Dec()
		becameOffline := hub.removeClient(c)
		if becameOffline {
			hub.broadcastJSON(PresenceEvent{
				Type:     "presence",
				UserID:   c.userID,
				Username: c.username,
				Status:   "offline",
			})
		}
		cancel()
		c.close()
		log.Printf("[WebSocket] user %d disconnected", c.userID)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !limiter.Allow() {
			log.Printf("[WebSocket] rate limit user=%d", c.userID)
			WSRateLimitRejections.Inc()
			c.closeWith(websocket.ClosePolicyViolation, "rate limit")
			return
		}

		mt, data, err := c.conn.ReadMessage()
		if err != nil {
			if ce, ok := err.(*websocket.CloseError); ok {
				log.Printf("[WebSocket] close user=%d code=%d text=%q", c.userID, ce.Code, ce.Text)
				return
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				log.Printf("[WebSocket] timeout user=%d: %v", c.userID, err)
				return
			}
			log.Printf("[WebSocket] read error user=%d: %v", c.userID, err)
			return
		}

		if mt != websocket.TextMessage {
			log.Printf("[WebSocket] unsupported message type user=%d mt=%d", c.userID, mt)
			continue
		}

		var base struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &base); err != nil {
			log.Printf("[WebSocket] invalid JSON user=%d: %v", c.userID, err)
			continue
		}

		WSMessagesReceived.WithLabelValues(base.Type).Inc()

		switch base.Type {
		case "typing":
			var ti TypingIncoming
			if err := json.Unmarshal(data, &ti); err != nil {
				log.Printf("[WebSocket] invalid typing JSON user=%d: %v", c.userID, err)
				continue
			}

			toUser, err := userStore.GetUserByUsername(ti.To)
			if err != nil {
				if errors.Is(err, store.ErrUserNotFound) {
					log.Printf("[WebSocket] typing to unknown user=%d to=%q", c.userID, ti.To)
					continue
				}
				log.Printf("[WebSocket] typing lookup error user=%d to=%q: %v", c.userID, ti.To, err)
				continue
			}

			hub.SendTypingToUser(c.username, toUser.ID, toUser.Username, ti.IsTyping)

		default:
			log.Printf("[WebSocket] unknown message type user=%d type=%q", c.userID, base.Type)
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

// Optional: clean close on server shutdown if you have a global cancel.
var ErrClientClosed = errors.New("client closed")