package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

func newAuthServer(t *testing.T) (*httptest.Server, *hub.Hub) {
	t.Helper()
	dir := t.TempDir()
	h, err := hub.New(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(Options{
		Hub:         h,
		AdminToken:  "admin-test",
		Version:     "test",
		InviteCodes: []string{"LET-ME-IN"},
		SessionKey:  []byte("test-session-key"),
	})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, h
}

func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestRegisterSuccess(t *testing.T) {
	ts, _ := newAuthServer(t)
	resp := postJSON(t, ts.URL+"/v1/auth/register",
		`{"username":"alice","password":"supersecret","invite_code":"LET-ME-IN"}`)
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["session_token"] == "" || body["account_id"] == "" {
		t.Errorf("missing fields: %+v", body)
	}
}

func TestRegisterBadInvite(t *testing.T) {
	ts, _ := newAuthServer(t)
	resp := postJSON(t, ts.URL+"/v1/auth/register",
		`{"username":"alice","password":"supersecret","invite_code":"WRONG"}`)
	if resp.StatusCode != 403 {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestRegisterDuplicateUsername(t *testing.T) {
	ts, _ := newAuthServer(t)
	_ = postJSON(t, ts.URL+"/v1/auth/register",
		`{"username":"alice","password":"supersecret","invite_code":"LET-ME-IN"}`)
	resp := postJSON(t, ts.URL+"/v1/auth/register",
		`{"username":"Alice","password":"supersecret","invite_code":"LET-ME-IN"}`)
	if resp.StatusCode != 409 {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

func TestLoginSuccessAndWrongPassword(t *testing.T) {
	ts, _ := newAuthServer(t)
	_ = postJSON(t, ts.URL+"/v1/auth/register",
		`{"username":"alice","password":"supersecret","invite_code":"LET-ME-IN"}`)
	ok := postJSON(t, ts.URL+"/v1/auth/login", `{"username":"alice","password":"supersecret"}`)
	if ok.StatusCode != 200 {
		t.Errorf("login status = %d, want 200", ok.StatusCode)
	}
	bad := postJSON(t, ts.URL+"/v1/auth/login", `{"username":"alice","password":"nope"}`)
	if bad.StatusCode != 401 {
		t.Errorf("bad login status = %d, want 401", bad.StatusCode)
	}
}

func TestRegistrationDisabledWhenNoInviteCodes(t *testing.T) {
	dir := t.TempDir()
	h, _ := hub.New(filepath.Join(dir, "state.json"))
	s := New(Options{Hub: h, AdminToken: "a", Version: "t", SessionKey: []byte("k")})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	resp := postJSON(t, ts.URL+"/v1/auth/register",
		`{"username":"alice","password":"supersecret","invite_code":""}`)
	if resp.StatusCode != 403 {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestAuthRoutesRejectNonPost(t *testing.T) {
	ts, _ := newAuthServer(t)
	for _, url := range []string{ts.URL + "/v1/auth/register", ts.URL + "/v1/auth/login"} {
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 405 {
			t.Errorf("GET %s = %d, want 405", url, resp.StatusCode)
		}
	}
}
