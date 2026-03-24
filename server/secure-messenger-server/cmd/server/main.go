package main

import (
	"context"
	"crypto/tls"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	serverhttp "github.com/cole/secure-messenger-server/internal/http"
	"github.com/cole/secure-messenger-server/internal/store"
	"go.uber.org/zap"
)

func parseAllowedOrigins(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

var logger *zap.Logger

func initLogger() {
	l, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	logger = l
}

func main() {
	initLogger()
	defer logger.Sync()

	// Environment
	env := os.Getenv("SM_ENV")
	if env == "" {
		env = "dev"
	}

	addr := os.Getenv("SM_SERVER_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	jwtSecret := store.ResolveSecret("JWT_SECRET", "/onyxchat/prod/JWT_SECRET")
	if env == "prod" && jwtSecret == "" {
		logger.Fatal("JWT_SECRET is required in prod (set SM_ENV=prod and JWT_SECRET)")
	}
	if env != "prod" && jwtSecret == "" {
		logger.Warn("JWT_SECRET not set; using insecure dev default", zap.String("SM_ENV", env))
	}

	// DB
	db := store.MustOpen()
	if err := store.EnsureSchema(db); err != nil {
		logger.Fatal("failed to ensure schema", zap.Error(err))
	}
	defer db.Close()

	// Redis
	redisAddr := os.Getenv("SM_REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "redis:6379"
	}

	redisOpts := &redis.Options{
		Addr:         redisAddr,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	if env == "prod" {
		redisOpts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		redisOpts.Password = store.ResolveSecret("SM_REDIS_AUTH_TOKEN", "/onyxchat/prod/SM_REDIS_AUTH_TOKEN")
	}

	rdb := redis.NewClient(redisOpts)
	defer rdb.Close()

	redisCtx, redisCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer redisCancel()
	if err := rdb.Ping(redisCtx).Err(); err != nil {
		logger.Fatal("failed to connect to Redis", zap.String("addr", redisAddr), zap.Error(err))
	}
	logger.Info("connected to Redis", zap.String("addr", redisAddr))

	// Deps
	userStore := store.NewUserStore(db)
	messageStore := store.NewMessageStore(db)
	hub := serverhttp.NewHub()
	jwtMgr := serverhttp.NewJWTManager(jwtSecret)
	publisher := &serverhttp.RedisPublisher{Client: rdb}

	// Start Redis subscriber with reconnect loop
	subscriberCtx, subscriberCancel := context.WithCancel(context.Background())
	defer subscriberCancel()
	go func() {
		for {
			select {
			case <-subscriberCtx.Done():
				return
			default:
			}
			if err := serverhttp.StartMessageSubscriber(subscriberCtx, rdb, messageStore, hub); err != nil {
				logger.Error("message subscriber exited, restarting in 2s", zap.Error(err))
			}
			select {
			case <-subscriberCtx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()

	// Router (NOTE: now includes jwtMgr)
	allowedOrigins := parseAllowedOrigins(os.Getenv("SM_ALLOWED_ORIGINS"))

	if env == "prod" && len(allowedOrigins) == 0 {
		logger.Fatal("SM_ALLOWED_ORIGINS is required in prod")
	}

	router := serverhttp.NewRouter(
		userStore,
		messageStore,
		hub,
		logger,
		jwtMgr,
		publisher,
		allowedOrigins,
		env,
		rdb,
	)

	// Server
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	go func() {
		logger.Info("server listening", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("listen failed", zap.Error(err))
		}
	}()

	// Shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	sig := <-stop
	logger.Info("shutdown signal received", zap.String("signal", sig.String()))
	hub.CloseAll("server shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", zap.Error(err))
	} else {
		logger.Info("graceful shutdown complete")
	}
}
