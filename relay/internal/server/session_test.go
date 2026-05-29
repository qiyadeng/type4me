package server

import (
	"strings"
	"testing"
	"time"
)

func TestSessionSignVerifyRoundTrip(t *testing.T) {
	s := newSessionSigner([]byte("test-key"))
	now := time.Unix(1_700_000_000, 0).UTC()
	tok := s.sign("acct-abc", now)
	aid, err := s.verify(tok, now.Add(time.Hour))
	if err != nil || aid != "acct-abc" {
		t.Fatalf("verify: aid=%q err=%v", aid, err)
	}
}

func TestSessionExpired(t *testing.T) {
	s := newSessionSigner([]byte("test-key"))
	now := time.Unix(1_700_000_000, 0).UTC()
	tok := s.sign("acct-abc", now)
	if _, err := s.verify(tok, now.Add(sessionTTL+time.Second)); err == nil {
		t.Error("expected expired error")
	}
}

func TestSessionTamperedAndWrongKey(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	tok := newSessionSigner([]byte("key-a")).sign("acct-abc", now)
	// wrong key must fail
	if _, err := newSessionSigner([]byte("key-b")).verify(tok, now); err == nil {
		t.Error("wrong key should fail")
	}
	// tampered payload must fail
	parts := strings.SplitN(tok, ".", 2)
	bad := "AAAA" + parts[0][4:] + "." + parts[1]
	if _, err := newSessionSigner([]byte("key-a")).verify(bad, now); err == nil {
		t.Error("tampered payload should fail")
	}
	// missing separator must fail
	if _, err := newSessionSigner([]byte("key-a")).verify("nodot", now); err == nil {
		t.Error("malformed token should fail")
	}
}
