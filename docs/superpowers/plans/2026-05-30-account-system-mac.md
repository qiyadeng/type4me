# 账号系统 · 第 2 期 Mac 端(登录与设备选择)实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 Mac 端 Type4Me 用账号登录自动完成远程输入配置:登录→自动登记本机为设备→拉设备列表→在内存合成 relay 目标,用户只在「激活目标」里手选目标设备,凭据进 Keychain。

**Architecture:** 方案 A——账号层是"自动配置器",不改路由内核。可测逻辑全在纯 Swift 单元:`RelayAccountClient`(网络)、`RelayTargetSynthesis`(peers→目标)、`AccountSession`(登录态机 + Keychain/UserDefaults 存取,依赖注入)、`RemoteTargetResolver`(来源/override 决策)。`AppState.remoteTargets` 按登录态由 `rebuildRemoteTargets()` 切换来源;Fyne 无关——SwiftUI 账号 UI 手动验证。

**Tech Stack:** Swift / SwiftUI / AppKit;Foundation `URLSession`(async);XCTest + 既有 `Type4MeTests/Helpers/TestHTTPServer`;`KeychainService`。

**上游 spec:** `docs/superpowers/specs/2026-05-30-account-system-mac-design.md`
**依赖:** 第 1 期 relay 后端已就绪。

**所有命令在仓库根 `/Users/dsy/Documents/code/shurufa/type4me` 运行。** 测试:`swift test --filter <ClassName>`。新文件放在对应目录即自动进 SPM target(无 xcodeproj)。

---

## 文件结构

| 文件 | 责任 | 本计划 |
|---|---|---|
| `Type4Me/Services/RelayConfig.swift` | 固化 relay URL | **新增** |
| `Type4Me/Services/RelayAccountClient.swift` | login/register/registerDevice/listDevices + 类型 + 错误映射 + 协议 | **新增** |
| `Type4Me/Services/RelayTargetSynthesis.swift` | peers → `[OutputTarget]`(纯) | **新增** |
| `Type4Me/Services/AccountSession.swift` | 登录态机 + SecretStore + 存取 + 合成入口 | **新增** |
| `Type4Me/Services/RemoteTargetResolver.swift` | 来源决策 + override 清洗(纯) | **新增** |
| `Type4Me/UI/AppState.swift` | 持有 `account`、`rebuildRemoteTargets()`、bootstrap/onChange 接线 | 改 |
| `Type4Me/UI/Settings/RemoteSettingsTab.swift` | 顶部账号区 | 改 |
| `Type4Me/UI/Settings/AccountCard.swift` | 账号区视图(从 RemoteSettingsTab 拆出) | **新增** |
| `Type4MeTests/RelayAccountClientTests.swift` 等 | 各单元测试 | **新增** |

---

## Task 1: RelayConfig 固化常量

**Files:** Create `Type4Me/Services/RelayConfig.swift`

- [ ] **Step 1: 创建常量**

```swift
import Foundation

enum RelayConfig {
    /// 固化的 relay 服务地址,与 Windows receiver / CLI 同值。用户从不输入;
    /// 打包/部署时按需改这里。
    static let defaultRelayURL = URL(string: "https://relay.example.com")!
}
```

- [ ] **Step 2: 编译**

Run: `swift build 2>&1 | tail -5`
Expected: 构建成功(无错误)。

- [ ] **Step 3: 提交**

```bash
git add Type4Me/Services/RelayConfig.swift
git commit -m "feat(mac/relay): RelayConfig.defaultRelayURL build constant" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: RelayAccountClient(网络客户端 + 协议 + 错误映射)

**Files:**
- Create: `Type4Me/Services/RelayAccountClient.swift`
- Test: `Type4MeTests/RelayAccountClientTests.swift`

- [ ] **Step 1: 写失败测试**

新建 `Type4MeTests/RelayAccountClientTests.swift`:

```swift
import XCTest
@testable import Type4Me

final class RelayAccountClientTests: XCTestCase {
    private var server: TestHTTPServer!
    private var client: RelayAccountClient!

    override func setUp() {
        super.setUp()
        server = TestHTTPServer()
        server.start()
        let url = URL(string: "http://127.0.0.1:\(server.port)")!
        client = RelayAccountClient(relayURL: url, session: .shared)
    }
    override func tearDown() { server.stop(); server = nil; client = nil; super.tearDown() }

