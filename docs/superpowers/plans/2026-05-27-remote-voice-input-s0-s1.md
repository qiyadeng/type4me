# Remote Voice Input — S0 + S1 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 Type4Me 的语音识别结果能通过 HTTP 投递到一台运行在远程机器(macOS)上的 Go 接收端,接收端调系统 API 把文字粘贴到当前焦点窗口。覆盖 spec 的 S0(Mac 端 OutputSink 重构,行为不变)和 S1(端到端 MVP,Mac↔Mac,无 UI)。

**Architecture:** Mac 端引入 `OutputSink` 协议,`LocalTextSink` 包装现有 `TextInjectionEngine`,`RemoteHTTPSink` 通过 `URLSession` POST 文字。`OutputRouter` 在录音开始那一刻查 `NSWorkspace.frontmostApplication` 并锁定 sink。Go 接收端 cgo 调 `NSPasteboard` + `CGEventCreateKeyboardEvent` 完成本地注入。S1 无 UI,目标通过 `credentials.json` 手动配置。

**Tech Stack:** Swift 6 (SPM), `URLSession`, `NSWorkspace`, Go 1.21+, cgo + Cocoa, `net/http` + `httptest`, `crypto/rand`, `crypto/subtle`。

**Spec:** `docs/superpowers/specs/2026-05-27-remote-voice-input-design.md`

---

## File Structure

**Phase A(Mac 端,S0 重构)**

- Create: `Type4Me/Injection/OutputSink.swift` —— 协议 + `InjectionOutcome` 已存在不复制
- Create: `Type4Me/Injection/LocalTextSink.swift` —— 包装 `TextInjectionEngine`
- Modify: `Type4Me/Session/RecognitionSession.swift` —— 字段 `injectionEngine` → `outputSink: OutputSink`,inject 调用点改为 sink

**Phase B(Mac 端,S1 路由 + 远程 sink)**

- Create: `Type4Me/Injection/OutputTarget.swift` —— 模型
- Create: `Type4Me/Services/OutputTargetStore.swift` —— 从 `credentials.json` 的 `tf_remote_targets` 节读取
- Create: `Type4Me/Injection/OutputRouter.swift` —— resolve(frontmostBundleId, override) → OutputSink
- Create: `Type4Me/Injection/RemoteHTTPSink.swift` —— HTTP client + 剪贴板兜底
- Modify: `Type4Me/UI/AppState.swift` —— 新增 `remoteTargets` / `outputOverride`(读 store + UserDefaults)
- Modify: `Type4Me/Session/RecognitionSession.swift` —— `startRecording` 锁定 sink

**Phase B 测试**

- Create: `Type4MeTests/OutputTargetTests.swift`
- Create: `Type4MeTests/OutputTargetStoreTests.swift`
- Create: `Type4MeTests/OutputRouterTests.swift`
- Create: `Type4MeTests/RemoteHTTPSinkTests.swift`

**Phase C(Go 接收端,macOS 平台)**

- Create: `receiver/go.mod`
- Create: `receiver/Makefile`
- Create: `receiver/cmd/type4me-receiver/main.go`
- Create: `receiver/internal/config/config.go`
- Create: `receiver/internal/inject/inject.go`(接口 + 平台无关测试桩)
- Create: `receiver/internal/inject/inject_darwin.go`(build tag darwin,cgo)
- Create: `receiver/internal/server/server.go`(handlers)
- Create: `receiver/internal/server/server_test.go`(httptest)

**Phase D(端到端冒烟)**

- Create: `scripts/test_remote_input.sh`

---

## Phase A — S0:OutputSink 抽象(行为不变)

### Task A1: 定义 `OutputSink` 协议与 `LocalTextSink`

**Files:**
- Create: `Type4Me/Injection/OutputSink.swift`
- Create: `Type4Me/Injection/LocalTextSink.swift`
- Create: `Type4MeTests/LocalTextSinkTests.swift`

- [ ] **Step 1: 写失败测试**

`Type4MeTests/LocalTextSinkTests.swift`:

```swift
import XCTest
@testable import Type4Me

final class LocalTextSinkTests: XCTestCase {
    func testInjectDelegatesToEngineAndReturnsOutcome() {
        let sink = LocalTextSink(engine: TextInjectionEngine())
        // We can't assert paste actually happened in unit test (no UI focus),
        // but we can assert the sink returns an InjectionOutcome value.
        let outcome = sink.inject("hello")
        // Outcome is one of the existing cases; just make sure call completes.
        switch outcome {
        case .pasted, .copiedToClipboard, .failed:
            break  // any of these is OK in a headless test environment
        }
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
swift test --filter LocalTextSinkTests
```

Expected: 编译失败 `cannot find type 'LocalTextSink' in scope`。

- [ ] **Step 3: 写 `OutputSink` 协议**

`Type4Me/Injection/OutputSink.swift`:

```swift
import Foundation

/// Abstraction for "where injected text goes".
///
/// Implementations:
/// - LocalTextSink: pastes into the frontmost macOS app via NSPasteboard + CGEvent
/// - RemoteHTTPSink: POSTs text to a remote receiver over HTTP
protocol OutputSink {
    /// Synchronously inject `text` and return the outcome.
    /// MUST be safe to call from a Task.detached context (see RecognitionSession).
    func inject(_ text: String) -> InjectionOutcome
}
```

注意:`InjectionOutcome` 已经在 `TextInjectionEngine.swift` 定义,不重复定义。

- [ ] **Step 4: 写 `LocalTextSink`**

`Type4Me/Injection/LocalTextSink.swift`:

```swift
import Foundation

/// OutputSink that pastes into the frontmost macOS app, using the existing
/// TextInjectionEngine. This is a thin wrapper to allow the engine to be
/// swapped for remote sinks at routing time.
final class LocalTextSink: OutputSink {
    private let engine: TextInjectionEngine

    init(engine: TextInjectionEngine = TextInjectionEngine()) {
        self.engine = engine
    }

    func inject(_ text: String) -> InjectionOutcome {
        engine.inject(text)
    }

    /// Pass-through: expose preserveClipboard so RecognitionSession can still
    /// configure it from defaults.
    var preserveClipboard: Bool {
        get { engine.preserveClipboard }
        set { engine.preserveClipboard = newValue }
    }

    /// Pass-through for the "finalize restore" lifecycle the engine already has.
    func finishClipboardRestore() {
        engine.finishClipboardRestore()
    }

    func copyToClipboard(_ text: String, transient: Bool = false) {
        engine.copyToClipboard(text, transient: transient)
    }
}
```

- [ ] **Step 5: 运行测试确认通过**

```bash
swift test --filter LocalTextSinkTests
```

Expected: PASS.

- [ ] **Step 6: 跑全量测试,确保没有连带影响**

```bash
swift test 2>&1 | tail -5
```

Expected: 全部通过(Phase A 这一步不动 RecognitionSession,所以是 0 风险增量)。

- [ ] **Step 7: Commit**

```bash
git add Type4Me/Injection/OutputSink.swift \
        Type4Me/Injection/LocalTextSink.swift \
        Type4MeTests/LocalTextSinkTests.swift
git commit -m "feat(injection): 引入 OutputSink 抽象 + LocalTextSink 包装

S0 第一步,无行为变化。后续 RemoteHTTPSink 会实现同一协议。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task A2: `RecognitionSession` 改用 `OutputSink`

**Files:**
- Modify: `Type4Me/Session/RecognitionSession.swift`(字段 `injectionEngine` → `outputSink`)

- [ ] **Step 1: 读取当前 RecognitionSession 中所有 `injectionEngine` 引用**

```bash
grep -n "injectionEngine" Type4Me/Session/RecognitionSession.swift
```

预期点位:line 56 字段定义、line 928 设置 `preserveClipboard`、line 935 `let engine = injectionEngine`、line 948 `engine.inject(finalText)`,以及其它对 `engine.copyToClipboard` 等的引用。

- [ ] **Step 2: 把字段类型从 `TextInjectionEngine` 改为 `LocalTextSink`**

`Type4Me/Session/RecognitionSession.swift` 改 line 56:

```swift
// 原:
private let injectionEngine = TextInjectionEngine()
// 改为:
private let outputSink: LocalTextSink = LocalTextSink()
```

并把后续所有 `injectionEngine` 重命名为 `outputSink`:

```bash
# 在 RecognitionSession.swift 内做局部替换;不要全局替换。
# 用编辑器把 `injectionEngine` → `outputSink` 在该文件里 replace all。
```

注意:`outputSink.preserveClipboard = ...`、`outputSink.copyToClipboard(...)`、`outputSink.finishClipboardRestore()` 调用都在 `LocalTextSink` 的 pass-through 里实现过。`engine.inject(finalText)` → `outputSink.inject(finalText)`。

- [ ] **Step 3: 编译**

```bash
swift build 2>&1 | tail -10
```

Expected: Build complete!  如果失败,有可能是漏改了某个 `injectionEngine` 引用 —— 修正后重编。

- [ ] **Step 4: 跑全量测试**

```bash
swift test 2>&1 | tail -5
```

Expected: 全部通过。S0 的成功信号:**行为不变,测试全过**。

- [ ] **Step 5: 手动冒烟(可选,推荐)**

```bash
swift build -c release
SKIP_QWEN3_BUILD=1 APP_PATH="$PWD/dist/Type4Me.app" VARIANT=cloud bash scripts/package-app.sh
open "$PWD/dist/Type4Me.app"
```

打开 Type4Me,用任意配置的 ASR provider 录一句话,文字应当像之前一样落到前台应用。

- [ ] **Step 6: Commit**

```bash
git add Type4Me/Session/RecognitionSession.swift
git commit -m "refactor(session): RecognitionSession 走 OutputSink 抽象

