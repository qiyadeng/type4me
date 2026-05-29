# 账号系统 · 第 3 期 Windows 端(Fyne 登录界面取代 CLI)— 详细设计

日期:2026-05-29
分支:feature/account-system
上游:`2026-05-29-account-system-design.md`(整体设计,本 spec 落实其第 3 节)
依赖:`2026-05-29-account-system-backend-design.md`(后端自助账号层必须先就绪)

## 背景与问题

整体设计第 3 期:把 Windows receiver 从**纯命令行**换成**最小登录窗口 + 系统托盘**,让用户在 Windows 上输入 用户名/密码(注册可选)即可自动登记本机为设备并开始接收,全程不碰 relay 地址、device id/token。

现状(`receiver/`):零外部依赖的纯 Go stdlib CLI。`config.Config` 已含 relay 模式字段(`RelayURL`/`DeviceID`/`DeviceToken`),`config.Load` 在 `mode=relay-subscriber` 且字段缺失时**直接 fatal**。`relay.Subscriber` 已实现 SSE 订阅 + 自动重连。`cmd/type4me-receiver/main.go` 按 `mode` 分叉 listener / relay-subscriber,relay 模式靠手填 env 或 config。

**缺口**:没有 GUI;relay 凭据要手工从 admin API 复制粘贴进 config/env。

## 目标

1. Windows 上提供**登录窗口**(用户名/密码 + 注册时加邀请码),登录后**自动登记本机为设备**并写入本地配置。
2. 登录后常驻**系统托盘**,显示连接状态,可退出登录/退出程序。
3. 已登录过(本地有 device 凭据)则**跳过登录直接订阅**。
4. 用户从不输入 relay 地址(build 固化)、device id/token(自动生成)。

## 非目标

- 不改 relay 后端(接口由第 1 期提供,本期无新增后端接口)。
- 不改 Mac 端。
- 不实现"选择目标设备"——Windows 只**接收**,目标选择是 Mac 端的事(故 Windows **不需要** `GET /v1/devices`)。
- 不做开机自启、自动更新、改密码/找回密码(YAGNI,后续期再说)。
- 保留原 CLI listener 模式与现有 `type4me-receiver` 二进制不动(headless/调试/LAN 直连仍可用)。

## 已确定的设计决策

1. **GUI 技术选型:Fyne(纯 Go GUI)**。一个工具包同时做窗口与托盘,原生控件,无 WebView2 运行时依赖。代价:需 CGO + OpenGL,二进制偏大——对自托管桌面工具可接受。
2. 新增独立 GUI 二进制 `type4me-receiver-gui`,复用现有 `internal/{config,relay,inject}`;原 CLI 二进制保留。
3. relay 地址 build 固化常量(与 Mac 同值),用户不输入。
4. 凭据存储:device_token 沿用 `config.json`(APPDATA 下 per-user 目录,`Save` 已用 0600);会话 token 仅存内存。Windows 凭据库(DPAPI)列为后续升级,本期 YAGNI。
5. 可测的"登录→登记→存→起订阅"编排独立成 `internal/appflow`,与 Fyne 渲染解耦;Fyne UI 本身手动验证。

## 设计

### 1. 固化常量

新增 build 期常量(`var` + `-ldflags -X` 可覆盖,默认填生产 relay 地址):

```go
// cmd/type4me-receiver-gui/build.go
var defaultRelayURL = "https://relay.example.com" // 部署时 ldflags 覆盖为真实地址
```

与 Mac 端固化的 `relayURL` 保持同一值(整体设计「固化参数」)。

### 2. relay 自助客户端(`internal/relayauth`,新增)

封装第 1 期的三个无状态调用(Windows 用到其中三件;`GET /v1/devices` 不用):

```go
type Client struct {
    RelayURL   string
    HTTPClient *http.Client // 默认 10s 超时
}

// POST /v1/auth/login → 会话 token + 过期
func (c *Client) Login(username, password string) (Session, error)
// POST /v1/auth/register → 同上
func (c *Client) Register(username, password, inviteCode string) (Session, error)
// POST /v1/devices(带会话 token)→ 登记本机
func (c *Client) RegisterDevice(session, label string, role string) (Device, error)

type Session struct { Token string; AccountID string; Username string; ExpiresAt time.Time }
type Device  struct { ID string; Token string; Label string; Role string }
```

- 错误:把后端 4xx 的 `error` 码映射成带可读消息的 `error`(如 `invalid_credentials`→"用户名或密码错误"、`invalid_invite`→"邀请码无效"、`username_taken`→"用户名已被占用"、`registration_disabled`→"该服务未开放注册"、`rate_limited`→"尝试过于频繁,请稍后再试"),供 UI 直接显示。
- 仅依赖 stdlib `net/http`/`encoding/json`,可独立单测。

