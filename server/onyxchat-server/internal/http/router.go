package http

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/cole/onyxchat-server/internal/store"
)

func NewRouter(
	userStore *store.UserStore,
	msgStore *store.MessageStore,
	hub *Hub,
	log *zap.Logger,
	jwtMgr *JWTManager,
	publisher EventPublisher,
	allowedOrigins []string,
	env string,
	rdb *redis.Client,
	adminUsername string,
) http.Handler {
	r := mux.NewRouter()

	// ---- middleware ----
	InitHTTPMetrics()
	r.Use(RequestID)
	r.Use(CORSMiddleware(allowedOrigins))
	r.Use(AccessLogAndMetrics(log))

	// ---- limiters ----
	ipLimiter := NewKeyedLimiter(rate.Limit(5), 10, 10*time.Minute)
	idLimiter := NewKeyedLimiter(rate.Limit(2), 4, 10*time.Minute)
	userLimiter := NewKeyedLimiter(rate.Limit(10), 20, 10*time.Minute)

	upgrader := NewUpgrader(allowedOrigins, env)

	// ---- basic routes ----
	if env != "prod" {
		r.Handle("/metrics", promhttp.Handler()).Methods(http.MethodGet)
	}
	r.HandleFunc("/health", HealthHandler).Methods(http.MethodGet)
	r.HandleFunc("/healthz", HealthHandler).Methods(http.MethodGet)
	r.HandleFunc("/health/live", LiveHandler).Methods(http.MethodGet)
	r.HandleFunc("/health/ready", ReadyHandler(userStore)).Methods(http.MethodGet)

	// ---- /api/v1 ----
	api := r.PathPrefix("/api/v1").Subrouter()

	// Bug fix: /api/v1/register now has IP rate limiting (was missing before)
	api.Handle("/register",
		LoginIPRateLimit(ipLimiter)(
			MaxBodyBytes(1<<20)(http.HandlerFunc(RegisterHandler(userStore, jwtMgr, rdb, log))),
		),
	).Methods(http.MethodPost, http.MethodOptions)

	api.Handle("/login",
		LoginIPRateLimit(ipLimiter)(
			MaxBodyBytes(1<<20)(
				http.HandlerFunc(LoginHandler(userStore, jwtMgr, idLimiter, rdb, log)),
			),
		),
	).Methods(http.MethodPost, http.MethodOptions)

	api.Handle("/refresh",
		MaxBodyBytes(1<<20)(http.HandlerFunc(RefreshHandler(userStore, jwtMgr, rdb, log))),
	).Methods(http.MethodPost, http.MethodOptions)

	api.Handle("/logout",
		MaxBodyBytes(1<<20)(http.HandlerFunc(LogoutHandler(rdb))),
	).Methods(http.MethodPost, http.MethodOptions)

	// ---- protected REST ----
	protected := api.NewRoute().Subrouter()
	protected.Use(AuthMiddleware(jwtMgr, userStore, log))
	protected.Use(PerUserRateLimit(userLimiter))

	protected.HandleFunc("/users", ListUsersHandler(userStore, log)).Methods(http.MethodGet, http.MethodOptions)

	protected.Handle("/users/me/password",
		MaxBodyBytes(1<<20)(http.HandlerFunc(ChangePasswordHandler(userStore))),
	).Methods(http.MethodPatch, http.MethodOptions)

	protected.Handle("/messages",
		MaxBodyBytes(1<<20)(http.HandlerFunc(SendMessageHandler(userStore, msgStore, hub, publisher, log))),
	).Methods(http.MethodPost, http.MethodOptions)

	protected.HandleFunc("/messages", ListMessagesHandler(userStore, msgStore, log)).Methods(http.MethodGet, http.MethodOptions)

	protected.HandleFunc("/contacts", ListContactsHandler(userStore, log)).Methods(http.MethodGet, http.MethodOptions)
	protected.Handle("/contacts",
		MaxBodyBytes(1<<20)(http.HandlerFunc(AddContactHandler(userStore, log))),
	).Methods(http.MethodPost, http.MethodOptions)
	protected.HandleFunc("/contacts/{username}", RemoveContactHandler(userStore, log)).Methods(http.MethodDelete, http.MethodOptions)

	// GDPR account deletion
	protected.Handle("/account",
		MaxBodyBytes(1<<20)(http.HandlerFunc(DeleteAccountHandler(userStore, log))),
	).Methods(http.MethodDelete, http.MethodOptions)

	// ---- admin ----
	admin := protected.NewRoute().Subrouter()
	admin.Use(AdminOnly(adminUsername))

	admin.HandleFunc("/admin/invites", AdminListInvitesHandler(userStore, log)).Methods(http.MethodGet, http.MethodOptions)
	admin.Handle("/admin/invites",
		MaxBodyBytes(1<<20)(http.HandlerFunc(AdminCreateInviteHandler(userStore, log)))).Methods(http.MethodPost, http.MethodOptions)
	admin.HandleFunc("/admin/invites/{code}/reset", AdminResetInviteHandler(userStore, log)).Methods(http.MethodPost, http.MethodOptions)

	// ---- E2E key endpoints ----
	// PUT  /api/v1/keys          — upload your own public key
	// GET  /api/v1/keys/{username} — fetch any user's public key
	protected.Handle("/keys",
		MaxBodyBytes(4096)(http.HandlerFunc(UploadKeyHandler(userStore))),
	).Methods(http.MethodPut, http.MethodOptions)

	protected.HandleFunc("/keys/{username}", GetKeyHandler(userStore)).Methods(http.MethodGet, http.MethodOptions)
	protected.Handle("/ws/ticket",
		MaxBodyBytes(1<<20)(http.HandlerFunc(WSTicketHandler(rdb))),
	).Methods(http.MethodPost, http.MethodOptions)

	// ---- websocket ----
	ws := api.NewRoute().Subrouter()
	ws.Use(WSAuthMiddleware(jwtMgr, rdb))
	ws.HandleFunc("/ws", WebSocketHandler(userStore, msgStore, hub, upgrader, log)).Methods(http.MethodGet)
	return r
}
