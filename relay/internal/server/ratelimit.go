package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	authRateLimit  = 10
	authRateWindow = time.Minute
)

type ipWindow struct {
	count int
	start time.Time
}

type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	hits   map[string]*ipWindow
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		limit:  limit,
		window: window,
		now:    time.Now,
		hits:   map[string]*ipWindow{},
	}
}

func (l *rateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	w, ok := l.hits[ip]
	if !ok || now.Sub(w.start) >= l.window {
		l.hits[ip] = &ipWindow{count: 1, start: now}
		return true
	}
	if w.count >= l.limit {
		return false
	}
	w.count++
	return true
}

func (l *rateLimiter) wrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(clientIP(r)) {
			writeJSON(w, 429, map[string]string{"error": "rate_limited"})
			return
		}
		next(w, r)
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