### 3. 配置完整性判定(`internal/config` 改动)

新增助手(不改现有 `Load` 的 CLI 语义):

```go
// relay 模式且三件齐全 ⇒ 可直接订阅,无需登录。
func (c *Config) IsRelayConfigured() bool {
    return c.Mode == ModeRelaySubscriber &&
        c.RelayURL != "" && c.DeviceID != "" && c.DeviceToken != ""
}
```

GUI 启动时**自行用 `os.ReadFile` + `json.Unmarshal` 读 config**(不走会 fatal 的 `config.Load`),据 `IsRelayConfigured()` 决定起订阅还是弹登录。写回继续用 `config.Save`(已 0600)。

### 4. 编排控制器(`internal/appflow`,新增 — 本期测试重心)

把全部流程逻辑从 Fyne 抽出,依赖注入假对象即可单测:

```go
type AuthClient interface {
    Login(user, pass string) (relayauth.Session, error)
    Register(user, pass, invite string) (relayauth.Session, error)
    RegisterDevice(session, label, role string) (relayauth.Device, error)
}

type Controller struct {
    Cfg        *config.Config
    CfgPath    string
    Auth       AuthClient
    Hostname   string
    StartSub   func(cfg *config.Config) // 注入"起订阅",生产传真 Subscriber 启动器
}

// 已配置则直接起订阅,返回 true(无需登录)。
func (c *Controller) ResumeIfConfigured() bool

// 登录或注册 → 自动登记本机设备 → 写 config(relay 模式 + 固化 relayURL)→ 起订阅。
func (c *Controller) LoginAndStart(user, pass, invite string, register bool) error
```

`LoginAndStart` 步骤:
1. `register ? Auth.Register(...) : Auth.Login(...)` → 拿 `Session`(仅内存)。
2. 本地若已有 `DeviceToken` 则跳过登记;否则 `Auth.RegisterDevice(session.Token, Hostname, "either")` → 得 `device_id`/`device_token`。
3. 写 `Cfg`:`Mode=relay-subscriber`、`RelayURL=固化常量`、`DeviceID`、`DeviceToken` → `config.Save(CfgPath)`。
4. `StartSub(Cfg)`。
   任一步失败:返回带可读消息的 error(UI 在登录窗顶部红字提示),不写半截 config(登记成功但保存失败时回退清空内存凭据,提示重试)。

`Logout`(退出登录):清空 config 的 `DeviceID`/`DeviceToken`(`Mode` 原样保留——`IsRelayConfigured` 因 token 已空即返回 false)+ `Save` + 停订阅 + 回到登录窗。

### 5. 订阅状态回调(`internal/relay` 小改)

`relay.Subscriber` 增加可选回调,供托盘显示状态:

```go
type Status string
const ( StatusConnecting Status = "connecting"; StatusConnected Status = "connected"
        StatusReconnecting Status = "reconnecting"; StatusError Status = "error" )

// 可选;nil 则不回调。Subscriber 在连接生命周期变更时调用。
OnStatus func(Status, error)
```

- 在现有 SSE 接通("connected"日志处,见 commit `f4161a7`)、重连退避、致命错误处分别回调。不改重连逻辑本身。
- CLI 路径不设 `OnStatus`,行为不变。

### 6. Fyne GUI(`cmd/type4me-receiver-gui/main.go`)

- `app.New()`;若支持托盘(`desk.App`)则 `SetSystemTrayMenu`。
- **启动**:读 config → `Controller.ResumeIfConfigured()`:
  - true:不显示登录窗,起订阅,托盘进入"连接中/已连接"。
  - false:显示登录窗。
- **登录窗**:Fyne 表单——用户名 `Entry`、密码 `Entry`(`Password=true` 遮罩)、邀请码 `Entry`(仅"注册"模式可见)、登录↔注册切换、提交按钮、顶部错误提示 `Label`。提交调 `Controller.LoginAndStart`;成功关窗、起订阅、托盘转"已连接";失败红字提示,窗口保留。
- **托盘菜单**:状态项(随 `OnStatus` 更新文案与图标:连接中/已连接/重连中/错误)、`退出登录`(→ `Controller.Logout`,重开登录窗)、`退出`(停订阅 + `app.Quit()`)。
- 订阅在后台 goroutine 跑,`OnStatus` 回调切回 Fyne 主线程(`fyne.Do`/`canvas.Refresh` 安全方式)更新托盘。
- 注入器 `inject.NewPlatform()` 在订阅前 `Ping()`,失败则托盘报错(无障碍/权限类问题),不静默。

### 7. 构建

