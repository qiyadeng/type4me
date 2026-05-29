# 账号系统 · 第 2 期 Mac 端(登录与设备选择)— 详细设计

日期:2026-05-30
分支:feature/account-system
上游:`2026-05-29-account-system-design.md`(整体设计,本 spec 落实其第 2 节)
依赖:第 1 期后端已就绪(`/v1/auth/{register,login}`、`POST/GET /v1/devices`),见 `2026-05-29-account-system-backend-design.md`。

## 背景与问题

整体设计第 2 期:让 Mac 端(Type4Me)用账号登录自动完成远程输入配置,用户只需选目标设备,不接触 relay 地址、device id/token。

现状(Mac):
- `OutputTarget`(`Injection/OutputTarget.swift`):`.direct`/`.relay` 两模式,relay 字段齐全(`relayURL`/`deviceID`/`deviceToken`/`targetDeviceID`),带 `matchBundleIds`。
- `OutputTargetStore`(`Services/OutputTargetStore.swift`):从 `credentials.json` 的 `tf_remote_targets` 加载 `[OutputTarget]`。
- `OutputRouter.resolve(frontmostBundleId:override:)`(`Injection/OutputRouter.swift`):`.auto`(按前台 bundle id 匹配 `matchBundleIds`)/ `.local` / `.remote(id)`。失败兜底落本机(`RemoteHTTPSink.fallback`,已实现)。
- `AppState.remoteTargets = OutputTargetStore().load()`;`outputOverride` 持久化于 UserDefaults `tf_output_override`;`reloadRemoteTargets()`。
- `RemoteSettingsTab`:已有「激活目标」单选器(列 自动 / 本机 / 各启用 target)、credentials 文件卡、逐 target 卡片。
- `KeychainService`:通用 `save(key:value:)`/`load(key:)`/`delete(key:)`(scalar service)。
- `RelayTransport`(`Injection/RelayTransport.swift`):按 target 打 `POST /v1/dispatch`。

**缺口**:面向用户的账号登录、自助登记本机为设备、拉取"我的设备"列表并合成可选目标;以及登录态的 UI 与凭据进 Keychain。relay 的 login/register/设备列表 Swift 客户端尚不存在。

## 目标

1. Mac 端用 用户名+密码(注册需邀请码)登录,拿会话凭据。
2. 登录后自动登记本机为设备(label=主机名),本地保存 device token;拉取账号设备列表。
3. 「激活目标」选择器在登录态列出 `本机 + 账号其它设备`,用户手选;`relayURL` 固化、device id/token 自动,用户不接触。
4. 凭据(会话 token、device token)进 Keychain,不再明文进 credentials.json。
5. LAN 直连与现有手动 relay 配置在**登出态**完全保持现状。

## 非目标

- 不改 relay 后端、receiver、路由内核(`OutputSink`/`OutputRouter`/`RemoteHTTPSink`/`RelayTransport`)。
- 不引入热键切换(远程桌面客户端抢键盘,见项目记忆);切换走非键盘的设置界面。
- 不做实时在线探测(`online` 仅取 `GET /v1/devices` 返回的快照值)。
- 不做改密码/找回密码/账号删除(YAGNI)。
- 不做设备改名/删除(本期只登记本机 + 列表 + 选目标)。

## 已确定的设计决策(承接整体设计 + 本期澄清)

1. **集成方式方案 A**:账号层是"自动配置器",不改路由内核;登录后在内存合成 `OutputTarget(.relay)`。
2. **登录后只显示账号目标**:`AppState.remoteTargets` 来源按登录态切换——登录态=账号合成目标;登出态=`OutputTargetStore().load()`(现状,含 LAN 直连与手动 relay)。
3. **`.auto` 范围限于账号层**:`.auto`/`matchBundleIds` 机制保留在路由内核;账号合成目标 `matchBundleIds=[]`,`.auto` 对其恒不命中(回退本机),登录态靠手选。登出态手动目标的 `.auto` 行为不变(不回退上一期成果)。
4. **UI 放进 `RemoteSettingsTab` 顶部账号区**。
5. **凭据进 Keychain**;非密的用户名 + 对端设备列表缓存进 UserDefaults,供启动时无会话也能填充选择器。

## 设计

纯 Mac 端改动。分三块:relay 账号客户端(网络)、账号会话状态(逻辑/存储/合成)、UI。

