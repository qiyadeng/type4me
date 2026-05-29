package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func registerAndToken(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	resp := postJSON(t, ts.URL+"/v1/auth/register",
		`{"username":"alice","password":"supersecret","invite_code":"LET-ME-IN"}`)
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	tok, _ := body["session_token"].(string)
	if tok == "" {
		t.Fatal("no session token")
	}
	return tok
}

func doWithSession(t *testing.T, method, url, token, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestDevicesRequireSession(t *testing.T) {
	ts, _ := newAuthServer(t)
	resp, _ := http.Get(ts.URL + "/v1/devices")
	if resp.StatusCode != 401 {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestRegisterDeviceThenList(t *testing.T) {
	ts, _ := newAuthServer(t)
	tok := registerAndToken(t, ts)

	post := doWithSession(t, "POST", ts.URL+"/v1/devices", tok, `{"label":"Win"}`)
	if post.StatusCode != 201 {
		t.Fatalf("post device = %d, want 201", post.StatusCode)
	}
	var dev map[string]any
	_ = json.NewDecoder(post.Body).Decode(&dev)
	if dev["device_token"] == "" || dev["device_id"] == "" {
		t.Fatalf("missing device fields: %+v", dev)
	}

	list := doWithSession(t, "GET", ts.URL+"/v1/devices", tok, "")
	if list.StatusCode != 200 {
		t.Fatalf("list = %d, want 200", list.StatusCode)
	}
	var body struct {
		Devices []map[string]any `json:"devices"`
	}
	_ = json.NewDecoder(list.Body).Decode(&body)
	if len(body.Devices) != 1 {
		t.Fatalf("device count = %d, want 1", len(body.Devices))
	}
	if body.Devices[0]["online"] != false {
		t.Errorf("online = %v, want false (no subscriber)", body.Devices[0]["online"])
	}
}

func TestDevicesAccountIsolation(t *testing.T) {
	ts, _ := newAuthServer(t)
	tokA := registerAndToken(t, ts)
	respB := postJSON(t, ts.URL+"/v1/auth/register",
		`{"username":"bobby","password":"supersecret","invite_code":"LET-ME-IN"}`)
	var bb map[string]any
	_ = json.NewDecoder(respB.Body).Decode(&bb)
	tokB, _ := bb["session_token"].(string)

	_ = doWithSession(t, "POST", ts.URL+"/v1/devices", tokA, `{"label":"A-Win"}`)
	_ = doWithSession(t, "POST", ts.URL+"/v1/devices", tokB, `{"label":"B-Win"}`)

	list := doWithSession(t, "GET", ts.URL+"/v1/devices", tokA, "")
	var body struct {
		Devices []map[string]any `json:"devices"`
	}
	_ = json.NewDecoder(list.Body).Decode(&body)
	if len(body.Devices) != 1 || body.Devices[0]["label"] != "A-Win" {
		t.Errorf("isolation broken: %+v", body.Devices)
	}
}

func TestDevicesRejectTamperedSession(t *testing.T) {
	ts, _ := newAuthServer(t)
	tok := registerAndToken(t, ts)
	bad := tok + "tamper" // corrupts the HMAC signature segment
	resp := doWithSession(t, "GET", ts.URL+"/v1/devices", bad, "")
	if resp.StatusCode != 401 {
		t.Errorf("tampered session = %d, want 401", resp.StatusCode)
	}
}
