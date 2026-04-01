package http

import (
	"strings"
	"testing"
	"time"

	"github.com/cole/onyxchat-server/internal/store"
)

func TestJWT_GenerateAndParse_RoundTrip(t *testing.T) {
	m := NewJWTManager("test-secret")

	u := &store.User{ID: 123, Username: "cole"}
	token, err := m.Generate(u)
	if err != nil {
		t.Fatalf("Generate() err: %v", err)
	}
	if token == "" {
		t.Fatalf("expected non-empty token")
	}

	claims, err := m.Parse(token)
	if err != nil {
		t.Fatalf("Parse() err: %v", err)
	}

	if claims.UserID != 123 {
		t.Fatalf("expected UserID=123, got %d", claims.UserID)
	}
	if claims.Username != "cole" {
		t.Fatalf("expected Username=cole, got %q", claims.Username)
	}
	if claims.Subject != "123" {
		t.Fatalf("expected Subject=123, got %q", claims.Subject)
	}
	if claims.ExpiresAt == nil || claims.IssuedAt == nil {
		t.Fatalf("expected ExpiresAt and IssuedAt to be set")
	}
}

func TestJWT_Parse_RejectsWrongSecret(t *testing.T) {
	u := &store.User{ID: 1, Username: "a"}

	m1 := NewJWTManager("secret-1")
	token, err := m1.Generate(u)
	if err != nil {
		t.Fatalf("Generate() err: %v", err)
	}

	m2 := NewJWTManager("secret-2")
	_, err = m2.Parse(token)
	if err == nil {
		t.Fatalf("expected Parse() to fail with wrong secret")
	}
}

func TestJWT_Parse_RejectsGarbage(t *testing.T) {
	m := NewJWTManager("test-secret")
	_, err := m.Parse("not-a-jwt")
	if err == nil {
		t.Fatalf("expected Parse() to fail for garbage input")
	}
}

func TestJWT_Parse_RejectsNonHMACAlg(t *testing.T) {
	// Build a token with an unexpected signing method (alg=none style attack).
	// Your Parse() explicitly rejects non-HMAC methods.
	m := NewJWTManager("test-secret")

	// This string doesn't need to be valid/signed; we just want to ensure
	// ParseWithClaims doesn't accept non-HMAC methods.
	// A typical "none" token has 3 parts; we'll provide one that triggers parsing.
	noneAlgToken := strings.Join([]string{
		"eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0", // {"alg":"none","typ":"JWT"}
		"eyJ1aWQiOjEsInVuYW1lIjoiYSJ9",        // {"uid":1,"uname":"a"}
		"",                                    // no signature
	}, ".")

	_, err := m.Parse(noneAlgToken)
	if err == nil {
		t.Fatalf("expected Parse() to fail for non-HMAC algorithm")
	}
}

func TestJWT_Expiry_Respected(t *testing.T) {
	m := NewJWTManager("test-secret")

	// Force an already-expired token
	m.ttl = -1 * time.Second

	u := &store.User{ID: 1, Username: "a"}
	token, err := m.Generate(u)
	if err != nil {
		t.Fatalf("Generate() err: %v", err)
	}

	_, err = m.Parse(token)
	if err == nil {
		t.Fatalf("expected Parse() to fail for expired token")
	}
}
