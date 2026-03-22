package http

import (
	"fmt"
	"time"

	"github.com/cole/secure-messenger-server/internal/store"
	"github.com/golang-jwt/jwt/v5"
)

// Claims are what we embed in the JWT.
type Claims struct {
	UserID   int64  `json:"uid"`
	Username string `json:"uname"`
	jwt.RegisteredClaims
}

// JWTManager handles signing/parsing tokens.
type JWTManager struct {
	secret []byte
	ttl    time.Duration
}

// NewJWTManager reads JWT_SECRET and sets a default TTL.
func NewJWTManager(secret string) *JWTManager {
	if secret == "" {
		secret = "dev-insecure-jwt-secret-change-me"
	}
	return &JWTManager{
		secret: []byte(secret),
		ttl:    24 * time.Hour,
	}
}

func (m *JWTManager) Generate(user *store.User) (string, error) {
	now := time.Now()

	claims := &Claims{
		UserID:   user.ID,
		Username: user.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprint(user.ID),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

func (m *JWTManager) Parse(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %T", t.Method)
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}
