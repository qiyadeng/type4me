# Self-Hosted Relay R0-R4 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 self-hosted relay 的 R0-R4 切片,完成跨网场景下 Mac→Win 文字投递的全套代码 + 部署 artifacts。R5(用户在自己 VPS + 跨网环境的真机手测)是用户操作,不在 plan 范围。

**Architecture:** 新增独立 Go 服务 `relay/`(SSE PubSub,无 DB,内存路由),修改现有 Mac Swift 端引入 `RemoteTransport` 抽象(`DirectTransport` + `RelayTransport`),改造 Win Go receiver 支持 `relay-subscriber` 模式与 listener 共存。

**Tech Stack:** Go 1.21+(relay + receiver 改造),Swift 6 v5 mode(Mac side),`golang.org/x/crypto/bcrypt`(relay 唯一外部依赖),systemd + Caddy(部署)。

**Spec:** `docs/superpowers/specs/2026-05-27-self-hosted-relay-design.md`

**前置:** S0+S1+S3 已完成(branch `feature/remote-voice-input-s0-s1` 30+ commits),所有现有测试(Swift 182/182 + Go 全过)green。

---

## File Structure

**新增顶层 `relay/`**(独立 go module,与 `receiver/` 同级):

```
relay/
├── go.mod                                 # module github.com/qiyadeng/type4me/relay
├── go.sum
├── Makefile                               # darwin/linux/windows cross-compile + test
├── .gitignore                             # dist/
├── cmd/type4me-relay/main.go              # 启动入口
└── internal/
    ├── hub/
    │   ├── types.go                       # Account / Device / Message / role 常量
    │   ├── errors.go                      # ErrReceiverOffline / ErrBackpressure / ErrCrossAccount 等
    │   ├── token.go                       # 生成 + bcrypt + token cache
    │   ├── token_test.go
    │   ├── state.go                       # state.json load/save 原子写
    │   ├── state_test.go
    │   ├── hub.go                         # Hub struct + 所有方法
    │   └── hub_test.go
    └── server/
        ├── server.go                      # routes + handler glue
        ├── auth.go                        # admin/device token middleware
        ├── sse.go                         # SSE writer + heartbeat
        ├── handlers_health.go             # /healthz + /v1/ping
        ├── handlers_admin.go              # /v1/admin/*
        ├── handlers_dispatch.go           # POST /v1/dispatch
        ├── handlers_subscribe.go          # GET /v1/subscribe (SSE)
        └── server_test.go                 # httptest 集成
```

**Mac 端**(`Type4Me/Injection/` 下):

```
Type4Me/Injection/
├── OutputTarget.swift                     # 改:加 mode + relay 字段 + 自定义 decoder
├── RemoteTransport.swift                  # 新:protocol
├── DirectTransport.swift                  # 新:从 RemoteHTTPSink 提取的直连 HTTP
├── RelayTransport.swift                   # 新:relay 模式 HTTP
└── RemoteHTTPSink.swift                   # 改:变成 Outcome 映射层,委托 transport

Type4MeTests/
├── DirectTransportTests.swift             # 改:从 RemoteHTTPSinkTests 改名 + 调整
├── RelayTransportTests.swift              # 新
├── OutputTargetTests.swift                # 改:加 mode codable 测试
└── RemoteHTTPSinkClipboardFallbackTests.swift  # 保留(已存在)
```

**Win 端 receiver**:

```
receiver/internal/
├── config/
│   └── config.go                          # 改:加 Mode + relay 字段 + 新 env vars
└── relay/                                 # 新目录
    ├── subscriber.go                      # SSE 客户端 + 自动重连
    └── subscriber_test.go

receiver/cmd/type4me-receiver/main.go      # 改:mode 分叉
```

**部署 artifacts**(R4):

```
deploy/
├── type4me-relay.service                  # systemd unit
├── Caddyfile.example                      # 反代示例
└── env.example                            # admin token + 配置示例

docs/
└── relay-deployment.md                    # step-by-step 部署文档

scripts/
└── test_relay_e2e.sh                      # 端到端冒烟脚本
```

---

## Phase R0 — Relay Hub 核心(无 HTTP)

7 个 task,完成后 `cd relay && go test ./internal/hub/...` 全过,但还没法跑 HTTP 服务。

### Task R0.1: Go 模块脚手架

**Files:** Create `relay/go.mod`, `relay/Makefile`, `relay/.gitignore`, `relay/cmd/type4me-relay/main.go`

- [ ] **Step 1: 初始化 module**

```bash
mkdir -p relay/cmd/type4me-relay relay/internal/hub relay/internal/server
cd relay && go mod init github.com/qiyadeng/type4me/relay && cd ..
```

- [ ] **Step 2: 加 bcrypt 依赖**

```bash
cd relay && go get golang.org/x/crypto/bcrypt && cd ..
```

- [ ] **Step 3: 占位 main.go**

`relay/cmd/type4me-relay/main.go`:

```go
package main

import "fmt"

func main() {
    fmt.Println("type4me-relay placeholder")
}
```

- [ ] **Step 4: Makefile**

`relay/Makefile`:

```makefile
.PHONY: build build-linux build-darwin build-windows test clean

VERSION ?= 0.1.0
DIST := dist

build: build-darwin

build-linux:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -ldflags "-X main.version=$(VERSION)" \
		-o $(DIST)/type4me-relay-linux-amd64 ./cmd/type4me-relay

build-darwin:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 \
		go build -ldflags "-X main.version=$(VERSION)" \
		-o $(DIST)/type4me-relay-darwin-arm64 ./cmd/type4me-relay

build-windows:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
		go build -ldflags "-X main.version=$(VERSION)" \
		-o $(DIST)/type4me-relay-windows-amd64.exe ./cmd/type4me-relay

test:
	go test ./...
	@echo "--- verifying linux cross-compile ---"
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go vet ./...
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/type4me-relay
	@echo "linux cross-compile OK"

clean:
	rm -rf $(DIST)
```

- [ ] **Step 5: .gitignore**

`relay/.gitignore`:

```
dist/
*.dSYM
```

- [ ] **Step 6: 验证**

```bash
make -C relay build-darwin && ./relay/dist/type4me-relay-darwin-arm64
```

Expected: `type4me-relay placeholder`

- [ ] **Step 7: Commit**

```bash
git add relay/go.mod relay/go.sum relay/Makefile relay/.gitignore \
        relay/cmd/type4me-relay/main.go
git commit -m "feat(relay): Go 模块脚手架

新顶层 relay/ 目录,跟 receiver/ 同级独立 module。
唯一外部依赖 bcrypt。Makefile cross-compile 三平台。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R0.2: 数据类型 + sentinel errors

**Files:** Create `relay/internal/hub/types.go`, `relay/internal/hub/errors.go`

无单测(纯结构定义);后续 task 引用时会编译验证。

- [ ] **Step 1: types.go**

`relay/internal/hub/types.go`:

```go
package hub

import "time"

// Role describes whether a device can send, receive, or both.
// v1 stores it but treats all devices as "either".
type Role string

const (
	RoleSender   Role = "sender"
	RoleReceiver Role = "receiver"
	RoleEither   Role = "either"
)

type Account struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Device struct {
	ID        string    `json:"id"`
	AccountID string    `json:"account_id"`
	Label     string    `json:"label"`
	Role      Role      `json:"role"`
	TokenHash string    `json:"token_hash"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
}

type Message struct {
	ID                string    `json:"id"`
	Text              string    `json:"text"`
	FromDevice        string    `json:"from_device"`
	RequestID         string    `json:"request_id"`
	PreserveClipboard bool      `json:"preserve_clipboard"`
	CreatedAt         time.Time `json:"created_at"`
}
```

- [ ] **Step 2: errors.go**

`relay/internal/hub/errors.go`:

```go
package hub

import "errors"

var (
	ErrAccountNotFound     = errors.New("account not found")
	ErrDeviceNotFound      = errors.New("device not found")
	ErrCrossAccount        = errors.New("target device not in sender's account")
	ErrReceiverOffline     = errors.New("receiver offline")
	ErrBackpressure        = errors.New("receiver backpressure")
	ErrInvalidToken        = errors.New("invalid token")
	ErrAccountNameRequired = errors.New("account name required")
)
```

- [ ] **Step 3: build**

```bash
cd relay && go build ./internal/hub/
```

Expected: 静默通过

- [ ] **Step 4: Commit**

```bash
git add relay/internal/hub/types.go relay/internal/hub/errors.go
git commit -m "feat(relay/hub): Account / Device / Message + sentinel errors

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R0.3: Token 生成 + bcrypt + cache

**Files:** Create `relay/internal/hub/token.go`, `relay/internal/hub/token_test.go`

- [ ] **Step 1: 失败测试**

`relay/internal/hub/token_test.go`:

```go
package hub

import (
	"strings"
	"testing"
)

func TestGenerateTokenLength(t *testing.T) {
	tok, err := generateToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 43 {
		t.Errorf("token len = %d, want 43 (32 bytes base64url)", len(tok))
	}
	tok2, _ := generateToken()
	if tok == tok2 {
		t.Errorf("two tokens equal: %q", tok)
	}
}

func TestHashAndVerifyToken(t *testing.T) {
	tok, _ := generateToken()
	hash, err := hashToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$2a$") && !strings.HasPrefix(hash, "$2b$") {
		t.Errorf("hash not bcrypt: %q", hash)
	}
	if !verifyToken(tok, hash) {
		t.Errorf("verify failed for correct token")
	}
	if verifyToken("wrong-token", hash) {
		t.Errorf("verify passed for wrong token")
	}
}

func TestTokenCacheHit(t *testing.T) {
	c := newTokenCache()
	tok, _ := generateToken()
	hash, _ := hashToken(tok)
	c.put(tok, "dev-1")
	got, ok := c.get(tok)
	if !ok || got != "dev-1" {
		t.Errorf("cache miss: ok=%v got=%q", ok, got)
	}
	_ = hash
}

func TestTokenCacheInvalidate(t *testing.T) {
	c := newTokenCache()
	tok, _ := generateToken()
	c.put(tok, "dev-1")
	c.invalidate("dev-1")
	if _, ok := c.get(tok); ok {
		t.Errorf("cache hit after invalidate")
	}
}
```

- [ ] **Step 2: 验证编译失败**

```bash
cd relay && go test ./internal/hub/...
```

Expected: 引用未定义符号(`generateToken`, `hashToken`, `verifyToken`, `newTokenCache`, etc.)

- [ ] **Step 3: 实现**

`relay/internal/hub/token.go`:

```go
package hub

import (
	"crypto/rand"
	"encoding/base64"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// generateToken returns 32 random bytes base64url-encoded (43 chars).
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	return string(h), err
}

func verifyToken(token, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)) == nil
}

// tokenCache memoizes token -> deviceID lookups so /dispatch hot path doesn't
// run bcrypt every request. Entries are invalidated on rotate / delete.
type tokenCache struct {
	mu sync.RWMutex
	m  map[string]string // token -> deviceID
	r  map[string]string // deviceID -> token (reverse, for invalidate)
}

func newTokenCache() *tokenCache {
	return &tokenCache{m: map[string]string{}, r: map[string]string{}}
}

func (c *tokenCache) get(token string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	d, ok := c.m[token]
	return d, ok
}

func (c *tokenCache) put(token, deviceID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// clear any stale forward entries for this device
	if oldTok, ok := c.r[deviceID]; ok {
		delete(c.m, oldTok)
	}
	c.m[token] = deviceID
	c.r[deviceID] = token
}

func (c *tokenCache) invalidate(deviceID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if tok, ok := c.r[deviceID]; ok {
		delete(c.m, tok)
		delete(c.r, deviceID)
	}
}
```

- [ ] **Step 4: 运行测试**

```bash
cd relay && go test ./internal/hub/... -v
```

Expected: 4 个 test 全过

- [ ] **Step 5: Commit**

```bash
git add relay/internal/hub/token.go relay/internal/hub/token_test.go
git commit -m "feat(relay/hub): token 生成 + bcrypt + cache

- generateToken: 32 字节 crypto/rand -> 43 字符 base64url
- hashToken/verifyToken: bcrypt 包装,DefaultCost
- tokenCache: token -> deviceID 反查 + invalidate

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R0.4: State.json 持久化(原子写)

**Files:** Create `relay/internal/hub/state.go`, `relay/internal/hub/state_test.go`

- [ ] **Step 1: 失败测试**

`relay/internal/hub/state_test.go`:

```go
package hub

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoadState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	now := time.Now().UTC().Truncate(time.Second)
	want := &State{
		Version: 1,
		Accounts: []*Account{
			{ID: "acct-1", Name: "test", CreatedAt: now},
		},
		Devices: []*Device{
			{ID: "dev-1", AccountID: "acct-1", Label: "Mac",
				Role: RoleEither, TokenHash: "$2a$10$xxx",
				CreatedAt: now, LastSeen: now},
		},
	}
	if err := saveState(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Accounts) != 1 || got.Accounts[0].Name != "test" {
		t.Errorf("accounts roundtrip failed: %+v", got.Accounts)
	}
	if len(got.Devices) != 1 || got.Devices[0].TokenHash != "$2a$10$xxx" {
		t.Errorf("devices roundtrip failed: %+v", got.Devices)
	}
}

func TestLoadStateMissingFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")
	st, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Accounts) != 0 || len(st.Devices) != 0 {
		t.Errorf("expected empty state, got %+v", st)
	}
	if st.Version != 1 {
		t.Errorf("expected version 1, got %d", st.Version)
	}
}

func TestSaveStateAtomicReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	_ = os.WriteFile(path, []byte("{}"), 0600)
	st := &State{Version: 1, Accounts: []*Account{{ID: "x", Name: "y"}}}
	if err := saveState(path, st); err != nil {
		t.Fatal(err)
	}
	// no .tmp leftover
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp leaked: %v", err)
	}
	got, _ := loadState(path)
	if got.Accounts[0].ID != "x" {
		t.Errorf("not overwritten: %+v", got)
	}
}

func TestSaveStatePermissions0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	_ = saveState(path, &State{Version: 1})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %o, want 0600", info.Mode().Perm())
	}
}
```

- [ ] **Step 2: 实现**

`relay/internal/hub/state.go`:

```go
package hub

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

type State struct {
	Version  int        `json:"version"`
	Accounts []*Account `json:"accounts"`
	Devices  []*Device  `json:"devices"`
}

const stateVersion = 1

// loadState reads state from path; missing file returns empty state (version=1).
func loadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &State{Version: stateVersion, Accounts: []*Account{}, Devices: []*Device{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	if st.Version == 0 {
		st.Version = stateVersion
	}
	if st.Accounts == nil {
		st.Accounts = []*Account{}
	}
	if st.Devices == nil {
		st.Devices = []*Device{}
	}
	return &st, nil
}

// saveState atomically writes state via tmp file + rename.
func saveState(path string, st *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
```

- [ ] **Step 3: 跑测试**

```bash
cd relay && go test ./internal/hub/... -v
```

Expected: 4 个新 test + 之前 4 个全过

- [ ] **Step 4: Commit**

```bash
git add relay/internal/hub/state.go relay/internal/hub/state_test.go
git commit -m "feat(relay/hub): state.json load/save 原子写

write-to-tmp + rename;权限 0600;缺文件返回 empty state(version=1)。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R0.5: Hub Account CRUD

**Files:** Create `relay/internal/hub/hub.go`, `relay/internal/hub/hub_test.go`

- [ ] **Step 1: 失败测试(只测 account 部分)**

`relay/internal/hub/hub_test.go`:

```go
package hub

import (
	"errors"
	"path/filepath"
	"testing"
)

func newTestHub(t *testing.T) *Hub {
	t.Helper()
	dir := t.TempDir()
	h, err := New(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestAddAccount(t *testing.T) {
	h := newTestHub(t)
	acc, err := h.AddAccount("Personal")
	if err != nil {
		t.Fatal(err)
	}
	if acc.ID == "" || acc.Name != "Personal" {
		t.Errorf("bad account: %+v", acc)
	}
}

func TestAddAccountEmptyNameRejected(t *testing.T) {
	h := newTestHub(t)
	_, err := h.AddAccount("")
	if !errors.Is(err, ErrAccountNameRequired) {
		t.Errorf("expected ErrAccountNameRequired, got %v", err)
	}
}

func TestListAccounts(t *testing.T) {
	h := newTestHub(t)
	_, _ = h.AddAccount("a")
	_, _ = h.AddAccount("b")
	accs := h.ListAccounts()
	if len(accs) != 2 {
		t.Errorf("len = %d, want 2", len(accs))
	}
}

func TestHubPersistsAccountsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	h1, _ := New(path)
	acc, _ := h1.AddAccount("Personal")
	h2, _ := New(path)
	accs := h2.ListAccounts()
	if len(accs) != 1 || accs[0].ID != acc.ID {
		t.Errorf("reload failed: %+v", accs)
	}
}
```

- [ ] **Step 2: 实现 Hub 基础 + AddAccount + ListAccounts**

`relay/internal/hub/hub.go`:

```go
package hub

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"sync"
	"time"
)

