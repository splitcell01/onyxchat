package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestLoginIPRateLimit_SecondRequestIs429(t *testing.T) {
	ipLimiter := NewKeyedLimiter(rate.Limit(0), 1, 1*time.Minute)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := LoginIPRateLimit(ipLimiter)(next)

	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "1.2.3.4:12345"

	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, req)
	if rr1.Code != http.StatusOK {
		t.Fatalf("expected 200 on first request, got %d", rr1.Code)
	}

	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on second request, got %d", rr2.Code)
	}
	if got := rr2.Header().Get("Retry-After"); got == "" {
		t.Fatalf("expected Retry-After header to be set")
	}
}

func TestPerUserRateLimit_AllowsWhenNoCurrentUser(t *testing.T) {
	userLimiter := NewKeyedLimiter(rate.Limit(0), 1, 1*time.Minute)

	nextCalled := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled++
		w.WriteHeader(http.StatusOK)
	})

	h := PerUserRateLimit(userLimiter)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if nextCalled != 1 {
		t.Fatalf("expected next to be called once, got %d", nextCalled)
	}
}

func TestPerUserRateLimit_SecondRequestIs429_WhenUserPresent(t *testing.T) {
	userLimiter := NewKeyedLimiter(rate.Limit(0), 1, 1*time.Minute)

	injectUser := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), userContextKey, &AuthUser{ID: 42, Username: "cole"})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := injectUser(PerUserRateLimit(userLimiter)(next))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)

	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, req)
	if rr1.Code != http.StatusOK {
		t.Fatalf("expected 200 on first request, got %d", rr1.Code)
	}

	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on second request, got %d", rr2.Code)
	}
}
