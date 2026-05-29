package appflow

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/qiyadeng/type4me/receiver/internal/config"
	"github.com/qiyadeng/type4me/receiver/internal/relayauth"
)

type fakeAuth struct {
	session                                relayauth.Session
	device                                 relayauth.Device
	loginErr, registerErr, regDevErr       error
	loginCalls, registerCalls, regDevCalls int
	lastInvite, lastSession, lastLabel, lastRole string
}

func (f *fakeAuth) Login(u, p string) (relayauth.Session, error) {
	f.loginCalls++
	return f.session, f.loginErr
}
func (f *fakeAuth) Register(u, p, invite string) (relayauth.Session, error) {
	f.registerCalls++
	f.lastInvite = invite
	return f.session, f.registerErr
}
func (f *fakeAuth) RegisterDevice(session, label, role string) (relayauth.Device, error) {
	f.regDevCalls++
	f.lastSession, f.lastLabel, f.lastRole = session, label, role
	return f.device, f.regDevErr
}

func newController(cfgPath string, auth *fakeAuth, started *bool) *Controller {
	return &Controller{
		Cfg:      &config.Config{Mode: config.ModeListener},
		CfgPath:  cfgPath,
		Auth:     auth,
		Hostname: "WIN-PC",
		RelayURL: "https://relay.example",
		StartSub: func(c *config.Config) { *started = true },
	}
}

func TestResumeIfConfigured(t *testing.T) {
	started := false
	c := &Controller{
		Cfg:      &config.Config{Mode: config.ModeRelaySubscriber, RelayURL: "u", DeviceID: "d", DeviceToken: "t"},
		StartSub: func(*config.Config) { started = true },
	}
	if got := c.ResumeIfConfigured(); !got || !started {
		t.Errorf("configured: resume=%v started=%v", got, started)
	}

	started2 := false
	c2 := &Controller{
		Cfg:      &config.Config{Mode: config.ModeListener},
		StartSub: func(*config.Config) { started2 = true },
	}
	if c2.ResumeIfConfigured() || started2 {
		t.Error("listener config should not resume")
	}
}

func TestLoginAndStartFreshRegistersDevice(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	auth := &fakeAuth{
		session: relayauth.Session{Token: "sess-tok", AccountID: "acct-1"},
		device:  relayauth.Device{ID: "dev-9", Token: "dev-tok"},
	}
	started := false
	c := newController(p, auth, &started)
	if err := c.LoginAndStart("alice", "supersecret", "", false); err != nil {
		t.Fatalf("LoginAndStart: %v", err)
	}
	if auth.loginCalls != 1 || auth.registerCalls != 0 {
		t.Errorf("login=%d register=%d", auth.loginCalls, auth.registerCalls)
	}
	if auth.regDevCalls != 1 || auth.lastSession != "sess-tok" || auth.lastLabel != "WIN-PC" || auth.lastRole != "either" {
		t.Errorf("register device wrong: %+v", auth)
	}
	if c.Cfg.Mode != config.ModeRelaySubscriber || c.Cfg.RelayURL != "https://relay.example" ||
		c.Cfg.DeviceID != "dev-9" || c.Cfg.DeviceToken != "dev-tok" {
		t.Errorf("cfg not set: %+v", c.Cfg)
	}
	saved, err := config.ReadFile(p)
	if err != nil || !saved.IsRelayConfigured() || saved.DeviceToken != "dev-tok" {
		t.Errorf("not persisted: %+v err=%v", saved, err)
	}
	if !started {
		t.Error("StartSub not called")
	}
}

func TestLoginAndStartSkipsDeviceRegistrationWhenTokenExists(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	auth := &fakeAuth{session: relayauth.Session{Token: "sess-tok"}}
	started := false
	c := newController(p, auth, &started)
	c.Cfg = &config.Config{Mode: config.ModeRelaySubscriber, DeviceID: "dev-old", DeviceToken: "tok-old"}
	if err := c.LoginAndStart("alice", "supersecret", "", false); err != nil {
		t.Fatalf("LoginAndStart: %v", err)
	}
	if auth.regDevCalls != 0 {
		t.Errorf("should not re-register device; regDevCalls=%d", auth.regDevCalls)
	}
	if c.Cfg.DeviceToken != "tok-old" {
		t.Errorf("device token changed: %q", c.Cfg.DeviceToken)
	}
}

func TestLoginAndStartRegisterMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	auth := &fakeAuth{
		session: relayauth.Session{Token: "sess-tok"},
		device:  relayauth.Device{ID: "dev-9", Token: "dev-tok"},
	}
	started := false
	c := newController(p, auth, &started)
	if err := c.LoginAndStart("alice", "supersecret", "INVITE-1", true); err != nil {
		t.Fatalf("LoginAndStart: %v", err)
	}
	if auth.registerCalls != 1 || auth.loginCalls != 0 || auth.lastInvite != "INVITE-1" {
		t.Errorf("register mode wrong: %+v", auth)
	}
}

func TestLoginAndStartLoginFailureNoSideEffects(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	auth := &fakeAuth{loginErr: errors.New("bad creds")}
	started := false
	c := newController(p, auth, &started)
	err := c.LoginAndStart("alice", "wrong", "", false)
	if err == nil {
		t.Fatal("expected login error")
	}
	if auth.regDevCalls != 0 || started {
		t.Errorf("side effects on login failure: regDev=%d started=%v", auth.regDevCalls, started)
	}
	if got, _ := config.ReadFile(p); got.IsRelayConfigured() {
		t.Error("config should not have been written on login failure")
	}
}

func TestLoginAndStartSaveFailureRollsBackCreds(t *testing.T) {
	// CfgPath under a non-existent directory makes config.Save fail.
	p := filepath.Join(t.TempDir(), "missing-dir", "config.json")
	auth := &fakeAuth{
		session: relayauth.Session{Token: "sess-tok"},
		device:  relayauth.Device{ID: "dev-9", Token: "dev-tok"},
	}
	started := false
	c := newController(p, auth, &started)
	err := c.LoginAndStart("alice", "supersecret", "", false)
	if err == nil {
		t.Fatal("expected save error")
	}
	if c.Cfg.DeviceID != "" || c.Cfg.DeviceToken != "" {
		t.Errorf("creds not rolled back: %+v", c.Cfg)
	}
	if started {
		t.Error("StartSub should not be called on save failure")
	}
}

func TestLoginAndStartRegisterDeviceFailure(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	auth := &fakeAuth{
		session:   relayauth.Session{Token: "sess-tok"},
		regDevErr: errors.New("relay down"),
	}
	started := false
	c := newController(p, auth, &started)
	err := c.LoginAndStart("alice", "supersecret", "", false)
	if err == nil {
		t.Fatal("expected register-device error")
	}
	if started {
		t.Error("StartSub should not be called when device registration fails")
	}
	if got, _ := config.ReadFile(p); got.IsRelayConfigured() {
		t.Error("config should not have been written on device-registration failure")
	}
}

func TestLoginAndStartSaveFailurePreExistingTokenRestoresState(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing-dir", "config.json") // Save will fail
	auth := &fakeAuth{session: relayauth.Session{Token: "sess-tok"}}
	started := false
	c := newController(p, auth, &started)
	c.Cfg = &config.Config{Mode: config.ModeListener, DeviceID: "dev-old", DeviceToken: "tok-old"}
	err := c.LoginAndStart("alice", "supersecret", "", false)
	if err == nil {
		t.Fatal("expected save error")
	}
	if auth.regDevCalls != 0 {
		t.Errorf("should not register a new device when token exists; got %d", auth.regDevCalls)
	}
	// Pre-existing creds preserved; Mode/RelayURL restored to pre-call values.
	if c.Cfg.DeviceToken != "tok-old" || c.Cfg.DeviceID != "dev-old" {
		t.Errorf("pre-existing creds changed: %+v", c.Cfg)
	}
	if c.Cfg.Mode != config.ModeListener || c.Cfg.RelayURL != "" {
		t.Errorf("mode/relayurl not restored on save failure: mode=%q relay=%q", c.Cfg.Mode, c.Cfg.RelayURL)
	}
	if started {
		t.Error("StartSub should not be called on save failure")
	}
}

func TestLogoutClearsCredsAndPersists(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	c := &Controller{
		Cfg:     &config.Config{Mode: config.ModeRelaySubscriber, RelayURL: "u", DeviceID: "d", DeviceToken: "t"},
		CfgPath: p,
	}
	if err := c.Cfg.Save(p); err != nil {
		t.Fatal(err)
	}
	if err := c.Logout(); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if c.Cfg.DeviceID != "" || c.Cfg.DeviceToken != "" {
		t.Errorf("creds not cleared: %+v", c.Cfg)
	}
	saved, _ := config.ReadFile(p)
	if saved.IsRelayConfigured() {
		t.Error("logout not persisted")
	}
}
