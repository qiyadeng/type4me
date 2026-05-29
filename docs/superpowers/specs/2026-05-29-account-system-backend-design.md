# 账号系统 · 第 1 期 后端(relay 自助账号层)— 详细设计

日期:2026-05-29
分支:feature/account-system
上游:`2026-05-29-account-system-design.md`(整体设计,本 spec 落实其第 1 节)

## 背景与问题

整体设计把"用账号自动化两端配置"分成 后端 → Mac → Windows 三期,本期做**后端自助账号层**,是另两端的依赖。

relay 已有账号系统的数据/通信内核(`relay/internal/hub/`):`Account`、`Device`、device token(32 字节随机 + bcrypt + 热路径缓存)、Bearer 鉴权、账号级隔离、`state.json` 原子持久化、`/v1/{dispatch,subscribe,ping}`(device 鉴权)与 `/v1/admin/*`(admin 鉴权)。

**缺口**:面向终端用户的自助层——用户名/密码注册登录、会话凭据、"用会话登记本机为设备"、"列出我的设备"。目前账号/设备只能由持 `TYPE4ME_RELAY_ADMIN_TOKEN` 的管理员经 admin API 创建。

## 目标

1. 用户能用 **用户名 + 密码 + 邀请码** 自助注册、用 用户名 + 密码 登录,拿到会话凭据。
2. 客户端用会话凭据**登记本机为设备**(复用 `AddDevice`),并**列出本账号设备**供目标选择。
3. 全程不接触 relay 地址、device id/token、admin token。
4. 防滥用:邀请码门槛 + `/v1/auth/*` 基本限流;密码 bcrypt;会话 token 有过期。

## 非目标

- 不改现有 `/v1/{dispatch,subscribe,ping}` 与 `/v1/admin/*` 行为。
- 无邮箱/短信/找回密码/改密码(YAGNI,后续期再说)。
- 邀请码不做一次性消费(可重复使用即够,自托管个人/小团队);也不做账号删除/封禁。
- 不做 Mac/Windows 端(各自分期)。
- 不引入数据库;沿用 `state.json`。

## 已确定的设计决策(承接整体设计)

1. 认证:用户名 + 密码;注册需邀请码。一个用户 = 一个账号(1:1)。
2. 设备登记:登录后客户端用会话 token 调 `POST /v1/devices` 自动登记,relay 复用现有 `AddDevice`。
3. 集成"方案 A":不改路由内核;账号层只是"自动配置器",这点完全落在客户端,后端无需感知。
4. 邀请码可重复使用 + 仅校验(一次性消费 YAGNI)。
5. 会话凭据用 **HMAC 签名 token**(无需新增存储)。

## 设计

纯 relay 后端改动。分 `hub`(数据/逻辑)与 `server`(HTTP/鉴权/限流)两层,沿用现有分层与错误返回约定。

### 1. 数据模型变更(`hub`)

`Account` 增加两字段:

```go
type Account struct {
    ID           string    `json:"id"`
    Name         string    `json:"name"`
    Username     string    `json:"username,omitempty"`      // 新增
    PasswordHash string    `json:"password_hash,omitempty"` // 新增,bcrypt
    CreatedAt    time.Time `json:"created_at"`
}
```

- `Device`、token、隔离、`Dispatch` 跨账号检查**不变**。
- `state.json` 版本 `stateVersion` 由 `1` 升到 `2`。
- 兼容旧记录:`loadState` 读到 version=1 的账号(无 `username`/`password_hash`)照常加载——它们是 admin 创建的遗留账号,不能用密码登录,但设备照常工作。加载后不强制回写;下次任何 `persistLocked` 自然写成 version=2。
- 用户名唯一性靠内存索引:`Hub` 增加 `usernames map[string]string`(lower(username) → accountID),`New` 时从已加载账号重建(跳过空 Username)。

### 2. 新增 hub 方法

```go
// 注册:校验用户名/密码规则与唯一性,bcrypt 哈希密码,建账号。
func (h *Hub) RegisterUser(username, password string) (*Account, error)

// 登录:按用户名查账号并校验密码。
func (h *Hub) Authenticate(username, password string) (*Account, error)

// 列出某账号的设备(给 GET /v1/devices)。
func (h *Hub) ListDevicesByAccount(accountID string) []*Device

// 设备是否在线(有活跃 SSE sub),给列表的 online 字段。
func (h *Hub) IsOnline(deviceID string) bool
```

