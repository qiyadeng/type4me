package relayauth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func decodeBody(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func asAPIError(err error, target **APIError) bool {
	return errors.As(err, target)
}

func newStubRelay(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return &Client{RelayURL: ts.URL, HTTPClient: ts.Client()}
}

func TestLoginSuccess(t *testing.T) {
	c := newStubRelay(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/login" || r.Method != "POST" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"session_token":"sess-1","account_id":"acct-1","username":"alice","expires_at":"2030-01-01T00:00:00Z"}`))
	})
	sess, err := c.Login("alice", "supersecret")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if sess.Token != "sess-1" || sess.AccountID != "acct-1" || sess.Username != "alice" {
		t.Errorf("session = %+v", sess)
	}
}

func TestRegisterSendsInviteCode(t *testing.T) {
	var gotInvite string
	c := newStubRelay(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = decodeBody(r, &body)
		gotInvite = body["invite_code"]
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"session_token":"sess-2","account_id":"acct-2","username":"bob"}`))
	})
	sess, err := c.Register("bob", "supersecret", "INVITE-123")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if gotInvite != "INVITE-123" {
		t.Errorf("invite sent = %q, want INVITE-123", gotInvite)
	}
	if sess.Token != "sess-2" {
		t.Errorf("session = %+v", sess)
	}
}

func TestRegisterDeviceUsesSessionBearer(t *testing.T) {
	var gotAuth, gotLabel, gotRole string
	c := newStubRelay(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var body map[string]string
		_ = decodeBody(r, &body)
		gotLabel, gotRole = body["label"], body["role"]
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"device_id":"dev-1","device_token":"dtok","label":"WIN-PC","role":"either"}`))
	})
	dev, err := c.RegisterDevice("sess-1", "WIN-PC", "either")
	if err != nil {
		t.Fatalf("register device: %v", err)
	}
	if gotAuth != "Bearer sess-1" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if gotLabel != "WIN-PC" || gotRole != "either" {
		t.Errorf("label/role = %q/%q", gotLabel, gotRole)
	}
	if dev.ID != "dev-1" || dev.Token != "dtok" {
		t.Errorf("device = %+v", dev)
	}
}

func TestErrorMappingReadableMessages(t *testing.T) {
	cases := []struct {
		status int
		code   string
	}{
		{401, "invalid_credentials"},
		{403, "invalid_invite"},
		{409, "username_taken"},
		{429, "rate_limited"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			c := newStubRelay(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":"` + tc.code + `"}`))
			})
			_, err := c.Login("alice", "supersecret")
			if err == nil {
				t.Fatalf("expected error")
			}
			var apiErr *APIError
			if !asAPIError(err, &apiErr) {
				t.Fatalf("expected *APIError, got %T", err)
			}
			if apiErr.Code != tc.code || apiErr.Message == "" {
				t.Errorf("apiErr = %+v", apiErr)
			}
		})
	}
}
