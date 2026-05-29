# 账号系统 · 第 3 期 Windows 端(Fyne 登录界面)实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 Windows receiver 从纯 CLI 升级为「Fyne 登录窗口 + 系统托盘」:用户输入用户名/密码(注册可选)即自动登记本机为设备并开始接收,relay 地址 build 固化,凭据本地保存,已登录则免登录直连。

**Architecture:** 全部可测逻辑抽到纯 Go 包——`internal/relayauth`(调 relay 自助接口)、`internal/appflow`(登录→登记→存→起订阅 的编排)、`internal/config` 助手、`internal/relay.Subscriber` 状态回调——这些在 macOS 上 `go test` 即可验证。Fyne GUI 与系统托盘放在新二进制 `cmd/type4me-receiver-gui`,用 `//go:build gui` 标签隔离,只在带 `-tags gui`(且 CGO)时编译,Windows 上手动验证。原 CLI 二进制 `type4me-receiver` 保持不变。

**Tech Stack:** Go 1.25,stdlib(`net/http`、`encoding/json`),`fyne.io/fyne/v2`(GUI,CGO)。测试用 stdlib `testing` + `net/http/httptest`。

**上游 spec:** `docs/superpowers/specs/2026-05-29-account-system-windows-design.md`
**依赖:** 后端第 1 期已就绪(`/v1/auth/{register,login}`、`POST/GET /v1/devices`),见 `2026-05-29-account-system-backend-design.md`。

**所有命令在 `receiver/` 目录下运行**(`cd receiver`),除非另行说明。receiver 的 Go module 是 `github.com/qiyadeng/type4me/receiver`。

---

## 关于 `//go:build gui` 标签(务必理解)

- `cmd/type4me-receiver-gui/` 下所有文件首行带 `//go:build gui`。默认构建标签下该目录**没有可构建文件**,故 `go test ./...`、`go build ./...`、`CGO_ENABLED=0 GOOS=windows go vet ./...` 都会**跳过**它(通配符 `./...` 对"约束排除全部文件"的包静默跳过,不报错),Fyne 永不被编译进这些流程。
- `go.mod` 仍会保留 `fyne.io/fyne/v2`(`go mod tidy` 会考虑所有构建标签的依赖),但只有 `go build -tags gui` 才真正编译它。
- 因此 Task 1–4 的纯 Go 逻辑测试在 macOS 上飞快、零 Fyne 依赖;Task 5 才引入 Fyne 并手动验证。

---

## 文件结构

| 文件 | 责任 | 本计划 |
|---|---|---|
| `internal/relayauth/client.go` | 调 relay 自助接口(login/register/register-device)+ 错误映射 | **新增** |
| `internal/config/config.go` | 配置 | 改:+ `IsRelayConfigured` + `ReadFile`(宽松读) |
| `internal/relay/subscriber.go` | SSE 订阅 | 改:+ 可选 `OnStatus` 状态回调 |
| `internal/appflow/controller.go` | 登录→登记→存→起订阅 编排(可测) | **新增** |
| `cmd/type4me-receiver-gui/build.go` | build 固化常量(tag gui) | **新增** |
| `cmd/type4me-receiver-gui/main.go` | Fyne app:启动判定 + 登录窗 + 托盘(tag gui) | **新增** |
| `Makefile` | windows GUI 构建目标 | 改 |
| `cmd/type4me-receiver/main.go` | 原 CLI | 不变 |

---

## Task 1: relayauth 自助客户端

**Files:**
- Create: `internal/relayauth/client.go`
- Test: `internal/relayauth/client_test.go`

- [ ] **Step 1: 写失败测试**

新建 `internal/relayauth/client_test.go`:

```go
package relayauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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
		c := newStubRelay(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"error":"` + tc.code + `"}`))
		})
		_, err := c.Login("alice", "supersecret")
		if err == nil {
			t.Fatalf("code %s: expected error", tc.code)
		}
		var apiErr *APIError
		if !asAPIError(err, &apiErr) {
			t.Fatalf("code %s: expected *APIError, got %T", tc.code, err)
		}
		if apiErr.Code != tc.code || apiErr.Message == "" {
			t.Errorf("code %s: apiErr = %+v", tc.code, apiErr)
		}
	}
}
```

(The test references helpers `decodeBody` and `asAPIError` — define them in the test file too:)

```go
import (
	"encoding/json"
	"errors"
	"net/http"
)