type Hub struct {
	mu        sync.RWMutex
	statePath string
	accounts  map[string]*Account
	devices   map[string]*Device
	subs      map[string]chan *Message
	cache     *tokenCache
}

// New creates a Hub, loading existing state from disk if present.
func New(statePath string) (*Hub, error) {
	st, err := loadState(statePath)
	if err != nil {
		return nil, err
	}
	h := &Hub{
		statePath: statePath,
		accounts:  map[string]*Account{},
		devices:   map[string]*Device{},
		subs:      map[string]chan *Message{},
		cache:     newTokenCache(),
	}
	for _, a := range st.Accounts {
		h.accounts[a.ID] = a
	}
	for _, d := range st.Devices {
		h.devices[d.ID] = d
	}
	return h, nil
}

func (h *Hub) AddAccount(name string) (*Account, error) {
	if strings.TrimSpace(name) == "" {
		return nil, ErrAccountNameRequired
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	acc := &Account{
		ID:        "acct-" + shortID(),
		Name:      name,
		CreatedAt: time.Now().UTC(),
	}
	h.accounts[acc.ID] = acc
	if err := h.persistLocked(); err != nil {
		delete(h.accounts, acc.ID)
		return nil, err
	}
	return acc, nil
}

func (h *Hub) ListAccounts() []*Account {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*Account, 0, len(h.accounts))
	for _, a := range h.accounts {
		out = append(out, a)
	}
	return out
}

// persistLocked writes current state to disk; caller holds h.mu.
func (h *Hub) persistLocked() error {
	st := &State{
		Version:  stateVersion,
		Accounts: make([]*Account, 0, len(h.accounts)),
		Devices:  make([]*Device, 0, len(h.devices)),
	}
	for _, a := range h.accounts {
		st.Accounts = append(st.Accounts, a)
	}
	for _, d := range h.devices {
		st.Devices = append(st.Devices, d)
	}
	return saveState(h.statePath, st)
}

// shortID returns a URL-safe 10-character random ID.
func shortID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:10]
}
```

- [ ] **Step 3: 跑测试**

```bash
cd relay && go test ./internal/hub/... -v
```

Expected: 全 12 个 test 过

- [ ] **Step 4: Commit**

```bash
git add relay/internal/hub/hub.go relay/internal/hub/hub_test.go
git commit -m "feat(relay/hub): Hub 基础 + AddAccount + ListAccounts

- Hub 持久化到 statePath;启动 load 旧 state
- AddAccount 空名拒绝 + 自动 shortID
- ListAccounts 返回拷贝 slice

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R0.6: Hub Device CRUD + token rotation

**Files:** Modify `relay/internal/hub/hub.go`, `relay/internal/hub/hub_test.go`

- [ ] **Step 1: 加测试**

追加到 `hub_test.go`:

```go
func TestAddDevice(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	dev, token, err := h.AddDevice(acc.ID, "My-Mac", RoleEither)
	if err != nil {
		t.Fatal(err)
	}
	if dev.ID == "" || dev.AccountID != acc.ID || dev.Label != "My-Mac" {
		t.Errorf("bad device: %+v", dev)
	}
	if len(token) != 43 {
		t.Errorf("token len = %d, want 43", len(token))
	}
	if dev.TokenHash == token {
		t.Errorf("hash should not equal plain token")
	}
}

func TestAddDeviceAccountNotFound(t *testing.T) {
	h := newTestHub(t)
	_, _, err := h.AddDevice("acct-missing", "Mac", RoleEither)
	if !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("expected ErrAccountNotFound, got %v", err)
	}
}

func TestListDevicesExcludesTokenHash(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	_, _, _ = h.AddDevice(acc.ID, "Mac", RoleEither)
	devs := h.ListDevices()
	if len(devs) != 1 {
		t.Errorf("len = %d", len(devs))
	}
	// ListDevices returns the same struct; caller is trusted not to leak TokenHash.
	// Server-side handler strips it before JSON serialization (verified in handler tests).
	if devs[0].TokenHash == "" {
		t.Errorf("hub should retain hash internally")
	}
}

func TestRotateDeviceToken(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	dev, oldTok, _ := h.AddDevice(acc.ID, "Mac", RoleEither)
	newTok, err := h.RotateDeviceToken(dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if newTok == oldTok {
		t.Errorf("rotation returned same token")
	}
	// Old token no longer verifies
	if _, err := h.ResolveDeviceByToken(oldTok); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("old token still valid: %v", err)
	}
	// New token does
	resolved, err := h.ResolveDeviceByToken(newTok)
	if err != nil || resolved.ID != dev.ID {
		t.Errorf("new token failed: dev=%v err=%v", resolved, err)
	}
}

func TestDeleteDevice(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	dev, tok, _ := h.AddDevice(acc.ID, "Mac", RoleEither)
	if err := h.DeleteDevice(dev.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.ResolveDeviceByToken(tok); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("token still valid after delete: %v", err)
	}
	if _, err := h.GetDevice(dev.ID); !errors.Is(err, ErrDeviceNotFound) {
		t.Errorf("get device after delete: %v", err)
	}
}

func TestResolveDeviceByTokenUsesCache(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	dev, tok, _ := h.AddDevice(acc.ID, "Mac", RoleEither)
	// First lookup: bcrypt path
	d1, err := h.ResolveDeviceByToken(tok)
	if err != nil || d1.ID != dev.ID {
		t.Fatal(err)
	}
	// Second lookup: cache should hit
	if _, ok := h.cache.get(tok); !ok {
		t.Errorf("cache should have entry after first resolve")
	}
}
```

- [ ] **Step 2: 实现 AddDevice / ListDevices / GetDevice / DeleteDevice / RotateDeviceToken / ResolveDeviceByToken**

追加到 `relay/internal/hub/hub.go`:

```go
func (h *Hub) AddDevice(accountID, label string, role Role) (*Device, string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.accounts[accountID]; !ok {
		return nil, "", ErrAccountNotFound
	}
	tok, err := generateToken()
	if err != nil {
		return nil, "", err
	}
	hash, err := hashToken(tok)
	if err != nil {
		return nil, "", err
	}
	if role == "" {
		role = RoleEither
	}
	now := time.Now().UTC()
	dev := &Device{
		ID:        "dev-" + shortID(),
		AccountID: accountID,
		Label:     label,
		Role:      role,
		TokenHash: hash,
		CreatedAt: now,
		LastSeen:  now,
	}
	h.devices[dev.ID] = dev
	if err := h.persistLocked(); err != nil {
		delete(h.devices, dev.ID)
		return nil, "", err
	}
	h.cache.put(tok, dev.ID)
	return dev, tok, nil
}

func (h *Hub) ListDevices() []*Device {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*Device, 0, len(h.devices))
	for _, d := range h.devices {
		out = append(out, d)
	}
	return out
}

func (h *Hub) GetDevice(id string) (*Device, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	d, ok := h.devices[id]
	if !ok {
		return nil, ErrDeviceNotFound
	}
	return d, nil
}

func (h *Hub) DeleteDevice(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.devices[id]; !ok {
		return ErrDeviceNotFound
	}
	delete(h.devices, id)
	h.cache.invalidate(id)
	if ch, ok := h.subs[id]; ok {
		close(ch)
		delete(h.subs, id)
	}
	return h.persistLocked()
}

func (h *Hub) RotateDeviceToken(id string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	dev, ok := h.devices[id]
	if !ok {
		return "", ErrDeviceNotFound
	}
	tok, err := generateToken()
	if err != nil {
		return "", err
	}
	hash, err := hashToken(tok)
	if err != nil {
		return "", err
	}
	dev.TokenHash = hash
	h.cache.invalidate(id)
	if err := h.persistLocked(); err != nil {
		return "", err
	}
	h.cache.put(tok, id)
	return tok, nil
}

// ResolveDeviceByToken returns the device matching `token`, using token cache
// to avoid bcrypt on every hit.
func (h *Hub) ResolveDeviceByToken(token string) (*Device, error) {
	if id, ok := h.cache.get(token); ok {
		h.mu.RLock()
		dev, ok := h.devices[id]
		h.mu.RUnlock()
		if ok && verifyToken(token, dev.TokenHash) {
			return dev, nil
		}
		// Stale cache (rotated/deleted) — fall through to full scan
		h.cache.invalidate(id)
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, dev := range h.devices {
		if verifyToken(token, dev.TokenHash) {
			h.cache.put(token, dev.ID)
			return dev, nil
		}
	}
	return nil, ErrInvalidToken
}
```

- [ ] **Step 3: 跑测试**

```bash
cd relay && go test ./internal/hub/... -v
```

Expected: 全部 test 过(此 task 加 6 个 + 之前 12 个 = 18 个)

- [ ] **Step 4: Commit**

```bash
git add relay/internal/hub/hub.go relay/internal/hub/hub_test.go
git commit -m "feat(relay/hub): Device CRUD + Rotate + ResolveByToken

- AddDevice 生成 token 立刻 cache,返回明文 token 一次
- DeleteDevice 同时关订阅 channel
- RotateDeviceToken 失效旧 cache 后 put 新
- ResolveDeviceByToken cache 命中走 bcrypt 验证,失败清 cache 全扫

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R0.7: Hub Subscribe + Dispatch + 后台扫尸

**Files:** Modify `relay/internal/hub/hub.go`, `relay/internal/hub/hub_test.go`

- [ ] **Step 1: 加测试**

追加到 `hub_test.go`:

```go
import "context"   // ensure this is in imports

