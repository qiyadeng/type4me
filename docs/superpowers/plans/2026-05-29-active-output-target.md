# 激活目标(Active Output Target)开关 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给已有但从未接到 UI 的 `outputOverride` 装上界面把手,让用户手动锁定唯一输出目标(本机 / 某台远程),解决同一远程桌面客户端多窗口共享 bundle id 导致 `.auto` 无法区分的串台问题;并在远程发送失败时兜底落到本机注入。

**Architecture:** 纯 Mac 端(Type4Me)改动,复用既有路由机制。`OutputRouter.resolve` 已消费 `outputOverride`;本计划(1)给 `RemoteHTTPSink` 增加可选本机 fallback,失败时改走本机注入而非剪贴板;(2)由 `OutputRouter` 把 `localSink` 作为该 fallback 注入;(3)给 `AppState.outputOverride` 的存取加测试;(4)在 `RemoteSettingsTab` 加单选控件写入 `outputOverride`。不动 relay / receiver / 账号模型。

**Tech Stack:** Swift Package Manager,SwiftUI(@Observable / @Environment / @Bindable),XCTest。

参考 spec:`docs/superpowers/specs/2026-05-29-active-output-target-design.md`

---

## File Structure

| 文件 | 职责 / 改动 |
|---|---|
| `Type4Me/Injection/RemoteHTTPSink.swift` | 新增内部 `let fallback: OutputSink?`;两个 init 加默认 `nil` 参数;`inject` 失败时优先委派 fallback |
| `Type4Me/Injection/OutputRouter.swift` | 构造 `RemoteHTTPSink` 两处(`.remote` / `.auto`)传入 `localSink` 作为 fallback;给 `OutputOverride` 加 `Hashable` |
| `Type4Me/UI/AppState.swift` | `loadOverride` / `saveOverride` 去掉 `private`(改为 `nonisolated static`,供测试调用) |
| `Type4Me/UI/Settings/RemoteSettingsTab.swift` | 新增「激活目标」单选卡片,绑定 `appState.outputOverride` |
| `Type4MeTests/RemoteHTTPSinkClipboardFallbackTests.swift` | 新增 fallback 委派测试 |
| `Type4MeTests/OutputRouterTests.swift` | 新增"远程 sink 的 fallback 是 LocalTextSink"测试 |
| `Type4MeTests/AppStateTests.swift` | 新增 `outputOverride` 存取往返测试 |

---

## Task 1: RemoteHTTPSink 失败兜底到本机注入

**Files:**
- Modify: `Type4Me/Injection/RemoteHTTPSink.swift`
- Test: `Type4MeTests/RemoteHTTPSinkClipboardFallbackTests.swift`

- [ ] **Step 1: 写失败测试 — fallback 存在时,transport 失败应委派给 fallback**

在 `Type4MeTests/RemoteHTTPSinkClipboardFallbackTests.swift` 末尾、`private final class MockTransport` 定义之前,加入一个记录型假 sink 和两个测试方法:

```swift
    func testInjectDelegatesToFallbackOnTransportFailure() {
        let fallback = RecordingSink(outcome: .inserted)
        let sink = RemoteHTTPSink(target: makeTarget(),
                                  transport: MockTransport(result: false),
                                  fallback: fallback)
        let outcome = sink.inject("to-fallback")
        XCTAssertEqual(fallback.injected, ["to-fallback"], "失败时应把文本交给本机 fallback")
        XCTAssertEqual(outcome, .inserted, "应返回 fallback 的结果")
    }

    func testInjectDoesNotUseFallbackOnTransportSuccess() {
        let fallback = RecordingSink(outcome: .inserted)
        let sink = RemoteHTTPSink(target: makeTarget(),
                                  transport: MockTransport(result: true),
                                  fallback: fallback)
        XCTAssertEqual(sink.inject("ok"), .inserted)
        XCTAssertTrue(fallback.injected.isEmpty, "成功时不应触发 fallback")
    }
```

并在文件末尾(`MockTransport` 之后)加入记录型假 sink:

```swift
private final class RecordingSink: OutputSink, @unchecked Sendable {
    let outcome: InjectionOutcome
    var injected: [String] = []
    init(outcome: InjectionOutcome) { self.outcome = outcome }
    func inject(_ text: String) -> InjectionOutcome {
        injected.append(text)
        return outcome
    }
}
```

注:文件中既有的 `testInjectCopiesToClipboardOnTransportFailure`(无 fallback → 写剪贴板)即为"无 fallback 时维持剪贴板"的回归用例,无需新增。

- [ ] **Step 2: 跑测试确认失败(编译失败:init 无 fallback 参数)**

Run: `swift test --filter RemoteHTTPSinkClipboardFallbackTests`
Expected: 编译失败,提示 `RemoteHTTPSink` 的 init 没有 `fallback:` 参数(extra argument 'fallback')。

- [ ] **Step 3: 在 RemoteHTTPSink 中实现 fallback**

修改 `Type4Me/Injection/RemoteHTTPSink.swift`。在 `let transport: RemoteTransport` 下方加入存储属性:

```swift
    /// Optional local-injection sink used when the remote transport fails.
    /// When nil, failures fall back to clipboard (legacy behaviour).
    let fallback: OutputSink?
```

把两个 init 改为(各加一个默认 `nil` 的 `fallback` 参数并赋值):

```swift
    init(target: OutputTarget, fallback: OutputSink? = nil) {
        self.target = target
        self.fallback = fallback
        switch target.mode {
        case .direct:
            self.transport = DirectTransport(target: target)
        case .relay:
            self.transport = RelayTransport(target: target)
        }
    }

    /// Injection point used by OutputRouter / tests that need a custom transport.
    init(target: OutputTarget, transport: RemoteTransport, fallback: OutputSink? = nil) {
        self.target = target
        self.transport = transport
        self.fallback = fallback
    }
```

把 `inject` 的失败分支改为优先走 fallback:

```swift
    func inject(_ text: String) -> InjectionOutcome {
        let requestID = UUID().uuidString
        if transport.dispatch(text: text, requestID: requestID, preserveClipboard: true) {
            return .inserted
        }
        if let fallback {
            return fallback.inject(text)
        }
        return copyToClipboardFallback(text)
    }
```

- [ ] **Step 4: 跑测试确认通过**

Run: `swift test --filter RemoteHTTPSinkClipboardFallbackTests`
Expected: PASS(含原有 4 个用例 + 新增 2 个,共 6 个)。

- [ ] **Step 5: 提交**

```bash
git add Type4Me/Injection/RemoteHTTPSink.swift Type4MeTests/RemoteHTTPSinkClipboardFallbackTests.swift
git commit -m "feat: RemoteHTTPSink falls back to local injection on transport failure

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: OutputRouter 把 localSink 注入为远程 fallback

**Files:**
- Modify: `Type4Me/Injection/OutputRouter.swift`
- Test: `Type4MeTests/OutputRouterTests.swift`

- [ ] **Step 1: 写失败测试 — 远程 sink 的 fallback 应是 LocalTextSink**

在 `Type4MeTests/OutputRouterTests.swift` 的 `OutputRouterTests` 类内加入两个测试(覆盖 `.remote` 与 `.auto` 两条远程路径):

```swift
    func testRemoteOverrideSinkCarriesLocalFallback() {
        let router = makeRouter(targets: [winTarget])
        let sink = router.resolve(frontmostBundleId: nil, override: .remote("win"))
        XCTAssertTrue((sink as? RemoteHTTPSink)?.fallback is LocalTextSink,
                      ".remote 路径构造的远程 sink 应带本机 fallback")
    }

    func testAutoRemoteSinkCarriesLocalFallback() {
        let router = makeRouter(targets: [winTarget])
        let sink = router.resolve(frontmostBundleId: "com.microsoft.rdc.macos", override: .auto)
        XCTAssertTrue((sink as? RemoteHTTPSink)?.fallback is LocalTextSink,
                      ".auto 路径构造的远程 sink 应带本机 fallback")
    }