- `receiver/go.mod` 增加 `fyne.io/fyne/v2`(及其传递依赖)。
- `receiver/Makefile` 增加 windows GUI 目标:`CGO_ENABLED=1 GOOS=windows GOARCH=amd64`,`-ldflags "-H windowsgui -X main.defaultRelayURL=<生产地址>"`(`-H windowsgui` 去掉控制台黑窗)。交叉编译需对应 C 工具链(如 mingw-w64)——Makefile 注释说明,或文档建议在 Windows 上原生构建。
- 原 CLI 目标 `type4me-receiver` 保持不变。

## 错误处理

- 登录/注册失败、邀请码无效、限流:`relayauth` 映射成可读消息,登录窗顶部红字提示,可重试。
- 登记设备成功但写 config 失败:回退内存凭据 + 提示重试,绝不留半截配置。
- 会话过期:仅影响"登记设备"这一步;已存 device_token 的常驻订阅不受影响(整体设计)。已配置用户重启直接订阅,不需要再次登录。
- 目标(本机)在线但发送方离线属正常,无需处理;relay `/v1/dispatch` 的 503 是 Mac 端的事。
- 注入器不可用(`Ping` 失败):托盘显示错误,不订阅。

## 安全

- device_token 存 APPDATA 下 per-user 目录的 `config.json`(`Save` 用 0600;Windows 下为该用户私有路径)。
- 会话 token 仅内存,不落盘。
- relay 地址固化,用户不接触;凭据不进 env(GUI 应用读不到 shell env,与 Mac 端经验一致)。
- DPAPI/Windows 凭据库为后续可选升级。

## 测试策略

**`internal/relayauth`(`client_test.go`)**
- 对 `httptest.Server` 跑 `Login`/`Register`/`RegisterDevice`:成功路径解析字段正确;各 4xx error 码映射成预期可读 error;网络/超时错误。

**`internal/appflow`(`controller_test.go`)— 重心**
- `ResumeIfConfigured`:已配置 → true 且调用 `StartSub`;未配置 → false 不调用。
- `LoginAndStart`(用假 `AuthClient` + 记录调用的假 `StartSub`):
  - 全新登录 → 登记设备 → 写 config(断言 `Mode=relay-subscriber`、`RelayURL=固化值`、`DeviceID/Token` 落盘)→ 起订阅。
  - 已有 device_token → 跳过登记,直接起订阅。
  - 注册模式 → 调 `Register` 带邀请码。
  - 登录失败 → 返回 error,不写 config、不起订阅。
  - 登记成功但 `Save` 失败 → 回退,返回 error。
- `Logout`:清凭据 + Save + 停订阅。

**`internal/config`(`config_test.go` 扩展)**
- `IsRelayConfigured` 各组合(齐全/缺任一/listener 模式)。

**`internal/relay`(`subscriber_test.go` 扩展)**
- `OnStatus` 在连接/重连/错误时按序被回调(假 SSE 服务端驱动);`OnStatus=nil` 时不 panic、行为同旧。

**Fyne UI**:手动验证——首次启动弹登录、登录成功转托盘、重启免登录直连、退出登录回到登录窗、断网时托盘转"重连中"。

## 受影响 / 新增文件

| 文件 | 改动 |
|---|---|
| `receiver/go.mod` / `go.sum` | += `fyne.io/fyne/v2` |
| `receiver/cmd/type4me-receiver-gui/main.go` | **新增** Fyne app:启动判定 + 登录窗 + 托盘 |
| `receiver/cmd/type4me-receiver-gui/build.go` | **新增** `defaultRelayURL` 固化常量 |
| `receiver/internal/relayauth/client.go` (+ `_test.go`) | **新增** login/register/register-device 客户端 + 错误映射 |
| `receiver/internal/appflow/controller.go` (+ `_test.go`) | **新增** 登录→登记→存→起订阅 编排(可测) |
| `receiver/internal/config/config.go` (+ `_test.go`) | += `IsRelayConfigured` |
| `receiver/internal/relay/subscriber.go` (+ `_test.go`) | += 可选 `OnStatus` 回调 |
| `receiver/Makefile` | += windows GUI 构建目标(CGO + `-H windowsgui` + ldflags 固化 relay) |
| `receiver/cmd/type4me-receiver/main.go` | 不变(原 CLI 保留) |

## 实现顺序(TDD,逐步可验证)

1. **`relayauth` 客户端**:对 httptest 先写测试(成功 + 各错误映射),再实现。
2. **`config.IsRelayConfigured`**:先测后写。
3. **`relay.Subscriber.OnStatus`**:先测后写,确保不改重连行为。
4. **`appflow.Controller`**:用假 client/StartSub 把所有流程分支测齐,再实现。
5. **`cmd/type4me-receiver-gui`**:Fyne 接线(go.mod 加依赖)——逻辑都在 1–4,这里只做 UI 绑定 + 托盘,手动验证。
6. **Makefile** windows GUI 目标 + 文档构建说明。

非 GUI 部分(1–4)每步 `go test ./...` 绿后再进下一步;GUI(5)手动验证。
