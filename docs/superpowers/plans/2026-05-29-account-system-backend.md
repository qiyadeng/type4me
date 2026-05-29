# 账号系统 · 第 1 期 后端(relay 自助账号层)实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给 relay 加一个面向终端用户的自助账号层——用户名/密码注册登录、邀请码、HMAC 会话 token、`requireSession` 中间件、按 IP 限流,以及会话鉴权的 `POST/GET /v1/devices`,让客户端无需 admin token 即可登记本机并列出设备。

**Architecture:** 沿用现有两层:`internal/hub`(状态/逻辑,持有锁与 `state.json`)与 `internal/server`(HTTP/鉴权/限流)。账号信息加到现有 `Account`,`state.json` 升到 v2 并向后兼容;会话 token 用自包含的 HMAC 签名串,无需新存储。现有 `/v1/{dispatch,subscribe,ping}` 与 `/v1/admin/*` 完全不动。

**Tech Stack:** Go 1.25,stdlib(`crypto/hmac`、`crypto/sha256`、`crypto/rand`、`encoding/base64`、`net/http`),`golang.org/x/crypto/bcrypt`(已用)。测试用 stdlib `testing` + `net/http/httptest`。

**上游 spec:** `docs/superpowers/specs/2026-05-29-account-system-backend-design.md`

**所有命令在 `relay/` 目录下运行**(`cd relay`),除非另行说明。每步 `go test ./...` 绿后再进下一步。

---

## 文件结构

| 文件 | 责任 | 本计划 |
|---|---|---|
| `internal/hub/types.go` | 数据结构 | 改:`Account` += 字段 |
| `internal/hub/errors.go` | hub 错误 | 改:+4 错误 |
| `internal/hub/state.go` | 持久化 | 改:版本 → 2 |
| `internal/hub/token.go` | bcrypt/缓存 | 改:+ 密码别名 |
| `internal/hub/hub.go` | 账号/设备逻辑 | 改:+ 用户名索引与 4 方法 |
| `internal/server/session.go` | 会话签名 | **新增** |
| `internal/server/ratelimit.go` | 限流 | **新增** |
| `internal/server/auth.go` | 鉴权中间件 | 改:+ `requireSession` |
| `internal/server/handlers_auth.go` | register/login | **新增** |
| `internal/server/handlers_devices.go` | `/v1/devices` | **新增** |
| `internal/server/server.go` | 装配 | 改:Options + 路由 |
| `cmd/type4me-relay/main.go` | 启动/env | 改:+2 env |
| `deploy/env.example` | 文档 | 改:+2 env(如不存在则建) |
| `scripts/test_relay_account_e2e.sh`(仓库根) | 端到端 | **新增** |

---

## Task 1: Account 新字段 + state v2 向后兼容 + 用户名索引

**Files:**
- Modify: `internal/hub/types.go`
- Modify: `internal/hub/state.go`
- Modify: `internal/hub/hub.go`
- Test: `internal/hub/account_test.go`(新增)

- [ ] **Step 1: 写失败测试 —— v1 旧状态可加载且索引跳过空用户名**

新建 `internal/hub/account_test.go`:

```go
package hub

import (
	"os"
	"path/filepath"
	"testing"
)

// 模拟一份 v1 state.json:account 无 username/password_hash 字段。
func TestLoadLegacyV1StateNoUsername(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	legacy := `{"version":1,"accounts":[{"id":"acct-legacy","name":"Old","created_at":"2024-01-01T00:00:00Z"}],"devices":[]}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(h.ListAccounts()) != 1 {
		t.Fatalf("legacy account not loaded")
	}
	// 空用户名不应进入用户名索引
	if len(h.usernames) != 0 {
		t.Errorf("usernames index = %d, want 0 (legacy account has no username)", len(h.usernames))
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/hub/ -run TestLoadLegacyV1StateNoUsername -v`
Expected: 编译失败 —— `h.usernames undefined`。

- [ ] **Step 3: 给 Account 加字段**

`internal/hub/types.go`,把 `Account` 改为:

```go
type Account struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Username     string    `json:"username,omitempty"`
	PasswordHash string    `json:"password_hash,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}
```

- [ ] **Step 4: state 版本升到 2**

`internal/hub/state.go`,把 `const stateVersion = 1` 改为 `const stateVersion = 2`。(`loadState` 现有逻辑已能读旧文件:它按字段反序列化,缺失的 `username`/`password_hash` 留空;`version` 字段不参与校验,仅在写回时用 `stateVersion`。)

- [ ] **Step 5: Hub 加用户名索引并在 New 重建**

`internal/hub/hub.go`。给 `Hub` struct 增加字段(放在 `cache` 后):

```go
type Hub struct {
	mu        sync.RWMutex
	statePath string
	accounts  map[string]*Account
	devices   map[string]*Device
	subs      map[string]chan *Message
	cache     *tokenCache
	usernames map[string]string // lower(username) -> accountID
}
```

在 `New` 里初始化并在加载账号时重建索引。把现有初始化与循环改为:

```go
	h := &Hub{
		statePath: statePath,
		accounts:  map[string]*Account{},
		devices:   map[string]*Device{},
		subs:      map[string]chan *Message{},
		cache:     newTokenCache(),
		usernames: map[string]string{},
	}
	for _, a := range st.Accounts {
		h.accounts[a.ID] = a
		if a.Username != "" {
			h.usernames[strings.ToLower(a.Username)] = a.ID
		}
	}
```

(`strings` 已在 `hub.go` 导入。)

- [ ] **Step 6: 跑测试确认通过**

Run: `go test ./internal/hub/ -run TestLoadLegacyV1StateNoUsername -v`
Expected: PASS

- [ ] **Step 7: 全 hub 包回归**

Run: `go test ./internal/hub/`
Expected: ok(现有测试不受影响)

- [ ] **Step 8: 提交**

```bash
git add internal/hub/types.go internal/hub/state.go internal/hub/hub.go internal/hub/account_test.go
git commit -m "feat(relay/hub): Account username/password fields + state v2 + username index"
```

---

## Task 2: 密码别名 + 账号逻辑(注册/登录/列表/在线)+ 错误

**Files:**
- Modify: `internal/hub/errors.go`
- Modify: `internal/hub/token.go`
- Modify: `internal/hub/hub.go`
- Test: `internal/hub/account_test.go`(续)

- [ ] **Step 1: 写失败测试 —— 注册/登录/隔离/在线**

追加到 `internal/hub/account_test.go`:

```go
import "errors" // 与文件顶部已有 import 合并

func TestRegisterUserAndAuthenticate(t *testing.T) {
	h := newTestHub(t)
	acc, err := h.RegisterUser("Alice", "supersecret")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if acc.Username != "Alice" || acc.PasswordHash == "" {
		t.Fatalf("bad account: %+v", acc)
	}
	got, err := h.Authenticate("Alice", "supersecret")
	if err != nil || got.ID != acc.ID {
		t.Fatalf("authenticate: got %+v err %v", got, err)
	}
	if _, err := h.Authenticate("Alice", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password: got %v", err)
	}
	if _, err := h.Authenticate("ghost", "whatever"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("unknown user: got %v", err)
	}
}

func TestRegisterUserValidation(t *testing.T) {
	h := newTestHub(t)
	if _, err := h.RegisterUser("ab", "supersecret"); !errors.Is(err, ErrUsernameRequired) {
		t.Errorf("short username: got %v", err)
	}
	if _, err := h.RegisterUser("alice", "short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Errorf("short password: got %v", err)
	}
	if _, err := h.RegisterUser("Alice", "supersecret"); err != nil {
		t.Fatal(err)
	}
	// 大小写不敏感唯一
	if _, err := h.RegisterUser("alice", "supersecret"); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("dup username: got %v", err)
	}
}

func TestListDevicesByAccountIsolation(t *testing.T) {
	h := newTestHub(t)
	a1, _ := h.RegisterUser("alice", "supersecret")
	a2, _ := h.RegisterUser("bobby", "supersecret")
	_, _, _ = h.AddDevice(a1.ID, "Mac", RoleEither)
	_, _, _ = h.AddDevice(a1.ID, "Win", RoleEither)
	_, _, _ = h.AddDevice(a2.ID, "Other", RoleEither)
	if got := len(h.ListDevicesByAccount(a1.ID)); got != 2 {
		t.Errorf("a1 devices = %d, want 2", got)
	}
	if got := len(h.ListDevicesByAccount(a2.ID)); got != 1 {
		t.Errorf("a2 devices = %d, want 1", got)
	}
}

func TestIsOnline(t *testing.T) {
	h := newTestHub(t)
	a, _ := h.RegisterUser("alice", "supersecret")
	dev, _, _ := h.AddDevice(a.ID, "Win", RoleEither)
	if h.IsOnline(dev.ID) {
		t.Error("should be offline before subscribe")
	}
	_, unsub := h.Subscribe(dev.ID)
	defer unsub()
	if !h.IsOnline(dev.ID) {
		t.Error("should be online after subscribe")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/hub/ -run 'TestRegisterUser|TestListDevicesByAccount|TestIsOnline' -v`
Expected: 编译失败 —— `RegisterUser`/`Authenticate`/`ListDevicesByAccount`/`IsOnline`/错误未定义。

- [ ] **Step 3: 加 4 个错误**

`internal/hub/errors.go`,在 `var (...)` 块内追加:

```go
	ErrUsernameRequired   = errors.New("username required")
	ErrPasswordTooShort   = errors.New("password too short")
	ErrUsernameTaken      = errors.New("username already taken")
	ErrInvalidCredentials = errors.New("invalid credentials")
```

- [ ] **Step 4: 密码别名**

`internal/hub/token.go`,文件末尾追加:

```go
// hashPassword / verifyPassword 是密码场景下 bcrypt 的语义别名。
func hashPassword(password string) (string, error) { return hashToken(password) }
func verifyPassword(password, hash string) bool     { return verifyToken(password, hash) }
```

- [ ] **Step 5: 实现 4 个方法**

`internal/hub/hub.go`,文件末尾追加。先加常量,再加方法:

```go
const (
	minUsernameLen = 3
	maxUsernameLen = 32
	minPasswordLen = 8
)

// RegisterUser 创建一个带用户名/密码的账号。
func (h *Hub) RegisterUser(username, password string) (*Account, error) {
	username = strings.TrimSpace(username)
	if len(username) < minUsernameLen || len(username) > maxUsernameLen {
		return nil, ErrUsernameRequired
	}
	if len(password) < minPasswordLen {
		return nil, ErrPasswordTooShort
	}
	key := strings.ToLower(username)
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.usernames[key]; exists {
		return nil, ErrUsernameTaken
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	acc := &Account{
		ID:           "acct-" + shortID(),
		Name:         username,
		Username:     username,
		PasswordHash: hash,
		CreatedAt:    time.Now().UTC(),
	}
	h.accounts[acc.ID] = acc
	h.usernames[key] = acc.ID
	if err := h.persistLocked(); err != nil {
		delete(h.accounts, acc.ID)
		delete(h.usernames, key)
		return nil, err
	}
	return acc, nil
}

// Authenticate 按用户名校验密码;失败统一返回 ErrInvalidCredentials(防枚举)。
func (h *Hub) Authenticate(username, password string) (*Account, error) {
	key := strings.ToLower(strings.TrimSpace(username))
	h.mu.RLock()
	id, ok := h.usernames[key]
	var acc *Account
	if ok {
		acc = h.accounts[id]
	}
	h.mu.RUnlock()
	if acc == nil || acc.PasswordHash == "" || !verifyPassword(password, acc.PasswordHash) {
		return nil, ErrInvalidCredentials
	}
	return acc, nil
}

// ListDevicesByAccount 返回某账号下的设备。
func (h *Hub) ListDevicesByAccount(accountID string) []*Device {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*Device, 0)
	for _, d := range h.devices {
		if d.AccountID == accountID {
			out = append(out, d)
		}
	}
	return out
}

// IsOnline 报告设备当前是否有活跃订阅。
func (h *Hub) IsOnline(deviceID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.subs[deviceID]
	return ok
}
```

- [ ] **Step 6: 跑测试确认通过**

Run: `go test ./internal/hub/ -run 'TestRegisterUser|TestListDevicesByAccount|TestIsOnline' -v`
Expected: PASS

- [ ] **Step 7: 全 hub 包回归**

Run: `go test ./internal/hub/`
Expected: ok

- [ ] **Step 8: 提交**

```bash
git add internal/hub/errors.go internal/hub/token.go internal/hub/hub.go internal/hub/account_test.go
git commit -m "feat(relay/hub): RegisterUser/Authenticate/ListDevicesByAccount/IsOnline"
```

---

## Task 3: 会话 token 签名(`server/session.go`)

**Files:**
- Create: `internal/server/session.go`
- Test: `internal/server/session_test.go`

- [ ] **Step 1: 写失败测试**

新建 `internal/server/session_test.go`:

```go
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
	// 换 key 验证应失败
	if _, err := newSessionSigner([]byte("key-b")).verify(tok, now); err == nil {
		t.Error("wrong key should fail")
	}
	// 篡改 payload 应失败
	parts := strings.SplitN(tok, ".", 2)
	bad := "AAAA" + parts[0][4:] + "." + parts[1]
	if _, err := newSessionSigner([]byte("key-a")).verify(bad, now); err == nil {
		t.Error("tampered payload should fail")
	}
	// 缺少分隔符应失败
	if _, err := newSessionSigner([]byte("key-a")).verify("nodot", now); err == nil {
		t.Error("malformed token should fail")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/server/ -run TestSession -v`
Expected: 编译失败 —— `newSessionSigner`/`sessionTTL` 未定义。

- [ ] **Step 3: 实现 session.go**

新建 `internal/server/session.go`:

```go
package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const sessionTTL = 24 * time.Hour

var errInvalidSession = errors.New("invalid session")

type sessionSigner struct {
	key []byte
}

func newSessionSigner(key []byte) sessionSigner {
	return sessionSigner{key: key}
}

type sessionClaims struct {
	AID string `json:"aid"`
	Exp int64  `json:"exp"`
}

func (s sessionSigner) mac(payload string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// sign 返回 base64url(claims) + "." + base64url(HMAC)。
func (s sessionSigner) sign(accountID string, now time.Time) string {
	claims := sessionClaims{AID: accountID, Exp: now.Add(sessionTTL).Unix()}
	raw, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + s.mac(payload)
}

// verify 校验签名与过期,返回 accountID。
func (s sessionSigner) verify(token string, now time.Time) (string, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return "", errInvalidSession
	}
	if !hmac.Equal([]byte(parts[1]), []byte(s.mac(parts[0]))) {
		return "", errInvalidSession
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", errInvalidSession
	}
	var c sessionClaims
	if err := json.Unmarshal(raw, &c); err != nil {
		return "", errInvalidSession
	}
	if now.Unix() >= c.Exp {
		return "", errInvalidSession
	}
	return c.AID, nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/server/ -run TestSession -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/server/session.go internal/server/session_test.go
git commit -m "feat(relay/server): HMAC-signed session tokens"
```

---

## Task 4: 限流(`server/ratelimit.go`)

**Files:**
- Create: `internal/server/ratelimit.go`
- Test: `internal/server/ratelimit_test.go`

- [ ] **Step 1: 写失败测试**

新建 `internal/server/ratelimit_test.go`:

```go
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
	// 另一个 IP 不受影响
	if !l.allow("5.6.7.8") {
		t.Error("different IP should be allowed")
	}
	// 窗口推进后重置
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
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/server/ -run 'TestRateLimiter|TestClientIP' -v`
Expected: 编译失败 —— `newRateLimiter`/`clientIP` 未定义。

- [ ] **Step 3: 实现 ratelimit.go**

新建 `internal/server/ratelimit.go`:

```go
package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	authRateLimit  = 10
	authRateWindow = time.Minute
)

type ipWindow struct {
	count int
	start time.Time
}

type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	hits   map[string]*ipWindow
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		limit:  limit,
		window: window,
		now:    time.Now,
		hits:   map[string]*ipWindow{},
	}
}

func (l *rateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	w, ok := l.hits[ip]
	if !ok || now.Sub(w.start) >= l.window {
		l.hits[ip] = &ipWindow{count: 1, start: now}
		return true
	}
	if w.count >= l.limit {
		return false
	}
	w.count++
	return true
}

func (l *rateLimiter) wrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(clientIP(r)) {
			writeJSON(w, 429, map[string]string{"error": "rate_limited"})
			return
		}
		next(w, r)
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/server/ -run 'TestRateLimiter|TestClientIP' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/server/ratelimit.go internal/server/ratelimit_test.go
git commit -m "feat(relay/server): per-IP fixed-window rate limiter"
```

---

## Task 5: requireSession 中间件 + auth/devices handlers + 路由装配

**Files:**
- Modify: `internal/server/auth.go`
- Create: `internal/server/handlers_auth.go`
- Create: `internal/server/handlers_devices.go`
- Modify: `internal/server/server.go`
- Test: `internal/server/handlers_auth_test.go`, `internal/server/handlers_devices_test.go`

- [ ] **Step 1: requireSession 中间件**

`internal/server/auth.go`。先在 import 块加入 `"time"`(保持原有 import,新增一行),然后文件末尾追加:

```go
type sessionHandler func(w http.ResponseWriter, r *http.Request, accountID string)

func requireSession(signer sessionSigner, next sessionHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accountID, err := signer.verify(extractBearer(r), time.Now().UTC())
		if err != nil {
			writeJSON(w, 401, map[string]string{"error": "invalid_session"})
			return
		}
		next(w, r, accountID)
	}
}
```

- [ ] **Step 2: Server 加 signer / inviteCodes / limiter 并接路由**

`internal/server/server.go`。把 `Options` 改为(追加两字段):

```go
type Options struct {
	Hub         *hub.Hub
	AdminToken  string
	Version     string
	InviteCodes []string
	SessionKey  []byte
}
```

把 `Server` 改为:

```go
type Server struct {
	opts        Options
	started     time.Time
	signer      sessionSigner
	inviteCodes map[string]struct{}
	limiter     *rateLimiter
}
```

把 `New` 改为:

```go
func New(opts Options) *Server {
	invites := map[string]struct{}{}
	for _, c := range opts.InviteCodes {
		c = strings.TrimSpace(c)
		if c != "" {
			invites[c] = struct{}{}
		}
	}
	return &Server{
		opts:        opts,
		started:     time.Now().UTC(),
		signer:      newSessionSigner(opts.SessionKey),
		inviteCodes: invites,
		limiter:     newRateLimiter(authRateLimit, authRateWindow),
	}
}
```

(`strings` 与 `time` 已在 `server.go` 导入。)

在 `Handler()` 里、`return mux` 之前,追加三条路由:

```go
	mux.HandleFunc("/v1/auth/register", s.limiter.wrap(s.handleRegister))
	mux.HandleFunc("/v1/auth/login", s.limiter.wrap(s.handleLogin))
	mux.HandleFunc("/v1/devices", requireSession(s.signer,
		func(w http.ResponseWriter, r *http.Request, accountID string) {
			switch r.Method {
			case "POST":
				s.handlePostDevice(w, r, accountID)
			case "GET":
				s.handleGetDevices(w, r, accountID)
			default:
				w.WriteHeader(405)
			}
		}))
```

- [ ] **Step 3: handlers_auth.go**

新建 `internal/server/handlers_auth.go`:

```go
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		InviteCode string `json:"invite_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	if len(s.inviteCodes) == 0 {
		writeJSON(w, 403, map[string]string{"error": "registration_disabled"})
		return
	}
	if _, ok := s.inviteCodes[req.InviteCode]; !ok {
		writeJSON(w, 403, map[string]string{"error": "invalid_invite"})
		return
	}
	acc, err := s.opts.Hub.RegisterUser(req.Username, req.Password)
	switch {
	case errors.Is(err, hub.ErrUsernameInvalid):
		writeJSON(w, 400, map[string]string{"error": "username_invalid"})
		return
	case errors.Is(err, hub.ErrPasswordTooShort):
		writeJSON(w, 400, map[string]string{"error": "password_too_short"})
		return
	case errors.Is(err, hub.ErrPasswordTooLong):
		writeJSON(w, 400, map[string]string{"error": "password_too_long"})
		return
	case errors.Is(err, hub.ErrUsernameTaken):
		writeJSON(w, 409, map[string]string{"error": "username_taken"})
		return
	case err != nil:
		writeJSON(w, 500, map[string]string{"error": "internal"})
		return
	}
	s.writeSession(w, 201, acc)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	acc, err := s.opts.Hub.Authenticate(req.Username, req.Password)
	if errors.Is(err, hub.ErrInvalidCredentials) {
		writeJSON(w, 401, map[string]string{"error": "invalid_credentials"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "internal"})
		return
	}
	s.writeSession(w, 200, acc)
}