- 密码哈希复用 `token.go` 的 `hashToken`/`verifyToken`(对任意字符串做 bcrypt;新增语义别名 `hashPassword`/`verifyPassword` 直接转调,提高可读性,不重复实现)。
- 校验规则(常量定义在 hub):
  - 用户名:`strings.TrimSpace` 后长度 `[3,32]`;唯一性用 `strings.ToLower` 比较(防 `Alice`/`alice` 混淆),但原样存储。
  - 密码:长度 `>= 8`(对原始串,不 trim)。
- `RegisterUser` 在锁内:查重 → 生成 `acct-` id → 存账号 + 更新 `usernames` 索引 → `persistLocked`(失败回滚内存)。
- `Authenticate`:查 `usernames[lower]` → 取账号 → `verifyPassword`;任一步失败返回 `ErrInvalidCredentials`(不区分"用户不存在"与"密码错",避免用户名枚举)。
- `ListDevicesByAccount`:`RLock` 过滤 `AccountID == accountID`。
- `IsOnline`:`RLock` 查 `h.subs[deviceID]` 是否存在。

### 3. 新增 hub 错误(`errors.go`)

```go
ErrUsernameRequired   // 用户名空/超长/过短
ErrPasswordTooShort   // 密码 < 8
ErrUsernameTaken      // 用户名已存在(大小写不敏感)
ErrInvalidCredentials // 登录失败(用户名或密码错)
```

### 4. 会话 token(`server/session.go`,新增)

不落库,用 HMAC-SHA256 签名的自包含 token。

- 结构:`base64url(payload) + "." + base64url(HMAC_SHA256(key, base64url(payload)))`,`payload = {"aid":"acct-xxx","exp":<unix秒>}`(JSON)。
- 签名密钥:`server.Options.SessionKey []byte`,来自 env `TYPE4ME_RELAY_SESSION_KEY`。
  - env 缺失:`main.go` 启动时用 `crypto/rand` 生成 32 字节临时密钥并**告警日志**("会话密钥未配置,进程重启后所有会话失效")。device token 不受影响,客户端只需重新登录。
- TTL 常量 `sessionTTL = 24 * time.Hour`。
- API:
  ```go
  type sessionSigner struct{ key []byte }
  func (s sessionSigner) sign(accountID string, now time.Time) string
  func (s sessionSigner) verify(token string, now time.Time) (accountID string, err error)
  ```
- `verify`:拆分两段 → 用 `hmac.Equal` 常数时间比对签名 → 解 payload → 检查 `exp > now`。任一失败返回 `errInvalidSession`(过期与签名错不细分)。
- 时间统一 `time.Now().UTC()`;`now` 作为参数传入以便测试。

### 5. 鉴权中间件 `requireSession`(`server/auth.go`)

挨着现有 `requireAdmin`/`requireDevice`:

```go
type sessionHandler func(w http.ResponseWriter, r *http.Request, accountID string)
func requireSession(signer sessionSigner, next sessionHandler) http.HandlerFunc
```

- 取 Bearer → `signer.verify` → 失败 `401 {"error":"invalid_session"}` → 成功把 `accountID` 透给 handler。

### 6. 限流(`server/ratelimit.go`,新增)

只挂在 `/v1/auth/register` 与 `/v1/auth/login`。

- 内存固定窗口:`map[ip]struct{count int; windowStart time.Time}`,`sync.Mutex` 保护。
- 阈值常量:`authRateLimit = 10`、`authRateWindow = time.Minute`(每 IP 每分钟 10 次)。
- 客户端 IP:`X-Forwarded-For` 首段(部署在 Caddy 反代后),否则 `r.RemoteAddr` 去端口。
- 超限返回 `429 {"error":"rate_limited"}`。
- 提供清理:窗口过期的条目惰性重置(命中时判断 `now - windowStart >= window` 即重置计数),无需后台 goroutine。
- 包装成中间件 `func (l *rateLimiter) wrap(next http.HandlerFunc) http.HandlerFunc`。`now` 通过可注入的时钟(默认 `time.Now`)以便测试。

### 7. 新增接口

现有 `/v1/dispatch`、`/v1/subscribe`、`/v1/ping`、`/v1/admin/*` **完全不变**。