```

- [ ] **Step 2: 跑测试确认失败**

Run: `swift test --filter OutputRouterTests`
Expected: 新增两个用例 FAIL —— `fallback` 为 `nil`,`nil is LocalTextSink` 为 `false`。

- [ ] **Step 3: 在 OutputRouter 中传入 fallback**

修改 `Type4Me/Injection/OutputRouter.swift` 的 `resolve`,把两处 `RemoteHTTPSink(target: t)` 改为带 fallback:

```swift
        case .remote(let id):
            if let t = targets.first(where: { $0.id == id && $0.enabled }) {
                return RemoteHTTPSink(target: t, fallback: localSink)
            }
            return localSink
        case .auto:
            guard let bundleId = frontmostBundleId else { return localSink }
            for t in targets where t.enabled {
                if t.matchBundleIds.contains(bundleId) {
                    return RemoteHTTPSink(target: t, fallback: localSink)
                }
            }
            return localSink
```

- [ ] **Step 4: 跑测试确认通过**

Run: `swift test --filter OutputRouterTests`
Expected: PASS(原有 8 个用例 + 新增 2 个)。

- [ ] **Step 5: 提交**

```bash
git add Type4Me/Injection/OutputRouter.swift Type4MeTests/OutputRouterTests.swift
git commit -m "feat: OutputRouter wires localSink as remote-failure fallback

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: AppState.outputOverride 存取往返测试

**Files:**
- Modify: `Type4Me/UI/AppState.swift`
- Test: `Type4MeTests/AppStateTests.swift`

- [ ] **Step 1: 写失败测试 — override 与 UserDefaults 字符串往返**

在 `Type4MeTests/AppStateTests.swift` 的 `AppStateTests` 类内加入(测试直接调用 `loadOverride`/`saveOverride`,并在前后保存/恢复 UserDefaults 键,避免污染):

```swift
    func testOutputOverrideRoundTrips() {
        let key = "tf_output_override"
        let saved = UserDefaults.standard.string(forKey: key)
        defer {
            if let saved { UserDefaults.standard.set(saved, forKey: key) }
            else { UserDefaults.standard.removeObject(forKey: key) }
        }

        for value: OutputOverride in [.auto, .local, .remote("win-7")] {
            AppState.saveOverride(value)
            XCTAssertEqual(AppState.loadOverride(), value, "应往返保持: \(value)")
        }
    }

    func testOutputOverrideDefaultsToAutoForUnknownRawValue() {
        let key = "tf_output_override"
        let saved = UserDefaults.standard.string(forKey: key)
        defer {
            if let saved { UserDefaults.standard.set(saved, forKey: key) }
            else { UserDefaults.standard.removeObject(forKey: key) }
        }

        UserDefaults.standard.set("garbage-value", forKey: key)
        XCTAssertEqual(AppState.loadOverride(), .auto, "无法识别的值应回退到 .auto")
    }
```

- [ ] **Step 2: 跑测试确认失败(`loadOverride`/`saveOverride` 为 private,不可见)**

Run: `swift test --filter AppStateTests`
Expected: 编译失败 —— `'loadOverride' is inaccessible due to 'private' protection level`(`saveOverride` 同理)。

- [ ] **Step 3: 把两个方法从 private 改为 internal**

修改 `Type4Me/UI/AppState.swift`:把

```swift
    nonisolated private static func loadOverride() -> OutputOverride {
```
改为
```swift
    nonisolated static func loadOverride() -> OutputOverride {
```

把

```swift
    nonisolated private static func saveOverride(_ o: OutputOverride) {
```
改为
```swift
    nonisolated static func saveOverride(_ o: OutputOverride) {
```

(仅去掉 `private`,其余不变。`@testable import Type4Me` 可访问 internal。)

- [ ] **Step 4: 跑测试确认通过**

Run: `swift test --filter AppStateTests`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add Type4Me/UI/AppState.swift Type4MeTests/AppStateTests.swift
git commit -m "test: round-trip coverage for AppState.outputOverride persistence

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: RemoteSettingsTab 「激活目标」单选控件

UI 改动,以手动验证为主(SwiftUI 视图无单元测试)。Picker 选中项类型为 `OutputOverride`,需要其可哈希。

**Files:**
- Modify: `Type4Me/Injection/OutputRouter.swift`(给 `OutputOverride` 加 `Hashable`)
- Modify: `Type4Me/UI/Settings/RemoteSettingsTab.swift`