    func testLoginSuccess() async throws {
        server.respond { req in
            XCTAssertEqual(req.method, "POST")
            XCTAssertEqual(req.path, "/v1/auth/login")
            return TestHTTPResponse(status: 200, body: #"{"session_token":"sess-1","account_id":"acct-1","username":"alice","expires_at":"2030-01-01T00:00:00Z"}"#)
        }
        let sess = try await client.login(username: "alice", password: "supersecret")
        XCTAssertEqual(sess.token, "sess-1")
        XCTAssertEqual(sess.accountID, "acct-1")
        XCTAssertEqual(sess.username, "alice")
    }

    func testRegisterSendsInviteCode() async throws {
        let box = BodyBox()
        server.respond { req in
            box.value = req.body
            return TestHTTPResponse(status: 201, body: #"{"session_token":"s","account_id":"a","username":"bob"}"#)
        }
        _ = try await client.register(username: "bob", password: "supersecret", inviteCode: "INVITE-9")
        XCTAssertTrue(box.value.contains("INVITE-9"), "invite_code should be in body: \(box.value)")
    }

    func testRegisterDeviceSetsBearer() async throws {
        let box = BodyBox()
        server.respond { req in
            box.value = req.headers["Authorization"] ?? ""
            return TestHTTPResponse(status: 201, body: #"{"device_id":"dev-1","device_token":"dtok","label":"Mac","role":"either"}"#)
        }
        let dev = try await client.registerDevice(session: "sess-1", label: "Mac", role: "either")
        XCTAssertEqual(box.value, "Bearer sess-1")
        XCTAssertEqual(dev.id, "dev-1")
        XCTAssertEqual(dev.token, "dtok")
    }

    func testListDevicesMapsOnline() async throws {
        server.respond { _ in
            TestHTTPResponse(status: 200, body: #"{"devices":[{"id":"d1","label":"Win","role":"either","online":true},{"id":"d2","label":"Mac","role":"either","online":false}]}"#)
        }
        let peers = try await client.listDevices(session: "sess-1")
        XCTAssertEqual(peers.count, 2)
        XCTAssertEqual(peers[0].id, "d1")
        XCTAssertEqual(peers[0].online, true)
        XCTAssertEqual(peers[1].online, false)
    }

    func testErrorMappingThrowsAPIError() async {
        server.respond { _ in TestHTTPResponse(status: 401, body: #"{"error":"invalid_credentials"}"#) }
        do {
            _ = try await client.login(username: "alice", password: "wrong")
            XCTFail("expected error")
        } catch let RelayAPIError.api(status, code, message) {
            XCTAssertEqual(status, 401)
            XCTAssertEqual(code, "invalid_credentials")
            XCTAssertFalse(message.isEmpty)
        } catch {
            XCTFail("expected RelayAPIError.api, got \(error)")
        }
    }
}

/// Thread-safe box for capturing request data from the server callback.
final class BodyBox: @unchecked Sendable {
    private let lock = NSLock()
    private var _v = ""
    var value: String {
        get { lock.lock(); defer { lock.unlock() }; return _v }
        set { lock.lock(); _v = newValue; lock.unlock() }
    }
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `swift test --filter RelayAccountClientTests 2>&1 | tail -20`
Expected: 编译失败 —— `RelayAccountClient` / `RelayAPIError` / `RelaySession` 等未定义。

- [ ] **Step 3: 实现 RelayAccountClient.swift**

新建 `Type4Me/Services/RelayAccountClient.swift`:

```swift
import Foundation

struct RelaySession: Equatable, Sendable {
    let token: String
    let accountID: String
    let username: String
    let expiresAt: Date?
}

struct RelayRegisteredDevice: Equatable, Sendable {
    let id: String
    let token: String
    let label: String
    let role: String
}

struct RelayPeerDevice: Equatable, Codable, Sendable {
    let id: String
    let label: String
    let online: Bool
}

enum RelayAPIError: Error, Equatable {
    case api(status: Int, code: String, message: String)
    case malformedResponse
    case transport(String)
}

extension RelayAPIError: LocalizedError {
    var errorDescription: String? {
        switch self {
        case .api(_, _, let message): return message
        case .malformedResponse: return "服务器响应异常"
        case .transport(let s): return s
        }
    }
}

/// Protocol so AccountSession can be tested with a fake.
protocol RelayAccountClienting: Sendable {
    func login(username: String, password: String) async throws -> RelaySession
    func register(username: String, password: String, inviteCode: String) async throws -> RelaySession
    func registerDevice(session: String, label: String, role: String) async throws -> RelayRegisteredDevice
    func listDevices(session: String) async throws -> [RelayPeerDevice]
}

final class RelayAccountClient: RelayAccountClienting, @unchecked Sendable {
    private let relayURL: URL
    private let session: URLSession

    init(relayURL: URL = RelayConfig.defaultRelayURL, session: URLSession = .shared) {
        self.relayURL = relayURL
        self.session = session
    }

    private static let messages: [String: String] = [
        "bad_json": "请求格式错误",
        "username_invalid": "用户名需为 3-32 个字符",
        "password_too_short": "密码至少 8 个字符",
        "password_too_long": "密码过长",
        "username_taken": "用户名已被占用",
        "registration_disabled": "该服务未开放注册",
        "invalid_invite": "邀请码无效",
        "invalid_credentials": "用户名或密码错误",
        "invalid_session": "登录已过期,请重新登录",
        "rate_limited": "尝试过于频繁,请稍后再试",
        "account_not_found": "账号不存在",
    ]
    private static func message(for code: String) -> String {
        if let m = messages[code] { return m }
        return code.isEmpty ? "请求失败" : "请求失败 (\(code))"
    }

    /// POST JSON to path (optional bearer); returns the raw 2xx body Data.
    private func post(_ path: String, bearer: String?, body: [String: Any]) async throws -> Data {
        var req = URLRequest(url: relayURL.appendingPathComponent(path))
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if let bearer { req.setValue("Bearer \(bearer)", forHTTPHeaderField: "Authorization") }
        req.httpBody = try JSONSerialization.data(withJSONObject: body)
        return try await send(req)
    }

    private func get(_ path: String, bearer: String) async throws -> Data {
        var req = URLRequest(url: relayURL.appendingPathComponent(path))
        req.httpMethod = "GET"
        req.setValue("Bearer \(bearer)", forHTTPHeaderField: "Authorization")
        return try await send(req)
    }

    private func send(_ req: URLRequest) async throws -> Data {
        let data: Data, resp: URLResponse
        do { (data, resp) = try await session.data(for: req) }
        catch { throw RelayAPIError.transport(error.localizedDescription) }
        guard let http = resp as? HTTPURLResponse else { throw RelayAPIError.malformedResponse }
        if (200..<300).contains(http.statusCode) { return data }
        let code = (try? JSONSerialization.jsonObject(with: data) as? [String: Any])?["error"] as? String ?? ""
        throw RelayAPIError.api(status: http.statusCode, code: code, message: Self.message(for: code))
    }

    private func decodeSession(_ data: Data) throws -> RelaySession {
        guard let d = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let token = d["session_token"] as? String,
              let acct = d["account_id"] as? String,
              let user = d["username"] as? String
        else { throw RelayAPIError.malformedResponse }
        let iso = ISO8601DateFormatter()
        iso.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        let exp = (d["expires_at"] as? String).flatMap { iso.date(from: $0) }
        return RelaySession(token: token, accountID: acct, username: user, expiresAt: exp)
    }

    func login(username: String, password: String) async throws -> RelaySession {
        try decodeSession(try await post("v1/auth/login", bearer: nil,
            body: ["username": username, "password": password]))
    }

    func register(username: String, password: String, inviteCode: String) async throws -> RelaySession {
        try decodeSession(try await post("v1/auth/register", bearer: nil,
            body: ["username": username, "password": password, "invite_code": inviteCode]))
    }

    func registerDevice(session sessionToken: String, label: String, role: String) async throws -> RelayRegisteredDevice {
        var body: [String: Any] = ["label": label]
        if !role.isEmpty { body["role"] = role }
        let data = try await post("v1/devices", bearer: sessionToken, body: body)
        guard let d = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let id = d["device_id"] as? String,
              let token = d["device_token"] as? String
        else { throw RelayAPIError.malformedResponse }
        return RelayRegisteredDevice(id: id, token: token,
                                     label: d["label"] as? String ?? label,
                                     role: d["role"] as? String ?? role)
    }

    func listDevices(session sessionToken: String) async throws -> [RelayPeerDevice] {
        let data = try await get("v1/devices", bearer: sessionToken)
        guard let d = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let arr = d["devices"] as? [[String: Any]]
        else { throw RelayAPIError.malformedResponse }
        return arr.compactMap { e in
            guard let id = e["id"] as? String, let label = e["label"] as? String else { return nil }
            return RelayPeerDevice(id: id, label: label, online: (e["online"] as? Bool) ?? false)
        }
    }
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `swift test --filter RelayAccountClientTests 2>&1 | tail -20`
Expected: 5 个测试 PASS。

- [ ] **Step 5: 提交**

```bash
git add Type4Me/Services/RelayAccountClient.swift Type4MeTests/RelayAccountClientTests.swift
git commit -m "feat(mac/relay): RelayAccountClient login/register/register-device/list + error mapping" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: RelayTargetSynthesis(peers → 目标,纯函数)

**Files:**
- Create: `Type4Me/Services/RelayTargetSynthesis.swift`
- Test: `Type4MeTests/RelayTargetSynthesisTests.swift`

- [ ] **Step 1: 写失败测试**

新建 `Type4MeTests/RelayTargetSynthesisTests.swift`:

```swift
import XCTest
@testable import Type4Me

final class RelayTargetSynthesisTests: XCTestCase {
    private let relayURL = URL(string: "https://relay.example.com")!

    func testMapsPeersExcludingSelf() {
        let peers = [
            RelayPeerDevice(id: "dev-win", label: "Win", online: true),
            RelayPeerDevice(id: "dev-mac", label: "ThisMac", online: true),
        ]
        let targets = RelayTargetSynthesis.targets(
            peers: peers, localDeviceID: "dev-mac", deviceToken: "mac-tok", relayURL: relayURL)
        XCTAssertEqual(targets.count, 1, "own device must be excluded")
        let t = targets[0]
        XCTAssertEqual(t.id, "dev-win")
        XCTAssertEqual(t.name, "Win")
        XCTAssertEqual(t.mode, .relay)
        XCTAssertEqual(t.matchBundleIds, [])
        XCTAssertTrue(t.enabled)
        XCTAssertEqual(t.relayURL, relayURL)
        XCTAssertEqual(t.deviceID, "dev-mac")
        XCTAssertEqual(t.deviceToken, "mac-tok")
        XCTAssertEqual(t.targetDeviceID, "dev-win")
        XCTAssertTrue(t.isValid)
    }

    func testEmptyPeersYieldsEmpty() {
        let targets = RelayTargetSynthesis.targets(
            peers: [], localDeviceID: "dev-mac", deviceToken: "t", relayURL: relayURL)
        XCTAssertTrue(targets.isEmpty)
    }
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `swift test --filter RelayTargetSynthesisTests 2>&1 | tail -20`
Expected: 编译失败 —— `RelayTargetSynthesis` 未定义。

- [ ] **Step 3: 实现**

新建 `Type4Me/Services/RelayTargetSynthesis.swift`:

```swift
import Foundation

/// Pure mapping from account peer devices to relay-mode OutputTargets.
/// Excludes this machine's own device. Account targets carry no matchBundleIds
/// (they never participate in .auto routing — the user picks them manually).
enum RelayTargetSynthesis {
    static func targets(peers: [RelayPeerDevice],
                        localDeviceID: String,
                        deviceToken: String,
                        relayURL: URL) -> [OutputTarget] {
        peers
            .filter { $0.id != localDeviceID }
            .map { peer in
                OutputTarget(
                    id: peer.id,
                    name: peer.label,
                    matchBundleIds: [],
                    enabled: true,
                    mode: .relay,
                    relayURL: relayURL,
                    deviceID: localDeviceID,
                    deviceToken: deviceToken,
                    targetDeviceID: peer.id
                )
            }
    }
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `swift test --filter RelayTargetSynthesisTests 2>&1 | tail -20`
Expected: 2 个测试 PASS。

- [ ] **Step 5: 提交**

```bash
git add Type4Me/Services/RelayTargetSynthesis.swift Type4MeTests/RelayTargetSynthesisTests.swift
git commit -m "feat(mac/relay): RelayTargetSynthesis peers->OutputTargets (pure)" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: AccountSession(登录态机 + 存储,依赖注入)

**Files:**
- Create: `Type4Me/Services/AccountSession.swift`
- Test: `Type4MeTests/AccountSessionTests.swift`

- [ ] **Step 1: 写失败测试**

新建 `Type4MeTests/AccountSessionTests.swift`:

```swift
import XCTest
@testable import Type4Me

// Fakes
final class InMemorySecretStore: SecretStore, @unchecked Sendable {
    private var store: [String: String] = [:]
    func load(_ key: String) -> String? { store[key] }
    func save(_ value: String, for key: String) throws { store[key] = value }
    @discardableResult func delete(_ key: String) -> Bool { store.removeValue(forKey: key) != nil }
}

final class FakeRelayClient: RelayAccountClienting, @unchecked Sendable {
    var session = RelaySession(token: "sess", accountID: "acct", username: "alice", expiresAt: nil)
    var device = RelayRegisteredDevice(id: "dev-mac", token: "mac-tok", label: "Mac", role: "either")
    var devices: [RelayPeerDevice] = []
    var loginError: Error?
    var registerError: Error?
    var registerDeviceError: Error?
    var listError: Error?
    private(set) var loginCount = 0, registerCount = 0, registerDeviceCount = 0, listCount = 0
    var lastInvite = ""

    func login(username: String, password: String) async throws -> RelaySession {
        loginCount += 1; if let e = loginError { throw e }; return session
    }
    func register(username: String, password: String, inviteCode: String) async throws -> RelaySession {
        registerCount += 1; lastInvite = inviteCode; if let e = registerError { throw e }; return session
    }
    func registerDevice(session: String, label: String, role: String) async throws -> RelayRegisteredDevice {
        registerDeviceCount += 1; if let e = registerDeviceError { throw e }; return device
    }
    func listDevices(session: String) async throws -> [RelayPeerDevice] {
        listCount += 1; if let e = listError { throw e }; return devices
    }
}

@MainActor
final class AccountSessionTests: XCTestCase {
    private func makeDefaults() -> UserDefaults { UserDefaults(suiteName: "t-\(UUID().uuidString)")! }

    private func makeSession(client: FakeRelayClient = FakeRelayClient(),
                             secrets: InMemorySecretStore = InMemorySecretStore()) -> AccountSession {
        AccountSession(client: client, secrets: secrets, defaults: makeDefaults(), hostname: "TestMac")
    }

    func testLoginRegistersDeviceAndGoesLoggedIn() async {
        let client = FakeRelayClient()
        client.devices = [RelayPeerDevice(id: "dev-win", label: "Win", online: true),
                          RelayPeerDevice(id: "dev-mac", label: "Mac", online: true)]
        let secrets = InMemorySecretStore()
        let s = makeSession(client: client, secrets: secrets)
        await s.login(username: "alice", password: "supersecret")
        XCTAssertEqual(s.state, .loggedIn)
        XCTAssertEqual(client.registerDeviceCount, 1)
        XCTAssertEqual(s.localDeviceID, "dev-mac")
        XCTAssertEqual(s.peers.map(\.id), ["dev-win"], "own device filtered out")
        XCTAssertEqual(secrets.load(AccountSession.kDeviceToken), "mac-tok")
        XCTAssertEqual(s.synthesizedTargets().map(\.id), ["dev-win"])
    }

    func testLoginSkipsDeviceRegistrationWhenTokenExists() async {
        let client = FakeRelayClient()
        let secrets = InMemorySecretStore()
        try? secrets.save("existing-tok", for: AccountSession.kDeviceToken)
        try? secrets.save("dev-mac", for: AccountSession.kDeviceID)
        let s = makeSession(client: client, secrets: secrets)
        await s.login(username: "alice", password: "supersecret")
        XCTAssertEqual(client.registerDeviceCount, 0)
        XCTAssertEqual(s.localDeviceID, "dev-mac")
    }

    func testRegisterModeSendsInvite() async {
        let client = FakeRelayClient()
        let s = makeSession(client: client)
        await s.register(username: "bob", password: "supersecret", inviteCode: "INV-1")
        XCTAssertEqual(client.registerCount, 1)
        XCTAssertEqual(client.lastInvite, "INV-1")
        XCTAssertEqual(s.state, .loggedIn)
    }

    func testLoginFailureGoesLoggedOutWithError() async {
        let client = FakeRelayClient()
        client.loginError = RelayAPIError.api(status: 401, code: "invalid_credentials", message: "用户名或密码错误")
        let s = makeSession(client: client)
        await s.login(username: "alice", password: "wrong")
        XCTAssertEqual(s.state, .loggedOut)
        XCTAssertEqual(s.lastError, "用户名或密码错误")
        XCTAssertEqual(client.registerDeviceCount, 0)
    }

    func testRefreshSessionExpiredOn401() async {
        let client = FakeRelayClient()
        let secrets = InMemorySecretStore()
        try? secrets.save("sess", for: AccountSession.kSessionToken)
        try? secrets.save("mac-tok", for: AccountSession.kDeviceToken)
        try? secrets.save("dev-mac", for: AccountSession.kDeviceID)
        client.listError = RelayAPIError.api(status: 401, code: "invalid_session", message: "登录已过期,请重新登录")
        let s = makeSession(client: client, secrets: secrets)
        s.bootstrap()
        XCTAssertEqual(s.state, .loggedIn) // device token present
        await s.refreshDevices()
        XCTAssertEqual(s.state, .sessionExpired)
    }

    func testBootstrapLoggedOutWhenNoDeviceToken() {
        let s = makeSession()
        s.bootstrap()
        XCTAssertEqual(s.state, .loggedOut)
    }

    func testLogoutClearsEverything() async {
        let client = FakeRelayClient()
        client.devices = [RelayPeerDevice(id: "dev-win", label: "Win", online: true)]
        let secrets = InMemorySecretStore()
        let s = makeSession(client: client, secrets: secrets)
        await s.login(username: "alice", password: "supersecret")
        XCTAssertEqual(s.state, .loggedIn)
        s.logout()
        XCTAssertEqual(s.state, .loggedOut)
        XCTAssertNil(secrets.load(AccountSession.kSessionToken))
        XCTAssertNil(secrets.load(AccountSession.kDeviceToken))
        XCTAssertNil(secrets.load(AccountSession.kDeviceID))
        XCTAssertTrue(s.peers.isEmpty)
        XCTAssertTrue(s.synthesizedTargets().isEmpty)
    }

    func testOnChangeFiresOnStateTransitions() async {
        let s = makeSession()
        var count = 0
        s.onChange = { count += 1 }
        await s.login(username: "alice", password: "supersecret")
        XCTAssertGreaterThan(count, 0)
        let before = count
        s.logout()
        XCTAssertGreaterThan(count, before)
    }
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `swift test --filter AccountSessionTests 2>&1 | tail -20`
Expected: 编译失败 —— `SecretStore` / `AccountSession` 未定义。

- [ ] **Step 3: 实现 AccountSession.swift(含 SecretStore 协议 + Keychain 实现)**

新建 `Type4Me/Services/AccountSession.swift`:

```swift
import Foundation

/// Abstraction over secret persistence so AccountSession is testable without Keychain.
protocol SecretStore {
    func load(_ key: String) -> String?
    func save(_ value: String, for key: String) throws
    @discardableResult func delete(_ key: String) -> Bool
}

/// Keychain-backed SecretStore (scalar service via KeychainService).
struct KeychainSecretStore: SecretStore {
    func load(_ key: String) -> String? { KeychainService.load(key: key) }
    func save(_ value: String, for key: String) throws { try KeychainService.save(key: key, value: value) }
    @discardableResult func delete(_ key: String) -> Bool { KeychainService.delete(key: key) }
}

@MainActor
@Observable
final class AccountSession {
    enum State: Equatable { case loggedOut, loggingIn, loggedIn, sessionExpired }

    private(set) var state: State = .loggedOut
    private(set) var username: String = ""
    private(set) var localDeviceID: String = ""
    private(set) var peers: [RelayPeerDevice] = []
    var lastError: String?

    /// Called after any successful state/peer change so AppState can rebuild targets.
    var onChange: (() -> Void)?

    static let kSessionToken = "tf_relay_session_token"
    static let kDeviceToken = "tf_relay_device_token"
    static let kDeviceID = "tf_relay_device_id"
    static let kUsername = "tf_relay_username"
    static let kDeviceList = "tf_relay_device_list"

    private let client: RelayAccountClienting
    private let secrets: SecretStore
    private let defaults: UserDefaults
    private let hostname: String

    init(client: RelayAccountClienting = RelayAccountClient(),
         secrets: SecretStore = KeychainSecretStore(),
         defaults: UserDefaults = .standard,
         hostname: String = Host.current().localizedName ?? "Mac") {
        self.client = client
        self.secrets = secrets
        self.defaults = defaults
        self.hostname = hostname
    }

    /// Synchronous: set initial state from stored credentials + cached peers.
    /// The caller should kick `refreshDevices()` afterwards.
    func bootstrap() {
        username = defaults.string(forKey: Self.kUsername) ?? ""
        peers = loadCachedPeers()
        localDeviceID = secrets.load(Self.kDeviceID) ?? ""
        if secrets.load(Self.kDeviceToken) != nil && !localDeviceID.isEmpty {
            state = .loggedIn
        } else {
            state = .loggedOut
        }
        notify()
    }

    func login(username u: String, password p: String) async {
        state = .loggingIn; lastError = nil; notify()
        do { try await completeAuth(client.login(username: u, password: p)) }
        catch { fail(error) }
    }

    func register(username u: String, password p: String, inviteCode: String) async {
        state = .loggingIn; lastError = nil; notify()
        do { try await completeAuth(client.register(username: u, password: p, inviteCode: inviteCode)) }
        catch { fail(error) }
    }

    func refreshDevices() async {
        guard let sess = secrets.load(Self.kSessionToken) else {
            state = .sessionExpired; notify(); return
        }
        do {
            let list = try await client.listDevices(session: sess)
            peers = list.filter { $0.id != localDeviceID }
            cachePeers(peers)
            if state != .loggedIn { state = .loggedIn }
            notify()
        } catch let RelayAPIError.api(status, code, _) where status == 401 || code == "invalid_session" {
            state = .sessionExpired; notify()
        } catch {
            lastError = error.localizedDescription; notify()
        }
    }

    func logout() {
        secrets.delete(Self.kSessionToken)
        secrets.delete(Self.kDeviceToken)
        secrets.delete(Self.kDeviceID)
        defaults.removeObject(forKey: Self.kUsername)
        defaults.removeObject(forKey: Self.kDeviceList)
        username = ""; localDeviceID = ""; peers = []
        state = .loggedOut
        notify()
    }

    func synthesizedTargets() -> [OutputTarget] {
        guard let token = secrets.load(Self.kDeviceToken), !localDeviceID.isEmpty else { return [] }
        return RelayTargetSynthesis.targets(peers: peers, localDeviceID: localDeviceID,
                                            deviceToken: token, relayURL: RelayConfig.defaultRelayURL)
    }

    // MARK: - Private

    private func completeAuth(_ sess: RelaySession) async throws {
        try secrets.save(sess.token, for: Self.kSessionToken)
        username = sess.username
        defaults.set(sess.username, forKey: Self.kUsername)
        if secrets.load(Self.kDeviceToken) == nil {
            let dev = try await client.registerDevice(session: sess.token, label: hostname, role: "either")
            try secrets.save(dev.token, for: Self.kDeviceToken)
            try secrets.save(dev.id, for: Self.kDeviceID)
            localDeviceID = dev.id
        } else {
            localDeviceID = secrets.load(Self.kDeviceID) ?? ""
        }
        let list = try await client.listDevices(session: sess.token)
        peers = list.filter { $0.id != localDeviceID }
        cachePeers(peers)
        state = .loggedIn
        notify()
    }

    private func fail(_ error: Error) {
        lastError = error.localizedDescription
        // If we already hold device creds (re-login from expired), stay expired; else logged out.
        state = (secrets.load(Self.kDeviceToken) != nil && !localDeviceID.isEmpty) ? .sessionExpired : .loggedOut
        notify()
    }

    private func notify() { onChange?() }

    private func cachePeers(_ peers: [RelayPeerDevice]) {
        if let data = try? JSONEncoder().encode(peers) {
            defaults.set(data, forKey: Self.kDeviceList)
        }
    }
    private func loadCachedPeers() -> [RelayPeerDevice] {
        guard let data = defaults.data(forKey: Self.kDeviceList),
              let peers = try? JSONDecoder().decode([RelayPeerDevice].self, from: data)
        else { return [] }
        return peers
    }
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `swift test --filter AccountSessionTests 2>&1 | tail -20`
Expected: 8 个测试 PASS。

- [ ] **Step 5: 提交**

```bash
git add Type4Me/Services/AccountSession.swift Type4MeTests/AccountSessionTests.swift
git commit -m "feat(mac/relay): AccountSession login/register/refresh/logout state machine" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: RemoteTargetResolver + AppState 接入

**Files:**
- Create: `Type4Me/Services/RemoteTargetResolver.swift`
- Test: `Type4MeTests/RemoteTargetResolverTests.swift`
- Modify: `Type4Me/UI/AppState.swift`

- [ ] **Step 1: 写失败测试(纯决策单元)**

新建 `Type4MeTests/RemoteTargetResolverTests.swift`:

```swift
import XCTest
@testable import Type4Me

final class RemoteTargetResolverTests: XCTestCase {
    private func manual() -> [OutputTarget] {
        [OutputTarget(id: "manual-win", name: "Win", host: "1.1.1.1", port: 1, token: "t",
                      matchBundleIds: ["x"], enabled: true)]
    }
    private func synth() -> [OutputTarget] {
        [OutputTarget(id: "acct-win", name: "AcctWin", matchBundleIds: [], enabled: true,
                      mode: .relay, relayURL: URL(string: "https://r")!, deviceID: "d",
                      deviceToken: "t", targetDeviceID: "acct-win")]
    }

    func testLoggedInUsesSynthesized() {
        let t = RemoteTargetResolver.targets(accountState: .loggedIn, synthesized: synth, manual: manual)
        XCTAssertEqual(t.map(\.id), ["acct-win"])
    }
    func testSessionExpiredUsesSynthesized() {
        let t = RemoteTargetResolver.targets(accountState: .sessionExpired, synthesized: synth, manual: manual)
        XCTAssertEqual(t.map(\.id), ["acct-win"])
    }
    func testLoggedOutUsesManual() {
        let t = RemoteTargetResolver.targets(accountState: .loggedOut, synthesized: synth, manual: manual)
        XCTAssertEqual(t.map(\.id), ["manual-win"])
    }

    func testSanitizeResetsStaleRemoteOverride() {
        let o = RemoteTargetResolver.sanitizedOverride(.remote("gone"), targets: manual())
        XCTAssertEqual(o, .auto)
    }
    func testSanitizeKeepsValidRemoteOverride() {
        let o = RemoteTargetResolver.sanitizedOverride(.remote("manual-win"), targets: manual())
        XCTAssertEqual(o, .remote("manual-win"))
    }
    func testSanitizeLeavesAutoAndLocal() {
        XCTAssertEqual(RemoteTargetResolver.sanitizedOverride(.auto, targets: []), .auto)
        XCTAssertEqual(RemoteTargetResolver.sanitizedOverride(.local, targets: []), .local)
    }
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `swift test --filter RemoteTargetResolverTests 2>&1 | tail -20`
Expected: 编译失败 —— `RemoteTargetResolver` 未定义。

- [ ] **Step 3: 实现 RemoteTargetResolver.swift**

新建 `Type4Me/Services/RemoteTargetResolver.swift`:

```swift
import Foundation

/// Decides which target source feeds the router/selector, and sanitizes a stale
/// override after the source changes. Pure — no AppState dependency.
enum RemoteTargetResolver {
    static func targets(accountState: AccountSession.State,
                        synthesized: () -> [OutputTarget],
                        manual: () -> [OutputTarget]) -> [OutputTarget] {
        switch accountState {
        case .loggedIn, .sessionExpired: return synthesized()
        case .loggedOut, .loggingIn: return manual()
        }
    }

    /// If `override` points at a `.remote(id)` not present in `targets`, reset to `.auto`.
    static func sanitizedOverride(_ override: OutputOverride, targets: [OutputTarget]) -> OutputOverride {
        if case let .remote(id) = override, !targets.contains(where: { $0.id == id }) {
            return .auto
        }
        return override
    }
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `swift test --filter RemoteTargetResolverTests 2>&1 | tail -20`
Expected: 6 个测试 PASS。

- [ ] **Step 5: 接入 AppState**

`Type4Me/UI/AppState.swift`。

(a) 在 `// MARK: - Remote output routing` 区,`remoteTargets` 属性之后,新增 `account` 属性(保留 `remoteTargets` 现有默认初始化不变):

```swift
    /// Account session: when logged in, drives `remoteTargets` from the synthesized
    /// account device targets instead of credentials.json.
    let account = AccountSession()
```

(b) 把 `reloadRemoteTargets()` 改为委派给新的 `rebuildRemoteTargets()`,并新增 `rebuildRemoteTargets()` 与 `setupAccount()`:

```swift
    /// Reload targets (e.g., user hand-edited credentials.json while logged out).
    func reloadRemoteTargets() {
        rebuildRemoteTargets()
    }

    /// Recompute `remoteTargets` from the correct source for the current login state,
    /// then drop a stale `.remote` override that no longer matches.
    func rebuildRemoteTargets() {
        remoteTargets = RemoteTargetResolver.targets(
            accountState: account.state,
            synthesized: { self.account.synthesizedTargets() },
            manual: { OutputTargetStore().load() }
        )
        outputOverride = RemoteTargetResolver.sanitizedOverride(outputOverride, targets: remoteTargets)
    }

    /// Wire the account session to drive target rebuilds, restore prior login, and
    /// refresh the device list in the background. Call once from init.
    func setupAccount() {
        account.onChange = { [weak self] in self?.rebuildRemoteTargets() }
        account.bootstrap()
        rebuildRemoteTargets()
        Task { await account.refreshDevices() }
    }
```

(c) 在 `AppState` 的初始化器(`init`)末尾调用一次 `setupAccount()`。(`AppState()` 是现有无参 init;找到它的函数体结尾,在所有现有初始化逻辑之后加 `setupAccount()`。)

- [ ] **Step 6: 跑全量测试 + 构建**

Run: `swift build 2>&1 | tail -5 && swift test 2>&1 | tail -15`
Expected: 构建成功;全部测试 PASS(新老都绿;尤其既有 `OutputRouterTests`/`AppStateTests` 不回归)。

- [ ] **Step 7: 提交**

```bash
git add Type4Me/Services/RemoteTargetResolver.swift Type4MeTests/RemoteTargetResolverTests.swift Type4Me/UI/AppState.swift
git commit -m "feat(mac/relay): account-driven remoteTargets source switching in AppState" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: RemoteSettingsTab 账号区 UI(手动验证)

> 业务逻辑都在 Task 2–5。本任务只做 SwiftUI 绑定。自动验证 = `swift build` 通过;交互行为手动验证。

**Files:**
- Create: `Type4Me/UI/Settings/AccountCard.swift`
- Modify: `Type4Me/UI/Settings/RemoteSettingsTab.swift`

- [ ] **Step 1: 创建 AccountCard 视图**

新建 `Type4Me/UI/Settings/AccountCard.swift`。一个绑定 `appState.account` 的卡片,三态渲染。用现有 `SettingsCardHelpers`/`settingsGroupCard` 风格(参考 `RemoteSettingsTab` 现有用法)与 `L(中,英)` 本地化助手:

```swift
import SwiftUI

struct AccountCard: View, SettingsCardHelpers {
    @Environment(AppState.self) private var appState

    @State private var username = ""
    @State private var password = ""
    @State private var invite = ""
    @State private var registerMode = false

    var body: some View {
        settingsGroupCard(L("账号", "Account"), icon: "person.crop.circle") {
            switch appState.account.state {
            case .loggedOut, .loggingIn:
                loginForm
            case .loggedIn, .sessionExpired:
                loggedInView
            }
        }
    }

    private var isBusy: Bool { appState.account.state == .loggingIn }

    private var loginForm: some View {
        VStack(alignment: .leading, spacing: 10) {
            if appState.account.state == .sessionExpired {
                Text(L("会话已过期,请重新登录以刷新设备列表(已配置目标仍可使用)。",
                       "Session expired — log in again to refresh the device list (configured targets still work)."))
                    .font(.system(size: 12))
                    .foregroundStyle(TF.settingsAccentAmber)
            }
            TextField(L("用户名", "Username"), text: $username).textFieldStyle(.roundedBorder)
            SecureField(L("密码", "Password"), text: $password).textFieldStyle(.roundedBorder)
            if registerMode {
                TextField(L("邀请码", "Invite code"), text: $invite).textFieldStyle(.roundedBorder)
            }
            if let err = appState.account.lastError, !err.isEmpty {
                Text(err).font(.system(size: 12)).foregroundStyle(TF.settingsAccentRed)
            }
            HStack {
                Button(registerMode ? L("注册", "Register") : L("登录", "Log in")) {
                    Task {
                        if registerMode {
                            await appState.account.register(username: username, password: password, inviteCode: invite)
                        } else {
                            await appState.account.login(username: username, password: password)
                        }
                    }
                }
                .disabled(isBusy || username.isEmpty || password.isEmpty)

                Button(registerMode ? L("已有账号?去登录", "Have an account? Log in")
                                    : L("没有账号?去注册", "No account? Register")) {
                    registerMode.toggle()
                }
                .buttonStyle(.plain)
                .foregroundStyle(TF.settingsTextSecondary)
            }
        }
    }

    private var loggedInView: some View {
        VStack(alignment: .leading, spacing: 8) {
            row(L("账号", "Account"), appState.account.username.isEmpty ? "—" : appState.account.username)
            Text(L("我的设备", "My devices")).font(.system(size: 12)).foregroundStyle(TF.settingsTextSecondary)
            if appState.account.peers.isEmpty {
                Text(L("(没有其它设备)", "(no other devices)"))
                    .font(.system(size: 12)).foregroundStyle(TF.settingsTextTertiary)
            } else {
                ForEach(appState.account.peers, id: \.id) { peer in
                    HStack(spacing: 6) {
                        Image(systemName: peer.online ? "circle.fill" : "circle")
                            .font(.system(size: 8))
                            .foregroundStyle(peer.online ? TF.settingsAccentGreen : TF.settingsTextTertiary)
                        Text(peer.label).font(.system(size: 12)).foregroundStyle(TF.settingsText)
                    }
                }
            }
            HStack(spacing: 8) {
                Button(L("刷新", "Refresh")) { Task { await appState.account.refreshDevices() } }
                Button(L("退出登录", "Log out")) { appState.account.logout() }
            }
        }
    }

    private func row(_ label: String, _ value: String) -> some View {
        HStack(alignment: .top) {
            Text(label).font(.system(size: 12)).foregroundStyle(TF.settingsTextSecondary)
                .frame(width: 100, alignment: .leading)
            Text(value).font(.system(size: 12)).foregroundStyle(TF.settingsText).textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}
```

> 注:`SettingsCardHelpers`、`settingsGroupCard`、`TF.*` 颜色、`L(_:_:)` 都是现有约定(见 `RemoteSettingsTab.swift` / `SettingsCardHelpers.swift`)。若 `row` 与 `RemoteSettingsTab` 私有 `row` 命名冲突无碍(不同类型作用域)。如某 `TF` 颜色名不存在,选最接近的现有颜色。

- [ ] **Step 2: 在 RemoteSettingsTab 顶部插入账号卡**

`Type4Me/UI/Settings/RemoteSettingsTab.swift` 的 `body`,把 `activeOutputCard` 之前插入 `AccountCard()`:

```swift
            SettingsSectionHeader(
                label: L("远程", "Remote"),
                title: L("远程目标", "Remote Targets"),
                description: L( ... )   // 保持现有 description 不变
            )

            AccountCard()

            activeOutputCard

            credentialsCard
            ...
```

(仅新增 `AccountCard()` 一行,其余结构不动。)

- [ ] **Step 3: 构建**

Run: `swift build 2>&1 | tail -5`
Expected: 构建成功。

- [ ] **Step 4: 手动验证清单(记录,不在 CI)**

需运行 app + 一个可达的 relay(配了邀请码/会话密钥)人工确认:
1. 设置 → 远程,顶部账号区显示登录表单。错密码 → 红字提示,保留。
2. 登录成功(或注册带邀请码)→ 切到已登录视图(账号名、我的设备列表)。
3. 「激活目标」选择器列出 `本机 Mac + 账号设备`;选某台账号设备 → 下次说话文本发往该机器。
4. 重启 app → 免登录(凭据在 Keychain);设备列表用缓存先填充,随后后台刷新。
5. 退出登录 → 回登录表单;`remoteTargets` 回退到 credentials.json 手动来源;若之前锁定的是账号目标,override 重置为「自动」。
6. 登出态下原有手动目标 / LAN 直连 / `.auto` 行为不变。

- [ ] **Step 5: 提交**

```bash
git add Type4Me/UI/Settings/AccountCard.swift Type4Me/UI/Settings/RemoteSettingsTab.swift
git commit -m "feat(mac/ui): account login + device list section in RemoteSettingsTab" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## 收尾验证

- [ ] **全量测试 + 构建**

Run: `swift build 2>&1 | tail -5 && swift test 2>&1 | tail -15`
Expected: 构建成功;全部测试 PASS。

- [ ] **确认未回归**

既有 `OutputRouterTests` / `RemoteHTTPSinkClipboardFallbackTests` / `OutputTargetStoreTests` / `AppStateTests` 仍全绿(本计划未改路由内核;登出态行为不变)。

---

## 自查记录(spec 覆盖)

- 固化 relay URL → Task 1。
- 用户名+密码登录 / 注册+邀请码 → Task 2(client)+ Task 4(state)+ Task 6(UI)。
- 自动登记本机为设备(label=主机名)→ Task 4 `completeAuth`(无 device token 时)。
- 拉设备列表 + 排除本机 + 缓存 → Task 4 `refreshDevices`/`completeAuth`;合成 → Task 3。
- 凭据进 Keychain(session/device token/device id);用户名+列表进 UserDefaults → Task 4 + `SecretStore`/`KeychainSecretStore`。
- `remoteTargets` 按登录态切换来源;`.auto` 登出态不变;账号目标 matchBundleIds 空 → Task 3 + Task 5。
- 启动免登录直连 + 后台刷新 + 会话过期 → Task 4 `bootstrap`/`refreshDevices` + Task 5 `setupAccount`。
- 登出清凭据 + 重置 override → Task 4 `logout` + Task 5 `sanitizedOverride`。
- 错误码→中文消息 → Task 2 表 + `RelayAPIError: LocalizedError`。
- UI 三态(未登录/已登录/会话过期)在 RemoteSettingsTab 顶部 → Task 6。
- LAN 直连登出态保持 → Task 5(loggedOut→manual 来源)+ 收尾回归。
- dispatch 失败落本机兜底 → 复用既有 `RemoteHTTPSink.fallback`(不在本期改)。
```