func TestSubscribeAndDispatch(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	mac, macTok, _ := h.AddDevice(acc.ID, "Mac", RoleEither)
	win, _, _ := h.AddDevice(acc.ID, "Win", RoleEither)

	ch, unsub := h.Subscribe(win.ID)
	defer unsub()

	msg, err := h.Dispatch(macTok, win.ID, "hello", "req-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if msg.ID == "" || msg.Text != "hello" {
		t.Errorf("bad msg: %+v", msg)
	}

	select {
	case got := <-ch:
		if got.Text != "hello" || got.FromDevice != mac.ID {
			t.Errorf("received wrong msg: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no message received within 1s")
	}
}

func TestDispatchReceiverOffline(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	_, macTok, _ := h.AddDevice(acc.ID, "Mac", RoleEither)
	win, _, _ := h.AddDevice(acc.ID, "Win", RoleEither)
	// no Subscribe -> offline
	_, err := h.Dispatch(macTok, win.ID, "x", "", false)
	if !errors.Is(err, ErrReceiverOffline) {
		t.Errorf("expected ErrReceiverOffline, got %v", err)
	}
}

func TestDispatchCrossAccount(t *testing.T) {
	h := newTestHub(t)
	a, _ := h.AddAccount("A")
	b, _ := h.AddAccount("B")
	_, aTok, _ := h.AddDevice(a.ID, "MacA", RoleEither)
	devB, _, _ := h.AddDevice(b.ID, "WinB", RoleEither)
	ch, unsub := h.Subscribe(devB.ID)
	defer unsub()
	_, err := h.Dispatch(aTok, devB.ID, "x", "", false)
	if !errors.Is(err, ErrCrossAccount) {
		t.Errorf("expected ErrCrossAccount, got %v", err)
	}
	select {
	case got := <-ch:
		t.Errorf("cross-account dispatch leaked: %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDispatchTargetNotFound(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	_, macTok, _ := h.AddDevice(acc.ID, "Mac", RoleEither)
	_, err := h.Dispatch(macTok, "dev-missing", "x", "", false)
	if !errors.Is(err, ErrDeviceNotFound) {
		t.Errorf("expected ErrDeviceNotFound, got %v", err)
	}
}

func TestDispatchInvalidSenderToken(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	win, _, _ := h.AddDevice(acc.ID, "Win", RoleEither)
	_, err := h.Dispatch("bogus-token", win.ID, "x", "", false)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken, got %v", err)
	}
}

func TestDispatchBackpressure(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	_, macTok, _ := h.AddDevice(acc.ID, "Mac", RoleEither)
	win, _, _ := h.AddDevice(acc.ID, "Win", RoleEither)
	ch, unsub := h.Subscribe(win.ID)
	defer unsub()
	// Fill buffer (cap is 16) without reading
	for i := 0; i < 16; i++ {
		_, err := h.Dispatch(macTok, win.ID, "fill", "", false)
		if err != nil {
			t.Fatalf("fill %d: %v", i, err)
		}
	}
	// 17th should backpressure
	_, err := h.Dispatch(macTok, win.ID, "overflow", "", false)
	if !errors.Is(err, ErrBackpressure) {
		t.Errorf("expected ErrBackpressure, got %v", err)
	}
	_ = ch
}

func TestSubscribeReconnectClosesOldChannel(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	win, _, _ := h.AddDevice(acc.ID, "Win", RoleEither)
	ch1, _ := h.Subscribe(win.ID)
	ch2, unsub := h.Subscribe(win.ID)
	defer unsub()
	// ch1 must be closed
	select {
	case _, ok := <-ch1:
		if ok {
			t.Errorf("ch1 should be closed but got value")
		}
	case <-time.After(100 * time.Millisecond):
		t.Errorf("ch1 not closed in time")
	}
	_ = ch2
}

func TestSelfDispatchAllowed(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	dev, tok, _ := h.AddDevice(acc.ID, "Mac", RoleEither)
	ch, unsub := h.Subscribe(dev.ID)
	defer unsub()
	_, err := h.Dispatch(tok, dev.ID, "echo", "", false)
	if err != nil {
		t.Fatalf("self-dispatch failed: %v", err)
	}
	select {
	case got := <-ch:
		if got.Text != "echo" {
			t.Errorf("self-dispatch wrong text: %q", got.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("self-dispatch didn't deliver")
	}
}

func TestConcurrentDispatch(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	_, macTok, _ := h.AddDevice(acc.ID, "Mac", RoleEither)
	win, _, _ := h.AddDevice(acc.ID, "Win", RoleEither)
	ch, unsub := h.Subscribe(win.ID)
	defer unsub()

	const n = 10
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			_, err := h.Dispatch(macTok, win.ID, "msg", "", false)
			errCh <- err
		}(i)
	}

	count := 0
	timeout := time.After(2 * time.Second)
	for count < n {
		select {
		case <-ch:
			count++
		case <-timeout:
			t.Fatalf("only %d/%d msgs delivered", count, n)
		}
	}
}
```

- [ ] **Step 2: 实现 Subscribe + Dispatch + ScrubStale**

追加到 `hub.go`:

```go
const (
	subBufferSize    = 16
	scrubInterval    = time.Minute
	scrubInactiveAge = 30 * time.Minute
)

// Subscribe registers an SSE channel for deviceID. Any previous subscription
// for the same device is closed and replaced. The returned unsubscribe
// function should be called when the SSE handler exits.
func (h *Hub) Subscribe(deviceID string) (<-chan *Message, func()) {
	ch := make(chan *Message, subBufferSize)
	h.mu.Lock()
	if old, ok := h.subs[deviceID]; ok {
		close(old)
	}
	h.subs[deviceID] = ch
	h.mu.Unlock()

	unsub := func() {
		h.mu.Lock()
		if cur, ok := h.subs[deviceID]; ok && cur == ch {
			delete(h.subs, deviceID)
			close(ch)
		}
		h.mu.Unlock()
	}
	return ch, unsub
}

// Dispatch delivers `text` from the sender (identified by token) to targetID.
// Returns the created Message on success.
func (h *Hub) Dispatch(senderToken, targetID, text, requestID string, preserveClipboard bool) (*Message, error) {
	sender, err := h.ResolveDeviceByToken(senderToken)
	if err != nil {
		return nil, err
	}
	h.mu.RLock()
	target, ok := h.devices[targetID]
	if !ok {
		h.mu.RUnlock()
		return nil, ErrDeviceNotFound
	}
	if target.AccountID != sender.AccountID {
		h.mu.RUnlock()
		return nil, ErrCrossAccount
	}
	ch, online := h.subs[targetID]
	h.mu.RUnlock()
	if !online {
		return nil, ErrReceiverOffline
	}

	msg := &Message{
		ID:                "msg-" + shortID(),
		Text:              text,
		FromDevice:        sender.ID,
		RequestID:         requestID,
		PreserveClipboard: preserveClipboard,
		CreatedAt:         time.Now().UTC(),
	}

	select {
	case ch <- msg:
	default:
		return nil, ErrBackpressure
	}

	h.mu.Lock()
	if d, ok := h.devices[sender.ID]; ok {
		d.LastSeen = msg.CreatedAt
	}
	if d, ok := h.devices[target.ID]; ok {
		d.LastSeen = msg.CreatedAt
	}
	h.mu.Unlock()

	return msg, nil
}

// RunScrubber periodically drops inactive subscriber channels. Blocks until
// ctx is canceled.
func (h *Hub) RunScrubber(ctx context.Context) {
	ticker := time.NewTicker(scrubInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.scrubOnce()
		}
	}
}

func (h *Hub) scrubOnce() {
	cutoff := time.Now().UTC().Add(-scrubInactiveAge)
	h.mu.Lock()
	for id, ch := range h.subs {
		if d, ok := h.devices[id]; ok && d.LastSeen.Before(cutoff) {
			close(ch)
			delete(h.subs, id)
		}
	}
	h.mu.Unlock()
}
```

加 import: `"context"` 到 `hub.go` 顶部。

- [ ] **Step 3: 跑测试**

```bash
cd relay && go test ./internal/hub/... -v
```

Expected: 全部 18+8 = 26 个 test 过

- [ ] **Step 4: Commit**

```bash
git add relay/internal/hub/hub.go relay/internal/hub/hub_test.go
git commit -m "feat(relay/hub): Subscribe + Dispatch + 后台扫尸

- Subscribe 自动 close 旧 channel(reconnect 场景)
- Dispatch 校验 sender token + 跨 account + offline + backpressure
- 16 buffer 防止 receiver 卡住堆积
- self-dispatch 允许(自检)
- RunScrubber 每分钟清 30 分钟无活动的 sub

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

**Phase R0 完工标志:** `cd relay && go test ./...` 全过(26+ tests)。`make -C relay test` 包括 linux 跨编译验证通过。

---

## Phase R1 — Relay HTTP API

7 个 task,完成后 `make -C relay test` 全过(含 httptest 集成),本地 curl 可完整跑 dispatch→subscribe 流程。

### Task R1.1: Auth middleware

**Files:** Create `relay/internal/server/auth.go`, `relay/internal/server/auth_test.go`

- [ ] **Step 1: 测试**

`relay/internal/server/auth_test.go`:

```go
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
```

- [ ] **Step 2: 实现**

`relay/internal/server/auth.go`:

```go
package server

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

const bearerPrefix = "Bearer "

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, bearerPrefix) {
		return ""
	}
	return h[len(bearerPrefix):]
}

func requireAdmin(adminToken string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := extractBearer(r)
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(adminToken)) != 1 {
			writeJSON(w, 401, map[string]string{"error": "invalid_admin_token"})
			return
		}
		next(w, r)
	}
}

type deviceHandler func(w http.ResponseWriter, r *http.Request, d *hub.Device)

func requireDevice(h *hub.Hub, next deviceHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := extractBearer(r)
		if tok == "" {
			writeJSON(w, 401, map[string]string{"error": "missing_token"})
			return
		}
		dev, err := h.ResolveDeviceByToken(tok)
		if err != nil {
			writeJSON(w, 401, map[string]string{"error": "invalid_sender_token"})
			return
		}
		next(w, r, dev)
	}
}

// writeJSON is the shared response helper used by all handlers.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
```

- [ ] **Step 3: 跑**

```bash
cd relay && go test ./internal/server/...
```

Expected: 5 个 test 全过

- [ ] **Step 4: Commit**

```bash
git add relay/internal/server/auth.go relay/internal/server/auth_test.go
git commit -m "feat(relay/server): admin / device token middleware

- requireAdmin 用 constant-time 比较
- requireDevice 走 Hub.ResolveDeviceByToken(token cache 已优化)
- writeJSON 统一响应 helper

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R1.2: SSE writer 工具

**Files:** Create `relay/internal/server/sse.go`, `relay/internal/server/sse_test.go`

- [ ] **Step 1: 测试**

`relay/internal/server/sse_test.go`:

```go
package server

import (
	"bytes"
	"strings"
	"testing"
)

type recordingFlusher struct {
	bytes.Buffer
	flushes int
}

func (r *recordingFlusher) Flush() { r.flushes++ }

func TestSSEWriteFrame(t *testing.T) {
	rf := &recordingFlusher{}
	w := &sseWriter{out: rf, flusher: rf}
	w.frame("msg-1", "inject", `{"text":"hello"}`)
	out := rf.String()
	if !strings.Contains(out, "id: msg-1\n") {
		t.Errorf("missing id: %q", out)
	}
	if !strings.Contains(out, "event: inject\n") {
		t.Errorf("missing event: %q", out)
	}
	if !strings.Contains(out, `data: {"text":"hello"}`+"\n") {
		t.Errorf("missing data: %q", out)
	}
	if !strings.HasSuffix(out, "\n\n") {
		t.Errorf("missing terminating blank line: %q", out)
	}
	if rf.flushes != 1 {
		t.Errorf("flushes = %d, want 1", rf.flushes)
	}
}

func TestSSEHeartbeat(t *testing.T) {
	rf := &recordingFlusher{}
	w := &sseWriter{out: rf, flusher: rf}
	w.heartbeat()
	if !strings.HasPrefix(rf.String(), ":") {
		t.Errorf("heartbeat should start with comment colon: %q", rf.String())
	}
	if !strings.HasSuffix(rf.String(), "\n\n") {
		t.Errorf("heartbeat missing terminator: %q", rf.String())
	}
}
```

- [ ] **Step 2: 实现**

`relay/internal/server/sse.go`:

```go
package server

import (
	"fmt"
	"io"
	"net/http"
)

type sseWriter struct {
	out     io.Writer
	flusher http.Flusher
}

func newSSEWriter(w http.ResponseWriter) *sseWriter {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	f, _ := w.(http.Flusher)
	return &sseWriter{out: w, flusher: f}
}

func (s *sseWriter) frame(id, event, data string) error {
	if _, err := fmt.Fprintf(s.out, "id: %s\nevent: %s\ndata: %s\n\n", id, event, data); err != nil {
		return err
	}
	s.flushIf()
	return nil
}

func (s *sseWriter) heartbeat() error {
	if _, err := fmt.Fprintf(s.out, ": ping\n\n"); err != nil {
		return err
	}
	s.flushIf()
	return nil
}

func (s *sseWriter) flushIf() {
	if s.flusher != nil {
		s.flusher.Flush()
	}
}
```

- [ ] **Step 3: 跑**

```bash
cd relay && go test ./internal/server/...
```

Expected: 7 个 test 全过

- [ ] **Step 4: Commit**

```bash
git add relay/internal/server/sse.go relay/internal/server/sse_test.go
git commit -m "feat(relay/server): SSE writer + heartbeat

- frame() 写 id/event/data + 终止空行,自动 flush
- heartbeat() 写 SSE 注释行,防中间代理切 idle 连接

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R1.3: /healthz + /v1/ping handler

**Files:** Create `relay/internal/server/server.go`, `relay/internal/server/handlers_health.go`

- [ ] **Step 1: 实现 server core + health handlers**

`relay/internal/server/server.go`:

```go
package server

import (
	"net/http"
	"time"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

type Options struct {
	Hub        *hub.Hub
	AdminToken string
	Version    string
}

type Server struct {
	opts    Options
	started time.Time
}

func New(opts Options) *Server {
	return &Server{opts: opts, started: time.Now().UTC()}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/v1/ping", requireDevice(s.opts.Hub, s.handlePing))
	return mux
}
```

`relay/internal/server/handlers_health.go`:

```go
package server

import (
	"net/http"
	"time"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"ok":         true,
		"version":    s.opts.Version,
		"uptime_sec": int(time.Since(s.started).Seconds()),
	})
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request, d *hub.Device) {
	writeJSON(w, 200, map[string]any{
		"ok":          true,
		"device_id":   d.ID,
		"account_id":  d.AccountID,
		"server_time": time.Now().UTC().Format(time.RFC3339),
	})
}
```

- [ ] **Step 2: 测试**

追加到 `server_test.go`(创建该文件):

`relay/internal/server/server_test.go`:

```go
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
```

- [ ] **Step 3: 跑**

```bash
cd relay && go test ./internal/server/...
```

Expected: 10 个 test 全过

- [ ] **Step 4: Commit**

```bash
git add relay/internal/server/server.go relay/internal/server/handlers_health.go \
        relay/internal/server/server_test.go
git commit -m "feat(relay/server): server core + /healthz + /v1/ping

- Options + Server + Handler() 注册 mux
- /healthz 无 auth,返回 ok+version+uptime
- /v1/ping device token auth,返回 device_id/account_id/server_time

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R1.4: Admin handlers

**Files:** Create `relay/internal/server/handlers_admin.go`,修改 `server.go` 注册路由,加测试

- [ ] **Step 1: 实现**

`relay/internal/server/handlers_admin.go`:

```go
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

func (s *Server) handleAdminCreateAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	acc, err := s.opts.Hub.AddAccount(req.Name)
	if errors.Is(err, hub.ErrAccountNameRequired) {
		writeJSON(w, 400, map[string]string{"error": "name_required"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "internal"})
		return
	}
	writeJSON(w, 201, acc)
}

func (s *Server) handleAdminListAccounts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"accounts": s.opts.Hub.ListAccounts()})
}

func (s *Server) handleAdminCreateDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID string   `json:"account_id"`
		Label     string   `json:"label"`
		Role      hub.Role `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	if req.AccountID == "" {
		writeJSON(w, 400, map[string]string{"error": "account_id_required"})
		return
	}
	dev, tok, err := s.opts.Hub.AddDevice(req.AccountID, req.Label, req.Role)
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
		"account_id":   dev.AccountID,
		"label":        dev.Label,
		"role":         dev.Role,
		"created_at":   dev.CreatedAt,
	})
}

func (s *Server) handleAdminListDevices(w http.ResponseWriter, r *http.Request) {
	devs := s.opts.Hub.ListDevices()
	out := make([]map[string]any, 0, len(devs))
	for _, d := range devs {
		out = append(out, map[string]any{
			"id":         d.ID,
			"account_id": d.AccountID,
			"label":      d.Label,
			"role":       d.Role,
			"created_at": d.CreatedAt,
			"last_seen":  d.LastSeen,
		})
	}
	writeJSON(w, 200, map[string]any{"devices": out})
}

func (s *Server) handleAdminRotateDevice(w http.ResponseWriter, r *http.Request) {
	id := extractDeviceID(r.URL.Path, "/v1/admin/devices/", "/rotate")
	tok, err := s.opts.Hub.RotateDeviceToken(id)
	if errors.Is(err, hub.ErrDeviceNotFound) {
		writeJSON(w, 404, map[string]string{"error": "device_not_found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "internal"})
		return
	}
	writeJSON(w, 200, map[string]string{"device_token": tok})
}

func (s *Server) handleAdminDeleteDevice(w http.ResponseWriter, r *http.Request) {
	id := extractDeviceID(r.URL.Path, "/v1/admin/devices/", "")
	if err := s.opts.Hub.DeleteDevice(id); err != nil {
		if errors.Is(err, hub.ErrDeviceNotFound) {
			writeJSON(w, 404, map[string]string{"error": "device_not_found"})
			return
		}
		writeJSON(w, 500, map[string]string{"error": "internal"})
		return
	}
	w.WriteHeader(204)
}

func extractDeviceID(path, prefix, suffix string) string {
	p := strings.TrimPrefix(path, prefix)
	return strings.TrimSuffix(p, suffix)
}
```

- [ ] **Step 2: 注册路由**

更新 `relay/internal/server/server.go` 的 `Handler()`:

```go
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/v1/ping", requireDevice(s.opts.Hub, s.handlePing))

	mux.HandleFunc("/v1/admin/accounts", requireAdmin(s.opts.AdminToken,
		func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case "POST":
				s.handleAdminCreateAccount(w, r)
			case "GET":
				s.handleAdminListAccounts(w, r)
			default:
				w.WriteHeader(405)
			}
		}))

	mux.HandleFunc("/v1/admin/devices", requireAdmin(s.opts.AdminToken,
		func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case "POST":
				s.handleAdminCreateDevice(w, r)
			case "GET":
				s.handleAdminListDevices(w, r)
			default:
				w.WriteHeader(405)
			}
		}))

	mux.HandleFunc("/v1/admin/devices/", requireAdmin(s.opts.AdminToken,
		func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/rotate"):
				s.handleAdminRotateDevice(w, r)
			case r.Method == "DELETE":
				s.handleAdminDeleteDevice(w, r)
			default:
				w.WriteHeader(405)
			}
		}))
	return mux
}
```

需要把 `"strings"` 加到 imports。

- [ ] **Step 3: 测试**

追加到 `server_test.go`:

```go
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
```

加 `"strings"` 到 `server_test.go` imports。

- [ ] **Step 4: 跑**

```bash
cd relay && go test ./internal/server/...
```

Expected: 17 个 test 全过

- [ ] **Step 5: Commit**

```bash
git add relay/internal/server/handlers_admin.go relay/internal/server/server.go \
        relay/internal/server/server_test.go
git commit -m "feat(relay/server): /v1/admin/{accounts,devices} 所有 CRUD

- POST/GET /v1/admin/accounts
- POST/GET /v1/admin/devices (token 仅创建时返回)
- POST /v1/admin/devices/{id}/rotate
- DELETE /v1/admin/devices/{id}
- 全部 admin token 鉴权

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R1.5: /v1/dispatch handler

**Files:** Create `relay/internal/server/handlers_dispatch.go`,改 `server.go`,加测试

- [ ] **Step 1: 实现**

`relay/internal/server/handlers_dispatch.go`:

```go
package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

const maxTextBytes = 8 * 1024

type dispatchRequest struct {
	TargetDeviceID    string `json:"target_device_id"`
	Text              string `json:"text"`
	RequestID         string `json:"request_id"`
	PreserveClipboard bool   `json:"preserve_clipboard"`
}

func (s *Server) handleDispatch(w http.ResponseWriter, r *http.Request, sender *hub.Device) {
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxTextBytes)*2)
	var req dispatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	if len(req.Text) > maxTextBytes {
		writeJSON(w, 413, map[string]any{"error": "text_too_large", "max_bytes": maxTextBytes})
		return
	}
	// Re-derive sender's token from header (we already have sender struct from middleware,
	// but Dispatch wants the token to re-resolve internally so the path is uniform).
	// Optimization: expose Hub.DispatchWithSender(sender, ...) to skip token re-lookup.
	// For v1 we keep the API symmetric.
	tok := extractBearer(r)
	msg, err := s.opts.Hub.Dispatch(tok, req.TargetDeviceID, req.Text, req.RequestID, req.PreserveClipboard)
	switch {
	case errors.Is(err, hub.ErrInvalidToken):
		writeJSON(w, 401, map[string]string{"error": "invalid_sender_token"})
	case errors.Is(err, hub.ErrDeviceNotFound):
		writeJSON(w, 404, map[string]string{"error": "target_not_found"})
	case errors.Is(err, hub.ErrCrossAccount):
		writeJSON(w, 403, map[string]string{"error": "cross_account"})
	case errors.Is(err, hub.ErrReceiverOffline):
		writeJSON(w, 503, map[string]string{"error": "receiver_offline"})
	case errors.Is(err, hub.ErrBackpressure):
		writeJSON(w, 503, map[string]string{"error": "receiver_backpressure"})
	case err != nil:
		writeJSON(w, 500, map[string]string{"error": "internal"})
	default:
		writeJSON(w, 202, map[string]any{"accepted": true, "message_id": msg.ID})
	}
	_ = sender // sender already validated by middleware, used implicitly via token
}
```

- [ ] **Step 2: 注册路由**

`server.go` 的 `Handler()` 里加:

```go
mux.HandleFunc("/v1/dispatch", requireDevice(s.opts.Hub, s.handleDispatch))
```

- [ ] **Step 3: 测试**

追加到 `server_test.go`:

```go
// dispatchHelper sets up account + 2 devices + subscribe channel, returns sender token + target id + the subscribed channel.
func dispatchHelper(t *testing.T, ts *httptest.Server, admin string) (senderTok, targetID string, ch <-chan *hub.Message, hubRef *hub.Hub) {
	t.Helper()
	_, accBody := adminPOST(t, ts, admin, "/v1/admin/accounts", `{"name":"P"}`)
	accID := accBody["account_id"].(string)
	_, macBody := adminPOST(t, ts, admin, "/v1/admin/devices",
		`{"account_id":"`+accID+`","label":"Mac"}`)
	_, winBody := adminPOST(t, ts, admin, "/v1/admin/devices",
		`{"account_id":"`+accID+`","label":"Win"}`)
	senderTok = macBody["device_token"].(string)
	targetID = winBody["device_id"].(string)
	// Subscribe to target via direct Hub call (not through HTTP — that's R1.6 territory).
	// Reach the hub via test scaffolding.
	return senderTok, targetID, nil, nil
}

