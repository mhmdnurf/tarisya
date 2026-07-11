package core

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	rateLimitClientTTL       = 10 * time.Minute
	rateLimitCleanupInterval = 5 * time.Minute
)

type clientRateLimiter struct {
	mu            sync.Mutex
	clients       map[string]*tokenBucket
	tokensPerSec  float64
	burst         float64
	now           func() time.Time
	lastCleanupAt time.Time
}

type tokenBucket struct {
	tokens   float64
	updated  time.Time
	lastSeen time.Time
}

func newClientRateLimiter(requestsPerMinute, burst int) *clientRateLimiter {
	return &clientRateLimiter{
		clients:      make(map[string]*tokenBucket),
		tokensPerSec: float64(requestsPerMinute) / 60,
		burst:        float64(burst),
		now:          time.Now,
	}
}

func (l *clientRateLimiter) allow(key string) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.lastCleanupAt.IsZero() || now.Sub(l.lastCleanupAt) >= rateLimitCleanupInterval {
		for client, bucket := range l.clients {
			if now.Sub(bucket.lastSeen) >= rateLimitClientTTL {
				delete(l.clients, client)
			}
		}
		l.lastCleanupAt = now
	}

	bucket, exists := l.clients[key]
	if !exists {
		bucket = &tokenBucket{tokens: l.burst, updated: now, lastSeen: now}
		l.clients[key] = bucket
	}
	elapsed := now.Sub(bucket.updated).Seconds()
	if elapsed > 0 {
		bucket.tokens = math.Min(l.burst, bucket.tokens+elapsed*l.tokensPerSec)
		bucket.updated = now
	}
	bucket.lastSeen = now
	if bucket.tokens >= 1 {
		bucket.tokens--
		return true, 0
	}
	retrySeconds := (1 - bucket.tokens) / l.tokensPerSec
	retry := time.Duration(math.Ceil(retrySeconds * float64(time.Second)))
	if retry < time.Second {
		retry = time.Second
	}
	return false, retry
}

func (h *Handler) rateLimit(limiter *clientRateLimiter, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed, retryAfter := limiter.allow(clientAddress(r))
		if !allowed {
			seconds := int(math.Ceil(retryAfter.Seconds()))
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next(w, r)
	})
}

// clientAddress intentionally ignores X-Forwarded-For and similar headers.
// Those headers are attacker-controlled unless a trusted reverse proxy is
// explicitly configured. Deployments behind a proxy should also rate-limit at
// that edge until trusted-proxy support is added.
func clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	if r.RemoteAddr != "" {
		return r.RemoteAddr
	}
	return "unknown"
}

func configuredRateLimit(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