### 1. 固化常量

新增 `Type4Me/Services/RelayConfig.swift`:

```swift
enum RelayConfig {
    /// 固化的 relay 服务地址,与 Windows receiver / CLI 同值。
    /// 用户从不输入。部署/打包时按需改这里。
    static let defaultRelayURL = URL(string: "https://relay.example.com")!
}
```

### 2. relay 账号客户端 `RelayAccountClient`

新增 `Type4Me/Services/RelayAccountClient.swift`。纯网络层,`async` 方法,可注入 `URLSession` 以便测试。

```swift
struct RelaySession: Equatable { let token: String; let accountID: String; let username: String; let expiresAt: Date }
struct RelayRegisteredDevice: Equatable { let id: String; let token: String; let label: String; let role: String }
struct RelayPeerDevice: Equatable { let id: String; let label: String; let online: Bool }

enum RelayAPIError: Error, Equatable {
    case api(status: Int, code: String, message: String) // 后端 4xx 的 {"error":code}
    case malformedResponse
    case transport(String)                                // 网络/解码层失败(取 localizedDescription)
}

final class RelayAccountClient {
    init(relayURL: URL = RelayConfig.defaultRelayURL, session: URLSession = .shared)
    func login(username: String, password: String) async throws -> RelaySession           // POST /v1/auth/login
    func register(username: String, password: String, inviteCode: String) async throws -> RelaySession // POST /v1/auth/register
    func registerDevice(session: String, label: String, role: String) async throws -> RelayRegisteredDevice // POST /v1/devices (Bearer session)
    func listDevices(session: String) async throws -> [RelayPeerDevice]                    // GET /v1/devices (Bearer session)
}
```

- 请求/响应字段与后端一致:login/register 解 `{session_token, account_id, username, expires_at}`;registerDevice 发 `{label, role}`、解 `{device_id, device_token, label, role}`;listDevices 解 `{devices:[{id,label,role,last_seen,online}]}`,映射成 `RelayPeerDevice{id,label,online}`(`role`/`last_seen` 本期不用)。
- 非 2xx:解 `{"error":code}` → 抛 `RelayAPIError.api`,`message` 取 code→中文消息表(见「错误处理」)。
- 仅依赖 Foundation(`URLSession`/`JSONDecoder`),可独立单测。

### 3. 账号会话状态 `AccountSession`

新增 `Type4Me/Services/AccountSession.swift`。`@Observable`(与 `AppState` 同 Observation 体系),被 `AppState` 持有(`appState.account`),封装登录态机、凭据存取、设备列表、目标合成。把这部分从已偏大的 `AppState` 隔离出来,便于测试。

**状态机**

```swift
enum AccountState: Equatable { case loggedOut; case loggingIn; case loggedIn; case sessionExpired }
```

转移:
- `loggedOut` --login/register 成功--> `loggedIn`
- `loggedIn` --logout--> `loggedOut`
- `loggedIn` --刷新遇 invalid_session(401)/无会话--> `sessionExpired`
- `sessionExpired` --重新登录成功--> `loggedIn`
- 启动:Keychain 有 device token+id ⇒ 起始 `loggedIn`(随后尝试刷新,可能转 `sessionExpired`);否则 `loggedOut`。

**字段**:`state`、`username`、`localDeviceID`、`peers: [RelayPeerDevice]`(对端,已排除本机)、`lastError: String?`。

**存储**(注入 `KeychainService` 与 `UserDefaults` 以便测试):
- Keychain(secret):`tf_relay_session_token`、`tf_relay_device_token`、`tf_relay_device_id`。
- UserDefaults(非密):`tf_relay_username`(String)、`tf_relay_device_list`(JSON 编码的 `[RelayPeerDevice]`,启动时填充选择器用)。

**关键方法**

```swift
func bootstrap()                                              // 启动调用:读 Keychain/UserDefaults,设初始 state,后台尝试刷新
func login(username:password:) async                          // 登录→(必要时)登记本机→拉列表→缓存→loggedIn;失败设 lastError、回 loggedOut
func register(username:password:inviteCode:) async            // 同上,走 register
func refreshDevices() async                                   // 用会话拉列表→更新 peers/缓存;401→sessionExpired
func logout()                                                 // 清 Keychain 两 token+id、清缓存、peers=[]、state=loggedOut
func synthesizedTargets() -> [OutputTarget]                   // peers → [OutputTarget(.relay)]
```

