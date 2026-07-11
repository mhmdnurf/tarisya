package core

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientRateLimiterRefillsTokens(t *testing.T) {
	now := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	limiter := newClientRateLimiter(60, 2)
	limiter.now = func() time.Time { return now }

	for i := 0; i < 2; i++ {
		if allowed, _ := limiter.allow("client"); !allowed {
			t.Fatalf("request %d was rejected within burst", i+1)
		}
	}
	allowed, retryAfter := limiter.allow("client")
	if allowed {
		t.Fatal("request beyond burst was allowed")
	}
	if retryAfter != time.Second {
		t.Fatalf("retry after = %v, want 1s", retryAfter)
	}

	now = now.Add(time.Second)
	if allowed, _ := limiter.allow("client"); !allowed {
		t.Fatal("request was rejected after a token refill")
	}
}

func TestRateLimitUsesSocketAddressAndIgnoresForwardedHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "203.0.113.10:4321"
	request.Header.Set("X-Forwarded-For", "198.51.100.20")

	if got := clientAddress(request); got != "203.0.113.10" {
		t.Fatalf("client address = %q, want socket address", got)
	}
}

func TestAuthenticationRateLimitReturnsRetryAfter(t *testing.T) {
	handler := NewHandler(nil, Config{
		AuthRateLimitPerMinute: 1,
		AuthRateLimitBurst:     1,
	})

	first := malformedJSONRequest("/api/v1/auth/login", "203.0.113.10:1000")
	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, first)
	if firstRecorder.Code != http.StatusBadRequest {
		t.Fatalf("first response status = %d, want %d", firstRecorder.Code, http.StatusBadRequest)
	}

	second := malformedJSONRequest("/api/v1/auth/login", "203.0.113.10:2000")
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, second)
	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second response status = %d, want %d", secondRecorder.Code, http.StatusTooManyRequests)
	}
	if got := secondRecorder.Header().Get("Retry-After"); got != "60" {
		t.Fatalf("Retry-After = %q, want 60", got)
	}

	differentClient := malformedJSONRequest("/api/v1/auth/login", "198.51.100.20:1000")
	differentRecorder := httptest.NewRecorder()
	handler.ServeHTTP(differentRecorder, differentClient)
	if differentRecorder.Code != http.StatusBadRequest {
		t.Fatalf("different client response status = %d, want %d", differentRecorder.Code, http.StatusBadRequest)
	}
}

func TestRateLimitPoliciesAreIndependent(t *testing.T) {
	handler := NewHandler(nil, Config{
		AuthRateLimitPerMinute:    1,
		AuthRateLimitBurst:        1,
		MetricsRateLimitPerMinute: 1,
		MetricsRateLimitBurst:     1,
	})
	client := "203.0.113.10:1000"

	authRecorder := httptest.NewRecorder()
	handler.ServeHTTP(authRecorder, malformedJSONRequest("/api/v1/auth/login", client))
	metricsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(metricsRecorder, malformedJSONRequest("/api/v1/metrics", client))

	if authRecorder.Code != http.StatusBadRequest || metricsRecorder.Code != http.StatusBadRequest {
		t.Fatalf("independent policy statuses auth=%d metrics=%d, want 400 and 400", authRecorder.Code, metricsRecorder.Code)
	}
}

func malformedJSONRequest(path, remoteAddress string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, path, http.NoBody)
	request.RemoteAddr = remoteAddress
	request.Header.Set("Content-Type", "application/json")
	return request
}
