package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

func newTestServer(t *testing.T) (*httptest.Server, *hub.Hub, string) {
	t.Helper()
	dir := t.TempDir()
	h, err := hub.New(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin := "admin-test-token"
	s := New(Options{Hub: h, AdminToken: admin, Version: "test"})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, h, admin
}

func TestHealthz(t *testing.T) {
	ts, _, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["ok"] != true || body["version"] != "test" {
		t.Errorf("body = %+v", body)
	}
}

func TestPingRequiresAuth(t *testing.T) {
	ts, _, _ := newTestServer(t)
	resp, _ := http.Get(ts.URL + "/v1/ping")
	if resp.StatusCode != 401 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestPingWithDeviceToken(t *testing.T) {
	ts, h, _ := newTestServer(t)
	acc, _ := h.AddAccount("Personal")
	dev, tok, _ := h.AddDevice(acc.ID, "Mac", hub.RoleEither)
	req, _ := http.NewRequest("GET", ts.URL+"/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["device_id"] != dev.ID || body["account_id"] != acc.ID {
		t.Errorf("body = %+v", body)
	}
}
