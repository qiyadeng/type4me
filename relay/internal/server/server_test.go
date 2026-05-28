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

func adminPOST(t *testing.T, ts *httptest.Server, admin, path, body string) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+admin)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&parsed)
	return resp, parsed
}

func TestAdminCreateAccount(t *testing.T) {
	ts, _, admin := newTestServer(t)
	resp, body := adminPOST(t, ts, admin, "/v1/admin/accounts", `{"name":"Personal"}`)
	if resp.StatusCode != 201 || body["account_id"] == nil {
		t.Errorf("status=%d body=%+v", resp.StatusCode, body)
	}
}

func TestAdminCreateAccountNoAuth(t *testing.T) {
	ts, _, _ := newTestServer(t)
	resp, err := http.Post(ts.URL+"/v1/admin/accounts", "application/json", strings.NewReader(`{"name":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 401 {
		t.Errorf("status=%d", resp.StatusCode)
	}
}

func TestAdminCreateAccountEmptyName(t *testing.T) {
	ts, _, admin := newTestServer(t)
	resp, _ := adminPOST(t, ts, admin, "/v1/admin/accounts", `{"name":""}`)
	if resp.StatusCode != 400 {
		t.Errorf("status=%d", resp.StatusCode)
	}
}

func TestAdminCreateDevice(t *testing.T) {
	ts, _, admin := newTestServer(t)
	_, accBody := adminPOST(t, ts, admin, "/v1/admin/accounts", `{"name":"P"}`)
	accID := accBody["account_id"].(string)
	resp, body := adminPOST(t, ts, admin, "/v1/admin/devices",
		`{"account_id":"`+accID+`","label":"Mac"}`)
	if resp.StatusCode != 201 || body["device_token"] == nil {
		t.Errorf("status=%d body=%+v", resp.StatusCode, body)
	}
	tok := body["device_token"].(string)
	if len(tok) != 43 {
		t.Errorf("token len=%d, want 43", len(tok))
	}
}

func TestAdminCreateDeviceAccountNotFound(t *testing.T) {
	ts, _, admin := newTestServer(t)
	resp, _ := adminPOST(t, ts, admin, "/v1/admin/devices",
		`{"account_id":"acct-missing","label":"Mac"}`)
	if resp.StatusCode != 404 {
		t.Errorf("status=%d", resp.StatusCode)
	}
}

func TestAdminRotateDevice(t *testing.T) {
	ts, _, admin := newTestServer(t)
	_, accBody := adminPOST(t, ts, admin, "/v1/admin/accounts", `{"name":"P"}`)
	accID := accBody["account_id"].(string)
	_, devBody := adminPOST(t, ts, admin, "/v1/admin/devices",
		`{"account_id":"`+accID+`","label":"Mac"}`)
	devID := devBody["device_id"].(string)
	oldTok := devBody["device_token"].(string)
	resp, body := adminPOST(t, ts, admin, "/v1/admin/devices/"+devID+"/rotate", "")
	if resp.StatusCode != 200 || body["device_token"] == oldTok {
		t.Errorf("status=%d body=%+v", resp.StatusCode, body)
	}
}

func TestAdminDeleteDevice(t *testing.T) {
	ts, _, admin := newTestServer(t)
	_, accBody := adminPOST(t, ts, admin, "/v1/admin/accounts", `{"name":"P"}`)
	accID := accBody["account_id"].(string)
	_, devBody := adminPOST(t, ts, admin, "/v1/admin/devices",
		`{"account_id":"`+accID+`","label":"Mac"}`)
	devID := devBody["device_id"].(string)
	req, _ := http.NewRequest("DELETE", ts.URL+"/v1/admin/devices/"+devID, nil)
	req.Header.Set("Authorization", "Bearer "+admin)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 204 {
		t.Errorf("status=%d", resp.StatusCode)
	}
}