`login`/`register` 内部:成功拿 session → 存 session token → 若 Keychain 无 device token 则 `registerDevice(session, 主机名, "either")` → 存 device id/token → `refreshDevices()`。任一网络步失败:`lastError` 设可读消息,`state` 回到调用前(loggedOut/sessionExpired),不写半截凭据(登记成功但后续失败时保留已存的 device token——它有效)。

主机名取 `Host.current().localizedName ?? "Mac"`。

### 4. 目标合成与路由接入

`synthesizedTargets()`:对 `peers` 每台合成

```swift
OutputTarget(
    id: peer.id,                        // 用对端 device id 作 target id(稳定、唯一)
    name: peer.label,
    matchBundleIds: [],                 // 账号目标不参与 .auto
    enabled: true,
    mode: .relay,
    relayURL: RelayConfig.defaultRelayURL,
    deviceID: localDeviceID,            // 本机发送方
    deviceToken: <Keychain device token>,
    targetDeviceID: peer.id
)
```

`AppState.remoteTargets` 仍是**存储属性**(选择器/路由读它,避免每次访问读磁盘),由一个 `rebuildRemoteTargets()` 在来源变化时重赋值:

```swift
func rebuildRemoteTargets() {
    switch account.state {
    case .loggedIn, .sessionExpired:
        remoteTargets = account.synthesizedTargets()   // device token 长效,sessionExpired 仍用缓存目标
    case .loggedOut, .loggingIn:
        remoteTargets = OutputTargetStore().load()      // 现状:手动配置(含 LAN 直连)
    }
}
```

调用时机:`init`(`account.bootstrap()` 之后)、账号状态变化后(login/register/refresh/logout 各自结尾回调 AppState 重建)、以及现有 `reloadRemoteTargets()`(登出态手改 credentials.json 后,内部改为调 `rebuildRemoteTargets()`)。`OutputRouter` 与「激活目标」选择器都读 `remoteTargets`,`resolve` 逻辑不变。账号目标经 `.remote(id)` 路由时仍走 `RemoteHTTPSink(target:fallback: localSink)`(失败落本机,已实现)。

账号状态变化如何触达 AppState 重建:`AccountSession` 暴露一个 `onChange: (() -> Void)?` 回调,`AppState` 在持有 `account` 时设为 `{ [weak self] in self?.rebuildRemoteTargets() }`(`AccountSession` 在 login/register/refresh/logout 成功改 state 后调用它)。这样 `AccountSession` 不反向依赖 `AppState`,仍可独立测试。

**登出重置**:`logout()` 后,若 `appState.outputOverride` 是 `.remote(id)` 且 `id` 不在新的(手动)目标里,重置为 `.auto`。

### 5. UI(`RemoteSettingsTab` 顶部账号区)

在 `RemoteSettingsTab` 顶部、「激活目标」卡之上新增账号区(新私有视图 `accountCard`,必要时拆到 `Type4Me/UI/Settings/AccountCard.swift` 以免文件过大):

- **未登录(loggedOut)**:用户名 + 密码(遮罩)输入;登录↔注册切换(注册显示邀请码字段);提交按钮(进行中禁用 + loggingIn 态);`lastError` 红字。下方仍显示现有手动目标卡片 + credentials 文件卡。
- **已登录(loggedIn)**:账号名、本机设备名、对端设备列表(只读:label + 在线徽标)、`刷新` 按钮(调 `refreshDevices`)、`退出登录` 按钮。「激活目标」选择器此时列 `本机 + 账号设备`。
- **会话过期(sessionExpired)**:顶部 banner「会话已过期,重新登录以刷新设备列表」+ 用户名/密码重登入口;缓存目标仍可选可发。

登出态的 credentials 文件卡 / 手动 target 卡片 / `.auto` 说明保持现状不变。

## 错误处理