func decodeBody(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func asAPIError(err error, target **APIError) bool {
	return errors.As(err, target)
}
```

(Merge those imports into the test file's single import block.)

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/relayauth/ -v`
Expected: 编译失败 —— `Client`/`APIError`/`Session`/`Device` 未定义。

- [ ] **Step 3: 实现 client.go**

新建 `internal/relayauth/client.go`:

```go
// Package relayauth is a small client for the relay's self-service account
// endpoints: login, register, and registering this machine as a device.
package relayauth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client talks to a relay's /v1/auth/* and /v1/devices endpoints.
type Client struct {
	RelayURL   string
	HTTPClient *http.Client // defaults to a 10s-timeout client
}

// Session is the result of a login/register.
type Session struct {
	Token     string
	AccountID string
	Username  string
	ExpiresAt time.Time
}

// Device is the result of registering this machine.
type Device struct {
	ID    string
	Token string
	Label string
	Role  string
}

// APIError carries the relay's error code plus a human-readable message.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string { return e.Message }

var errorMessages = map[string]string{
	"bad_json":              "请求格式错误",
	"username_invalid":      "用户名需为 3-32 个字符",
	"password_too_short":    "密码至少 8 个字符",
	"password_too_long":     "密码过长",
	"username_taken":        "用户名已被占用",
	"registration_disabled": "该服务未开放注册",
	"invalid_invite":        "邀请码无效",
	"invalid_credentials":   "用户名或密码错误",
	"invalid_session":       "登录已过期,请重新登录",
	"rate_limited":          "尝试过于频繁,请稍后再试",
	"account_not_found":     "账号不存在",
}

func messageFor(code string) string {
	if m, ok := errorMessages[code]; ok {
		return m
	}
	if code == "" {
		return "请求失败"
	}
	return "请求失败 (" + code + ")"
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// do POSTs JSON to path (with optional bearer) and decodes a 2xx body into out.
func (c *Client) do(path, bearer string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", c.RelayURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil {
			return nil
		}
		return json.NewDecoder(resp.Body).Decode(out)
	}
	var e struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&e)
	return &APIError{Status: resp.StatusCode, Code: e.Error, Message: messageFor(e.Error)}
}

type sessionResp struct {
	SessionToken string    `json:"session_token"`
	AccountID    string    `json:"account_id"`
	Username     string    `json:"username"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (c *Client) Login(username, password string) (Session, error) {
	var r sessionResp
	err := c.do("/v1/auth/login", "", map[string]string{
		"username": username, "password": password,
	}, &r)
	if err != nil {
		return Session{}, err
	}
	return Session{Token: r.SessionToken, AccountID: r.AccountID, Username: r.Username, ExpiresAt: r.ExpiresAt}, nil
}

func (c *Client) Register(username, password, inviteCode string) (Session, error) {
	var r sessionResp
	err := c.do("/v1/auth/register", "", map[string]string{
		"username": username, "password": password, "invite_code": inviteCode,
	}, &r)
	if err != nil {
		return Session{}, err
	}
	return Session{Token: r.SessionToken, AccountID: r.AccountID, Username: r.Username, ExpiresAt: r.ExpiresAt}, nil
}

// RegisterDevice registers this machine under the session's account.
func (c *Client) RegisterDevice(session, label, role string) (Device, error) {
	body := map[string]string{"label": label}
	if role != "" {
		body["role"] = role
	}
	var r struct {
		DeviceID    string `json:"device_id"`
		DeviceToken string `json:"device_token"`
		Label       string `json:"label"`
		Role        string `json:"role"`
	}
	if err := c.do("/v1/devices", session, body, &r); err != nil {
		return Device{}, err
	}
	if r.DeviceID == "" || r.DeviceToken == "" {
		return Device{}, fmt.Errorf("relay returned empty device id/token")
	}
	return Device{ID: r.DeviceID, Token: r.DeviceToken, Label: r.Label, Role: r.Role}, nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/relayauth/ -v`
Expected: PASS(全部 4 个测试)。

- [ ] **Step 5: 提交**

```bash
git add internal/relayauth/
git commit -m "feat(receiver/relayauth): self-service login/register/register-device client" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: config 助手(IsRelayConfigured + ReadFile)

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`(追加)

- [ ] **Step 1: 写失败测试**

追加到 `internal/config/config_test.go`(确保该文件 import 了 `os`、`path/filepath`、`testing`;若缺则补):

```go
func TestIsRelayConfigured(t *testing.T) {
	full := &Config{Mode: ModeRelaySubscriber, RelayURL: "u", DeviceID: "d", DeviceToken: "t"}
	if !full.IsRelayConfigured() {
		t.Error("complete relay config should be considered configured")
	}
	notConfigured := []*Config{
		{Mode: ModeListener, RelayURL: "u", DeviceID: "d", DeviceToken: "t"},
		{Mode: ModeRelaySubscriber, DeviceID: "d", DeviceToken: "t"},
		{Mode: ModeRelaySubscriber, RelayURL: "u", DeviceToken: "t"},
		{Mode: ModeRelaySubscriber, RelayURL: "u", DeviceID: "d"},
	}
	for i, c := range notConfigured {
		if c.IsRelayConfigured() {
			t.Errorf("case %d should not be configured: %+v", i, c)
		}
	}
}

func TestReadFileMissingReturnsListenerDefaults(t *testing.T) {
	cfg, err := ReadFile(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if cfg.Mode != ModeListener {
		t.Errorf("mode = %q, want listener", cfg.Mode)
	}
}

func TestReadFileParsesRelayConfig(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	data := `{"mode":"relay-subscriber","relay_url":"https://r","device_id":"dev-1","device_token":"tok"}`
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !cfg.IsRelayConfigured() || cfg.DeviceID != "dev-1" {
		t.Errorf("cfg = %+v", cfg)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/config/ -run 'TestIsRelayConfigured|TestReadFile' -v`
Expected: 编译失败 —— `IsRelayConfigured`/`ReadFile` 未定义。

- [ ] **Step 3: 实现助手**

`internal/config/config.go`,文件末尾追加:

```go
// IsRelayConfigured reports whether the config is a complete relay-subscriber
// setup ready to start a Subscriber without prompting for login.
func (c *Config) IsRelayConfigured() bool {
	return c.Mode == ModeRelaySubscriber &&
		c.RelayURL != "" && c.DeviceID != "" && c.DeviceToken != ""
}

// ReadFile loads config from path WITHOUT enforcing relay-mode completeness or
// generating a listener token (unlike Load). A missing file returns
// listener-mode defaults. Used by the GUI, which decides login-vs-resume itself.
func ReadFile(path string) (*Config, error) {
	cfg := &Config{
		Mode:     ModeListener,
		Port:     DefaultPort,
		BindAddr: DefaultBindAddr,
		Name:     hostname(),
	}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}
```

(`os`、`encoding/json` 已在 `config.go` 导入;`hostname()` 已存在。)

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/config/ -v`
Expected: PASS(新老测试全绿)。

- [ ] **Step 5: 提交**

```bash
git add internal/config/
git commit -m "feat(receiver/config): IsRelayConfigured + lenient ReadFile" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Subscriber 状态回调(OnStatus)

**Files:**
- Modify: `internal/relay/subscriber.go`
- Test: `internal/relay/subscriber_test.go`(追加)

- [ ] **Step 1: 写失败测试**

追加到 `internal/relay/subscriber_test.go`(其 import 已含 `context`、`net/http`、`net/http/httptest`、`sync`、`testing`、`time`):

```go
func TestSubscriberOnStatusLifecycle(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	defer ts.Close()

	var mu sync.Mutex
	var got []Status
	s := &Subscriber{
		RelayURL: ts.URL, DeviceToken: "tok", Injector: &mockInjector{},
		HTTPClient: &http.Client{},
		OnStatus: func(st Status, err error) {
			mu.Lock()
			got = append(got, st)
			mu.Unlock()
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = s.Run(ctx)

	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 || got[0] != StatusConnecting {
		t.Fatalf("statuses = %+v, want first=connecting", got)
	}
	connected := false
	for _, st := range got {
		if st == StatusConnected {
			connected = true
		}
	}
	if !connected {
		t.Errorf("expected a connected status; got %+v", got)
	}
}

func TestSubscriberOnStatusErrorOn401(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer ts.Close()
	var mu sync.Mutex
	var got []Status
	s := &Subscriber{
		RelayURL: ts.URL, DeviceToken: "bad", Injector: &mockInjector{},
		HTTPClient: &http.Client{},
		OnStatus: func(st Status, err error) {
			mu.Lock()
			got = append(got, st)
			mu.Unlock()
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.Run(ctx)
	mu.Lock()
	defer mu.Unlock()
	sawError := false
	for _, st := range got {
		if st == StatusError {
			sawError = true
		}
	}
	if !sawError {
		t.Errorf("expected error status on 401; got %+v", got)
	}
}
```

(Existing tests don't set `OnStatus` — they implicitly verify the nil-callback path doesn't panic.)

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/relay/ -run TestSubscriberOnStatus -v`
Expected: 编译失败 —— `Status`/`StatusConnecting`/... 与 `OnStatus` 字段未定义。

- [ ] **Step 3: 加 Status 类型 + OnStatus 字段 + emit**

`internal/relay/subscriber.go`。在 `type Subscriber struct {...}` 内、`ReconnectMin` 字段之后追加一个字段:

```go
	// OnStatus, if non-nil, is called on connection lifecycle changes (for UI).
	OnStatus func(Status, error)
```

在 `type Subscriber struct` 定义之后(文件中部),新增类型与常量:

```go
// Status reports the subscriber's connection lifecycle for UI display.
type Status string

const (
	StatusConnecting   Status = "connecting"
	StatusConnected    Status = "connected"
	StatusReconnecting Status = "reconnecting"
	StatusError        Status = "error"
)

func (s *Subscriber) emit(st Status, err error) {
	if s.OnStatus != nil {
		s.OnStatus(st, err)
	}
}
```

- [ ] **Step 4: 在 Run / connectAndStream 里发状态(不改重连逻辑)**

`internal/relay/subscriber.go` 的 `Run`。把 for 循环体改为(只新增 emit 调用,其余原样):

```go
	for {
		if ctx.Err() != nil {
			return nil
		}
		s.emit(StatusConnecting, nil)
		connStart := time.Now()
		err := s.connectAndStream(ctx, &lastEventID)
		if errors.Is(err, errAuth) {
			s.emit(StatusError, err)
			return fmt.Errorf("subscribe: %w", err)
		}
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(connStart) >= 10*time.Second {
			backoff = min
		}
		s.emit(StatusReconnecting, err)
		log.Printf("subscribe: %v; reconnecting in %s", err, backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
```

在 `connectAndStream` 里,紧跟现有 `log.Printf("connected to %s ...")` 之后插入一行:

```go
	s.emit(StatusConnected, nil)
```

- [ ] **Step 5: 跑测试确认通过 + 全包回归 + windows 交叉编译**

Run: `go test ./internal/relay/ -v`
Expected: PASS(含新测试,旧测试不受影响)。

Run: `go test ./...`
Expected: ok。

Run: `CGO_ENABLED=0 GOOS=windows go build -o /dev/null ./cmd/type4me-receiver && echo WIN_OK`
Expected: `WIN_OK`(确认纯 Go 路径仍可交叉编译)。

- [ ] **Step 6: 提交**

```bash
git add internal/relay/subscriber.go internal/relay/subscriber_test.go
git commit -m "feat(receiver/relay): optional Subscriber.OnStatus lifecycle callback" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: appflow 编排控制器

**Files:**
- Create: `internal/appflow/controller.go`
- Test: `internal/appflow/controller_test.go`

- [ ] **Step 1: 写失败测试**

新建 `internal/appflow/controller_test.go`:

```go
package appflow

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/qiyadeng/type4me/receiver/internal/config"
	"github.com/qiyadeng/type4me/receiver/internal/relayauth"
)

type fakeAuth struct {
	session                            relayauth.Session
	device                             relayauth.Device
	loginErr, registerErr, regDevErr   error
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
	if !c.ResumeIfConfigured() || !started {
		t.Errorf("configured: resume=%v started=%v", c.ResumeIfConfigured(), started)
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
	if _, statErr := config.ReadFile(p); statErr != nil {
		t.Errorf("ReadFile: %v", statErr)
	}
	// nothing written: a fresh ReadFile yields listener defaults
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
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/appflow/ -v`
Expected: 编译失败 —— `Controller` 等未定义。

- [ ] **Step 3: 实现 controller.go**

新建 `internal/appflow/controller.go`:

```go
// Package appflow orchestrates the GUI login flow without any UI dependency,
// so it can be unit-tested with fakes: login/register -> auto-register this
// device -> persist relay config -> start the subscriber.
package appflow

import (
	"github.com/qiyadeng/type4me/receiver/internal/config"
	"github.com/qiyadeng/type4me/receiver/internal/relayauth"
)

// AuthClient is the subset of relayauth.Client the controller needs.
type AuthClient interface {
	Login(username, password string) (relayauth.Session, error)
	Register(username, password, inviteCode string) (relayauth.Session, error)
	RegisterDevice(session, label, role string) (relayauth.Device, error)
}

// Controller drives the login/resume/logout flow.
type Controller struct {
	Cfg      *config.Config
	CfgPath  string
	Auth     AuthClient
	Hostname string // device label on first registration
	RelayURL string // build-baked relay URL written into config
	StartSub func(cfg *config.Config)
}

// ResumeIfConfigured starts the subscriber and returns true if the config is
// already a complete relay setup (no login needed); otherwise returns false.
func (c *Controller) ResumeIfConfigured() bool {
	if c.Cfg.IsRelayConfigured() {
		c.StartSub(c.Cfg)
		return true
	}
	return false
}

// LoginAndStart logs in (or registers), auto-registers this device if needed,
// persists the relay config, and starts the subscriber.
func (c *Controller) LoginAndStart(username, password, inviteCode string, register bool) error {
	var (
		sess relayauth.Session
		err  error
	)
	if register {
		sess, err = c.Auth.Register(username, password, inviteCode)
	} else {
		sess, err = c.Auth.Login(username, password)
	}
	if err != nil {
		return err
	}

	registeredNow := false
	if c.Cfg.DeviceToken == "" {
		dev, derr := c.Auth.RegisterDevice(sess.Token, c.Hostname, "either")
		if derr != nil {
			return derr
		}
		c.Cfg.DeviceID = dev.ID
		c.Cfg.DeviceToken = dev.Token
		registeredNow = true
	}

	c.Cfg.Mode = config.ModeRelaySubscriber
	c.Cfg.RelayURL = c.RelayURL
	if err := c.Cfg.Save(c.CfgPath); err != nil {
		if registeredNow {
			c.Cfg.DeviceID = ""
			c.Cfg.DeviceToken = ""
		}
		return err
	}

	c.StartSub(c.Cfg)
	return nil
}

// Logout clears the device credentials and persists, so the next launch shows
// the login window. Stopping the running subscriber is the GUI's responsibility
// (it cancels the subscriber's context).
func (c *Controller) Logout() error {
	c.Cfg.DeviceID = ""
	c.Cfg.DeviceToken = ""
	return c.Cfg.Save(c.CfgPath)
}
```

- [ ] **Step 4: 跑测试确认通过 + 全包回归**

Run: `go test ./internal/appflow/ -v`
Expected: PASS(全部 7 个测试)。

Run: `go test ./... && CGO_ENABLED=0 GOOS=windows go build -o /dev/null ./cmd/type4me-receiver && echo OK`
Expected: 末行 `OK`。

- [ ] **Step 5: 提交**

```bash
git add internal/appflow/
git commit -m "feat(receiver/appflow): login->register-device->save->subscribe controller" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Fyne GUI 二进制 + 托盘 + 构建目标(`-tags gui`)

> 这是 GUI 接线任务:全部业务逻辑都在 Task 1–4,这里只做 Fyne 绑定 + 系统托盘。**自动验证 = 在 macOS 上 `go build -tags gui` 编译通过**;完整运行(登录窗、托盘、订阅)在 **Windows 上手动验证**(交叉编译 CGO Fyne 需 Windows 工具链或 fyne-cross)。

**Files:**
- Create: `cmd/type4me-receiver-gui/build.go`
- Create: `cmd/type4me-receiver-gui/main.go`
- Modify: `Makefile`

- [ ] **Step 1: 加 Fyne 依赖**

Run: `go get fyne.io/fyne/v2@latest`
Expected: `go.mod`/`go.sum` 新增 `fyne.io/fyne/v2`。(此命令会下载 Fyne 到 module cache;不影响其它包的测试,因为它们不 import fyne。)

- [ ] **Step 2: build.go(固化常量,tag gui)**

新建 `cmd/type4me-receiver-gui/build.go`:

```go
//go:build gui

package main

// Build-time overridable values for the GUI binary. Override the relay URL with:
//   go build -tags gui -ldflags "-X main.defaultRelayURL=https://your-relay"
var (
	version         = "dev"
	defaultRelayURL = "https://relay.example.com"
)
```

- [ ] **Step 3: main.go(Fyne app + 登录窗 + 托盘,tag gui)**

新建 `cmd/type4me-receiver-gui/main.go`:

```go
//go:build gui

package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"github.com/qiyadeng/type4me/receiver/internal/appflow"
	"github.com/qiyadeng/type4me/receiver/internal/config"
	"github.com/qiyadeng/type4me/receiver/internal/inject"
	"github.com/qiyadeng/type4me/receiver/internal/relay"
	"github.com/qiyadeng/type4me/receiver/internal/relayauth"
)

func main() {
	cfgPath := configPath()
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0o700)

	cfg, err := config.ReadFile(cfgPath)
	if err != nil {
		cfg = &config.Config{Mode: config.ModeListener}
	}

	a := app.NewWithID("com.type4me.receiver")
	win := a.NewWindow("Type4Me 登录")

	statusItem := fyne.NewMenuItem("未连接", nil)
	setStatus := func(text string) {
		statusItem.Label = text
		fyne.Do(func() {}) // hop to UI thread so the tray refreshes
	}

	inj := inject.NewPlatform()
	var cancelSub context.CancelFunc

	startSub := func(c *config.Config) {
		ctx, cancel := context.WithCancel(context.Background())
		cancelSub = cancel
		sub := &relay.Subscriber{
			RelayURL:    c.RelayURL,
			DeviceToken: c.DeviceToken,
			Injector:    inj,
			HTTPClient:  &http.Client{Timeout: 0}, // SSE long-poll
			OnStatus: func(st relay.Status, _ error) {
				switch st {
				case relay.StatusConnecting:
					setStatus("连接中…")
				case relay.StatusConnected:
					setStatus("已连接")
				case relay.StatusReconnecting:
					setStatus("重连中…")
				case relay.StatusError:
					setStatus("连接失败")
				}
			},
		}
		go func() { _ = sub.Run(ctx) }()
	}

	ctrl := &appflow.Controller{
		Cfg:      cfg,
		CfgPath:  cfgPath,
		Auth:     &relayauth.Client{RelayURL: defaultRelayURL},
		Hostname: hostname(),
		RelayURL: defaultRelayURL,
		StartSub: startSub,
	}

	buildLoginForm(win, ctrl)

	if desk, ok := a.(desktop.App); ok {
		logoutItem := fyne.NewMenuItem("退出登录", func() {
			if cancelSub != nil {
				cancelSub()
			}
			_ = ctrl.Logout()
			setStatus("未连接")
			win.Show()
		})
		quitItem := fyne.NewMenuItem("退出", func() { a.Quit() })
		desk.SetSystemTrayMenu(fyne.NewMenu("Type4Me", statusItem, logoutItem, quitItem))
	}

	if ctrl.ResumeIfConfigured() {
		win.Hide() // already configured: live in the tray
	} else {
		win.Show()
	}
	a.Run()
}

// buildLoginForm wires the username/password/(invite) form to the controller.
func buildLoginForm(win fyne.Window, ctrl *appflow.Controller) {
	username := widget.NewEntry()
	username.SetPlaceHolder("用户名")
	password := widget.NewPasswordEntry()
	password.SetPlaceHolder("密码")
	invite := widget.NewEntry()
	invite.SetPlaceHolder("邀请码(仅注册时需要)")
	invite.Hide()

	registerMode := false
	errLabel := widget.NewLabel("")
	errLabel.Wrapping = fyne.TextWrapWord

	submit := widget.NewButton("登录", nil)
	toggle := widget.NewButton("没有账号?去注册", nil)

	applyMode := func() {
		if registerMode {
			invite.Show()
			submit.SetText("注册")
			toggle.SetText("已有账号?去登录")
		} else {
			invite.Hide()
			submit.SetText("登录")
			toggle.SetText("没有账号?去注册")
		}
	}
	toggle.OnTapped = func() {
		registerMode = !registerMode
		applyMode()
	}
	submit.OnTapped = func() {
		errLabel.SetText("")
		submit.Disable()
		go func() {
			err := ctrl.LoginAndStart(username.Text, password.Text, invite.Text, registerMode)
			fyne.Do(func() {
				submit.Enable()
				if err != nil {
					errLabel.SetText(err.Error())
					return
				}
				win.Hide()
			})
		}()
	}
	applyMode()

	win.SetContent(container.NewVBox(username, password, invite, submit, toggle, errLabel))
	win.Resize(fyne.NewSize(320, 240))
	win.SetCloseIntercept(func() { win.Hide() }) // closing the window keeps the tray alive
}

func hostname() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "type4me-receiver"
}

func configPath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "type4me-receiver", "config.json")
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "type4me-receiver", "config.json")
	default:
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "type4me-receiver", "config.json")
	}
}
```

> 注:`fyne.Do(...)` 用于把 UI 更新切回主线程,Fyne v2.4+ 提供。若安装的 Fyne 版本 API 名不同(如 `fyne.DoAndWait` 或旧版需 `widget.Refresh`),按实际版本微调——这是本任务唯一可能需要按版本适配的点。

- [ ] **Step 4: 默认标签下不破坏既有流程(关键回归)**

Run: `go test ./...`
Expected: ok —— gui 目录因 `//go:build gui` 被跳过,Fyne 不被编译;Task1–4 的包正常测试。

Run: `CGO_ENABLED=0 GOOS=windows go vet ./... && CGO_ENABLED=0 GOOS=windows go build -o /dev/null ./cmd/type4me-receiver && echo WIN_CLI_OK`
Expected: `WIN_CLI_OK`(纯 Go 的 windows 交叉编译/vet 不受 Fyne 影响)。

- [ ] **Step 5: GUI 编译验证(macOS,CGO + Fyne)**

Run: `go build -tags gui -o /tmp/t4m-gui ./cmd/type4me-receiver-gui && echo GUI_BUILD_OK`
Expected: `GUI_BUILD_OK`(首次会下载/编译 Fyne,耗时较长;需 macOS Xcode CLT)。
若因 Fyne 版本 API 差异报错(如 `fyne.Do` 未定义),按 Step 3 注释适配后重试。

- [ ] **Step 6: Makefile 加 windows GUI 目标**

`Makefile`,在 `.PHONY` 行追加 `build-windows-gui`,并在 `VERSION ?= 0.1.0` 附近加一行 `RELAY_URL ?= https://relay.example.com`,然后在 `build-windows:` 目标之后追加:

```makefile
# Windows GUI (Fyne + tray). Requires CGO + a Windows C toolchain — typically
# built ON Windows, or via fyne-cross. -H windowsgui suppresses the console.
build-windows-gui:
	mkdir -p $(DIST)
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 \
		go build -tags gui \
		-ldflags "-H windowsgui -X main.version=$(VERSION) -X main.defaultRelayURL=$(RELAY_URL)" \
		-o $(DIST)/type4me-receiver-gui-windows-amd64.exe ./cmd/type4me-receiver-gui
```

(不运行此目标——交叉编译 CGO Fyne 需 Windows 工具链;仅提供构建入口。)

- [ ] **Step 7: 提交**

```bash
git add cmd/type4me-receiver-gui/ Makefile go.mod go.sum
git commit -m "feat(receiver/gui): Fyne login window + system tray (gui build tag)" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 8: Windows 手动验证清单(记录,不在 CI)**

在 Windows 上构建并运行后,人工确认:
1. 首次启动弹登录窗;输错密码 → 顶部红字「用户名或密码错误」,窗口保留。
2. 正确登录(或注册带邀请码)→ 窗口隐藏,托盘出现,状态转「连接中…」→「已连接」。
3. 从 Mac 端发文字 → Windows 前台收到注入。
4. 重启程序 → 免登录,直接「已连接」(凭据已存 `%APPDATA%\type4me-receiver\config.json`)。
5. 托盘「退出登录」→ 停止订阅、清凭据、重新弹登录窗。
6. 托盘「退出」→ 进程结束。

---

## 收尾验证

- [ ] **全量纯 Go 测试**

Run(receiver/): `go test ./... && CGO_ENABLED=0 GOOS=windows go vet ./... && echo ALL_OK`
Expected: 末行 `ALL_OK`。

- [ ] **GUI 仍可编译**

Run(receiver/,macOS): `go build -tags gui -o /tmp/t4m-gui ./cmd/type4me-receiver-gui && echo GUI_OK`
Expected: `GUI_OK`。

---

## 自查记录(spec 覆盖)

- relayauth 客户端(login/register/register-device + 错误映射)→ Task 1。
- relay 地址 build 固化(`defaultRelayURL`,ldflags 可覆盖)→ Task 5(build.go)。
- `config.IsRelayConfigured` + 宽松读 `ReadFile`(GUI 自行判定起订阅/弹登录)→ Task 2。
- `Subscriber.OnStatus` 状态回调(连接/重连/错误,nil 安全)→ Task 3。
- appflow 编排(登录→自动登记设备→存 relay 配置→起订阅;已有 token 跳过登记;注册模式;失败兜底回退)→ Task 4。
- device_token 存 config.json(沿用 `Save`,APPDATA per-user);会话 token 仅内存(GUI 不持久化 session)→ Task 4 + Task 5。
- Fyne 登录窗(用户名/密码遮罩/邀请码 + 登录↔注册切换 + 错误提示)+ 系统托盘(状态/退出登录/退出)→ Task 5。
- 启动判定:已配置免登录直连,否则弹登录 → Task 5(`ResumeIfConfigured`)。
- 原 CLI 二进制不变;windows 交叉编译不受 Fyne 影响 → `//go:build gui` 隔离 + Task 3/4/5 回归步骤。
- Windows 不需要 `GET /v1/devices`(只收不选目标)→ relayauth 故意不实现该方法。
- GUI 技术选型 Fyne → Task 5。
- 凭据进 Windows 凭据库(DPAPI)= 后续升级,本期 YAGNI(config.json 0600 语义)→ 一致。

**与 spec 的有意偏差(记录,非遗漏):**
- spec 第 6 节提到「注入器 `Ping()` 失败则托盘报错」。本计划未在 GUI 启动时显式 Ping —— Windows 上 `SendInput` 注入器基本恒可用,且订阅期的 inject 失败已在 `subscriber.go` 里 log。若实现期想严格对齐 spec,可在 `startSub` 起订阅前加 `inj.Ping()` 检查并经 `setStatus` 报错;列为 Task 5 的可选增强,不阻塞。