func (s *Server) writeSession(w http.ResponseWriter, status int, acc *hub.Account) {
	now := time.Now().UTC()
	writeJSON(w, status, map[string]any{
		"session_token": s.signer.sign(acc.ID, now),
		"account_id":    acc.ID,
		"username":      acc.Username,
		"expires_at":    now.Add(sessionTTL),
	})
}
```

- [ ] **Step 4: handlers_devices.go**

新建 `internal/server/handlers_devices.go`:

```go
package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

func (s *Server) handlePostDevice(w http.ResponseWriter, r *http.Request, accountID string) {
	var req struct {
		Label string   `json:"label"`
		Role  hub.Role `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	dev, tok, err := s.opts.Hub.AddDevice(accountID, req.Label, req.Role)
	if errors.Is(err, hub.ErrAccountNotFound) {
		writeJSON(w, 404, map[string]string{"error": "account_not_found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "internal"})
		return
	}
	writeJSON(w, 201, map[string]any{
		"device_id":    dev.ID,
		"device_token": tok,
		"label":        dev.Label,
		"role":         dev.Role,
		"created_at":   dev.CreatedAt,
	})
}

func (s *Server) handleGetDevices(w http.ResponseWriter, r *http.Request, accountID string) {
	devs := s.opts.Hub.ListDevicesByAccount(accountID)
	out := make([]map[string]any, 0, len(devs))
	for _, d := range devs {
		out = append(out, map[string]any{
			"id":        d.ID,
			"label":     d.Label,
			"role":      d.Role,
			"last_seen": d.LastSeen,
			"online":    s.opts.Hub.IsOnline(d.ID),
		})
	}
	writeJSON(w, 200, map[string]any{"devices": out})
}
```

- [ ] **Step 5: 写 handler 测试**

新建 `internal/server/handlers_auth_test.go`:

```go
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

// newAuthServer 起一个带邀请码与会话密钥的 server。
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
```

新建 `internal/server/handlers_devices_test.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// registerAndToken 注册一个用户并返回其会话 token。
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
	// 第二个用户
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
```

- [ ] **Step 6: 跑全 server 包测试确认通过**

Run: `go test ./internal/server/`
Expected: ok(新老测试全绿)

- [ ] **Step 7: 提交**

```bash
git add internal/server/auth.go internal/server/server.go internal/server/handlers_auth.go internal/server/handlers_devices.go internal/server/handlers_auth_test.go internal/server/handlers_devices_test.go
git commit -m "feat(relay/server): requireSession + /v1/auth/{register,login} + /v1/devices"
```

---

## Task 6: main.go 接 env + env.example 文档

**Files:**
- Modify: `cmd/type4me-relay/main.go`
- Modify/Create: `deploy/env.example`

- [ ] **Step 1: main.go 读两个新 env**

`cmd/type4me-relay/main.go`。在 import 块加入 `"crypto/rand"` 与 `"strings"`(保持其余 import)。在 `srv := server.New(...)` **之前**插入:

```go
	inviteCodes := splitAndTrim(os.Getenv("TYPE4ME_RELAY_INVITE_CODES"))
	sessionKey := []byte(os.Getenv("TYPE4ME_RELAY_SESSION_KEY"))
	if len(sessionKey) == 0 {
		sessionKey = make([]byte, 32)
		if _, err := rand.Read(sessionKey); err != nil {
			log.Fatalf("generate session key: %v", err)
		}
		log.Println("WARNING: TYPE4ME_RELAY_SESSION_KEY unset; using a random key. All sessions invalidate on restart.")
	}
```

把 `server.New(...)` 调用改为带上两字段:

```go
	srv := server.New(server.Options{
		Hub:         h,
		AdminToken:  admin,
		Version:     version,
		InviteCodes: inviteCodes,
		SessionKey:  sessionKey,
	})
```

在文件末尾(`main` 之外)加助手:

```go
// splitAndTrim 按逗号拆分并去空白,丢弃空项。
func splitAndTrim(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
```

- [ ] **Step 2: 编译并跑构建二进制**

Run: `go build ./... && go vet ./...`
Expected: 无输出(成功)

- [ ] **Step 3: 更新 env.example**

打开 `deploy/env.example`(若不存在则新建),追加:

```bash
# 自助账号:逗号分隔的邀请码集合。留空则关闭注册(register 返回 403)。
TYPE4ME_RELAY_INVITE_CODES=

# 会话 token 的 HMAC 签名密钥(任意长随机串,生产务必固定)。
# 留空则每次启动随机生成 —— 所有会话在重启后失效(device token 不受影响)。
TYPE4ME_RELAY_SESSION_KEY=
```

- [ ] **Step 4: 提交**

```bash
git add cmd/type4me-relay/main.go deploy/env.example
git commit -m "feat(relay): wire TYPE4ME_RELAY_INVITE_CODES / SESSION_KEY env"
```

---

## Task 7: 端到端冒烟脚本(自助流程)

**Files:**
- Create: `scripts/test_relay_account_e2e.sh`(仓库根目录,**不是** relay/)

- [ ] **Step 1: 写脚本**

新建 `scripts/test_relay_account_e2e.sh`(参考已有 `scripts/test_relay_e2e.sh` 的启动/清理结构,但走自助注册而非 admin):

```bash
#!/bin/bash
# End-to-end smoke for the self-service account layer:
# register -> register a receiver device (session auth) -> register a sender
# device -> GET /v1/devices -> dispatch sender->receiver -> verify clipboard.
# macOS dev machine only.

set -euo pipefail
cd "$(dirname "$0")/.."

TMP=$(mktemp -d)
SAVED_CLIP=$(pbpaste || true)
ADMIN="admin-$(openssl rand -hex 8)"
INVITE="invite-$(openssl rand -hex 4)"
SESSION_KEY="sk-$(openssl rand -hex 16)"
RELAY_PID=""
SUB_PID=""
BASE="http://127.0.0.1:8443"

cleanup() {
    [ -n "$RELAY_PID" ] && kill "$RELAY_PID" 2>/dev/null || true
    [ -n "$SUB_PID" ] && kill "$SUB_PID" 2>/dev/null || true
    rm -rf "$TMP"
    echo "$SAVED_CLIP" | pbcopy || true
}
trap cleanup EXIT

make -C relay build-darwin >/dev/null
make -C receiver build-darwin-arm64 >/dev/null
RELAY=./relay/dist/type4me-relay-darwin-arm64
RECV=./receiver/dist/type4me-receiver-darwin-arm64

# 1. Start relay with invite code + session key
TYPE4ME_RELAY_ADMIN_TOKEN="$ADMIN" \
TYPE4ME_RELAY_STATE_DIR="$TMP/state" \
TYPE4ME_RELAY_INVITE_CODES="$INVITE" \
TYPE4ME_RELAY_SESSION_KEY="$SESSION_KEY" \
"$RELAY" serve > "$TMP/relay.log" 2>&1 &
RELAY_PID=$!
for i in $(seq 1 30); do
    curl -fs "$BASE/healthz" >/dev/null 2>&1 && break
    sleep 0.1
done

# 2. Register a user (self-service)
SESSION=$(curl -sf -X POST -H "Content-Type: application/json" \
    -d "{\"username\":\"e2euser\",\"password\":\"supersecret\",\"invite_code\":\"$INVITE\"}" \
    "$BASE/v1/auth/register" | jq -r .session_token)
[ -n "$SESSION" ] && [ "$SESSION" != "null" ] || { echo "FAIL: no session token"; cat "$TMP/relay.log"; exit 1; }

# 3. Register receiver (Win) + sender (Mac) via session auth
WIN=$(curl -sf -X POST -H "Authorization: Bearer $SESSION" \
    -H "Content-Type: application/json" -d '{"label":"Win"}' "$BASE/v1/devices")
MAC=$(curl -sf -X POST -H "Authorization: Bearer $SESSION" \
    -H "Content-Type: application/json" -d '{"label":"Mac"}' "$BASE/v1/devices")
WIN_ID=$(echo "$WIN" | jq -r .device_id)
WIN_TOK=$(echo "$WIN" | jq -r .device_token)
MAC_TOK=$(echo "$MAC" | jq -r .device_token)

# 4. GET /v1/devices should list both
COUNT=$(curl -sf -H "Authorization: Bearer $SESSION" "$BASE/v1/devices" | jq '.devices | length')
[ "$COUNT" = "2" ] || { echo "FAIL: device count=$COUNT want 2"; exit 1; }

# 5. Start subscriber as the receiver device
TYPE4ME_MODE=relay-subscriber \
TYPE4ME_RELAY_URL="$BASE" \
TYPE4ME_DEVICE_ID="$WIN_ID" \
TYPE4ME_DEVICE_TOKEN="$WIN_TOK" \
"$RECV" --config "$TMP/recv.json" > "$TMP/subscriber.log" 2>&1 &
SUB_PID=$!
sleep 1

# 6. Dispatch sender -> receiver
TEXT="account e2e $(date +%s)"
curl -sf -X POST -H "Authorization: Bearer $MAC_TOK" \
    -H "Content-Type: application/json" \
    -d "{\"target_device_id\":\"$WIN_ID\",\"text\":\"$TEXT\"}" \
    "$BASE/v1/dispatch" >/dev/null
sleep 0.5

GOT=$(pbpaste)
if [ "$GOT" = "$TEXT" ]; then
    echo "PASS: self-service register -> device -> list -> dispatch works"
    exit 0
else
    echo "FAIL: clipboard='$GOT' expected='$TEXT'"
    echo "--- relay log ---"; cat "$TMP/relay.log"
    echo "--- subscriber log ---"; cat "$TMP/subscriber.log"
    exit 1
fi
```

- [ ] **Step 2: 赋可执行权限**

Run(仓库根): `chmod +x scripts/test_relay_account_e2e.sh`

- [ ] **Step 3: 跑端到端(macOS 开发机,需 jq/openssl/swift 工具链)**

Run(仓库根): `bash scripts/test_relay_account_e2e.sh`
Expected: 末行 `PASS: self-service register -> device -> list -> dispatch works`
(若环境无法构建 receiver/clipboard,记录跳过原因;CI 之外的手动冒烟即可。)

- [ ] **Step 4: 提交**

```bash
git add scripts/test_relay_account_e2e.sh
git commit -m "test(relay): e2e smoke for self-service account flow"
```

---

## 收尾验证

- [ ] **全量测试**

Run(relay/): `go test ./... && go vet ./...`
Expected: 全 ok,无 vet 警告。

- [ ] **确认现有接口未回归**

`go test ./internal/server/` 中原有的 `TestHealthz`/`TestPing*`/dispatch/subscribe 测试仍全绿(本计划未触碰其逻辑)。

---

## 自查记录(spec 覆盖)

- 用户名/密码注册登录 → Task 2(hub)+ Task 5(handlers)。
- 邀请码(env、可重复、空集合关闭)→ Task 5(校验)+ Task 6(env)。
- HMAC 会话 token + 24h 过期 → Task 3。
- `requireSession` 中间件 → Task 5 Step 1。
- 按 IP 限流 → Task 4 + Task 5(挂到 auth 路由)。
- `POST/GET /v1/devices`(隔离 + online)→ Task 5。
- `state.json` v2 向后兼容 → Task 1。
- 错误码表(bad_json/username_invalid/password_too_short/password_too_long/username_taken/registration_disabled/invalid_invite/invalid_credentials/invalid_session/rate_limited)→ Task 4/5。
- 端到端冒烟 → Task 7。
- 现有 `/v1/{dispatch,subscribe,ping,admin}` 不变 → 收尾验证回归确认。