行为不变。把 TextInjectionEngine 的直接持有替换为 LocalTextSink,
为后续 RemoteHTTPSink 路由做准备。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

**Phase A 完工标志**:`swift test` 全过,手动冒烟现有的本机注入路径完整可用。

---

## Phase B — S1 Mac 端:路由 + 远程 sink

### Task B1: `OutputTarget` 数据模型

**Files:**
- Create: `Type4Me/Injection/OutputTarget.swift`
- Create: `Type4MeTests/OutputTargetTests.swift`

- [ ] **Step 1: 写失败测试**

`Type4MeTests/OutputTargetTests.swift`:

```swift
import XCTest
@testable import Type4Me

final class OutputTargetTests: XCTestCase {
    func testCodableRoundTrip() throws {
        let original = OutputTarget(
            id: "win-pc",
            name: "Win-PC",
            host: "192.168.1.42",
            port: 47318,
            token: "abc123",
            matchBundleIds: ["com.microsoft.rdc.macos", "com.parsecgaming.parsec"],
            enabled: true
        )
        let data = try JSONEncoder().encode(original)
        let decoded = try JSONDecoder().decode(OutputTarget.self, from: data)
        XCTAssertEqual(decoded, original)
    }

    func testBaseURLConstruction() {
        let t = OutputTarget(id: "x", name: "X", host: "10.0.0.1", port: 47318,
                             token: "tok", matchBundleIds: [], enabled: true)
        XCTAssertEqual(t.baseURL.absoluteString, "http://10.0.0.1:47318")
    }

    func testIPv6HostBracketed() {
        let t = OutputTarget(id: "x", name: "X", host: "fd7a::1", port: 47318,
                             token: "tok", matchBundleIds: [], enabled: true)
        XCTAssertEqual(t.baseURL.absoluteString, "http://[fd7a::1]:47318")
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
swift test --filter OutputTargetTests
```

Expected: 编译失败 `cannot find type 'OutputTarget' in scope`。

- [ ] **Step 3: 写 `OutputTarget`**

`Type4Me/Injection/OutputTarget.swift`:

```swift
import Foundation

/// A configured remote receiver Type4Me can route ASR text to.
///
/// Persistence: serialized into `credentials.json` under `tf_remote_targets`.
/// NOTE (S2): `token` will move to Keychain when the Settings UI lands.
struct OutputTarget: Codable, Equatable, Identifiable {
    /// Stable id, e.g. "win-pc" or a UUID. Used in HistoryStore.target_id and Keychain account.
    let id: String
    /// Human-readable name displayed in the menu bar override.
    var name: String
    /// Hostname or IP. IPv6 addresses are bracketed automatically in baseURL.
    var host: String
    /// TCP port the receiver listens on. Default 47318.
    var port: Int
    /// Bearer token for `Authorization: Bearer <token>`.
    var token: String
    /// macOS bundle ids that, when frontmost, route to this target.
    var matchBundleIds: [String]
    /// If false, this target is skipped during auto-route.
    var enabled: Bool

    var baseURL: URL {
        // Bracket IPv6 literals: detect by presence of ':' without prior bracket.
        let h = (host.contains(":") && !host.contains("[")) ? "[\(host)]" : host
        return URL(string: "http://\(h):\(port)")!
    }
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
swift test --filter OutputTargetTests
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add Type4Me/Injection/OutputTarget.swift Type4MeTests/OutputTargetTests.swift
git commit -m "feat(routing): OutputTarget 数据模型

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task B2: `OutputTargetStore` 从 credentials.json 读取

**Files:**
- Create: `Type4Me/Services/OutputTargetStore.swift`
- Create: `Type4MeTests/OutputTargetStoreTests.swift`

`credentials.json` 新节 `tf_remote_targets`,形如:

```json
{
  "tf_remote_targets": [
    {
      "id": "mac-self",
      "name": "Mac Self (smoke test)",
      "host": "127.0.0.1",
      "port": 47318,
      "token": "test-token-mvp",
      "matchBundleIds": ["com.microsoft.rdc.macos"],
      "enabled": true
    }
  ]
}
```

- [ ] **Step 1: 写失败测试**

`Type4MeTests/OutputTargetStoreTests.swift`:

```swift
import XCTest
@testable import Type4Me

final class OutputTargetStoreTests: XCTestCase {
    private var tempDir: URL!