- code→可读消息(与 Windows 同语义):`invalid_credentials`→"用户名或密码错误"、`invalid_invite`→"邀请码无效"、`username_taken`→"用户名已被占用"、`registration_disabled`→"该服务未开放注册"、`username_invalid`→"用户名需为 3-32 个字符"、`password_too_short`→"密码至少 8 个字符"、`password_too_long`→"密码过长"、`rate_limited`→"尝试过于频繁,请稍后再试"、`invalid_session`→"登录已过期,请重新登录";未知 code → "请求失败 (code)";网络层失败 → 取 `localizedDescription`。
- 刷新/登记遇 `invalid_session`(401)→ `sessionExpired`(不清 device token,缓存目标仍可用)。
- dispatch 失败 → 现有 `RemoteHTTPSink` 落本机注入兜底(已实现,不在本期改动)。
- 登记设备成功但写 Keychain 失败 → `lastError` 提示重试。

## 安全

- session token、device token、device id 进 Keychain(`KeychainService` scalar service);不再明文进 credentials.json。
- 会话 token 仅用于管理类操作(登记/列表);收发用长效 device token。会话过期不影响已配置目标的发送。
- relay 地址固化,用户不接触;凭据不走 env(GUI 应用读不到 shell env)。
- 登出彻底清除 Keychain 凭据 + UserDefaults 缓存。

## 测试策略

- **`RelayAccountClient`**(打 `Type4MeTests/Helpers/TestHTTPServer`):`login`/`register`/`registerDevice`/`listDevices` 成功解析;各错误码 → `RelayAPIError.api` 且 message 非空;`registerDevice` 带 `Bearer <session>`、`register` 带 `invite_code`;listDevices 映射 `online`。
- **目标合成**:`peers → [OutputTarget]` —— 排除本机 device、`relayURL` 取固化值、`matchBundleIds` 为空、`targetDeviceID`/`deviceID` 正确、id=对端 id。
- **`AccountSession` 状态机**:login 成功(含首次自动登记 vs 已有 token 跳过)→ loggedIn;login 失败 → loggedOut + lastError;refresh 401 → sessionExpired;logout → loggedOut + 凭据/缓存清空 + peers 空。用假 `RelayAccountClient` + 假 Keychain/UserDefaults。
- **Keychain 往返**:三个 token/id 存取(可用临时 service 或注入的假存储)。
- **`AppState.remoteTargets` 来源切换**:loggedOut→手动来源;loggedIn/sessionExpired→合成来源。
- **登出重置 override**:`.remote(账号目标id)` 在 logout 后重置为 `.auto`。
- **回归**:登出态 `OutputRouter` 的 `.auto`/`.local`/`.remote` 与 LAN 直连行为不变(沿用既有测试 + 新增登出态断言)。
- UI(SwiftUI 视图)以手动验证为主。

## 受影响 / 新增文件

| 文件 | 改动 |
|---|---|
| `Type4Me/Services/RelayConfig.swift` | **新增** 固化 relay URL 常量 |
| `Type4Me/Services/RelayAccountClient.swift` | **新增** login/register/registerDevice/listDevices + 错误映射 |
| `Type4Me/Services/AccountSession.swift` | **新增** 登录态机 + Keychain/UserDefaults 存取 + 目标合成 |
| `Type4Me/UI/AppState.swift` | 持有 `account: AccountSession` 并设 `account.onChange`;新增 `rebuildRemoteTargets()`(`reloadRemoteTargets` 改调它);启动调 `account.bootstrap()`;logout 后重置 override 的接线 |
| `Type4Me/UI/Settings/RemoteSettingsTab.swift` | 顶部新增账号区;登录态切换显示 |
| `Type4Me/UI/Settings/AccountCard.swift` | **新增**(若账号区较大,拆出账号卡视图) |
| `Type4Me/Services/KeychainService.swift` | 不改逻辑;复用 `save/load/delete(key:)` |
| `Type4MeTests/RelayAccountClientTests.swift` 等 | **新增** 客户端 / 合成 / 状态机 / 来源切换 测试 |

## 实现顺序(TDD)

1. **`RelayConfig`** 常量。
2. **`RelayAccountClient`**:对 `TestHTTPServer` 先写测试(成功 + 错误映射),再实现。
3. **目标合成**(`synthesizedTargets` 纯函数,可先于状态机单测):peers→targets 映射。
4. **`AccountSession`**:状态机 + Keychain/UserDefaults 存取 + login/register/refresh/logout;用假 client/存储 TDD。
5. **`AppState` 接入**:`remoteTargets` 来源切换 + bootstrap + 登出重置 override;来源切换测试。
6. **`RemoteSettingsTab` 账号区 UI**(手动验证为主)。

每步 `swift test` 绿后再进下一步;UI 手动验证。