| 方法 | 路径 | 鉴权 | 请求体 | 成功响应 |
|---|---|---|---|---|
| POST | `/v1/auth/register` | 无(限流 + 邀请码) | `{username,password,invite_code}` | 201 `{session_token,account_id,username,expires_at}` |
| POST | `/v1/auth/login` | 无(限流) | `{username,password}` | 200 `{session_token,account_id,username,expires_at}` |
| POST | `/v1/devices` | 会话 | `{label,role}` | 201 `{device_id,device_token,label,role,created_at}` |
| GET | `/v1/devices` | 会话 | — | 200 `{devices:[{id,label,role,last_seen,online}]}` |

`expires_at` = 签发时刻 + 24h(RFC3339)。

#### `handlers_auth.go`

- `handleRegister`:
  1. 解 JSON(失败 `400 bad_json`)。
  2. 校验邀请码 ∈ `s.opts.InviteCodes`(空集合 ⇒ `403 registration_disabled`;不匹配 ⇒ `403 invalid_invite`)。
  3. `Hub.RegisterUser` → 映射 `ErrUsernameRequired`→`400 username_invalid`、`ErrPasswordTooShort`→`400 password_too_short`、`ErrUsernameTaken`→`409 username_taken`。
  4. `signer.sign(acc.ID, now)` → `201`。
- `handleLogin`:解 JSON → `Hub.Authenticate` → `ErrInvalidCredentials`→`401 invalid_credentials` → 签发 → `200`。
- 邀请码集合用 `map[string]struct{}` 存于 `Server`,构造时由 `Options.InviteCodes []string` 建立。

#### `handlers_devices.go`

- `handlePostDevice(w,r,accountID)`:解 `{label,role}`(label 空则后端不强制,沿用 `AddDevice` 行为;role 空 → `AddDevice` 内部默认 `either`)→ `Hub.AddDevice(accountID,label,role)` → `201`(字段同现有 admin 建设备,但**不回传 account_id**,客户端无需关心)。
- `handleGetDevices(w,r,accountID)`:`Hub.ListDevicesByAccount` → 每项附 `online: Hub.IsOnline(id)` → `200`。

### 8. 路由装配与配置(`server.go` / `main.go`)

`server.Options` 增加:

```go
InviteCodes []string // 来自 TYPE4ME_RELAY_INVITE_CODES
SessionKey  []byte   // 来自 TYPE4ME_RELAY_SESSION_KEY(缺失则随机)
```

`Handler()` 注册(`/v1/devices` 用方法分叉,类似现有 admin 写法):

```go
mux.HandleFunc("/v1/auth/register", rl.wrap(s.handleRegister))
mux.HandleFunc("/v1/auth/login",    rl.wrap(s.handleLogin))
mux.HandleFunc("/v1/devices", requireSession(s.signer, func(w,r,aid){
    switch r.Method {
    case "POST": s.handlePostDevice(w,r,aid)
    case "GET":  s.handleGetDevices(w,r,aid)
    default:     w.WriteHeader(405)
    }
}))
```

`main.go`:读两个新 env;`InviteCodes` 用逗号分隔解析并去空白;`SessionKey` 缺失则 `rand` 32 字节 + 告警。

`deploy/env.example` 增加 `TYPE4ME_RELAY_INVITE_CODES=` 与 `TYPE4ME_RELAY_SESSION_KEY=` 注释说明。

## 错误处理一览

| 场景 | HTTP | error 码 |
|---|---|---|
| JSON 解析失败 | 400 | `bad_json` |
| 用户名非法(空/过短/超长) | 400 | `username_invalid` |
| 密码过短 | 400 | `password_too_short` |
| 用户名已存在 | 409 | `username_taken` |
| 邀请码功能未开(空集合) | 403 | `registration_disabled` |
| 邀请码错误 | 403 | `invalid_invite` |
| 登录凭据错误 | 401 | `invalid_credentials` |
| 会话无效/过期 | 401 | `invalid_session` |
| 限流 | 429 | `rate_limited` |

## 安全

- 密码与 device token 同 bcrypt(`bcrypt.DefaultCost`)。
- 登录失败不区分"用户不存在/密码错",防用户名枚举;签名比对 `hmac.Equal` 常数时间。
- 会话 token 有 24h 过期;签名密钥由 env 配置,建议生产固定。
- 邀请码门槛防公网滥注册;`/v1/auth/*` 限流防暴力破解。
- 跨账号隔离沿用现有 `Dispatch` 检查与按账号过滤的 `ListDevicesByAccount`,会话只能操作自己账号。