    override func setUpWithError() throws {
        tempDir = URL(fileURLWithPath: NSTemporaryDirectory())
            .appendingPathComponent("OutputTargetStoreTests-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: tempDir, withIntermediateDirectories: true)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: tempDir)
    }

    func testLoadFromEmptyFileReturnsEmpty() {
        let file = tempDir.appendingPathComponent("credentials.json")
        try? "{}".write(to: file, atomically: true, encoding: .utf8)
        let store = OutputTargetStore(credentialsFile: file)
        XCTAssertEqual(store.load(), [])
    }

    func testLoadFromMissingFileReturnsEmpty() {
        let file = tempDir.appendingPathComponent("nonexistent.json")
        let store = OutputTargetStore(credentialsFile: file)
        XCTAssertEqual(store.load(), [])
    }

    func testLoadParsesTargetsArray() throws {
        let file = tempDir.appendingPathComponent("credentials.json")
        let json = #"""
        {
          "tf_remote_targets": [
            {
              "id": "win-pc", "name": "Win-PC", "host": "10.0.0.5", "port": 47318,
              "token": "tok", "matchBundleIds": ["com.microsoft.rdc.macos"], "enabled": true
            }
          ]
        }
        """#
        try json.write(to: file, atomically: true, encoding: .utf8)
        let store = OutputTargetStore(credentialsFile: file)
        let targets = store.load()
        XCTAssertEqual(targets.count, 1)
        XCTAssertEqual(targets.first?.id, "win-pc")
        XCTAssertEqual(targets.first?.matchBundleIds, ["com.microsoft.rdc.macos"])
    }

    func testLoadIgnoresMalformedEntries() throws {
        let file = tempDir.appendingPathComponent("credentials.json")
        let json = #"""
        {"tf_remote_targets": [{"name": "no-id"}, {"id": "ok", "name": "OK",
          "host": "1.1.1.1", "port": 80, "token": "t", "matchBundleIds": [], "enabled": true}]}
        """#
        try json.write(to: file, atomically: true, encoding: .utf8)
        let store = OutputTargetStore(credentialsFile: file)
        let targets = store.load()
        XCTAssertEqual(targets.map { $0.id }, ["ok"])
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
swift test --filter OutputTargetStoreTests
```

Expected: 编译失败。

- [ ] **Step 3: 写 `OutputTargetStore`**

`Type4Me/Services/OutputTargetStore.swift`:

```swift
import Foundation

/// Loads OutputTarget entries from `credentials.json` (key `tf_remote_targets`).
/// S1 stores all fields (including token) in the JSON file. S2 will migrate
/// the token to Keychain when the Settings UI is introduced.
final class OutputTargetStore {
    static let jsonKey = "tf_remote_targets"

    private let credentialsFile: URL

    init(credentialsFile: URL = OutputTargetStore.defaultCredentialsFile) {
        self.credentialsFile = credentialsFile
    }

    /// Returns the OutputTarget array, or empty if the file/key is missing or malformed.
    func load() -> [OutputTarget] {
        guard let data = try? Data(contentsOf: credentialsFile),
              let dict = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let arr = dict[Self.jsonKey] as? [[String: Any]]
        else { return [] }

        var out: [OutputTarget] = []
        for entry in arr {
            guard
                let id = entry["id"] as? String,
                let name = entry["name"] as? String,
                let host = entry["host"] as? String,
                let port = entry["port"] as? Int,
                let token = entry["token"] as? String,
                let bundleIds = entry["matchBundleIds"] as? [String],
                let enabled = entry["enabled"] as? Bool
            else { continue }
            out.append(OutputTarget(
                id: id, name: name, host: host, port: port, token: token,
                matchBundleIds: bundleIds, enabled: enabled
            ))
        }
        return out
    }

    static var defaultCredentialsFile: URL {
        let dir = FileManager.default
            .urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("Type4Me")
        return dir.appendingPathComponent("credentials.json")
    }
}
```

- [ ] **Step 4: 运行测试**

```bash
swift test --filter OutputTargetStoreTests
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add Type4Me/Services/OutputTargetStore.swift Type4MeTests/OutputTargetStoreTests.swift
git commit -m "feat(routing): OutputTargetStore 从 credentials.json 读取目标

S1 暂存所有字段(含 token)在 JSON;S2 引入 UI 时迁移 token 到 Keychain。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task B3: `OutputRouter.resolve` 路由解析

**Files:**
- Create: `Type4Me/Injection/OutputRouter.swift`
- Create: `Type4MeTests/OutputRouterTests.swift`

- [ ] **Step 1: 写失败测试**

`Type4MeTests/OutputRouterTests.swift`:

```swift
import XCTest
@testable import Type4Me

final class OutputRouterTests: XCTestCase {
    private let local = LocalTextSink()
    private let winTarget = OutputTarget(
        id: "win", name: "Win", host: "1.1.1.1", port: 47318,
        token: "t", matchBundleIds: ["com.microsoft.rdc.macos"], enabled: true
    )
    private let macTarget = OutputTarget(
        id: "mac2", name: "Mac2", host: "1.1.1.2", port: 47318,
        token: "t", matchBundleIds: ["com.apple.ScreenSharingViewer"], enabled: true
    )

    func makeRouter(targets: [OutputTarget]) -> OutputRouter {
        OutputRouter(localSink: local, targetsProvider: { targets })
    }

    func testManualOverrideLocalAlwaysWins() {
        let router = makeRouter(targets: [winTarget])
        let sink = router.resolve(frontmostBundleId: "com.microsoft.rdc.macos",
                                  override: .local)
        XCTAssertTrue(sink is LocalTextSink)
    }

    func testManualOverrideRemoteSelectsTarget() {
        let router = makeRouter(targets: [winTarget])
        let sink = router.resolve(frontmostBundleId: nil,
                                  override: .remote("win"))
        XCTAssertTrue(sink is RemoteHTTPSink)
    }

    func testManualOverrideRemoteUnknownIdFallsBackToLocal() {
        let router = makeRouter(targets: [winTarget])
        let sink = router.resolve(frontmostBundleId: "com.microsoft.rdc.macos",
                                  override: .remote("missing"))
        XCTAssertTrue(sink is LocalTextSink, "missing target id should not silently select another")
    }

    func testAutoMatchesBundleId() {
        let router = makeRouter(targets: [winTarget, macTarget])
        let sink = router.resolve(frontmostBundleId: "com.microsoft.rdc.macos",
                                  override: .auto)
        XCTAssertTrue(sink is RemoteHTTPSink)
        XCTAssertEqual((sink as? RemoteHTTPSink)?.target.id, "win")
    }

    func testAutoNoMatchReturnsLocal() {
        let router = makeRouter(targets: [winTarget])
        let sink = router.resolve(frontmostBundleId: "com.apple.finder",
                                  override: .auto)
        XCTAssertTrue(sink is LocalTextSink)
    }

    func testAutoFrontmostNilReturnsLocal() {
        let router = makeRouter(targets: [winTarget])
        let sink = router.resolve(frontmostBundleId: nil, override: .auto)
        XCTAssertTrue(sink is LocalTextSink)
    }

    func testAutoDisabledTargetSkipped() {
        var disabled = winTarget
        disabled.enabled = false
        let router = makeRouter(targets: [disabled])
        let sink = router.resolve(frontmostBundleId: "com.microsoft.rdc.macos",
                                  override: .auto)
        XCTAssertTrue(sink is LocalTextSink)
    }

    func testAutoFirstMatchWinsOnPriority() {
        // Two targets matching same bundle id — first wins.
        var alt = winTarget
        alt.id = "win-alt"
        let router = makeRouter(targets: [winTarget, alt])
        let sink = router.resolve(frontmostBundleId: "com.microsoft.rdc.macos",
                                  override: .auto)
        XCTAssertEqual((sink as? RemoteHTTPSink)?.target.id, "win")
    }
}
```

注意:该 test 引用了 `RemoteHTTPSink`,Task B4 才会实现 —— 这里先写测试,B4 完成后再 run。或者先用 stub `RemoteHTTPSink` 占位。**推荐做法**:先做 B4(RemoteHTTPSink)再做 B3 的 run/pass 步骤,或者临时在 B3 内插一个最小 RemoteHTTPSink 占位让编译过。

为不让任务之间互相阻塞,本计划顺序调整:**先 B4,再 B3**。

跳到 Task B4 实现 RemoteHTTPSink,再回来跑 B3 的测试。

---

### Task B4: `RemoteHTTPSink` HTTP 客户端

**Files:**
- Create: `Type4Me/Injection/RemoteHTTPSink.swift`
- Create: `Type4MeTests/RemoteHTTPSinkTests.swift`

- [ ] **Step 1: 写失败测试**

`Type4MeTests/RemoteHTTPSinkTests.swift`:

```swift
import XCTest
@testable import Type4Me

final class RemoteHTTPSinkTests: XCTestCase {
    /// Spin up a tiny in-process HTTP server, point RemoteHTTPSink at it,
    /// verify request shape and response handling.
    private var server: TestHTTPServer!
    private var target: OutputTarget!

    override func setUp() {
        super.setUp()
        server = TestHTTPServer()
        server.start()
        target = OutputTarget(
            id: "test", name: "Test", host: "127.0.0.1", port: server.port,
            token: "tok-123", matchBundleIds: [], enabled: true
        )
    }

    override func tearDown() {
        server.stop()
        server = nil
        super.tearDown()
    }

    func testInjectSendsBearerTokenAndJSONBody() {
        server.respond { req in
            XCTAssertEqual(req.method, "POST")
            XCTAssertEqual(req.path, "/inject")
            XCTAssertEqual(req.headers["Authorization"], "Bearer tok-123")
            XCTAssertEqual(req.headers["Content-Type"], "application/json")
            XCTAssertTrue(req.body.contains("\"text\":\"你好\""))
            return TestHTTPResponse(status: 200,
                body: #"{"ok":true,"outcome":{"pasted":true}}"#)
        }
        let sink = RemoteHTTPSink(target: target)
        let outcome = sink.inject("你好")
        XCTAssertEqual(outcome, .pasted)
    }

    func testInject401ReturnsFailed() {
        server.respond { _ in TestHTTPResponse(status: 401, body: #"{"error":"invalid_token"}"#) }
        let sink = RemoteHTTPSink(target: target)
        let outcome = sink.inject("hi")
        if case .failed = outcome {} else { XCTFail("expected .failed, got \(outcome)") }
    }

    func testInjectTimeoutReturnsCopiedToClipboardWhenFallbackEnabled() {
        server.respond { _ in
            Thread.sleep(forTimeInterval: 2.0)  // longer than 800ms timeout
            return TestHTTPResponse(status: 200, body: "{}")
        }
        let sink = RemoteHTTPSink(target: target, timeout: 0.3, clipboardFallback: true)
        let outcome = sink.inject("hi")
        if case .copiedToClipboard = outcome {} else { XCTFail("expected .copiedToClipboard") }
    }

    func testInjectConnectionRefused() {
        // Point at a port nothing is listening on
        let dead = OutputTarget(id: "x", name: "x", host: "127.0.0.1", port: 1,
                                token: "t", matchBundleIds: [], enabled: true)
        let sink = RemoteHTTPSink(target: dead, timeout: 0.3, clipboardFallback: false)
        let outcome = sink.inject("hi")
        if case .failed = outcome {} else { XCTFail("expected .failed") }
    }

    func testInjectPastedFalseWithReason() {
        server.respond { _ in
            TestHTTPResponse(status: 200,
                body: #"{"ok":false,"outcome":{"pasted":false,"reason":"no-focus"}}"#)
        }
        let sink = RemoteHTTPSink(target: target)
        let outcome = sink.inject("hi")
        // Treat "no-focus" as failed: the remote didn't accept the paste.
        if case .failed = outcome {} else { XCTFail("expected .failed for no-focus") }
    }
}
```

需要一个 `TestHTTPServer` helper。

`Type4MeTests/Helpers/TestHTTPServer.swift`(创建):

```swift
import Foundation
import Network

/// Minimal HTTP/1.1 server for in-process tests. Single-request lifecycle.
/// Not RFC-compliant; just enough to assert headers/body and return canned responses.
final class TestHTTPServer {
    struct Request {
        let method: String
        let path: String
        let headers: [String: String]
        let body: String
    }
    typealias Responder = (Request) -> TestHTTPResponse

    private(set) var port: Int = 0
    private var listener: NWListener?
    private var responder: Responder = { _ in TestHTTPResponse(status: 500, body: "no responder") }

    func start() {
        let listener = try! NWListener(using: .tcp, on: .any)
        self.listener = listener
        listener.newConnectionHandler = { [weak self] conn in
            self?.handle(conn)
        }
        let group = DispatchGroup()
        group.enter()
        listener.stateUpdateHandler = { state in
            if case .ready = state { group.leave() }
        }
        listener.start(queue: .global())
        _ = group.wait(timeout: .now() + 2)
        self.port = Int(listener.port?.rawValue ?? 0)
    }

    func stop() {
        listener?.cancel()
        listener = nil
    }

    func respond(_ r: @escaping Responder) {
        self.responder = r
    }

    private func handle(_ conn: NWConnection) {
        conn.start(queue: .global())
        conn.receive(minimumIncompleteLength: 1, maximumLength: 64 * 1024) { [weak self] data, _, _, _ in
            guard let self, let data, let raw = String(data: data, encoding: .utf8) else {
                conn.cancel(); return
            }
            let req = self.parse(raw)
            let resp = self.responder(req)
            let respData = self.serialize(resp)
            conn.send(content: respData, completion: .contentProcessed { _ in conn.cancel() })
        }
    }

    private func parse(_ raw: String) -> Request {
        let parts = raw.components(separatedBy: "\r\n\r\n")
        let head = parts.first ?? ""
        let body = parts.count > 1 ? parts[1] : ""
        var lines = head.components(separatedBy: "\r\n")
        let reqLine = lines.removeFirst().components(separatedBy: " ")
        var headers: [String: String] = [:]
        for line in lines {
            if let idx = line.firstIndex(of: ":") {
                let k = String(line[..<idx])
                let v = String(line[line.index(after: idx)...]).trimmingCharacters(in: .whitespaces)
                headers[k] = v
            }
        }
        return Request(
            method: reqLine.count > 0 ? reqLine[0] : "",
            path: reqLine.count > 1 ? reqLine[1] : "/",
            headers: headers,
            body: body
        )
    }

    private func serialize(_ r: TestHTTPResponse) -> Data {
        let bodyData = r.body.data(using: .utf8) ?? Data()
        var head = "HTTP/1.1 \(r.status) OK\r\n"
        head += "Content-Type: application/json\r\n"
        head += "Content-Length: \(bodyData.count)\r\n"
        head += "Connection: close\r\n\r\n"
        return (head.data(using: .utf8) ?? Data()) + bodyData
    }
}

struct TestHTTPResponse {
    let status: Int
    let body: String
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
swift test --filter RemoteHTTPSinkTests
```

Expected: 编译失败 `cannot find type 'RemoteHTTPSink'`.

- [ ] **Step 3: 实现 `RemoteHTTPSink`**

`Type4Me/Injection/RemoteHTTPSink.swift`:

```swift
import Foundation
import AppKit

/// OutputSink that posts text to a remote receiver over HTTP.
///
/// Synchronous: blocks the calling thread for up to `timeout` seconds.
/// RecognitionSession already calls this from a Task.detached, so the block is safe.
final class RemoteHTTPSink: OutputSink {
    let target: OutputTarget
    private let timeout: TimeInterval
    private let clipboardFallback: Bool
    private let session: URLSession

    init(target: OutputTarget,
         timeout: TimeInterval = 0.8,
         clipboardFallback: Bool = true,
         session: URLSession = .shared) {
        self.target = target
        self.timeout = timeout
        self.clipboardFallback = clipboardFallback
        self.session = session
    }

    func inject(_ text: String) -> InjectionOutcome {
        let url = target.baseURL.appendingPathComponent("inject")
        var req = URLRequest(url: url, timeoutInterval: timeout)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.setValue("Bearer \(target.token)", forHTTPHeaderField: "Authorization")

        let body: [String: Any] = [
            "text": text,
            "request_id": UUID().uuidString,
            "preserve_clipboard": true
        ]
        req.httpBody = try? JSONSerialization.data(withJSONObject: body)

        // Synchronous bridge over URLSession's async API.
        let sem = DispatchSemaphore(value: 0)
        var result: (Data?, URLResponse?, Error?) = (nil, nil, nil)
        let task = session.dataTask(with: req) { data, resp, err in
            result = (data, resp, err)
            sem.signal()
        }
        task.resume()
        _ = sem.wait(timeout: .now() + timeout + 0.2)

        if let err = result.2 {
            return fallback(text: text, reason: "transport: \(err.localizedDescription)")
        }
        guard let http = result.1 as? HTTPURLResponse else {
            return fallback(text: text, reason: "no-response")
        }
        switch http.statusCode {
        case 200:
            if let data = result.0,
               let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
               (obj["ok"] as? Bool) == true {
                return .pasted
            }
            return fallback(text: text, reason: "remote-rejected")
        case 401:
            return fallback(text: text, reason: "auth")
        case 413:
            return fallback(text: text, reason: "too-large")
        case 423:
            return fallback(text: text, reason: "concurrent")
        default:
            return fallback(text: text, reason: "http-\(http.statusCode)")
        }
    }

    private func fallback(text: String, reason: String) -> InjectionOutcome {
        if clipboardFallback {
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(text, forType: .string)
            return .copiedToClipboard
        }
        return .failed
    }
}
```

- [ ] **Step 4: 检查 `InjectionOutcome` 实际有哪些 case**

```bash
grep -nE "enum InjectionOutcome|case \." Type4Me/Injection/TextInjectionEngine.swift | head -10
```

如果实际 case 名字不是 `.pasted / .copiedToClipboard / .failed`,把 `RemoteHTTPSink` 与测试里对应的 case 改成实际名字(查实际定义为准)。**这一步是契合现有 enum 的必需校准。**

- [ ] **Step 5: 运行测试**

```bash
swift test --filter RemoteHTTPSinkTests
```

Expected: PASS。如有失败,根据测试报告调整 outcome 映射。

- [ ] **Step 6: Commit**

```bash
git add Type4Me/Injection/RemoteHTTPSink.swift \
        Type4MeTests/RemoteHTTPSinkTests.swift \
        Type4MeTests/Helpers/TestHTTPServer.swift
git commit -m "feat(routing): RemoteHTTPSink + TestHTTPServer 测试 helper

POST /inject 到远程接收端,失败时落到剪贴板兜底(可关)。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task B3 (resumed): `OutputRouter`

回到 Task B3。现在 `RemoteHTTPSink` 已存在,B3 的测试可以编译。

- [ ] **Step 1: 写 `OutputRouter`**

`Type4Me/Injection/OutputRouter.swift`:

```swift
import Foundation

enum OutputOverride: Equatable {
    case auto
    case local
    case remote(_ targetId: String)
}

/// Resolves the OutputSink for a recording session, given the frontmost app
/// and any user override.
final class OutputRouter {
    private let localSink: OutputSink
    private let targetsProvider: () -> [OutputTarget]

    init(localSink: OutputSink, targetsProvider: @escaping () -> [OutputTarget]) {
        self.localSink = localSink
        self.targetsProvider = targetsProvider
    }

    func resolve(frontmostBundleId: String?, override: OutputOverride) -> OutputSink {
        let targets = targetsProvider()

        switch override {
        case .local:
            return localSink
        case .remote(let id):
            if let t = targets.first(where: { $0.id == id && $0.enabled }) {
                return RemoteHTTPSink(target: t)
            }
            // Unknown / disabled override target — fall back to local rather than silently
            // picking a different remote. (Auto-route would do the same.)
            return localSink
        case .auto:
            guard let bundleId = frontmostBundleId else { return localSink }
            for t in targets where t.enabled {
                if t.matchBundleIds.contains(bundleId) {
                    return RemoteHTTPSink(target: t)
                }
            }
            return localSink
        }
    }
}
```

- [ ] **Step 2: 运行 B3 的测试**

```bash
swift test --filter OutputRouterTests
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add Type4Me/Injection/OutputRouter.swift Type4MeTests/OutputRouterTests.swift
git commit -m "feat(routing): OutputRouter 路由解析

manualOverride > auto-match > local;disabled / 未知 id 不静默回落到其他远程。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task B5: AppState 增加 outputOverride + remoteTargets

**Files:**
- Modify: `Type4Me/UI/AppState.swift`

- [ ] **Step 1: 查清 AppState 结构**

```bash
grep -nE "@Published|@AppStorage|init\(|class AppState" Type4Me/UI/AppState.swift | head -10
```

观察 AppState 用 SwiftUI 的 `@Observable` / `ObservableObject` / `@MainActor` 等模式。沿用其约定。

- [ ] **Step 2: 加属性**

在 `final class AppState { ... }` 内部加:

```swift
// MARK: - Remote output routing

/// All configured remote targets (loaded from credentials.json on startup).
@Published private(set) var remoteTargets: [OutputTarget] = OutputTargetStore().load()

/// Current user override. .auto unless user explicitly picked a target via menu bar.
/// Persisted across launches via UserDefaults `tf_output_override`.
@Published var outputOverride: OutputOverride = AppState.loadOverride() {
    didSet { AppState.saveOverride(outputOverride) }
}

private static let overrideKey = "tf_output_override"

private static func loadOverride() -> OutputOverride {
    let raw = UserDefaults.standard.string(forKey: overrideKey) ?? "auto"
    if raw == "auto" { return .auto }
    if raw == "local" { return .local }
    if raw.hasPrefix("remote:") { return .remote(String(raw.dropFirst("remote:".count))) }
    return .auto
}

private static func saveOverride(_ o: OutputOverride) {
    let raw: String
    switch o {
    case .auto: raw = "auto"
    case .local: raw = "local"
    case .remote(let id): raw = "remote:\(id)"
    }
    UserDefaults.standard.set(raw, forKey: overrideKey)
}

/// Reload targets from disk (e.g., user hand-edited credentials.json).
func reloadRemoteTargets() {
    remoteTargets = OutputTargetStore().load()
}
```

如果 AppState 不是 ObservableObject 而是 `@Observable`(Swift 5.9+ 宏),把 `@Published` 去掉,直接 `var`。具体看现有同文件其它属性的写法。

- [ ] **Step 3: 编译**

```bash
swift build 2>&1 | tail -5
```

Expected: Build complete.

- [ ] **Step 4: Commit**

```bash
git add Type4Me/UI/AppState.swift
git commit -m "feat(routing): AppState 增加 remoteTargets + outputOverride

S1 暂无 UI,通过 hand-edit credentials.json 配置远程目标。
outputOverride 默认 .auto,持久化在 UserDefaults。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task B6: `RecognitionSession` 在 startRecording 锁定 sink

**Files:**
- Modify: `Type4Me/Session/RecognitionSession.swift`

- [ ] **Step 1: 在 RecognitionSession 加 router 依赖与 lockedSink 状态**

```swift
// 替换 line 56 的 outputSink 字段(Task A2 改过):
private let localSink = LocalTextSink()
private let router: OutputRouter
private var lockedSink: OutputSink?

// init 里:
init(/* existing args */, appState: AppState) {
    /* existing assignments */
    self.router = OutputRouter(
        localSink: self.localSink,
        targetsProvider: { [weak appState] in appState?.remoteTargets ?? [] }
    )
}
```

具体 init 签名要顺着现有调用点改。先 grep 找出 `RecognitionSession(` 实例化位置:

```bash
grep -rn "RecognitionSession(" Type4Me/ --include="*.swift"
```

- [ ] **Step 2: 在 startRecording 锁定 sink**

找到既有的 `targetBundleId = NSWorkspace.shared.frontmostApplication?.bundleIdentifier`(line 236),紧接其后加:

```swift
let override = appState.outputOverride  // capture once
lockedSink = router.resolve(
    frontmostBundleId: NSWorkspace.shared.frontmostApplication?.bundleIdentifier,
    override: override
)
DebugFileLogger.log("startRecording: sink locked = \(type(of: lockedSink!))")
```

- [ ] **Step 3: 在 inject 调用点用 lockedSink**

`Type4Me/Session/RecognitionSession.swift` line 935-948 区域,原代码大致:

```swift
let engine = outputSink           // (Task A2 改过的名字)
// ...
outcome = engine.inject(finalText)
```

改为:

```swift
let sink: OutputSink = lockedSink ?? localSink
// ...
outcome = sink.inject(finalText)
```

注意 `outputSink.preserveClipboard / .copyToClipboard / .finishClipboardRestore` 这些 LocalTextSink 专有的 pass-through 调用,**只对 local sink 有意义**。在调用前判断:

```swift
if let local = lockedSink as? LocalTextSink ?? localSink as? LocalTextSink {
    local.preserveClipboard = defaults.object(forKey: "tf_preserveClipboard") != nil
}
// 调用 inject:
outcome = (lockedSink ?? localSink).inject(finalText)
```

剪贴板 finalize / restore 对 RemoteHTTPSink 无意义,跳过即可。

- [ ] **Step 4: 编译**

```bash
swift build 2>&1 | tail -10
```

Expected: Build complete!

- [ ] **Step 5: 跑全量测试**

```bash
swift test 2>&1 | tail -5
```

Expected: 全过。

- [ ] **Step 6: Commit**

```bash
git add Type4Me/Session/RecognitionSession.swift
git commit -m "feat(session): RecognitionSession 在 startRecording 锁定 OutputSink

读 NSWorkspace.frontmostApplication + AppState.outputOverride,通过
OutputRouter.resolve 选定 sink,inject 时使用锁定值。Mid-recording
切换前台 app 不会改变路由。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task B7: 剪贴板兜底的端到端验证测试

**Files:**
- 无新代码;只验证已有 RemoteHTTPSink 的 `clipboardFallback` 在集成场景下行为正确

- [ ] **Step 1: 加一个集成测试**

`Type4MeTests/RemoteHTTPSinkClipboardFallbackTests.swift`(创建):

```swift
import XCTest
import AppKit
@testable import Type4Me

final class RemoteHTTPSinkClipboardFallbackTests: XCTestCase {
    func testClipboardContainsTextAfterConnectionFailure() {
        // Save current pasteboard so we don't pollute the dev machine.
        let pb = NSPasteboard.general
        let saved = pb.string(forType: .string)
        defer {
            pb.clearContents()
            if let saved { pb.setString(saved, forType: .string) }
        }

        let dead = OutputTarget(
            id: "dead", name: "dead", host: "127.0.0.1", port: 1,
            token: "t", matchBundleIds: [], enabled: true
        )
        let sink = RemoteHTTPSink(target: dead, timeout: 0.2, clipboardFallback: true)
        let outcome = sink.inject("火锅底料")
        if case .copiedToClipboard = outcome {} else {
            XCTFail("expected .copiedToClipboard, got \(outcome)")
        }
        XCTAssertEqual(pb.string(forType: .string), "火锅底料")
    }
}
```

- [ ] **Step 2: 运行**

```bash
swift test --filter RemoteHTTPSinkClipboardFallbackTests
```

Expected: PASS。如果 macOS 测试环境对剪贴板有沙箱限制,可能 NSPasteboard 在 test bundle 内行为受限 —— 真出问题改为手测,标注此测试为 `.skip` 留作 v2 补。

- [ ] **Step 3: Commit**

```bash
git add Type4MeTests/RemoteHTTPSinkClipboardFallbackTests.swift
git commit -m "test(routing): RemoteHTTPSink 剪贴板兜底集成测试

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

**Phase B 完工标志:**
- 全部 `swift test` 通过
- AppState 加载 `credentials.json` 的 `tf_remote_targets` 节
- `OutputRouter.resolve` 行为符合 spec § 3.5
- Mac 端代码就绪,只差远程接收端可用即可端到端验证

---

## Phase C — S1 接收端:Go HTTP 服务(macOS 平台)

### Task C1: Go 项目脚手架

**Files:**
- Create: `receiver/go.mod`
- Create: `receiver/Makefile`
- Create: `receiver/cmd/type4me-receiver/main.go`(占位)
- Create: `receiver/.gitignore`

- [ ] **Step 1: 初始化 Go module**

```bash
mkdir -p receiver/cmd/type4me-receiver receiver/internal
cd receiver && go mod init github.com/qiyadeng/type4me/receiver && cd ..
```

`receiver/go.mod` 应类似:

```
module github.com/qiyadeng/type4me/receiver

go 1.21
```

- [ ] **Step 2: 写占位 main.go**

`receiver/cmd/type4me-receiver/main.go`:

```go
package main

import "fmt"

func main() {
    fmt.Println("type4me-receiver placeholder")
}
```

- [ ] **Step 3: 写 Makefile**

`receiver/Makefile`:

```makefile
.PHONY: build build-darwin build-darwin-arm64 build-darwin-amd64 build-windows test clean

VERSION ?= 0.1.0
DIST := dist

build: build-darwin

build-darwin: build-darwin-arm64 build-darwin-amd64

build-darwin-arm64:
	mkdir -p $(DIST)
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
		go build -ldflags "-X main.version=$(VERSION)" \
		-o $(DIST)/type4me-receiver-darwin-arm64 ./cmd/type4me-receiver

build-darwin-amd64:
	mkdir -p $(DIST)
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 \
		go build -ldflags "-X main.version=$(VERSION)" \
		-o $(DIST)/type4me-receiver-darwin-amd64 ./cmd/type4me-receiver

# Windows: cgo not used (pure syscall in inject_windows.go, added in S3).
build-windows:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
		go build -ldflags "-X main.version=$(VERSION)" \
		-o $(DIST)/type4me-receiver-windows-amd64.exe ./cmd/type4me-receiver

test:
	go test ./...

clean:
	rm -rf $(DIST)
```

- [ ] **Step 4: 写 .gitignore**

`receiver/.gitignore`:

```
dist/
*.exe
*.dSYM
```

- [ ] **Step 5: 验证 build 与 run**

```bash
cd receiver && make build-darwin-arm64 && ./dist/type4me-receiver-darwin-arm64
```

Expected: 输出 `type4me-receiver placeholder`.

- [ ] **Step 6: Commit**

```bash
git add receiver/go.mod receiver/Makefile receiver/.gitignore \
        receiver/cmd/type4me-receiver/main.go
git commit -m "feat(receiver): Go 接收端脚手架

cmd/main.go 占位 + Makefile 支持 darwin-arm64/amd64 + windows-amd64
(Windows inject 在 S3 实现)。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task C2: 配置加载

**Files:**
- Create: `receiver/internal/config/config.go`
- Create: `receiver/internal/config/config_test.go`

- [ ] **Step 1: 写失败测试**

`receiver/internal/config/config_test.go`:

```go
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	data, _ := json.Marshal(map[string]any{"port": 9999, "bind_addr": "0.0.0.0", "name": "from-file"})
	os.WriteFile(cfgFile, data, 0600)

	t.Setenv("TYPE4ME_PORT", "47318")
	t.Setenv("TYPE4ME_TOKEN", "env-token")
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 47318 {
		t.Errorf("env did not override port: got %d", cfg.Port)
	}
	if cfg.Token != "env-token" {
		t.Errorf("token from env not picked up: got %q", cfg.Token)
	}
	if cfg.Name != "from-file" {
		t.Errorf("name from file lost: got %q", cfg.Name)
	}
}

func TestLoadGeneratesTokenWhenMissing(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	os.WriteFile(cfgFile, []byte("{}"), 0600)
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Token) < 32 {
		t.Errorf("expected generated token >=32 chars, got %d", len(cfg.Token))
	}
}

func TestSavePersistsNonSecretFields(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	os.WriteFile(cfgFile, []byte("{}"), 0600)
	cfg, _ := Load(cfgFile)
	cfg.Name = "Test"
	if err := cfg.Save(cfgFile); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cfg2, _ := Load(cfgFile)
	if cfg2.Name != "Test" {
		t.Errorf("Save didn't persist Name: got %q", cfg2.Name)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd receiver && go test ./internal/config/...
```

Expected: 编译失败 `undefined: Load`.

- [ ] **Step 3: 实现 Config**

`receiver/internal/config/config.go`:

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

// Config holds the receiver's runtime configuration.
//
// Sources, in priority order:
//   1. Environment variables (TYPE4ME_PORT, TYPE4ME_TOKEN, TYPE4ME_BIND_ADDR, TYPE4ME_NAME)
//   2. Config file JSON
//   3. Defaults
//
// Token: if absent in both env and file, one is generated (32 random bytes,
// base64url-encoded) and persisted to the file on next Save.
type Config struct {
	Port     int    `json:"port"`
	BindAddr string `json:"bind_addr"`
	Name     string `json:"name"`
	Token    string `json:"token"`  // S1: stored in file; S2+ moves to Keychain.
}

const (
	DefaultPort     = 47318
	DefaultBindAddr = "0.0.0.0"
)

// Load reads config from the given file path, applies env overrides, and
// generates a token if missing. Save() must be called to persist a generated token.
func Load(path string) (*Config, error) {
	cfg := &Config{
		Port:     DefaultPort,
		BindAddr: DefaultBindAddr,
		Name:     hostname(),
	}

	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, cfg)  // best effort; ignore parse errors
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

	if cfg.Token == "" {
		tok, err := generateToken()
		if err != nil {
			return nil, fmt.Errorf("generate token: %w", err)
		}
		cfg.Token = tok
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

- [ ] **Step 4: 运行测试**

```bash
cd receiver && go test ./internal/config/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add receiver/internal/config/
git commit -m "feat(receiver): 配置加载,支持 env > file > defaults

token 缺失时自动生成 32 字节 base64url。S1 直接落 JSON 文件;
后续 S2 Mac 端通用 hybrid 存储模型时,可在接收端引入 Keychain/DPAPI。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task C3: Injector 接口与 fake 实现

**Files:**
- Create: `receiver/internal/inject/inject.go`
- Create: `receiver/internal/inject/fake.go`
- Create: `receiver/internal/inject/fake_test.go`

- [ ] **Step 1: 写失败测试**

`receiver/internal/inject/fake_test.go`:

```go
package inject

import "testing"

func TestFakeInjectorRecordsCalls(t *testing.T) {
	f := &Fake{}
	out, err := f.Inject("hello")
	if err != nil { t.Fatal(err) }
	if !out.Pasted { t.Errorf("default fake should report pasted=true") }
	if len(f.Calls) != 1 || f.Calls[0] != "hello" {
		t.Errorf("Calls = %v", f.Calls)
	}
}

func TestFakeInjectorReturnsNoFocus(t *testing.T) {
	f := &Fake{NextReason: "no-focus"}
	out, _ := f.Inject("x")
	if out.Pasted { t.Errorf("Pasted should be false when reason set") }
	if out.Reason != "no-focus" { t.Errorf("Reason = %q", out.Reason) }
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd receiver && go test ./internal/inject/...
```

Expected: 编译失败。

- [ ] **Step 3: 实现接口与 Fake**

`receiver/internal/inject/inject.go`:

```go
package inject

// Outcome describes what happened during an inject attempt.
type Outcome struct {
	Pasted bool   `json:"pasted"`
	Reason string `json:"reason,omitempty"` // "" | "no-focus" | "clipboard-locked" | "paste-blocked"
}

// Injector is the platform-abstraction for putting `text` into the current
// foreground application's focused control.
type Injector interface {
	// Inject sets the clipboard to `text` and simulates a paste keystroke.
	// Returns Outcome (never partial); err is reserved for internal API failures.
	Inject(text string) (Outcome, error)
	// Ping returns nil if the platform API is available.
	Ping() error
}
```

`receiver/internal/inject/fake.go`:

```go
package inject

import "sync"

// Fake is an in-memory Injector for tests.
type Fake struct {
	mu         sync.Mutex
	Calls      []string
	NextReason string  // if set, the next Inject returns Pasted=false with this reason
	NextError  error
}

func (f *Fake) Inject(text string) (Outcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, text)
	if f.NextError != nil {
		err := f.NextError
		f.NextError = nil
		return Outcome{}, err
	}
	if f.NextReason != "" {
		r := f.NextReason
		f.NextReason = ""
		return Outcome{Pasted: false, Reason: r}, nil
	}
	return Outcome{Pasted: true}, nil
}

func (f *Fake) Ping() error { return nil }
```

- [ ] **Step 4: 运行测试**

```bash
cd receiver && go test ./internal/inject/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add receiver/internal/inject/
git commit -m "feat(receiver): Injector 接口 + Fake 测试实现

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task C4: HTTP 服务器(/ping + /inject + 鉴权)

**Files:**
- Create: `receiver/internal/server/server.go`
- Create: `receiver/internal/server/server_test.go`

- [ ] **Step 1: 写失败测试**

`receiver/internal/server/server_test.go`:

```go
package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qiyadeng/type4me/receiver/internal/inject"
)

func newTestServer(t *testing.T, inj inject.Injector, token string) (*httptest.Server, *Server) {
	t.Helper()
	s := New(Options{
		Token:    token,
		Injector: inj,
		Name:     "TestRX",
		Platform: "darwin",
		Version:  "test",
	})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, s
}

func TestPingNoAuth(t *testing.T) {
	ts, _ := newTestServer(t, &inject.Fake{}, "secret")
	resp, err := http.Get(ts.URL + "/ping")
	if err != nil { t.Fatal(err) }
	if resp.StatusCode != 200 { t.Errorf("status = %d", resp.StatusCode) }
}

func TestInjectMissingTokenReturns401(t *testing.T) {
	ts, _ := newTestServer(t, &inject.Fake{}, "secret")
	resp, err := http.Post(ts.URL+"/inject", "application/json",
		strings.NewReader(`{"text":"hi"}`))
	if err != nil { t.Fatal(err) }
	if resp.StatusCode != 401 { t.Errorf("status = %d", resp.StatusCode) }
}

func TestInjectWrongTokenReturns401(t *testing.T) {
	ts, _ := newTestServer(t, &inject.Fake{}, "secret")
	req, _ := http.NewRequest("POST", ts.URL+"/inject",
		strings.NewReader(`{"text":"hi"}`))
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil { t.Fatal(err) }
	if resp.StatusCode != 401 { t.Errorf("status = %d", resp.StatusCode) }
}

func TestInjectOKReturns200(t *testing.T) {
	fake := &inject.Fake{}
	ts, _ := newTestServer(t, fake, "secret")
	body, _ := json.Marshal(map[string]any{"text": "hello"})
	req, _ := http.NewRequest("POST", ts.URL+"/inject", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil { t.Fatal(err) }
	if resp.StatusCode != 200 { t.Errorf("status = %d", resp.StatusCode) }
	if len(fake.Calls) != 1 || fake.Calls[0] != "hello" {
		t.Errorf("inject not called as expected: %v", fake.Calls)
	}
}

func TestInjectTooLargeReturns413(t *testing.T) {
	ts, _ := newTestServer(t, &inject.Fake{}, "secret")
	body, _ := json.Marshal(map[string]any{"text": strings.Repeat("x", 9000)})
	req, _ := http.NewRequest("POST", ts.URL+"/inject", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil { t.Fatal(err) }
	if resp.StatusCode != 413 { t.Errorf("status = %d", resp.StatusCode) }
}

// Slow fake to test the 423 path: hold the mutex for longer than the next
// request will wait.
type slowFake struct {
	hold time.Duration
}

func (s *slowFake) Inject(text string) (inject.Outcome, error) {
	time.Sleep(s.hold)
	return inject.Outcome{Pasted: true}, nil
}
func (s *slowFake) Ping() error { return nil }

func TestInjectConcurrentReturns423(t *testing.T) {
	ts, _ := newTestServer(t, &slowFake{hold: 800 * time.Millisecond}, "secret")
	body := []byte(`{"text":"a"}`)
	mkReq := func() *http.Request {
		req, _ := http.NewRequest("POST", ts.URL+"/inject", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		return req
	}
	var wg sync.WaitGroup
	wg.Add(2)
	var statuses [2]int
	for i := 0; i < 2; i++ {
		go func(idx int) {
			defer wg.Done()
			resp, err := http.DefaultClient.Do(mkReq())
			if err != nil { t.Error(err); return }
			statuses[idx] = resp.StatusCode
		}(i)
		time.Sleep(50 * time.Millisecond)  // ensure stable ordering
	}
	wg.Wait()
	saw423 := statuses[0] == 423 || statuses[1] == 423
	if !saw423 {
		t.Errorf("expected one 423 among %v", statuses)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd receiver && go test ./internal/server/...
```

Expected: 编译失败。

- [ ] **Step 3: 实现 server**

`receiver/internal/server/server.go`:

```go
package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/qiyadeng/type4me/receiver/internal/inject"
)

const (
	MaxBodyBytes = 16 * 1024 // 16 KB headroom; we enforce 8KB text below
	MaxTextBytes = 8 * 1024
	MutexHold    = 500 * time.Millisecond // wait at most 500ms for injector mutex
)

type Options struct {
	Token    string
	Injector inject.Injector
	Name     string
	Platform string
	Version  string
}

type Server struct {
	opts Options
	mu   chan struct{} // 1-slot semaphore (acts as the inject mutex)
}

func New(opts Options) *Server {
	s := &Server{opts: opts, mu: make(chan struct{}, 1)}
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", s.handlePing)
	mux.HandleFunc("/info", s.requireAuth(s.handleInfo))
	mux.HandleFunc("/inject", s.requireAuth(s.handleInject))
	return mux
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"ok":       true,
		"name":     s.opts.Name,
		"platform": s.opts.Platform,
		"version":  s.opts.Version,
	})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"name":     s.opts.Name,
		"platform": s.opts.Platform,
	})
}

type injectRequest struct {
	Text              string `json:"text"`
	RequestID         string `json:"request_id"`
	PreserveClipboard *bool  `json:"preserve_clipboard"`
}

func (s *Server) handleInject(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	var req injectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	if len(req.Text) > MaxTextBytes {
		writeJSON(w, 413, map[string]any{"error": "text_too_large", "max_bytes": MaxTextBytes})
		return
	}

	// Acquire inject mutex (best-effort, 500ms ceiling).
	select {
	case s.mu <- struct{}{}:
		defer func() { <-s.mu }()
	case <-time.After(MutexHold):
		writeJSON(w, 423, map[string]string{"error": "concurrent_inject"})
		return
	}

	out, err := s.opts.Injector.Inject(req.Text)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "platform_error", "detail": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok":         out.Pasted,
		"request_id": req.RequestID,
		"outcome":    out,
	})
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(h, prefix) {
			writeJSON(w, 401, map[string]string{"error": "invalid_token"})
			return
		}
		got := h[len(prefix):]
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.opts.Token)) != 1 {
			writeJSON(w, 401, map[string]string{"error": "invalid_token"})
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil && !errors.Is(err, http.ErrBodyNotAllowed) {
		// best effort; nothing else to do
	}
}
```

- [ ] **Step 4: 运行测试**

```bash
cd receiver && go test ./internal/server/...
```

Expected: PASS(全部 6 个 case)。

- [ ] **Step 5: Commit**

```bash
git add receiver/internal/server/
git commit -m "feat(receiver): HTTP server /ping + /inject + /info

- Bearer token 鉴权,constant-time 比较
- 8KB text 限制(413)
- 500ms 抢锁超时返回 423 concurrent_inject
- inject 调用走 Injector 接口,平台层在后续 task 接入

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task C5: macOS Injector(cgo)

**Files:**
- Create: `receiver/internal/inject/inject_darwin.go`(build tag `darwin`)
- Create: `receiver/internal/inject/inject_darwin.m`(cgo C/Objective-C 实现)

cgo 调 Cocoa 无法在 CI 跨平台测试,本 task **不写自动化测试**;Task D1 提供端到端 shell 冒烟脚本来验证。

- [ ] **Step 1: 写平台实现**

`receiver/internal/inject/inject_darwin.go`:

```go
//go:build darwin

package inject

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework Carbon

#include <stdlib.h>

// Returns 1 on success, 0 on failure.
int t4m_set_clipboard(const char* utf8);

// Returns 1 on success, 0 on failure.
int t4m_simulate_paste(void);
*/
import "C"
import (
	"errors"
	"time"
	"unsafe"
)

type macInjector struct{}

func NewPlatform() Injector { return &macInjector{} }

func (m *macInjector) Inject(text string) (Outcome, error) {
	if text == "" {
		return Outcome{Pasted: false, Reason: "empty"}, nil
	}
	cs := C.CString(text)
	defer C.free(unsafe.Pointer(cs))
	if C.t4m_set_clipboard(cs) != 1 {
		return Outcome{}, errors.New("set clipboard failed")
	}
	// Small delay so the focused app sees the clipboard change before paste.
	time.Sleep(20 * time.Millisecond)
	if C.t4m_simulate_paste() != 1 {
		return Outcome{Pasted: false, Reason: "paste-blocked"}, nil
	}
	return Outcome{Pasted: true}, nil
}

func (m *macInjector) Ping() error {
	// On macOS, NSPasteboard is always available within a Cocoa runtime; nothing to check.
	return nil
}
```

- [ ] **Step 2: 写 Objective-C 实现**

`receiver/internal/inject/inject_darwin.m`:

```objc
//go:build darwin

#import <Cocoa/Cocoa.h>
#import <Carbon/Carbon.h>

int t4m_set_clipboard(const char* utf8) {
    @autoreleasepool {
        NSString *s = [NSString stringWithUTF8String:utf8];
        if (s == nil) return 0;
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        return [pb setString:s forType:NSPasteboardTypeString] ? 1 : 0;
    }
}

int t4m_simulate_paste(void) {
    // Cmd+V keystroke
    CGEventSourceRef src = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
    if (!src) return 0;
    CGEventRef down = CGEventCreateKeyboardEvent(src, (CGKeyCode)kVK_ANSI_V, true);
    CGEventRef up   = CGEventCreateKeyboardEvent(src, (CGKeyCode)kVK_ANSI_V, false);
    if (!down || !up) {
        if (down) CFRelease(down);
        if (up)   CFRelease(up);
        CFRelease(src);
        return 0;
    }
    CGEventSetFlags(down, kCGEventFlagMaskCommand);
    CGEventSetFlags(up,   kCGEventFlagMaskCommand);
    CGEventPost(kCGHIDEventTap, down);
    CGEventPost(kCGHIDEventTap, up);
    CFRelease(down); CFRelease(up); CFRelease(src);
    return 1;
}
```

- [ ] **Step 3: 编译验证**

```bash
cd receiver && CGO_ENABLED=1 go build ./internal/inject/
```

Expected: 编译通过(无输出 = 成功)。

- [ ] **Step 4: 手动验证最小集成**

写一个临时 sandbox 程序验证 cgo 桥能用(不 commit):

```bash
cat > /tmp/t4m_smoke.go << 'EOF'
package main

import (
	"fmt"
	"github.com/qiyadeng/type4me/receiver/internal/inject"
)

func main() {
	inj := inject.NewPlatform()
	out, err := inj.Inject("hello from cgo")
	fmt.Printf("outcome=%+v err=%v\n", out, err)
}
EOF
cd receiver && go run /tmp/t4m_smoke.go
```

预期看到剪贴板里有 "hello from cgo",且如果当前有焦点输入框,会粘贴一次。验证完删 `/tmp/t4m_smoke.go`。

- [ ] **Step 5: Commit**

```bash
git add receiver/internal/inject/inject_darwin.go \
        receiver/internal/inject/inject_darwin.m
git commit -m "feat(receiver): macOS Injector(cgo + Cocoa + Carbon)

NSPasteboard setString + CGEvent Cmd+V 模拟。仅 darwin build tag。
Windows 实现在 S3。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task C6: 串起 main.go

**Files:**
- Modify: `receiver/cmd/type4me-receiver/main.go`

- [ ] **Step 1: 写 main.go**

`receiver/cmd/type4me-receiver/main.go`(覆盖占位):

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

	printPairingInfo(cfg, addr)

	go func() {
		log.Printf("listening on %s", addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	log.Println("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
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

// printPairingInfo prints a developer-friendly summary to stdout. Once we add
// tray UI (S4), it will move into a pairing window.
func printPairingInfo(cfg *config.Config, addr string) {
	fmt.Println()
	fmt.Println("================ type4me-receiver pairing ================")
	fmt.Printf("  Name:    %s\n", cfg.Name)
	fmt.Printf("  Addr:    %s\n", addr)
	fmt.Printf("  Token:   %s\n", cfg.Token)
	fmt.Printf("  URL:     type4me://pair?host=%s&port=%d&token=%s&name=%s&platform=%s\n",
		cfg.BindAddr, cfg.Port, cfg.Token, cfg.Name, runtime.GOOS)
	fmt.Println("==========================================================")
	fmt.Println()
}
```

- [ ] **Step 2: 编译**

```bash
cd receiver && make build-darwin-arm64
```

Expected: `dist/type4me-receiver-darwin-arm64` 生成。

- [ ] **Step 3: 启动一次确认能监听**

```bash
cd receiver && TYPE4ME_TOKEN=smoke-token TYPE4ME_PORT=47318 ./dist/type4me-receiver-darwin-arm64 &
sleep 1
curl -s http://127.0.0.1:47318/ping
echo
kill %1 || true
```

Expected: `/ping` 返回 `{"name":"...","ok":true,"platform":"darwin","version":"dev"}`.

- [ ] **Step 4: Commit**

```bash
git add receiver/cmd/type4me-receiver/main.go
git commit -m "feat(receiver): main.go 串起 config + inject + server

启动时打印 pairing URL,SIGINT 优雅退出。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Phase D — 端到端冒烟

### Task D1: 写自动化端到端冒烟脚本 + 手动验证

**Files:**
- Create: `scripts/test_remote_input.sh`

- [ ] **Step 1: 写脚本**

`scripts/test_remote_input.sh`:

```bash
#!/bin/bash
# End-to-end smoke: start receiver on loopback, POST text via curl, verify
# the clipboard contains the text. Does NOT exercise the Mac-side ASR/Type4Me
# UI — pure receiver + HTTP transport sanity check.

set -euo pipefail

cd "$(dirname "$0")/.."

PORT=47318
TOKEN="smoke-token-$(date +%s)"
TEXT="你好 type4me $(date +%H%M%S)"

# Save current clipboard so we can restore it
SAVED_CLIP=$(pbpaste || true)
trap 'echo "$SAVED_CLIP" | pbcopy || true' EXIT

# Build if needed
( cd receiver && make build-darwin-arm64 >/dev/null 2>&1 )

BIN="receiver/dist/type4me-receiver-darwin-arm64"

# Start receiver
TYPE4ME_TOKEN="$TOKEN" TYPE4ME_PORT="$PORT" "$BIN" >/tmp/t4m-receiver.log 2>&1 &
RX_PID=$!
trap "kill $RX_PID 2>/dev/null || true; echo \"$SAVED_CLIP\" | pbcopy || true" EXIT

# Wait until /ping is up
for i in $(seq 1 20); do
    if curl -fs "http://127.0.0.1:$PORT/ping" >/dev/null 2>&1; then break; fi
    sleep 0.1
done

# Send inject
RESP=$(curl -s -X POST "http://127.0.0.1:$PORT/inject" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"text\":\"$TEXT\"}")

echo "Receiver response: $RESP"

# Give it a beat to settle clipboard
sleep 0.3
GOT=$(pbpaste)

if [ "$GOT" = "$TEXT" ]; then
    echo "PASS: clipboard contains expected text"
    exit 0
else
    echo "FAIL: clipboard='$GOT' expected='$TEXT'"
    cat /tmp/t4m-receiver.log
    exit 1
fi
```

- [ ] **Step 2: 让脚本可执行并跑一次**

```bash
chmod +x scripts/test_remote_input.sh
./scripts/test_remote_input.sh
```

Expected: `PASS: clipboard contains expected text`.

注意:该脚本会在执行瞬间触发一次 Cmd+V,如果当前 macOS 前台是输入框,文字会被粘进去。建议关闭所有输入聚焦的窗口或聚焦到 Finder 桌面后再跑。

- [ ] **Step 3: 完整端到端(Mac 端)手动验证**

(a) 编辑 credentials.json,加入一个 loopback 目标:

```bash
CRED_PATH="$HOME/Library/Application Support/Type4Me/credentials.json"
python3 -c "
import json, os
p = os.path.expanduser('~/Library/Application Support/Type4Me/credentials.json')
d = {}
if os.path.exists(p):
    with open(p) as f: d = json.load(f)
d['tf_remote_targets'] = [{
    'id': 'mac-self',
    'name': 'Mac Self (smoke)',
    'host': '127.0.0.1',
    'port': 47318,
    'token': 'smoke-token',
    'matchBundleIds': ['com.apple.finder'],
    'enabled': True
}]
os.makedirs(os.path.dirname(p), exist_ok=True)
with open(p, 'w') as f: json.dump(d, f, indent=2)
print(f'wrote {p}')
"
```

(b) 启动接收端:

```bash
TYPE4ME_TOKEN=smoke-token TYPE4ME_PORT=47318 \
  receiver/dist/type4me-receiver-darwin-arm64 &
```

(c) 构建并启动 Type4Me dist 版本:

```bash
swift build -c release
SKIP_QWEN3_BUILD=1 APP_PATH="$PWD/dist/Type4Me.app" VARIANT=cloud \
  bash scripts/package-app.sh
open "$PWD/dist/Type4Me.app"
```

(d) 把 Finder 设为前台,触发 Type4Me 录音(任意已配置的 ASR provider),说"测试一二三",松开快捷键。

(e) 预期:Finder 不会被粘贴(它是 Bundle id `com.apple.finder`,在 matchBundleIds 列表里),Mac 端 Type4Me 把 ASR 文本 POST 到 receiver,receiver 当前焦点的剪贴板有 "测试一二三",并触发了 Cmd+V — 因为前台就是 Finder,Finder 地址栏 / 桌面对 Cmd+V 反应不明显。改个简单验证:把 matchBundleIds 改成 `com.apple.TextEdit`,打开 TextEdit 空白文档,前台 TextEdit,触发 Type4Me 录音,松开,**文字应当出现在 TextEdit 编辑区**。

(f) 反向验证:把 TextEdit 切走,前台变成 Finder(matchBundleIds 没命中),触发录音,**文字应当落到本机 sink**(走原 TextInjectionEngine 路径)。

- [ ] **Step 4: 清理冒烟配置**

```bash
python3 -c "
import json, os
p = os.path.expanduser('~/Library/Application Support/Type4Me/credentials.json')
with open(p) as f: d = json.load(f)
d.pop('tf_remote_targets', None)
with open(p, 'w') as f: json.dump(d, f, indent=2)
"
```

或者保留供后续 S2 测试。

- [ ] **Step 5: Commit**

```bash
git add scripts/test_remote_input.sh
git commit -m "test(remote): scripts/test_remote_input.sh 端到端冒烟脚本

不依赖真实远程桌面客户端,只验证 receiver HTTP → clipboard → Cmd+V 链路。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## 完成判据(S0 + S1)

- [ ] `swift test` 全过
- [ ] `cd receiver && go test ./...` 全过
- [ ] `scripts/test_remote_input.sh` PASS
- [ ] Mac 端启动后,在 credentials.json 里配置 loopback 目标 + matchBundleIds 命中 TextEdit,语音识别文字出现在 TextEdit
- [ ] matchBundleIds 没命中时(例如前台是 Finder),文字仍落到本机(走 LocalTextSink,行为不变)
- [ ] 接收端 kill 后,Type4Me 触发识别,文字落到 macOS 剪贴板(`.copiedToClipboard` outcome)而不是默地静默失败

S2 起点:把 credentials.json 的 hand-edit 替换为 SwiftUI 设置 UI;`type4me://pair` URL Scheme 接管字段填充;token 迁移到 Keychain。

---

## 备注

**Spec 未覆盖但本计划已默认的工程细节**

- `OutputTarget.token` 在 S1 仍存 JSON;`OutputTargetStore.swift` 与 `Config.go` 都打了注释说明 S2 迁 Keychain
- `RemoteHTTPSink` 的 `clipboardFallback` 默认开;router 没有把这个标志暴露给用户,S2 加 UI 时考虑(目前合理默认)
- macOS receiver 的 `t4m_simulate_paste` 不做 `kCGSessionEventTap` 与 `kCGHIDEventTap` 双发,只发 HID。多数情况已够;若用户报告某些场景下不响应,可以补 session-level post
- Phase C 没有给 Windows inject 实现 —— spec § 8 切片定义为 S3,本计划 S0+S1 不含
- `HistoryStore` 的 `target_id` 列迁移也是 spec 的一部分,但放在 S4(配合 history UI 一起改更合理),本计划不含
