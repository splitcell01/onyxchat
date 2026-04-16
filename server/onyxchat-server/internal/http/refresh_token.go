package http

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	refreshTokenTTL    = 30 * 24 * time.Hour
	refreshTokenPrefix = "rt:"
)

func newRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func storeRefreshToken(ctx context.Context, rdb *redis.Client, token string, userID int64) error {
	defer ObserveRedisOp("token_set", time.Now())
	return rdb.Set(ctx, refreshTokenPrefix+token, userID, refreshTokenTTL).Err()
}

func lookupRefreshToken(ctx context.Context, rdb *redis.Client, token string) (int64, error) {
	defer ObserveRedisOp("token_get", time.Now())
	val, err := rdb.Get(ctx, refreshTokenPrefix+token).Int64()
	if err == redis.Nil {
		return 0, fmt.Errorf("invalid or expired refresh token")
	}
	return val, err
}

func deleteRefreshToken(ctx context.Context, rdb *redis.Client, token string) error {
	defer ObserveRedisOp("token_del", time.Now())
	return rdb.Del(ctx, refreshTokenPrefix+token).Err()
}
