package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterAllowsUpToLimitThenBlocks(t *testing.T) {
	cur := time.Unix(0, 0)
	l := newRateLimiter(3, time.Minute)
	l.now = func() time.Time { return cur }
	for i := 0; i < 3; i++ {
		if !l.allow("1.2.3.4") {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if l.allow("1.2.3.4") {
		t.Error("4th request should be blocked")
	}
	// a different IP is unaffected
	if !l.allow("5.6.7.8") {
		t.Error("different IP should be allowed")
	}
	// window advance resets
	cur = cur.Add(time.Minute + time.Second)
	if !l.allow("1.2.3.4") {
		t.Error("after window, should be allowed again")
	}
}

func TestRateLimiterWrap429(t *testing.T) {
	cur := time.Unix(0, 0)
	l := newRateLimiter(1, time.Minute)
	l.now = func() time.Time { return cur }
	h := l.wrap(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })

	rec1 := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/auth/login", nil)
	req.RemoteAddr = "9.9.9.9:1234"
	h(rec1, req)
	if rec1.Code != 200 {
		t.Fatalf("first = %d, want 200", rec1.Code)
	}
	rec2 := httptest.NewRecorder()
	h(rec2, req)
	if rec2.Code != 429 {
		t.Errorf("second = %d, want 429", rec2.Code)
	}
}

func TestClientIPPrefersXFF(t *testing.T) {
	req := httptest.NewRequest("POST", "/", nil)
	req.RemoteAddr = "10.0.0.1:55555"
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	if got := clientIP(req); got != "203.0.113.7" {
		t.Errorf("clientIP = %q, want 203.0.113.7", got)
	}
	req2 := httptest.NewRequest("POST", "/", nil)
	req2.RemoteAddr = "10.0.0.1:55555"
	if got := clientIP(req2); got != "10.0.0.1" {
		t.Errorf("clientIP = %q, want 10.0.0.1", got)
	}
}
