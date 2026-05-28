package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

func newAuthTest(t *testing.T) (*hub.Hub, string) {
	t.Helper()
	dir := t.TempDir()
	h, err := hub.New(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return h, "admin-token-test"
}

func TestRequireAdminMissingHeader(t *testing.T) {
	h, admin := newAuthTest(t)
	called := false
	handler := requireAdmin(admin, func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != 401 || called {
		t.Errorf("status=%d called=%v", w.Code, called)
	}
	_ = h
}

func TestRequireAdminWrongToken(t *testing.T) {
	_, admin := newAuthTest(t)
	handler := requireAdmin(admin, func(w http.ResponseWriter, r *http.Request) {})
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != 401 {
		t.Errorf("status=%d", w.Code)
	}
}

func TestRequireAdminCorrectToken(t *testing.T) {
	_, admin := newAuthTest(t)
	called := false
	handler := requireAdmin(admin, func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+admin)
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != 200 || !called {
		t.Errorf("status=%d called=%v", w.Code, called)
	}
}

func TestRequireDeviceTokenResolves(t *testing.T) {
	h, _ := newAuthTest(t)
	acc, _ := h.AddAccount("Personal")
	dev, tok, _ := h.AddDevice(acc.ID, "Mac", hub.RoleEither)

	var seen *hub.Device
	handler := requireDevice(h, func(w http.ResponseWriter, r *http.Request, d *hub.Device) {
		seen = d
	})
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != 200 || seen == nil || seen.ID != dev.ID {
		t.Errorf("status=%d seen=%v", w.Code, seen)
	}
}

func TestRequireDeviceTokenInvalid(t *testing.T) {
	h, _ := newAuthTest(t)
	handler := requireDevice(h, func(w http.ResponseWriter, r *http.Request, d *hub.Device) {})
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer bogus")
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != 401 {
		t.Errorf("status=%d", w.Code)
	}
}