// We need access to the Hub for these tests. Update newTestServer to also return hub:
// (newTestServer already does — h is the 2nd return value.)

func TestDispatchHappyPath(t *testing.T) {
	ts, h, admin := newTestServer(t)
	_, accBody := adminPOST(t, ts, admin, "/v1/admin/accounts", `{"name":"P"}`)
	accID := accBody["account_id"].(string)
	_, macBody := adminPOST(t, ts, admin, "/v1/admin/devices",
		`{"account_id":"`+accID+`","label":"Mac"}`)
	_, winBody := adminPOST(t, ts, admin, "/v1/admin/devices",
		`{"account_id":"`+accID+`","label":"Win"}`)
	macTok := macBody["device_token"].(string)
	winID := winBody["device_id"].(string)

	ch, unsub := h.Subscribe(winID)
	defer unsub()

	body := `{"target_device_id":"` + winID + `","text":"hello"}`
	req, _ := http.NewRequest("POST", ts.URL+"/v1/dispatch", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+macTok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 202 {
		t.Errorf("status=%d", resp.StatusCode)
	}
	select {
	case msg := <-ch:
		if msg.Text != "hello" {
			t.Errorf("wrong msg: %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("no delivery")
	}
}

func TestDispatchInvalidToken(t *testing.T) {
	ts, _, _ := newTestServer(t)
	req, _ := http.NewRequest("POST", ts.URL+"/v1/dispatch",
		strings.NewReader(`{"target_device_id":"x","text":"hi"}`))
	req.Header.Set("Authorization", "Bearer bogus")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 401 {
		t.Errorf("status=%d", resp.StatusCode)
	}
}

func TestDispatchCrossAccount(t *testing.T) {
	ts, h, admin := newTestServer(t)
	_, a, _ := adminPOST(t, ts, admin, "/v1/admin/accounts", `{"name":"A"}`), 0, 0
	accA := a["account_id"].(string)
	_, b := adminPOST(t, ts, admin, "/v1/admin/accounts", `{"name":"B"}`)
	accB := b["account_id"].(string)
	_, macA := adminPOST(t, ts, admin, "/v1/admin/devices", `{"account_id":"`+accA+`","label":"MacA"}`)
	_, winB := adminPOST(t, ts, admin, "/v1/admin/devices", `{"account_id":"`+accB+`","label":"WinB"}`)
	macTok := macA["device_token"].(string)
	winID := winB["device_id"].(string)
	_, unsub := h.Subscribe(winID)
	defer unsub()
	body := `{"target_device_id":"` + winID + `","text":"x"}`
	req, _ := http.NewRequest("POST", ts.URL+"/v1/dispatch", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+macTok)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 403 {
		t.Errorf("status=%d", resp.StatusCode)
	}
}

func TestDispatchTargetNotFound(t *testing.T) {
	ts, _, admin := newTestServer(t)
	_, accBody := adminPOST(t, ts, admin, "/v1/admin/accounts", `{"name":"P"}`)
	_, macBody := adminPOST(t, ts, admin, "/v1/admin/devices",
		`{"account_id":"`+accBody["account_id"].(string)+`","label":"Mac"}`)
	tok := macBody["device_token"].(string)
	body := `{"target_device_id":"dev-missing","text":"x"}`
	req, _ := http.NewRequest("POST", ts.URL+"/v1/dispatch", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 404 {
		t.Errorf("status=%d", resp.StatusCode)
	}
}

func TestDispatchReceiverOffline(t *testing.T) {
	ts, _, admin := newTestServer(t)
	_, accBody := adminPOST(t, ts, admin, "/v1/admin/accounts", `{"name":"P"}`)
	accID := accBody["account_id"].(string)
	_, macBody := adminPOST(t, ts, admin, "/v1/admin/devices",
		`{"account_id":"`+accID+`","label":"Mac"}`)
	_, winBody := adminPOST(t, ts, admin, "/v1/admin/devices",
		`{"account_id":"`+accID+`","label":"Win"}`)
	tok := macBody["device_token"].(string)
	winID := winBody["device_id"].(string)
	// no subscribe -> offline
	body := `{"target_device_id":"` + winID + `","text":"x"}`
	req, _ := http.NewRequest("POST", ts.URL+"/v1/dispatch", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 503 {
		t.Errorf("status=%d", resp.StatusCode)
	}
}

func TestDispatchTextTooLarge(t *testing.T) {
	ts, h, admin := newTestServer(t)
	_, accBody := adminPOST(t, ts, admin, "/v1/admin/accounts", `{"name":"P"}`)
	accID := accBody["account_id"].(string)
	_, macBody := adminPOST(t, ts, admin, "/v1/admin/devices",
		`{"account_id":"`+accID+`","label":"Mac"}`)
	_, winBody := adminPOST(t, ts, admin, "/v1/admin/devices",
		`{"account_id":"`+accID+`","label":"Win"}`)
	tok := macBody["device_token"].(string)
	winID := winBody["device_id"].(string)
	_, unsub := h.Subscribe(winID)
	defer unsub()
	big := strings.Repeat("x", 9000)
	body := `{"target_device_id":"` + winID + `","text":"` + big + `"}`
	req, _ := http.NewRequest("POST", ts.URL+"/v1/dispatch", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 413 {
		t.Errorf("status=%d", resp.StatusCode)
	}
}
```

加 `"time"` import 到 `server_test.go`。删除 `dispatchHelper` 函数(不再用),保留 inline 设置。

- [ ] **Step 4: 跑**

```bash
cd relay && go test ./internal/server/...
```

Expected: 24 个 test 全过(11 admin/health + 6 dispatch + 7 auth/sse)

- [ ] **Step 5: Commit**

```bash
git add relay/internal/server/handlers_dispatch.go relay/internal/server/server.go \
        relay/internal/server/server_test.go
git commit -m "feat(relay/server): POST /v1/dispatch + 全部状态码

错误映射:401 invalid_sender_token / 403 cross_account /
404 target_not_found / 413 text_too_large / 503 offline+backpressure。
成功返回 202 + message_id。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R1.6: /v1/subscribe SSE handler

**Files:** Create `relay/internal/server/handlers_subscribe.go`,改 `server.go`,加测试

- [ ] **Step 1: 实现**

`relay/internal/server/handlers_subscribe.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

const heartbeatInterval = 25 * time.Second

func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request, d *hub.Device) {
	sw := newSSEWriter(w)
	// Tell client to wait 5s before reconnecting on disconnect
	_, _ = w.Write([]byte("retry: 5000\n\n"))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	ch, unsub := s.opts.Hub.Subscribe(d.ID)
	defer unsub()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := sw.heartbeat(); err != nil {
				return
			}
		case msg, ok := <-ch:
			if !ok {
				return
			}
			payload := map[string]any{
				"text":               msg.Text,
				"request_id":         msg.RequestID,
				"from_device":        msg.FromDevice,
				"preserve_clipboard": msg.PreserveClipboard,
			}
			data, _ := json.Marshal(payload)
			if err := sw.frame(msg.ID, "inject", string(data)); err != nil {
				return
			}
		}
	}
}
```

- [ ] **Step 2: 注册路由**

`server.go` 的 `Handler()` 里加:

```go
mux.HandleFunc("/v1/subscribe", requireDevice(s.opts.Hub, s.handleSubscribe))
```

- [ ] **Step 3: 测试**

追加到 `server_test.go`:

```go
import "bufio"   // ensure in imports

func TestSubscribeRequiresAuth(t *testing.T) {
	ts, _, _ := newTestServer(t)
	resp, _ := http.Get(ts.URL + "/v1/subscribe")
	if resp.StatusCode != 401 {
		t.Errorf("status=%d", resp.StatusCode)
	}
}

func TestSubscribeDeliversDispatchedMessage(t *testing.T) {
	ts, _, admin := newTestServer(t)
	_, accBody := adminPOST(t, ts, admin, "/v1/admin/accounts", `{"name":"P"}`)
	accID := accBody["account_id"].(string)
	_, macBody := adminPOST(t, ts, admin, "/v1/admin/devices",
		`{"account_id":"`+accID+`","label":"Mac"}`)
	_, winBody := adminPOST(t, ts, admin, "/v1/admin/devices",
		`{"account_id":"`+accID+`","label":"Win"}`)
	macTok := macBody["device_token"].(string)
	winTok := winBody["device_token"].(string)
	winID := winBody["device_id"].(string)

	// Subscribe (background goroutine)
	subReq, _ := http.NewRequest("GET", ts.URL+"/v1/subscribe", nil)
	subReq.Header.Set("Authorization", "Bearer "+winTok)
	subResp, err := http.DefaultClient.Do(subReq)
	if err != nil {
		t.Fatal(err)
	}
	defer subResp.Body.Close()
	if subResp.StatusCode != 200 {
		t.Fatalf("subscribe status=%d", subResp.StatusCode)
	}
	if ct := subResp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type=%q", ct)
	}

	// Give subscribe time to register
	time.Sleep(100 * time.Millisecond)

	// Dispatch
	body := `{"target_device_id":"` + winID + `","text":"hello-sse"}`
	dispReq, _ := http.NewRequest("POST", ts.URL+"/v1/dispatch", strings.NewReader(body))
	dispReq.Header.Set("Authorization", "Bearer "+macTok)
	dispResp, err := http.DefaultClient.Do(dispReq)
	if err != nil {
		t.Fatal(err)
	}
	if dispResp.StatusCode != 202 {
		t.Fatalf("dispatch status=%d", dispResp.StatusCode)
	}

	// Read SSE frame from subscribe response
	scanner := bufio.NewScanner(subResp.Body)
	gotID, gotEvent, gotData := "", "", ""
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "id: "):
			gotID = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			gotEvent = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			gotData = strings.TrimPrefix(line, "data: ")
		case line == "" && gotData != "":
			// Frame complete
			goto done
		}
	}
done:
	if gotID == "" || gotEvent != "inject" {
		t.Errorf("bad frame: id=%q event=%q", gotID, gotEvent)
	}
	if !strings.Contains(gotData, `"text":"hello-sse"`) {
		t.Errorf("data missing text: %q", gotData)
	}
}
```

- [ ] **Step 4: 跑**

```bash
cd relay && go test ./internal/server/...
```

Expected: 26 个 test 全过

- [ ] **Step 5: Commit**

```bash
git add relay/internal/server/handlers_subscribe.go relay/internal/server/server.go \
        relay/internal/server/server_test.go
git commit -m "feat(relay/server): GET /v1/subscribe SSE handler

- 长连接 retry:5000 + 25s heartbeat
- 帧格式 id/event/data + 空行
- 客户端断开 -> r.Context().Done() 触发 unsub
- payload: text/request_id/from_device/preserve_clipboard

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R1.7: main.go 串接 + 跑起来

**Files:** Modify `relay/cmd/type4me-relay/main.go`

- [ ] **Step 1: 实现真 main.go**

```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/qiyadeng/type4me/relay/internal/hub"
	"github.com/qiyadeng/type4me/relay/internal/server"
)

var version = "dev"

func main() {
	var stateDir string
	flag.StringVar(&stateDir, "state-dir", "", "directory for state.json (overrides $TYPE4ME_RELAY_STATE_DIR)")
	flag.Parse()

	if len(os.Args) < 2 || os.Args[1] != "serve" {
		fmt.Fprintln(os.Stderr, "usage: type4me-relay serve")
		os.Exit(2)
	}

	admin := os.Getenv("TYPE4ME_RELAY_ADMIN_TOKEN")
	if admin == "" {
		log.Fatal("TYPE4ME_RELAY_ADMIN_TOKEN env var required")
	}
	bind := os.Getenv("TYPE4ME_RELAY_BIND")
	if bind == "" {
		bind = "127.0.0.1:8443"
	}
	if stateDir == "" {
		stateDir = os.Getenv("TYPE4ME_RELAY_STATE_DIR")
		if stateDir == "" {
			stateDir = "/var/lib/type4me-relay"
		}
	}

	h, err := hub.New(filepath.Join(stateDir, "state.json"))
	if err != nil {
		log.Fatalf("hub init: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go h.RunScrubber(ctx)

	srv := server.New(server.Options{
		Hub:        h,
		AdminToken: admin,
		Version:    version,
	})

	httpSrv := &http.Server{
		Addr:              bind,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("type4me-relay %s listening on %s (state: %s)", version, bind, stateDir)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(sctx)
}
```

- [ ] **Step 2: 本地跑一次冒烟**

```bash
cd relay && make build-darwin
mkdir -p /tmp/relay-smoke
TYPE4ME_RELAY_ADMIN_TOKEN=smoke-admin \
TYPE4ME_RELAY_STATE_DIR=/tmp/relay-smoke \
./dist/type4me-relay-darwin-arm64 serve &
RELAY_PID=$!
sleep 1
curl -s http://127.0.0.1:8443/healthz
echo
ACCT=$(curl -s -X POST -H "Authorization: Bearer smoke-admin" \
  -H "Content-Type: application/json" \
  -d '{"name":"P"}' http://127.0.0.1:8443/v1/admin/accounts | jq -r .account_id)
echo "acct=$ACCT"
kill $RELAY_PID
rm -rf /tmp/relay-smoke
```

Expected: healthz 返回 `{"ok":true,...}`,创建 account 返回非空 ID。

- [ ] **Step 3: 全套测试 + 跨编译**

```bash
make -C relay test
```

Expected: 全过 + `linux cross-compile OK`。

- [ ] **Step 4: Commit**

```bash
git add relay/cmd/type4me-relay/main.go
git commit -m "feat(relay): main.go 串接 hub + server + scrubber

- ENV: ADMIN_TOKEN(必填) / BIND(默认 127.0.0.1:8443) / STATE_DIR
- SIGINT/SIGTERM 优雅关停 + 3s grace
- 后台 RunScrubber goroutine

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

**Phase R1 完工标志:** relay binary 能跑起来,curl 可以完整跑通 account 创建 → device 创建 → dispatch → subscribe 收到 SSE 帧。`make -C relay test` 全过。**强烈建议这里停 1-2 天**,你 curl 自测一下协议手感。

---

## Phase R2 — Mac side relay 支持

5 个 task,完成后 `swift test` 全过(含新 transport 测试),现有 LAN 直连模式不变。

### Task R2.1: OutputTarget 加 mode + relay 字段 + 自定义 decoder

**Files:** Modify `Type4Me/Injection/OutputTarget.swift`, `Type4MeTests/OutputTargetTests.swift`

- [ ] **Step 1: 改 OutputTarget**

替换 `Type4Me/Injection/OutputTarget.swift`:

```swift
import Foundation

/// A configured remote receiver Type4Me can route ASR text to.
///
/// Persistence: serialized into `credentials.json` under `tf_remote_targets`.
/// NOTE (S2): tokens will move to Keychain when the Settings UI lands.
struct OutputTarget: Codable, Equatable, Identifiable, Sendable {
    enum Mode: String, Codable, Sendable {
        case direct
        case relay
    }

    let id: String
    var name: String
    var enabled: Bool
    var matchBundleIds: [String]

    /// Defaults to `.direct` when missing from JSON (old format compatible).
    var mode: Mode = .direct

    // mode == .direct
    var host: String?
    var port: Int?
    var token: String?

    // mode == .relay
    var relayURL: URL?
    var deviceID: String?
    var deviceToken: String?
    var targetDeviceID: String?

    init(id: String, name: String, enabled: Bool, matchBundleIds: [String],
         mode: Mode = .direct,
         host: String? = nil, port: Int? = nil, token: String? = nil,
         relayURL: URL? = nil, deviceID: String? = nil,
         deviceToken: String? = nil, targetDeviceID: String? = nil) {
        self.id = id
        self.name = name
        self.enabled = enabled
        self.matchBundleIds = matchBundleIds
        self.mode = mode
        self.host = host
        self.port = port
        self.token = token
        self.relayURL = relayURL
        self.deviceID = deviceID
        self.deviceToken = deviceToken
        self.targetDeviceID = targetDeviceID
    }

    enum CodingKeys: String, CodingKey {
        case id, name, enabled, matchBundleIds, mode
        case host, port, token
        case relayURL = "relay_url"
        case deviceID = "device_id"
        case deviceToken = "device_token"
        case targetDeviceID = "target_device_id"
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.id = try c.decode(String.self, forKey: .id)
        self.name = try c.decode(String.self, forKey: .name)
        self.enabled = try c.decode(Bool.self, forKey: .enabled)
        self.matchBundleIds = try c.decode([String].self, forKey: .matchBundleIds)
        self.mode = try c.decodeIfPresent(Mode.self, forKey: .mode) ?? .direct
        self.host = try c.decodeIfPresent(String.self, forKey: .host)
        self.port = try c.decodeIfPresent(Int.self, forKey: .port)
        self.token = try c.decodeIfPresent(String.self, forKey: .token)
        self.relayURL = try c.decodeIfPresent(URL.self, forKey: .relayURL)
        self.deviceID = try c.decodeIfPresent(String.self, forKey: .deviceID)
        self.deviceToken = try c.decodeIfPresent(String.self, forKey: .deviceToken)
        self.targetDeviceID = try c.decodeIfPresent(String.self, forKey: .targetDeviceID)
    }

    /// True if the target has all the required fields for its declared mode.
    var isValid: Bool {
        switch mode {
        case .direct:
            return host != nil && port != nil && token != nil
        case .relay:
            return relayURL != nil && deviceID != nil &&
                   deviceToken != nil && targetDeviceID != nil
        }
    }

    /// baseURL for direct mode only. Crashes for relay mode (caller must check).
    var baseURL: URL {
        precondition(mode == .direct, "baseURL is direct-mode only")
        let h = (host!.contains(":") && !host!.contains("[")) ? "[\(host!)]" : host!
        return URL(string: "http://\(h):\(port!)")!
    }
}
```

- [ ] **Step 2: 改 OutputTargetTests**

追加到 `Type4MeTests/OutputTargetTests.swift`:

```swift
func testCodableModeDefaultsToDirectWhenMissing() throws {
    let json = #"""
    {"id":"t","name":"T","enabled":true,"matchBundleIds":[],
     "host":"1.1.1.1","port":80,"token":"tok"}
    """#.data(using: .utf8)!
    let t = try JSONDecoder().decode(OutputTarget.self, from: json)
    XCTAssertEqual(t.mode, .direct)
    XCTAssertTrue(t.isValid)
}

func testCodableRelayMode() throws {
    let json = #"""
    {"id":"t","name":"T","enabled":true,"matchBundleIds":[],"mode":"relay",
     "relay_url":"https://relay.example.com","device_id":"dev-Mac",
     "device_token":"tok","target_device_id":"dev-Win"}
    """#.data(using: .utf8)!
    let t = try JSONDecoder().decode(OutputTarget.self, from: json)
    XCTAssertEqual(t.mode, .relay)
    XCTAssertEqual(t.deviceID, "dev-Mac")
    XCTAssertEqual(t.targetDeviceID, "dev-Win")
    XCTAssertTrue(t.isValid)
}

func testIsValidFailsForRelayMissingFields() throws {
    let json = #"""
    {"id":"t","name":"T","enabled":true,"matchBundleIds":[],"mode":"relay",
     "relay_url":"https://relay.example.com"}
    """#.data(using: .utf8)!
    let t = try JSONDecoder().decode(OutputTarget.self, from: json)
    XCTAssertFalse(t.isValid)
}

func testIsValidFailsForDirectMissingFields() throws {
    let json = #"""
    {"id":"t","name":"T","enabled":true,"matchBundleIds":[]}
    """#.data(using: .utf8)!
    let t = try JSONDecoder().decode(OutputTarget.self, from: json)
    XCTAssertEqual(t.mode, .direct)
    XCTAssertFalse(t.isValid)
}
```

- [ ] **Step 3: 检查现有 OutputTargetStore 解析仍兼容**

`OutputTargetStore.load()` 当前手写解析 JSON。改为用 Codable + isValid 筛:

替换 `Type4Me/Services/OutputTargetStore.swift` 的 `load()` 方法:

```swift
func load() -> [OutputTarget] {
    guard let data = try? Data(contentsOf: credentialsFile),
          let dict = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
          let arr = dict[Self.jsonKey] as? [[String: Any]]
    else { return [] }

    var out: [OutputTarget] = []
    for entry in arr {
        guard let entryData = try? JSONSerialization.data(withJSONObject: entry),
              let target = try? JSONDecoder().decode(OutputTarget.self, from: entryData),
              target.isValid
        else { continue }
        out.append(target)
    }
    return out
}
```

- [ ] **Step 4: 跑测试**

```bash
swift test --filter OutputTargetTests
swift test --filter OutputTargetStoreTests
```

Expected: 全过(老 3 个 + 新 4 个 OutputTarget;老 4 个 Store 仍通过 —— 老格式不带 mode 字段照样解出 .direct)

- [ ] **Step 5: 跑全套**

```bash
swift test
```

Expected: 全过(180+ 个原有测试不应受影响)

- [ ] **Step 6: Commit**

```bash
git add Type4Me/Injection/OutputTarget.swift \
        Type4Me/Services/OutputTargetStore.swift \
        Type4MeTests/OutputTargetTests.swift
git commit -m "feat(routing): OutputTarget 加 mode + relay 字段

- enum Mode: direct / relay
- mode 缺失默认 .direct(老 credentials.json 100% 兼容)
- 自定义 CodingKeys + decoder
- isValid 按 mode 校验必填字段
- OutputTargetStore 改用 Codable 解析,不再手写 guard let

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R2.2: RemoteTransport protocol

**Files:** Create `Type4Me/Injection/RemoteTransport.swift`

简单 protocol 定义,无测试(后续 transport 实现会有测试)。

- [ ] **Step 1: 写 protocol**

`Type4Me/Injection/RemoteTransport.swift`:

```swift
import Foundation

/// Abstracts the "send text to a remote endpoint" detail away from
/// RemoteHTTPSink. Two implementations:
///
/// - DirectTransport: POSTs to a receiver's HTTP server on a known host:port
/// - RelayTransport: POSTs to a relay server's /v1/dispatch endpoint
///
/// Synchronous: blocks the calling thread for up to ~800 ms.
/// RemoteHTTPSink already calls inject from a Task.detached, so blocking is safe.
protocol RemoteTransport: Sendable {
    /// Send `text` to the configured remote. Return true on success.
    /// Failure (network error, auth error, anything non-success) returns false;
    /// caller (RemoteHTTPSink) writes text to clipboard and reports .copiedToClipboard.
    func dispatch(text: String, requestID: String, preserveClipboard: Bool) -> Bool
}
```

- [ ] **Step 2: 编译验证**

```bash
swift build 2>&1 | tail -3
```

Expected: `Build complete!`

- [ ] **Step 3: Commit**

```bash
git add Type4Me/Injection/RemoteTransport.swift
git commit -m "feat(routing): RemoteTransport protocol

DirectTransport (R2.3) + RelayTransport (R2.4) will implement this.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R2.3: DirectTransport(从 RemoteHTTPSink 提取)

**Files:** Create `Type4Me/Injection/DirectTransport.swift`, `Type4MeTests/DirectTransportTests.swift`(从 `RemoteHTTPSinkTests` rename + 调整)

- [ ] **Step 1: 提取代码到 DirectTransport.swift**

`Type4Me/Injection/DirectTransport.swift`:

```swift
import Foundation

/// Sends text via direct HTTP POST to a receiver's listener (LAN mode).
final class DirectTransport: RemoteTransport, @unchecked Sendable {
    private let target: OutputTarget
    private let timeout: TimeInterval
    private let session: URLSession

    init(target: OutputTarget, timeout: TimeInterval = 0.8,
         session: URLSession = .shared) {
        precondition(target.mode == .direct, "DirectTransport requires .direct target")
        self.target = target
        self.timeout = timeout
        self.session = session
    }

    func dispatch(text: String, requestID: String, preserveClipboard: Bool) -> Bool {
        let url = target.baseURL.appendingPathComponent("inject")
        var req = URLRequest(url: url, timeoutInterval: timeout)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.setValue("Bearer \(target.token!)", forHTTPHeaderField: "Authorization")
        let body: [String: Any] = [
            "text": text,
            "request_id": requestID,
            "preserve_clipboard": preserveClipboard
        ]
        req.httpBody = try? JSONSerialization.data(withJSONObject: body)

        let resultBox = ResultBox()
        let sem = DispatchSemaphore(value: 0)
        let task = session.dataTask(with: req) { data, resp, err in
            resultBox.set((data, resp, err))
            sem.signal()
        }
        task.resume()
        _ = sem.wait(timeout: .now() + timeout + 0.2)
        let result = resultBox.get()
        if result.1 == nil && result.2 == nil {
            task.cancel()
        }
        let finalResult = resultBox.get()
        if finalResult.2 != nil { return false }
        guard let http = finalResult.1 as? HTTPURLResponse, http.statusCode == 200 else {
            return false
        }
        guard let data = finalResult.0,
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              (obj["ok"] as? Bool) == true else {
            return false
        }
        return true
    }
}

/// Lock-protected result box for the URLSession callback ↔ caller race.
/// (Same pattern as RemoteHTTPSink had pre-refactor.)
private final class ResultBox: @unchecked Sendable {
    private let lock = NSLock()
    private var value: (Data?, URLResponse?, Error?) = (nil, nil, nil)
    func set(_ v: (Data?, URLResponse?, Error?)) {
        lock.lock(); value = v; lock.unlock()
    }
    func get() -> (Data?, URLResponse?, Error?) {
        lock.lock(); defer { lock.unlock() }
        return value
    }
}
```

- [ ] **Step 2: 写测试 — rename RemoteHTTPSinkTests → DirectTransportTests + 调整**

新建 `Type4MeTests/DirectTransportTests.swift`(从原 `RemoteHTTPSinkTests.swift` 改写):

```swift
import XCTest
import AppKit
@testable import Type4Me

final class DirectTransportTests: XCTestCase {
    private var server: TestHTTPServer!
    private var target: OutputTarget!

    override func setUp() {
        super.setUp()
        server = TestHTTPServer()
        server.start()
        target = OutputTarget(
            id: "test", name: "Test", enabled: true, matchBundleIds: [],
            mode: .direct,
            host: "127.0.0.1", port: server.port, token: "tok-123"
        )
    }

    override func tearDown() {
        server.stop()
        server = nil
        super.tearDown()
    }

    func testDispatchSendsBearerTokenAndJSONBody() {
        server.respond { req in
            XCTAssertEqual(req.method, "POST")
            XCTAssertEqual(req.path, "/inject")
            XCTAssertEqual(req.headers["Authorization"], "Bearer tok-123")
            guard let body = req.body.data(using: .utf8),
                  let parsed = try? JSONSerialization.jsonObject(with: body) as? [String: Any] else {
                XCTFail("body not valid JSON: \(req.body)")
                return TestHTTPResponse(status: 500, body: "")
            }
            XCTAssertEqual(parsed["text"] as? String, "你好")
            return TestHTTPResponse(status: 200, body: #"{"ok":true,"outcome":{"pasted":true}}"#)
        }
        let transport = DirectTransport(target: target)
        XCTAssertTrue(transport.dispatch(text: "你好", requestID: "r1", preserveClipboard: true))
    }

    func testDispatch401ReturnsFalse() {
        server.respond { _ in TestHTTPResponse(status: 401, body: #"{"error":"invalid_token"}"#) }
        let transport = DirectTransport(target: target)
        XCTAssertFalse(transport.dispatch(text: "x", requestID: "r", preserveClipboard: true))
    }

    func testDispatchTimeoutReturnsFalse() {
        server.respond { _ in
            Thread.sleep(forTimeInterval: 2.0)
            return TestHTTPResponse(status: 200, body: "{}")
        }
        let transport = DirectTransport(target: target, timeout: 0.3)
        XCTAssertFalse(transport.dispatch(text: "x", requestID: "r", preserveClipboard: true))
    }

    func testDispatchConnectionRefusedReturnsFalse() {
        let dead = OutputTarget(id: "x", name: "x", enabled: true, matchBundleIds: [],
                                mode: .direct, host: "127.0.0.1", port: 1, token: "t")
        let transport = DirectTransport(target: dead, timeout: 0.3)
        XCTAssertFalse(transport.dispatch(text: "x", requestID: "r", preserveClipboard: true))
    }

    func testDispatchOkFalseReturnsFalse() {
        server.respond { _ in
            TestHTTPResponse(status: 200, body: #"{"ok":false,"outcome":{"pasted":false}}"#)
        }
        let transport = DirectTransport(target: target)
        XCTAssertFalse(transport.dispatch(text: "x", requestID: "r", preserveClipboard: true))
    }
}
```

- [ ] **Step 3: 删除老的 `RemoteHTTPSinkTests.swift`**(R2.5 才会把 RemoteHTTPSink 改完,但 transport 测试已经从 sink 测试 migrate 走了,老文件留着会跟 R2.5 改造冲突。**这一步先 delete**)

```bash
rm Type4MeTests/RemoteHTTPSinkTests.swift
```

注意:`RemoteHTTPSinkClipboardFallbackTests.swift` 保留(那个独立测试剪贴板兜底逻辑,跟 RemoteHTTPSink 的 transport 抽象正交)。

- [ ] **Step 4: build 验证**

```bash
swift build 2>&1 | tail -3
```

Expected: `Build complete!`(此时 RemoteHTTPSink.swift 还是老代码,但 DirectTransport.swift 编译通过)

- [ ] **Step 5: Commit**

```bash
git add Type4Me/Injection/DirectTransport.swift \
        Type4MeTests/DirectTransportTests.swift
git rm Type4MeTests/RemoteHTTPSinkTests.swift
git commit -m "feat(routing): DirectTransport 提取自 RemoteHTTPSink

LAN 直连模式的 HTTP POST 逻辑独立成 RemoteTransport 协议实现。
测试从 RemoteHTTPSinkTests 重写为 DirectTransportTests,5 case 覆盖。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R2.4: RelayTransport

**Files:** Create `Type4Me/Injection/RelayTransport.swift`, `Type4MeTests/RelayTransportTests.swift`

- [ ] **Step 1: 实现**

`Type4Me/Injection/RelayTransport.swift`:

```swift
import Foundation

/// Sends text via HTTP POST to a relay's /v1/dispatch endpoint.
final class RelayTransport: RemoteTransport, @unchecked Sendable {
    private let target: OutputTarget
    private let timeout: TimeInterval
    private let session: URLSession

    init(target: OutputTarget, timeout: TimeInterval = 0.8,
         session: URLSession = .shared) {
        precondition(target.mode == .relay, "RelayTransport requires .relay target")
        self.target = target
        self.timeout = timeout
        self.session = session
    }

    func dispatch(text: String, requestID: String, preserveClipboard: Bool) -> Bool {
        let url = target.relayURL!.appendingPathComponent("v1/dispatch")
        var req = URLRequest(url: url, timeoutInterval: timeout)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.setValue("Bearer \(target.deviceToken!)", forHTTPHeaderField: "Authorization")
        let body: [String: Any] = [
            "target_device_id": target.targetDeviceID!,
            "text": text,
            "request_id": requestID,
            "preserve_clipboard": preserveClipboard
        ]
        req.httpBody = try? JSONSerialization.data(withJSONObject: body)

        let resultBox = ResultBox()
        let sem = DispatchSemaphore(value: 0)
        let task = session.dataTask(with: req) { data, resp, err in
            resultBox.set((data, resp, err))
            sem.signal()
        }
        task.resume()
        _ = sem.wait(timeout: .now() + timeout + 0.2)
        let result = resultBox.get()
        if result.1 == nil && result.2 == nil {
            task.cancel()
        }
        let finalResult = resultBox.get()
        if finalResult.2 != nil { return false }
        guard let http = finalResult.1 as? HTTPURLResponse else { return false }
        // relay returns 202 on accepted; anything else (401/403/404/413/503) is failure
        return http.statusCode == 202
    }
}

private final class ResultBox: @unchecked Sendable {
    private let lock = NSLock()
    private var value: (Data?, URLResponse?, Error?) = (nil, nil, nil)
    func set(_ v: (Data?, URLResponse?, Error?)) {
        lock.lock(); value = v; lock.unlock()
    }
    func get() -> (Data?, URLResponse?, Error?) {
        lock.lock(); defer { lock.unlock() }
        return value
    }
}
```

- [ ] **Step 2: 测试**

`Type4MeTests/RelayTransportTests.swift`:

```swift
import XCTest
@testable import Type4Me

final class RelayTransportTests: XCTestCase {
    private var server: TestHTTPServer!
    private var target: OutputTarget!

    override func setUp() {
        super.setUp()
        server = TestHTTPServer()
        server.start()
        target = OutputTarget(
            id: "rly", name: "Rly", enabled: true, matchBundleIds: [],
            mode: .relay,
            relayURL: URL(string: "http://127.0.0.1:\(server.port)")!,
            deviceID: "dev-Mac",
            deviceToken: "tok-mac",
            targetDeviceID: "dev-Win"
        )
    }
    override func tearDown() { server.stop(); server = nil; super.tearDown() }

    func testDispatchPathAndBody() {
        server.respond { req in
            XCTAssertEqual(req.method, "POST")
            XCTAssertEqual(req.path, "/v1/dispatch")
            XCTAssertEqual(req.headers["Authorization"], "Bearer tok-mac")
            guard let body = req.body.data(using: .utf8),
                  let parsed = try? JSONSerialization.jsonObject(with: body) as? [String: Any] else {
                XCTFail("bad body: \(req.body)")
                return TestHTTPResponse(status: 500, body: "")
            }
            XCTAssertEqual(parsed["target_device_id"] as? String, "dev-Win")
            XCTAssertEqual(parsed["text"] as? String, "你好")
            return TestHTTPResponse(status: 202, body: #"{"accepted":true,"message_id":"msg-1"}"#)
        }
        let t = RelayTransport(target: target)
        XCTAssertTrue(t.dispatch(text: "你好", requestID: "r", preserveClipboard: true))
    }

    func testDispatch401ReturnsFalse() {
        server.respond { _ in TestHTTPResponse(status: 401, body: #"{"error":"invalid_sender_token"}"#) }
        XCTAssertFalse(RelayTransport(target: target).dispatch(text: "x", requestID: "r", preserveClipboard: true))
    }

    func testDispatch403CrossAccountReturnsFalse() {
        server.respond { _ in TestHTTPResponse(status: 403, body: #"{"error":"cross_account"}"#) }
        XCTAssertFalse(RelayTransport(target: target).dispatch(text: "x", requestID: "r", preserveClipboard: true))
    }

    func testDispatch503ReceiverOfflineReturnsFalse() {
        server.respond { _ in TestHTTPResponse(status: 503, body: #"{"error":"receiver_offline"}"#) }
        XCTAssertFalse(RelayTransport(target: target).dispatch(text: "x", requestID: "r", preserveClipboard: true))
    }

    func testDispatchTimeoutReturnsFalse() {
        server.respond { _ in
            Thread.sleep(forTimeInterval: 2.0)
            return TestHTTPResponse(status: 202, body: "{}")
        }
        let t = RelayTransport(target: target, timeout: 0.3)
        XCTAssertFalse(t.dispatch(text: "x", requestID: "r", preserveClipboard: true))
    }

    func testDispatch200NotEnoughIsFailure() {
        // relay returns 202 on accepted; if backend mistakenly returned 200 we treat as failure
        server.respond { _ in TestHTTPResponse(status: 200, body: #"{"accepted":true}"#) }
        XCTAssertFalse(RelayTransport(target: target).dispatch(text: "x", requestID: "r", preserveClipboard: true))
    }
}
```

- [ ] **Step 3: 跑**

```bash
swift test --filter RelayTransportTests
```

Expected: 6 个 test 全过

- [ ] **Step 4: Commit**

```bash
git add Type4Me/Injection/RelayTransport.swift \
        Type4MeTests/RelayTransportTests.swift
git commit -m "feat(routing): RelayTransport — POST /v1/dispatch 到 relay

只有 HTTP 202 视为成功;其它(401/403/404/413/503/timeout)失败,
让 RemoteHTTPSink 落到剪贴板兜底。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R2.5: RemoteHTTPSink 重构为 transport-based

**Files:** Modify `Type4Me/Injection/RemoteHTTPSink.swift`

- [ ] **Step 1: 重写 RemoteHTTPSink.swift**

替换 `Type4Me/Injection/RemoteHTTPSink.swift`:

```swift
import Foundation
import AppKit

/// OutputSink that delegates the actual HTTP transport to RemoteTransport
/// (DirectTransport for LAN mode, RelayTransport for relay mode), and
/// handles Outcome mapping + clipboard fallback.
///
/// Failure handling: ANY transport failure unconditionally writes the text
/// to the system pasteboard and returns .copiedToClipboard. This is the
/// same guarantee as pre-refactor — losing the transcript silently is the
/// worst outcome.
final class RemoteHTTPSink: OutputSink, @unchecked Sendable {
    let target: OutputTarget
    private let transport: RemoteTransport

    init(target: OutputTarget) {
        self.target = target
        switch target.mode {
        case .direct:
            self.transport = DirectTransport(target: target)
        case .relay:
            self.transport = RelayTransport(target: target)
        }
    }

    /// Injection point used by OutputRouter / tests that need a custom transport.
    init(target: OutputTarget, transport: RemoteTransport) {
        self.target = target
        self.transport = transport
    }

    func inject(_ text: String) -> InjectionOutcome {
        let requestID = UUID().uuidString
        if transport.dispatch(text: text, requestID: requestID, preserveClipboard: true) {
            return .inserted
        }
        return copyToClipboardFallback(text)
    }

    private func copyToClipboardFallback(_ text: String) -> InjectionOutcome {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
        return .copiedToClipboard
    }
}
```

- [ ] **Step 2: 检查 OutputRouter 调用兼容**

```bash
grep -n "RemoteHTTPSink(target:" Type4Me/Injection/OutputRouter.swift
```

Expected: 一处 `RemoteHTTPSink(target: t)`,使用单参数 init,自动选 transport。无需改动 OutputRouter。

- [ ] **Step 3: build + 跑全套测试**

```bash
swift build && swift test 2>&1 | grep "Executed [0-9]+ tests" | tail -2
```

Expected: 全部 PASS;test 总数比之前多约 10 个(5 DirectTransport + 4 OutputTarget + 6 RelayTransport - 5 删掉的 RemoteHTTPSinkTests)。

- [ ] **Step 4: Commit**

```bash
git add Type4Me/Injection/RemoteHTTPSink.swift
git commit -m "refactor(routing): RemoteHTTPSink 走 RemoteTransport 抽象

- init(target) 根据 target.mode 自动选 DirectTransport / RelayTransport
- inject() 简化为 dispatch+map:成功 .inserted,失败统一剪贴板兜底
- 测试用的 init(target, transport) 重载允许注入 mock transport
- OutputRouter 调用方式不变(仍 RemoteHTTPSink(target: t))

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

**Phase R2 完工标志:** `swift test` 全过,现有 LAN 直连场景(`mode=direct` 或缺 mode 字段)行为完全不变,新 `mode=relay` 配置走 RelayTransport。Mac 端 relay 支持就绪。

---

## Phase R3 — Win side relay-subscriber

4 个 task。完成后 receiver 支持 mode 二选一,本地 `relay + subscriber + curl dispatch` 可端到端验证。

### Task R3.1: config.Config 加 Mode + relay 字段 + env

**Files:** Modify `receiver/internal/config/config.go`, `receiver/internal/config/config_test.go`

- [ ] **Step 1: 改 Config struct**

替换 `receiver/internal/config/config.go`:

```go
package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// Mode determines whether the receiver listens on a port (LAN mode) or
// subscribes to a relay (relay mode).
type Mode string

const (
	ModeListener        Mode = "listener"
	ModeRelaySubscriber Mode = "relay-subscriber"
)

type Config struct {
	Mode Mode `json:"mode"`

	// Listener mode fields (LAN, existing S0+S1+S3 behavior):
	Port     int    `json:"port"`
	BindAddr string `json:"bind_addr"`
	Name     string `json:"name"`
	Token    string `json:"token"`

	// Relay-subscriber mode fields:
	RelayURL    string `json:"relay_url"`
	DeviceID    string `json:"device_id"`
	DeviceToken string `json:"device_token"`
}

const (
	DefaultPort     = 47318
	DefaultBindAddr = "0.0.0.0"
)

// Load reads config from the given file path, applies env overrides, and
// generates a listener-mode token if missing.
//
// Env vars (in priority over file):
//   - TYPE4ME_MODE          (listener | relay-subscriber)
//   - TYPE4ME_PORT, TYPE4ME_BIND_ADDR, TYPE4ME_TOKEN, TYPE4ME_NAME  (listener)
//   - TYPE4ME_RELAY_URL, TYPE4ME_DEVICE_ID, TYPE4ME_DEVICE_TOKEN     (relay)
func Load(path string) (*Config, error) {
	cfg := &Config{
		Mode:     ModeListener,
		Port:     DefaultPort,
		BindAddr: DefaultBindAddr,
		Name:     hostname(),
	}

	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, cfg)
	}

	if v := os.Getenv("TYPE4ME_MODE"); v != "" {
		cfg.Mode = Mode(v)
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeListener
	}

	if v := os.Getenv("TYPE4ME_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Port = n
		}
	}
	if v := os.Getenv("TYPE4ME_BIND_ADDR"); v != "" {
		cfg.BindAddr = v
	}
	if v := os.Getenv("TYPE4ME_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("TYPE4ME_NAME"); v != "" {
		cfg.Name = v
	}
	if v := os.Getenv("TYPE4ME_RELAY_URL"); v != "" {
		cfg.RelayURL = v
	}
	if v := os.Getenv("TYPE4ME_DEVICE_ID"); v != "" {
		cfg.DeviceID = v
	}
	if v := os.Getenv("TYPE4ME_DEVICE_TOKEN"); v != "" {
		cfg.DeviceToken = v
	}

	// Auto-generate listener token only when in listener mode and missing.
	if cfg.Mode == ModeListener && cfg.Token == "" {
		tok, err := generateToken()
		if err != nil {
			return nil, fmt.Errorf("generate token: %w", err)
		}
		cfg.Token = tok
	}

	// Validate per mode.
	if cfg.Mode == ModeRelaySubscriber {
		if cfg.RelayURL == "" || cfg.DeviceID == "" || cfg.DeviceToken == "" {
			return nil, fmt.Errorf("mode=relay-subscriber requires relay_url, device_id, device_token")
		}
	}

	return cfg, nil
}

func (c *Config) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hostname() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "type4me-receiver"
}
```

- [ ] **Step 2: 追加测试**

追加到 `receiver/internal/config/config_test.go`:

```go
func TestLoadDefaultsToListenerMode(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	os.WriteFile(cfgFile, []byte("{}"), 0600)
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != ModeListener {
		t.Errorf("mode = %q, want listener", cfg.Mode)
	}
}

func TestLoadRelaySubscriberRequiresFields(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	os.WriteFile(cfgFile, []byte(`{"mode":"relay-subscriber"}`), 0600)
	_, err := Load(cfgFile)
	if err == nil {
		t.Error("expected error for missing relay fields, got nil")
	}
}

func TestLoadRelaySubscriberFromEnv(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	os.WriteFile(cfgFile, []byte("{}"), 0600)
	t.Setenv("TYPE4ME_MODE", "relay-subscriber")
	t.Setenv("TYPE4ME_RELAY_URL", "https://relay.example.com")
	t.Setenv("TYPE4ME_DEVICE_ID", "dev-Win")
	t.Setenv("TYPE4ME_DEVICE_TOKEN", "tok-win")
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != ModeRelaySubscriber {
		t.Errorf("mode = %q", cfg.Mode)
	}
	if cfg.RelayURL != "https://relay.example.com" || cfg.DeviceID != "dev-Win" {
		t.Errorf("relay fields wrong: %+v", cfg)
	}
}

func TestLoadListenerModeStillGeneratesToken(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	os.WriteFile(cfgFile, []byte("{}"), 0600)
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != ModeListener {
		t.Fatal("not listener mode")
	}
	if len(cfg.Token) < 32 {
		t.Errorf("token len=%d", len(cfg.Token))
	}
}

func TestLoadRelayModeDoesNotGenerateListenerToken(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	os.WriteFile(cfgFile, []byte(`{"mode":"relay-subscriber","relay_url":"x","device_id":"y","device_token":"z"}`), 0600)
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "" {
		t.Errorf("listener token should be empty in relay mode, got %q", cfg.Token)
	}
}
```

- [ ] **Step 3: 跑**

```bash
cd receiver && go test ./internal/config/...
```

Expected: 全 8 个 test(原 3 + 新 5)过

- [ ] **Step 4: Commit**

```bash
git add receiver/internal/config/config.go receiver/internal/config/config_test.go
git commit -m "feat(receiver/config): Mode 字段 + relay 字段 + env vars

- Mode: listener (默认) / relay-subscriber
- relay 模式必填 relay_url / device_id / device_token,缺则启动 fatal
- listener 模式自动生成 token,relay 模式不生成
- 老 config.json 不带 mode -> 默认 listener,完全兼容

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R3.2: SSE 解析工具

**Files:** Create `receiver/internal/relay/sse.go`, `receiver/internal/relay/sse_test.go`

抽出 SSE 解析作为独立小单元,易测。

- [ ] **Step 1: 测试**

`receiver/internal/relay/sse_test.go`:

```go
package relay

import (
	"strings"
	"testing"
)

func TestParseSSEStream(t *testing.T) {
	stream := "id: msg-1\nevent: inject\ndata: {\"text\":\"hello\"}\n\n" +
		": ping\n\n" +
		"id: msg-2\nevent: inject\ndata: {\"text\":\"world\"}\n\n"
	var events []SSEEvent
	err := ParseSSE(strings.NewReader(stream), func(e SSEEvent) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(events), events)
	}
	if events[0].ID != "msg-1" || events[0].Event != "inject" {
		t.Errorf("event 0: %+v", events[0])
	}
	if events[0].Data != `{"text":"hello"}` {
		t.Errorf("event 0 data: %q", events[0].Data)
	}
	if events[1].ID != "msg-2" {
		t.Errorf("event 1: %+v", events[1])
	}
}

func TestParseSSESkipsHeartbeatComments(t *testing.T) {
	stream := ": ping\n\n: another comment\n\nid: m\nevent: e\ndata: d\n\n"
	count := 0
	_ = ParseSSE(strings.NewReader(stream), func(e SSEEvent) error {
		count++
		return nil
	})
	if count != 1 {
		t.Errorf("got %d events, want 1", count)
	}
}

func TestParseSSEMultilineDataConcatenates(t *testing.T) {
	stream := "id: m\nevent: e\ndata: line1\ndata: line2\n\n"
	var got SSEEvent
	_ = ParseSSE(strings.NewReader(stream), func(e SSEEvent) error {
		got = e
		return nil
	})
	if got.Data != "line1\nline2" {
		t.Errorf("data = %q, want line1\\nline2", got.Data)
	}
}
```

- [ ] **Step 2: 实现**

`receiver/internal/relay/sse.go`:

```go
package relay

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

type SSEEvent struct {
	ID    string
	Event string
	Data  string
}

// ParseSSE reads SSE-formatted events from r and invokes onEvent for each
// complete event. Comments (lines starting with ":") are skipped.
// Returns io.EOF or another error when the stream ends.
//
// If onEvent returns a non-nil error, parsing stops with that error.
func ParseSSE(r io.Reader, onEvent func(SSEEvent) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 32*1024), 1024*1024)
	var cur SSEEvent
	var dataLines []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if cur.ID != "" || cur.Event != "" || len(dataLines) > 0 {
				cur.Data = strings.Join(dataLines, "\n")
				if err := onEvent(cur); err != nil {
					return err
				}
			}
			cur = SSEEvent{}
			dataLines = nil
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // SSE comment / heartbeat
		}
		switch {
		case strings.HasPrefix(line, "id:"):
			cur.ID = strings.TrimSpace(line[3:])
		case strings.HasPrefix(line, "event:"):
			cur.Event = strings.TrimSpace(line[6:])
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(line[5:]))
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
```

- [ ] **Step 3: 跑**

```bash
cd receiver && go test ./internal/relay/...
```

Expected: 3 个 test 过

- [ ] **Step 4: Commit**

```bash
git add receiver/internal/relay/sse.go receiver/internal/relay/sse_test.go
git commit -m "feat(receiver/relay): SSE 流式解析器

- 解析 id: / event: / data: 三字段
- 跳过 \":\" 开头注释行(心跳)
- 多行 data 字段用 \\n 拼接
- 跨平台,纯 stdlib + bufio.Scanner

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R3.3: Subscriber 实现 + 自动重连

**Files:** Create `receiver/internal/relay/subscriber.go`, `receiver/internal/relay/subscriber_test.go`

- [ ] **Step 1: 测试**

`receiver/internal/relay/subscriber_test.go`:

```go
package relay

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qiyadeng/type4me/receiver/internal/inject"
)

// mockInjector records calls and lets tests control outcome.
type mockInjector struct {
	mu    sync.Mutex
	calls []string
}

func (m *mockInjector) Inject(text string) (inject.Outcome, error) {
	m.mu.Lock()
	m.calls = append(m.calls, text)
	m.mu.Unlock()
	return inject.Outcome{Pasted: true}, nil
}
func (m *mockInjector) Ping() error { return nil }
func (m *mockInjector) Calls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.calls...)
}

func TestSubscriberReceivesAndInjects(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		f, _ := w.(http.Flusher)
		fmt.Fprintf(w, "id: msg-1\nevent: inject\ndata: {\"text\":\"hello\",\"request_id\":\"r1\"}\n\n")
		f.Flush()
		// Keep connection open until context cancelled
		<-r.Context().Done()
	}))
	defer ts.Close()

	inj := &mockInjector{}
	s := &Subscriber{
		RelayURL:    ts.URL,
		DeviceToken: "tok",
		Injector:    inj,
		HTTPClient:  &http.Client{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.Run(ctx)

	calls := inj.Calls()
	if len(calls) != 1 || calls[0] != "hello" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestSubscriber401ReturnsImmediately(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer ts.Close()
	inj := &mockInjector{}
	s := &Subscriber{
		RelayURL: ts.URL, DeviceToken: "bad", Injector: inj,
		HTTPClient: &http.Client{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := s.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 error, got %v", err)
	}
}

func TestSubscriberReconnectsOnDisconnect(t *testing.T) {
	var connectCount int32
	var mu sync.Mutex
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connectCount++
		count := connectCount
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		f, _ := w.(http.Flusher)
		fmt.Fprintf(w, "id: msg-%d\nevent: inject\ndata: {\"text\":\"hello-%d\"}\n\n", count, count)
		f.Flush()
		// First connection closes immediately, second stays open
		if count == 1 {
			return
		}
		<-r.Context().Done()
	}))
	defer ts.Close()

	inj := &mockInjector{}
	s := &Subscriber{
		RelayURL: ts.URL, DeviceToken: "tok", Injector: inj,
		HTTPClient:   &http.Client{},
		ReconnectMin: 50 * time.Millisecond, // fast reconnect for test
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.Run(ctx)

	calls := inj.Calls()
	if len(calls) < 2 {
		t.Errorf("expected at least 2 calls (reconnect), got %d", len(calls))
	}
}

func TestSubscriberSendsLastEventIDOnReconnect(t *testing.T) {
	var lastIDReceived string
	var mu sync.Mutex
	first := true
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if !first {
			lastIDReceived = r.Header.Get("Last-Event-ID")
		}
		isFirst := first
		first = false
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		f, _ := w.(http.Flusher)
		if isFirst {
			fmt.Fprintf(w, "id: msg-abc\nevent: inject\ndata: {\"text\":\"x\"}\n\n")
			f.Flush()
			return
		}
		<-r.Context().Done()
	}))
	defer ts.Close()

	s := &Subscriber{
		RelayURL: ts.URL, DeviceToken: "tok", Injector: &mockInjector{},
		HTTPClient:   &http.Client{},
		ReconnectMin: 50 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.Run(ctx)

	mu.Lock()
	defer mu.Unlock()
	if lastIDReceived != "msg-abc" {
		t.Errorf("Last-Event-ID on reconnect = %q, want msg-abc", lastIDReceived)
	}
}

func TestSubscriberIgnoresBadJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		f, _ := w.(http.Flusher)
		fmt.Fprintf(w, "id: m1\nevent: inject\ndata: NOT-JSON\n\n")
		f.Flush()
		fmt.Fprintf(w, "id: m2\nevent: inject\ndata: {\"text\":\"good\"}\n\n")
		f.Flush()
		<-r.Context().Done()
	}))
	defer ts.Close()

	inj := &mockInjector{}
	s := &Subscriber{
		RelayURL: ts.URL, DeviceToken: "tok", Injector: inj,
		HTTPClient: &http.Client{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.Run(ctx)

	calls := inj.Calls()
	if len(calls) != 1 || calls[0] != "good" {
		t.Errorf("calls = %+v, expected only 'good'", calls)
	}
}
```

- [ ] **Step 2: 实现**

`receiver/internal/relay/subscriber.go`:

```go
package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/qiyadeng/type4me/receiver/internal/inject"
)

// Subscriber connects to a relay's /v1/subscribe SSE endpoint, parses incoming
// messages, and forwards them to the local Injector.
//
// Reconnects automatically on disconnect with exponential backoff capped at 30s.
// A 401 from the relay terminates the loop (no auto-reconnect on auth failure).
type Subscriber struct {
	RelayURL    string
	DeviceToken string
	Injector    inject.Injector
	HTTPClient  *http.Client

	// ReconnectMin defaults to 1s if zero. Backoff doubles on each failure,
	// capped at 30s.
	ReconnectMin time.Duration
}

type sseInjectPayload struct {
	Text              string `json:"text"`
	RequestID         string `json:"request_id"`
	FromDevice        string `json:"from_device"`
	PreserveClipboard bool   `json:"preserve_clipboard"`
}

// Run blocks until ctx is canceled or a fatal error (401) occurs.
func (s *Subscriber) Run(ctx context.Context) error {
	min := s.ReconnectMin
	if min == 0 {
		min = time.Second
	}
	backoff := min
	var lastEventID string

	for {
		if ctx.Err() != nil {
			return nil
		}
		err := s.connectAndStream(ctx, &lastEventID)
		// Fatal errors break the loop:
		if errors.Is(err, errAuth) {
			return fmt.Errorf("subscribe: %w", err)
		}
		if ctx.Err() != nil {
			return nil
		}
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
}

var errAuth = errors.New("HTTP 401 from relay")

func (s *Subscriber) connectAndStream(ctx context.Context, lastID *string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", s.RelayURL+"/v1/subscribe", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.DeviceToken)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if *lastID != "" {
		req.Header.Set("Last-Event-ID", *lastID)
	}

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return errAuth
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return ParseSSE(resp.Body, func(ev SSEEvent) error {
		if ev.Event != "inject" {
			return nil
		}
		var payload sseInjectPayload
		if err := json.Unmarshal([]byte(ev.Data), &payload); err != nil {
			log.Printf("subscribe: bad data: %v", err)
			return nil
		}
		out, ierr := s.Injector.Inject(payload.Text)
		if ierr != nil {
			log.Printf("inject err: %v (event %s)", ierr, ev.ID)
		} else {
			log.Printf("inject ok=%v reason=%q text-len=%d req=%s event=%s",
				out.Pasted, out.Reason, len(payload.Text), payload.RequestID, ev.ID)
		}
		*lastID = ev.ID
		return nil
	})
}
```

- [ ] **Step 3: 跑**

```bash
cd receiver && go test ./internal/relay/...
```

Expected: 8 个 test 全过(3 SSE + 5 Subscriber)

- [ ] **Step 4: Commit**

```bash
git add receiver/internal/relay/subscriber.go receiver/internal/relay/subscriber_test.go
git commit -m "feat(receiver/relay): Subscriber SSE 客户端 + 自动重连

- 指数 backoff 重连(min 1s 默认,cap 30s)
- 401 -> fatal,不再重连(配置错了重连无意义)
- Last-Event-ID header 在重连时带上(为未来 replay 留口)
- bad JSON 跳过 + 继续,不掉链路
- 每次成功 inject 打访问 log

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R3.4: main.go mode 分叉

**Files:** Modify `receiver/cmd/type4me-receiver/main.go`

把当前 main.go 拆成 `runListener` + `runRelaySubscriber` 两条分支。

- [ ] **Step 1: 改 main.go**

替换 `receiver/cmd/type4me-receiver/main.go`:

```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/qiyadeng/type4me/receiver/internal/config"
	"github.com/qiyadeng/type4me/receiver/internal/inject"
	"github.com/qiyadeng/type4me/receiver/internal/relay"
	"github.com/qiyadeng/type4me/receiver/internal/server"
)

var version = "dev"

func main() {
	var cfgPath string
	flag.StringVar(&cfgPath, "config", defaultConfigPath(), "path to config.json")
	flag.Parse()

	if err := os.MkdirAll(filepath.Dir(cfgPath), 0700); err != nil {
		log.Fatalf("mkdir config dir: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("config load: %v", err)
	}
	if err := cfg.Save(cfgPath); err != nil {
		log.Printf("config save (will retry on changes): %v", err)
	}

	inj := inject.NewPlatform()
	if err := inj.Ping(); err != nil {
		log.Fatalf("inject platform unavailable: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch cfg.Mode {
	case config.ModeListener:
		runListener(ctx, cfg, inj)
	case config.ModeRelaySubscriber:
		runRelaySubscriber(ctx, cfg, inj)
	default:
		log.Fatalf("unknown mode: %s", cfg.Mode)
	}
}

func runListener(ctx context.Context, cfg *config.Config, inj inject.Injector) {
	s := server.New(server.Options{
		Token:    cfg.Token,
		Injector: inj,
		Name:     cfg.Name,
		Platform: runtime.GOOS,
		Version:  version,
	})
	addr := fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.Port)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 3 * time.Second,
	}
	printListenerPairing(cfg, addr)
	go func() {
		log.Printf("listening on %s", addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http: %v", err)
		}
	}()
	<-ctx.Done()
	log.Println("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(sctx)
}

func runRelaySubscriber(ctx context.Context, cfg *config.Config, inj inject.Injector) {
	sub := &relay.Subscriber{
		RelayURL:    cfg.RelayURL,
		DeviceToken: cfg.DeviceToken,
		Injector:    inj,
		HTTPClient:  &http.Client{Timeout: 0}, // SSE 长连,不设总超时
	}
	log.Printf("subscribing to %s as %s (platform=%s)",
		cfg.RelayURL, cfg.DeviceID, runtime.GOOS)
	if err := sub.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("subscriber: %v", err)
	}
	log.Println("subscriber exited")
}

func defaultConfigPath() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support",
			"type4me-receiver", "config.json")
	case "windows":
		appdata := os.Getenv("APPDATA")
		return filepath.Join(appdata, "type4me-receiver", "config.json")
	default:
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "type4me-receiver", "config.json")
	}
}

func printListenerPairing(cfg *config.Config, addr string) {
	urlHost := cfg.BindAddr
	if urlHost == "0.0.0.0" || urlHost == "::" {
		urlHost = "127.0.0.1"
	}
	fmt.Println()
	fmt.Println("================ type4me-receiver pairing (LISTENER MODE) ================")
	fmt.Printf("  Name:    %s\n", cfg.Name)
	fmt.Printf("  Addr:    %s\n", addr)
	fmt.Printf("  Token:   %s\n", cfg.Token)
	fmt.Printf("  URL:     type4me://pair?host=%s&port=%d&token=%s&name=%s&platform=%s\n",
		urlHost, cfg.Port, cfg.Token, cfg.Name, runtime.GOOS)
	fmt.Println("==========================================================================")
	fmt.Println()
}
```

- [ ] **Step 2: 全套测试 + 跨编译**

```bash
make -C receiver test
```

Expected: 全过(includes 之前 windows cross-compile verification)

- [ ] **Step 3: 本地端到端冒烟(relay + subscriber)**

```bash
# 先启动 relay (来自 R1)
mkdir -p /tmp/relay-r3-smoke
TYPE4ME_RELAY_ADMIN_TOKEN=admin-smoke \
TYPE4ME_RELAY_STATE_DIR=/tmp/relay-r3-smoke \
./relay/dist/type4me-relay-darwin-arm64 serve > /tmp/relay-r3.log 2>&1 &
RELAY_PID=$!
sleep 1

# 创 account + 两 device
ACCT=$(curl -s -X POST -H "Authorization: Bearer admin-smoke" \
  -H "Content-Type: application/json" \
  -d '{"name":"E2E"}' http://127.0.0.1:8443/v1/admin/accounts | jq -r .account_id)
MAC=$(curl -s -X POST -H "Authorization: Bearer admin-smoke" \
  -H "Content-Type: application/json" \
  -d "{\"account_id\":\"$ACCT\",\"label\":\"Mac\"}" http://127.0.0.1:8443/v1/admin/devices)
WIN=$(curl -s -X POST -H "Authorization: Bearer admin-smoke" \
  -H "Content-Type: application/json" \
  -d "{\"account_id\":\"$ACCT\",\"label\":\"Win\"}" http://127.0.0.1:8443/v1/admin/devices)
MAC_TOK=$(echo "$MAC" | jq -r .device_token)
WIN_ID=$(echo "$WIN" | jq -r .device_id)
WIN_TOK=$(echo "$WIN" | jq -r .device_token)

# 重建 receiver (R3.4 的 main.go 已支持 subscriber mode)
make -C receiver build-darwin-arm64 > /dev/null

# subscriber 启动
TYPE4ME_MODE=relay-subscriber \
TYPE4ME_RELAY_URL=http://127.0.0.1:8443 \
TYPE4ME_DEVICE_ID=$WIN_ID \
TYPE4ME_DEVICE_TOKEN=$WIN_TOK \
./receiver/dist/type4me-receiver-darwin-arm64 > /tmp/subscriber.log 2>&1 &
SUB_PID=$!
sleep 1

# 保存剪贴板,dispatch,验证
SAVED=$(pbpaste)
TEXT="r3 smoke $(date +%s)"
curl -s -X POST -H "Authorization: Bearer $MAC_TOK" \
  -H "Content-Type: application/json" \
  -d "{\"target_device_id\":\"$WIN_ID\",\"text\":\"$TEXT\"}" \
  http://127.0.0.1:8443/v1/dispatch
sleep 0.5
GOT=$(pbpaste)
echo "$SAVED" | pbcopy

# 清理
kill $RELAY_PID $SUB_PID 2>/dev/null
rm -rf /tmp/relay-r3-smoke

# 报告
if [ "$GOT" = "$TEXT" ]; then echo "R3 E2E PASS"; else echo "R3 E2E FAIL: '$GOT' != '$TEXT'"; cat /tmp/subscriber.log; exit 1; fi
```

Expected: `R3 E2E PASS`

- [ ] **Step 4: Commit**

```bash
git add receiver/cmd/type4me-receiver/main.go
git commit -m "feat(receiver): main.go mode 分叉 listener / relay-subscriber

- 老 listener 路径完全保留(runListener),pairing 信息打印改加 'LISTENER MODE' 标识
- 新 relay-subscriber 路径(runRelaySubscriber)通过 relay.Subscriber 跑 SSE 循环
- ctx.Cancel 时优雅退出

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

**Phase R3 完工标志:** receiver `make test` 全过,本地起 relay + subscriber + curl dispatch 链路通(R3.4 Step 3 的冒烟)。Win 端 binary 跨编译过。

---

## Phase R4 — 部署 artifacts + E2E 脚本

3 个 task,完成后部署文档自洽 + E2E 脚本 PASS。

### Task R4.1: 端到端冒烟脚本

**Files:** Create `scripts/test_relay_e2e.sh`

把 R3.4 Step 3 的手动 shell 流程提炼成可复用脚本。

- [ ] **Step 1: 写脚本**

`scripts/test_relay_e2e.sh`:

```bash
#!/bin/bash
# End-to-end smoke for relay: start relay binary + receiver in subscriber mode,
# curl dispatch, verify clipboard. macOS dev machine only.

set -euo pipefail
cd "$(dirname "$0")/.."

TMP=$(mktemp -d)
SAVED_CLIP=$(pbpaste || true)
ADMIN="admin-$(openssl rand -hex 8)"

cleanup() {
    kill $RELAY_PID $SUB_PID 2>/dev/null || true
    rm -rf "$TMP"
    echo "$SAVED_CLIP" | pbcopy || true
}
trap cleanup EXIT

# Build both binaries
make -C relay build-darwin >/dev/null
make -C receiver build-darwin-arm64 >/dev/null

RELAY=./relay/dist/type4me-relay-darwin-arm64
RECV=./receiver/dist/type4me-receiver-darwin-arm64

# 1. Start relay
TYPE4ME_RELAY_ADMIN_TOKEN="$ADMIN" \
TYPE4ME_RELAY_STATE_DIR="$TMP/state" \
"$RELAY" serve > "$TMP/relay.log" 2>&1 &
RELAY_PID=$!

for i in $(seq 1 30); do
    curl -fs http://127.0.0.1:8443/healthz >/dev/null 2>&1 && break
    sleep 0.1
done

# 2. Create account + 2 devices
ACCT=$(curl -sf -X POST -H "Authorization: Bearer $ADMIN" \
    -H "Content-Type: application/json" \
    -d '{"name":"E2E"}' http://127.0.0.1:8443/v1/admin/accounts | jq -r .account_id)
MAC=$(curl -sf -X POST -H "Authorization: Bearer $ADMIN" \
    -H "Content-Type: application/json" \
    -d "{\"account_id\":\"$ACCT\",\"label\":\"Mac\"}" \
    http://127.0.0.1:8443/v1/admin/devices)
WIN=$(curl -sf -X POST -H "Authorization: Bearer $ADMIN" \
    -H "Content-Type: application/json" \
    -d "{\"account_id\":\"$ACCT\",\"label\":\"Win\"}" \
    http://127.0.0.1:8443/v1/admin/devices)
MAC_TOK=$(echo "$MAC" | jq -r .device_token)
WIN_ID=$(echo "$WIN" | jq -r .device_id)
WIN_TOK=$(echo "$WIN" | jq -r .device_token)

# 3. Start subscriber
TYPE4ME_MODE=relay-subscriber \
TYPE4ME_RELAY_URL=http://127.0.0.1:8443 \
TYPE4ME_DEVICE_ID="$WIN_ID" \
TYPE4ME_DEVICE_TOKEN="$WIN_TOK" \
"$RECV" > "$TMP/subscriber.log" 2>&1 &
SUB_PID=$!
sleep 1

# 4. Dispatch
TEXT="relay e2e $(date +%s)"
RESP=$(curl -sf -X POST -H "Authorization: Bearer $MAC_TOK" \
    -H "Content-Type: application/json" \
    -d "{\"target_device_id\":\"$WIN_ID\",\"text\":\"$TEXT\"}" \
    http://127.0.0.1:8443/v1/dispatch)
echo "Dispatch response: $RESP"

sleep 0.5
GOT=$(pbpaste)

if [ "$GOT" = "$TEXT" ]; then
    echo "PASS: clipboard contains expected text"
    exit 0
else
    echo "FAIL: clipboard='$GOT' expected='$TEXT'"
    echo "--- relay log ---"; cat "$TMP/relay.log"
    echo "--- subscriber log ---"; cat "$TMP/subscriber.log"
    exit 1
fi
```

- [ ] **Step 2: 跑**

```bash
chmod +x scripts/test_relay_e2e.sh
./scripts/test_relay_e2e.sh
```

Expected: `PASS: clipboard contains expected text`(过程中会发一次 Cmd+V;前台最好是 Finder 桌面避免误粘到编辑器)

- [ ] **Step 3: Commit**

```bash
git add scripts/test_relay_e2e.sh
git commit -m "test(relay): scripts/test_relay_e2e.sh 端到端冒烟脚本

启动 relay + receiver(subscriber mode)+ curl dispatch + pbpaste 验证。
macOS dev 机用,2 秒跑完一次。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R4.2: 部署 artifacts(systemd + Caddyfile + env example)

**Files:** Create `deploy/type4me-relay.service`, `deploy/Caddyfile.example`, `deploy/env.example`

- [ ] **Step 1: systemd unit**

`deploy/type4me-relay.service`:

```ini
[Unit]
Description=Type4Me relay (SSE PubSub)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=type4me
Group=type4me
EnvironmentFile=/etc/type4me-relay/env
ExecStart=/usr/local/bin/type4me-relay serve
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal

# 安全 hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ReadWritePaths=/var/lib/type4me-relay
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
RestrictNamespaces=true
LockPersonality=true
MemoryDenyWriteExecute=true
SystemCallArchitectures=native

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 2: Caddyfile 示例**

`deploy/Caddyfile.example`:

```caddy
# Append this block to your Caddyfile.
# Replace `relay.your-domain.com` with your actual hostname.

relay.your-domain.com {
    encode gzip

    reverse_proxy 127.0.0.1:8443 {
        flush_interval -1
        transport http {
            response_header_timeout 0s
            read_buffer 4KB
        }
    }

    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains"
        X-Content-Type-Options    "nosniff"
        Referrer-Policy           "no-referrer"
    }

    log {
        output file /var/log/caddy/relay-access.log {
            roll_size 10mb
            roll_keep 5
        }
        format json
    }
}
```

- [ ] **Step 3: env example**

`deploy/env.example`:

```env
# Copy to /etc/type4me-relay/env (owner root:type4me, mode 0640)
# Generate ADMIN_TOKEN via: openssl rand -base64 48 | tr -d '=' | head -c 64

TYPE4ME_RELAY_ADMIN_TOKEN=REPLACE_WITH_RANDOM_64_CHARS
TYPE4ME_RELAY_BIND=127.0.0.1:8443
TYPE4ME_RELAY_STATE_DIR=/var/lib/type4me-relay
```

- [ ] **Step 4: Commit**

```bash
git add deploy/type4me-relay.service deploy/Caddyfile.example deploy/env.example
git commit -m "build(deploy): systemd unit + Caddyfile + env example

部署到 VPS 的三件套:
- systemd unit 含安全 hardening (NoNewPrivileges, ProtectSystem 等)
- Caddyfile 反代配 SSE 友好(flush_interval -1, 0 response timeout)
- env example 标明 ADMIN_TOKEN 生成命令

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task R4.3: 部署文档 + Makefile build-linux

**Files:** Create `docs/relay-deployment.md`, 改 `relay/Makefile`(已含 build-linux,确认)

- [ ] **Step 1: 写部署文档**

`docs/relay-deployment.md`:

```markdown
# Self-Hosted Relay 部署指南

从空 VPS 到生产可用的 step-by-step。前提:已有一台 Linux VPS,带公网 IP +
域名 + Caddy(本指南假设你用 Caddy 反代;nginx 类似)。

## 一、上传二进制

本地构建:

\`\`\`bash
make -C relay build-linux
ls -la relay/dist/type4me-relay-linux-amd64
\`\`\`

scp 到 VPS:

\`\`\`bash
scp relay/dist/type4me-relay-linux-amd64 vps:/tmp/
\`\`\`

## 二、VPS 上一次性 setup

\`\`\`bash
# 1. 创建专用 user
sudo useradd --system --no-create-home --shell /usr/sbin/nologin type4me

# 2. 创建目录
sudo mkdir -p /etc/type4me-relay /var/lib/type4me-relay
sudo chown type4me:type4me /var/lib/type4me-relay
sudo chmod 0700 /var/lib/type4me-relay

# 3. 装二进制
sudo install -m 0755 /tmp/type4me-relay-linux-amd64 /usr/local/bin/type4me-relay

# 4. 写 env 文件 (replace TOKEN with output of: openssl rand -base64 48 | tr -d '=' | head -c 64)
sudo cp deploy/env.example /etc/type4me-relay/env
sudo vim /etc/type4me-relay/env           # 改 TYPE4ME_RELAY_ADMIN_TOKEN
sudo chown root:type4me /etc/type4me-relay/env
sudo chmod 0640 /etc/type4me-relay/env

# 5. 装 systemd unit
sudo cp deploy/type4me-relay.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now type4me-relay
sudo systemctl status type4me-relay --no-pager
\`\`\`

## 三、Caddy 配置

把 `deploy/Caddyfile.example` 的内容 append 到 \`/etc/caddy/Caddyfile\`(改 hostname 为你的实际域名),然后:

\`\`\`bash
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
\`\`\`

第一次 reload 时 Caddy 会自动通过 HTTP-01 challenge 拿 Let's Encrypt cert。等几秒看到 `certificate obtained`。

## 四、防火墙

\`\`\`bash
sudo ufw allow 22/tcp 80/tcp 443/tcp
sudo ufw enable
\`\`\`

## 五、健康检查

\`\`\`bash
curl https://relay.your-domain.com/healthz
# {"ok":true,"uptime_sec":N,"version":"..."}
\`\`\`

## 六、创建 account + 2 device

\`\`\`bash
RELAY=https://relay.your-domain.com
ADMIN="Bearer YOUR_ADMIN_TOKEN"

# 创 account
curl -X POST $RELAY/v1/admin/accounts \\
  -H "Authorization: $ADMIN" -H "Content-Type: application/json" \\
  -d '{"name":"Personal"}'
# → 记 account_id

ACCT=acct-XXX

# 创 Mac device
curl -X POST $RELAY/v1/admin/devices \\
  -H "Authorization: $ADMIN" -H "Content-Type: application/json" \\
  -d "{\\"account_id\\":\\"$ACCT\\",\\"label\\":\\"My-Mac\\"}"
# → 立刻记 device_token (43 字符,仅此一次显示)

# 创 Win device
curl -X POST $RELAY/v1/admin/devices \\
  -H "Authorization: $ADMIN" -H "Content-Type: application/json" \\
  -d "{\\"account_id\\":\\"$ACCT\\",\\"label\\":\\"Win-PC\\"}"
# → 同上,记 device_token
\`\`\`

## 七、配置 Mac 端

编辑 \`~/Library/Application Support/Type4Me/credentials.json\`,加 \`tf_remote_targets\`:

\`\`\`json
{
  "tf_remote_targets": [{
    "id": "win-via-relay",
    "name": "Win-PC (via relay)",
    "mode": "relay",
    "relay_url": "https://relay.your-domain.com",
    "device_id": "dev-Mac01",
    "device_token": "AAA...",
    "target_device_id": "dev-Win01",
    "matchBundleIds": ["com.youqu.todesk.mac"],
    "enabled": true
  }]
}
\`\`\`

退出并重启 Type4Me dist(让它重读 credentials.json)。

## 八、配置 Win 端

在 Windows 上,\`%APPDATA%\\type4me-receiver\\config.json\`:

\`\`\`json
{
  "mode": "relay-subscriber",
  "relay_url": "https://relay.your-domain.com",
  "device_id": "dev-Win01",
  "device_token": "BBB..."
}
\`\`\`

或 PowerShell env vars:

\`\`\`powershell
$env:TYPE4ME_MODE = "relay-subscriber"
$env:TYPE4ME_RELAY_URL = "https://relay.your-domain.com"
$env:TYPE4ME_DEVICE_ID = "dev-Win01"
$env:TYPE4ME_DEVICE_TOKEN = "BBB..."
.\\type4me-receiver-windows-amd64.exe
\`\`\`

启动后看到 \`subscribing to https://relay.your-domain.com as dev-Win01\` 即成功。

## 九、验证端到端

Mac 上:

1. 打开 ToDesk 连到 Windows
2. Windows 端聚焦某个输入框(Notepad / 浏览器都行)
3. ToDesk 窗口为 Mac 前台时按 Type4Me hotkey(Fn / Right Option / 你配的那个)说话
4. 文字应当出现在 Windows 输入框

Windows receiver 控制台应有:

\`\`\`
inject ok=true reason="" text-len=NN req=<uuid> event=msg-<id>
\`\`\`

## 十、备份

\`\`\`bash
# 加到 crontab
0 3 * * * type4me cp /var/lib/type4me-relay/state.json /backup/type4me-relay-$(date +\\%F).json
\`\`\`

state.json < 10 KB。100 个 device 都不到几十 KB。

## 十一、升级

\`\`\`bash
make -C relay build-linux
scp relay/dist/type4me-relay-linux-amd64 vps:/tmp/
ssh vps 'sudo install -m 0755 /tmp/type4me-relay-linux-amd64 /usr/local/bin/type4me-relay && sudo systemctl restart type4me-relay'
curl https://relay.your-domain.com/healthz
\`\`\`

restart 约 5 秒,期间 receiver 会断开 + 自动重连。

## 十二、轮换 / 撤销 device token

\`\`\`bash
# Rotate (生成新 token,老 token 立刻 401)
curl -X POST $RELAY/v1/admin/devices/dev-Win01/rotate \\
  -H "Authorization: $ADMIN"
# → {"device_token":"NEW..."} 更新 Win config + 重启 receiver

# Delete (该 device 完全注销,token 永久失效)
curl -X DELETE $RELAY/v1/admin/devices/dev-Win01 \\
  -H "Authorization: $ADMIN"
\`\`\`

## 十三、卸载

\`\`\`bash
sudo systemctl disable --now type4me-relay
sudo rm -rf /usr/local/bin/type4me-relay /etc/type4me-relay /var/lib/type4me-relay
sudo userdel type4me
# 从 Caddyfile 删 relay.your-domain.com 那段,reload Caddy
\`\`\`
```

- [ ] **Step 2: 检查 Makefile 的 build-linux 已经在 R0.1 加了**

```bash
grep -A 3 "build-linux:" relay/Makefile
```

Expected: 看到对应 target。不需要改。

- [ ] **Step 3: Commit**

```bash
git add docs/relay-deployment.md
git commit -m "docs(deploy): relay 部署 step-by-step

从空 VPS 到生产可用 + 跨网完整验证 + 升级 / 卸载 / token 轮换。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

**Phase R4 完工标志:** `./scripts/test_relay_e2e.sh` PASS;`deploy/` 三件套 + `docs/relay-deployment.md` 存在;`make -C relay build-linux` 能产出 Linux 二进制。

---

## 完成判据(R0-R4)

- [ ] `cd relay && make test` 全过(R0 + R1 单测 + httptest 集成 + linux 跨编译)
- [ ] `cd receiver && make test` 全过(包括新 config + relay subscriber 测试)
- [ ] `swift test` 全过(原有 + 新 transport 测试)
- [ ] `./scripts/test_relay_e2e.sh` PASS
- [ ] `relay/dist/type4me-relay-linux-amd64` 产出
- [ ] `deploy/type4me-relay.service` + `deploy/Caddyfile.example` + `deploy/env.example` 存在
- [ ] `docs/relay-deployment.md` 步骤可执行(检查内部链接、命令正确)

**R5 是用户操作**(部署到自己 VPS + 跨网真机手测),不在 plan 范围。完成 R4 后告诉用户走 `docs/relay-deployment.md`。

---

## 备注 / 已知限制

- **Device token 还在 JSON 文件**(Mac credentials.json + Win config.json):S2 一并迁 Keychain
- **消息从不持久化**:receiver 离线 = 文字丢(Mac 端剪贴板兜底保留)
- **bcrypt 单次 ~80ms**:首次 token 验证慢,后续 hits cache。冷启动后第一次 dispatch 慢一点是正常的
- **Last-Event-ID echo 不重放**:协议保留 header,但 server 不真做消息回放
- **Mac 端 OutputRouter.resolve 仍每次 inject 时 new RemoteHTTPSink + new transport**:不是热路径(每次 inject = 一次说话,QPS << 1),不优化

实现时如果发现 spec 跟 plan 哪里不一致,**以 spec 为准**(`docs/superpowers/specs/2026-05-27-self-hosted-relay-design.md`)。