## 测试策略

**hub(`hub_test.go` 扩展或新 `account_test.go`)**
- `RegisterUser`:成功;用户名过短/超长/空 → `ErrUsernameRequired`;密码 < 8 → `ErrPasswordTooShort`;重复用户名(含大小写变体)→ `ErrUsernameTaken`。
- `Authenticate`:正确;错密码、不存在用户名均 → `ErrInvalidCredentials`。
- `ListDevicesByAccount`:只返回本账号设备(建两账号交叉验证)。
- `IsOnline`:有/无 sub。
- 状态兼容:加载 version=1(无 username)账号不报错,可正常列设备;`usernames` 索引重建跳过空用户名。

**session(`session_test.go`)**
- `sign`→`verify` 往返拿回 accountID;过期(`now` 前移)→ err;篡改 payload/签名 → err;不同 key → err。

**server(`server_test.go` / `handlers_*_test.go`)**
- register:成功 201 + 可解析的 session token;无邀请码集合 → 403;错邀请码 → 403;重复用户名 → 409。
- login:成功 200;错密码 401。
- `requireSession`:无/错/过期 token → 401;有效 → 放行。
- `/v1/devices` POST:用会话建设备返回 token;GET 只列本账号且 `online` 标记正确(配合一个假 sub)。
- 限流:同 IP 连打超阈值 → 429(注入假时钟,验证窗口重置)。

**端到端(`scripts/test_relay_e2e.sh` 扩展)**
- 起 relay(配 `TYPE4ME_RELAY_INVITE_CODES`、`TYPE4ME_RELAY_SESSION_KEY`)→ register → 用会话登记一台 receiver 设备 → 登记一台 sender 设备 → `GET /v1/devices` 看到两台 → sender 用 device token `dispatch` 给 receiver → 断言收到。

## 受影响 / 新增文件

| 文件 | 改动 |
|---|---|
| `relay/internal/hub/types.go` | `Account` += `Username`/`PasswordHash` |
| `relay/internal/hub/hub.go` | `usernames` 索引 + `RegisterUser`/`Authenticate`/`ListDevicesByAccount`/`IsOnline`;`New` 重建索引 |
| `relay/internal/hub/token.go` | `hashPassword`/`verifyPassword` 语义别名 |
| `relay/internal/hub/errors.go` | 4 个新错误 |
| `relay/internal/hub/state.go` | `stateVersion` → 2;兼容旧记录 |
| `relay/internal/server/session.go` | **新增** HMAC 会话签名/校验 |
| `relay/internal/server/ratelimit.go` | **新增** 按 IP 固定窗口限流 |
| `relay/internal/server/auth.go` | += `requireSession` |
| `relay/internal/server/handlers_auth.go` | **新增** register/login |
| `relay/internal/server/handlers_devices.go` | **新增** POST/GET `/v1/devices` |
| `relay/internal/server/server.go` | `Options` += `InviteCodes`/`SessionKey`;装配新路由 + signer + limiter |
| `relay/cmd/type4me-relay/main.go` | 读 `TYPE4ME_RELAY_INVITE_CODES`/`TYPE4ME_RELAY_SESSION_KEY`(后者缺失随机 + 告警) |
| `relay/deploy/env.example` 等 | 记录两个新 env |
| `scripts/test_relay_e2e.sh` | 扩展自助注册→登记→列表→dispatch |

## 实现顺序(TDD,逐步可验证)

1. **hub 数据层**:`Account` 新字段 + `state` v2 兼容 + `usernames` 索引;先写加载兼容/索引重建测试,再实现。
2. **hub 账号逻辑**:`RegisterUser`/`Authenticate`/`ListDevicesByAccount`/`IsOnline` + 新错误 + 密码别名;TDD。
3. **session**:`sign`/`verify` 先测后写。
4. **ratelimit**:固定窗口 + 假时钟,先测后写。
5. **server 装配**:`Options` 扩展 + `requireSession` + `handlers_auth`/`handlers_devices` + 路由;handler 级测试。
6. **main + env + env.example**:接 env。
7. **e2e 脚本**扩展并跑通。

每步 `go test ./...` 绿后再进下一步。