- [ ] **Step 1: 给 OutputOverride 加 Hashable**

修改 `Type4Me/Injection/OutputRouter.swift` 顶部枚举声明:

```swift
enum OutputOverride: Equatable, Hashable, Sendable {
    case auto
    case local
    case remote(_ targetId: String)
}
```

(关联值为 `String`,合成 `Hashable` 即可;Picker 的 `selection` / `.tag` 需要 `Hashable`。)

- [ ] **Step 2: 编译确认枚举改动不破坏现有代码**

Run: `swift build`
Expected: 编译通过(`Hashable` 为附加能力,不影响既有 `Equatable` 用法)。

- [ ] **Step 3: 在 RemoteSettingsTab 加「激活目标」卡片**

修改 `Type4Me/UI/Settings/RemoteSettingsTab.swift`。

(a) 在 `var body` 内,把 `credentialsCard` 一行替换为先放激活目标卡、再放配置文件卡:

```swift
            activeOutputCard

            credentialsCard
```

(b) 在 `credentialsCard` 计算属性之前,新增 `activeOutputCard`。它用一个 `@Bindable` 局部变量拿到 `appState` 的双向绑定,Picker 选项为 自动 / 本机 + 每个启用的远程 target:

```swift
    // MARK: - Active output target

    private var activeOutputCard: some View {
        @Bindable var appState = appState
        return settingsGroupCard(L("激活目标", "Active Output"), icon: "scope") {
            VStack(alignment: .leading, spacing: 8) {
                Text(L(
                    "锁定转写文本的去向。选「自动」时按前台 App 的 bundle id 匹配;选某台远程时,无视前台 App 直接发往该机器(适用于同一远程桌面客户端开多台时,自动匹配分不清的情况)。切换在下一次说话生效。",
                    "Lock where transcribed text goes. \"Auto\" matches by the frontmost app's bundle id; picking a remote sends there regardless of the frontmost app (use this when one remote-desktop client has several machines open and auto-match can't tell them apart). Takes effect on your next recording."
                ))
                .font(.system(size: 12))
                .foregroundStyle(TF.settingsTextSecondary)
                .frame(maxWidth: .infinity, alignment: .leading)

                Picker("", selection: $appState.outputOverride) {
                    Text(L("自动(按前台 App 匹配)", "Auto (match frontmost app)")).tag(OutputOverride.auto)
                    Text(L("本机 Mac", "This Mac")).tag(OutputOverride.local)
                    ForEach(appState.remoteTargets.filter { $0.enabled }) { t in
                        Text(t.name).tag(OutputOverride.remote(t.id))
                    }
                }
                .labelsHidden()
                .pickerStyle(.radioGroup)
            }
        }
    }
```

- [ ] **Step 4: 编译确认通过**

Run: `swift build`
Expected: 编译通过。

- [ ] **Step 5: 手动验证**

构建并运行:

```bash
swift build -c release && bash scripts/deploy.sh
```

验证清单:
1. 打开 设置 → 远程标签页,顶部出现「激活目标」单选,默认选中「自动」。
2. 配置 ≥2 个启用的远程 target 后,它们都出现在单选列表中;禁用某个 target 后(改 credentials.json 的 enabled 后点「重新加载」)它从列表消失。
3. 选中某台远程 → 退出设置 → 录一段话,文本发往选中那台(可用 receiver 日志或目标机器实际输入确认),与前台是哪个远程窗口无关。
4. 关掉/断开选中那台远程后再录一段 → 文本落到本机注入(打入 Mac 前台窗口),不丢字。
5. 重启 App → 之前选中的激活目标仍保持(持久化生效)。

- [ ] **Step 6: 提交**

```bash
git add Type4Me/Injection/OutputRouter.swift Type4Me/UI/Settings/RemoteSettingsTab.swift
git commit -m "feat: add Active Output target selector in Remote settings

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## 完成校验

全部任务后跑一次相关测试套件:

```bash
swift test --filter RemoteHTTPSinkClipboardFallbackTests
swift test --filter OutputRouterTests
swift test --filter AppStateTests
```

Expected: 全部 PASS。
