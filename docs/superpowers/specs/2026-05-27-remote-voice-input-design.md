# Remote Voice Input — 设计文档

- **日期**: 2026-05-27
- **作者**: dsy (with Claude Code)
- **状态**: Draft (待 review)

## 1. 问题陈述

Type4Me 当前的注入路径是 `NSPasteboard` 写入 + `CGEvent` 模拟 `Cmd+V`,把识别结果落到 macOS 前台应用。当前台应用是远程桌面客户端(Microsoft Remote Desktop / Parsec / Moonlight / AnyDesk / Apple 屏幕共享 等)时,粘贴是否能到达远程机器完全取决于该客户端的剪贴板同步能力 —— 多数情况下中文短串能过,但长文本、IME 状态、双向同步常出问题,且每个会话都要手动启用同步开关。

**目标**:让 Mac 上的语音识别结果可靠落到远程机器的焦点输入框,跨 Windows / macOS 两个远程平台,覆盖 LAN / Tailscale-VPN / 同机虚拟机 三种网络拓扑。

## 2. 范围(v1)

**做**

- Mac 端引入"输出目标"抽象,支持 0..N 个远程目标
- 远程接收端是单文件 Go 二进制(Win + Mac),通过 HTTP 接收文本,本地剪贴板 + 模拟 Ctrl/Cmd+V 注入
- 自动路由:当前台 app bundle id 命中目标的匹配列表,则路由到该远程
- menu bar 手动 override(本机 / Local Mac / Win-PC 等)
- 配对走 `type4me://pair?...` URL Scheme,沿用现有 scheme 注册
- token 鉴权;失败时**绝不**静默回落到本机,而是把文本进 macOS 剪贴板作为用户可控兜底
- 历史记录加 `target_id` 列区分流向

**不做(显式 YAGNI)**

- 中继服务器 / SaaS 后端(用户用 Tailscale/VPN 自接通)
- mDNS 自动发现
- 流式 partial 注入(v1 只发 final text)
- 端到端加密 / mTLS / 自签 cert pinning
- 浏览器扩展 / 网页接收端(无法覆盖远程原生应用)
- 多 token / 设备绑定 / token 软过期(完全手动 rotate)
- 同一远程客户端的多窗口区分(Microsoft RD 开多台机时)
- 开机自启动 / 守护
- 接收端历史落盘

## 3. 架构

### 3.1 总体数据流

```
┌──────────────────────┐
│ RecognitionSession   │  record → ASR → text
└──────────┬───────────┘
           │ inject(text)
           ▼
┌──────────────────────┐    routeResolver.pick()
│   OutputRouter       ├─────────────────────────► OutputTarget
└──────────┬───────────┘   (frontmost bundle id → target)
           │
           ▼  protocol OutputSink { inject(_:) -> InjectionOutcome }
   ┌───────┴────────┐
   │                │
LocalTextSink    RemoteHTTPSink
(包装现有        (POST /inject 到
TextInjection-    一台或多台远程接收端)
Engine)
```

### 3.2 Mac 端组件

新增/修改文件:

| 文件 | 责任 | 类型 |
|---|---|---|
| `Type4Me/Injection/OutputSink.swift` | `OutputSink` 协议 + 复用 `InjectionOutcome` | 新 |
| `Type4Me/Injection/LocalTextSink.swift` | 包装现有 `TextInjectionEngine` | 新 |
| `Type4Me/Injection/RemoteHTTPSink.swift` | `URLSession` POST `/inject`,带 token,处理超时/重试 | 新 |
| `Type4Me/Injection/OutputRouter.swift` | 读 `NSWorkspace.shared.frontmostApplication?.bundleIdentifier`,查映射,返回 sink | 新 |
| `Type4Me/Injection/OutputTarget.swift` | 模型:`id / name / kind / host:port / matchBundleIds: [String] / enabled` | 新 |
| `Type4Me/Services/OutputTargetStore.swift` | 持久化(token 进 Keychain,其它进 `credentials.json`) | 新 |
| `Type4Me/UI/Settings/OutputTargetsTab.swift` | 设置 UI:目标增删改 + 自动路由表 + Ping 测试 | 新 |
| `Type4Me/Session/RecognitionSession.swift` | 注入点改走 `lockedSink.inject` | 改 |
| `Type4Me/Injection/TextInjectionEngine.swift` | 不动核心逻辑,作为 `LocalTextSink` 的实现细节 | 不动 |
| `Type4Me/Type4MeApp.swift` | URL Scheme handler 新增 `type4me://pair` 分支 | 改 |
| `Type4Me/UI/AppState.swift` | 新增 `outputOverride: OutputOverride` + `remoteTargets: [OutputTarget]` | 改 |
| `Type4Me/Storage/HistoryStore.swift` | schema 加 `target_id` 列 + 迁移(NULL → `"local"`) | 改 |

### 3.3 接收端组件(`receiver/`)

```
receiver/
├── cmd/type4me-receiver/main.go
├── internal/
│   ├── config/      (~/.../config.json + Keychain/DPAPI token)
│   ├── server/      HTTP routes + auth middleware
│   ├── inject/
│   │   ├── inject.go        // 接口 Injector
│   │   ├── inject_darwin.go // build tag: darwin
│   │   └── inject_windows.go// build tag: windows
│   └── tray/        系统托盘 (systray)
├── go.mod
└── Makefile         cross-compile: darwin-arm64 / darwin-amd64 / windows-amd64
```

与现有 `qwen3-asr-server/`(Python ASR 服务)平级独立。

### 3.4 路由决策时机

**录音开始(record-start)那一刻读一次前台 app,锁定到 inject 时使用**。

理由:用户心智模型是"我在看着 X 开始说话,文字应该到 X";全局快捷键场景下,从按下到松开几秒内可能扫一眼 menu bar、被通知抢焦点 —— inject 时再读会把语音打到错的地方。

```swift
// RecognitionSession.startRecording()
let frontmost = NSWorkspace.shared.frontmostApplication
self.lockedSink = router.resolve(
    frontmostBundleId: frontmost?.bundleIdentifier,
    manualOverride: appState.outputOverride
)
// ... existing record path ...
// 之后 inject 时:lockedSink.inject(text)
```

`TextInjectionEngine.captureFocusedElementSnapshot()` 仍在 inject 时再读 AX 元素用于 outcome 推断 —— 那是另一件事,保留。

### 3.5 路由解析算法

```
func resolve(frontmostBundleId, manualOverride) -> OutputSink:
    1. if manualOverride != .auto:
           return sink for manualOverride.target
    2. if frontmostBundleId == nil:
           return localSink
    3. for target in remoteTargets where target.enabled,
            sorted by user-defined priority:
           if frontmostBundleId in target.matchBundleIds:
               return RemoteHTTPSink(target)
    4. return localSink
```

优先级:`manualOverride` > `auto match` > `local`。多 target 命中同一 bundle id 时,列表顺序在前的赢(Settings UI 支持拖拽排序)。

### 3.6 内置 bundle id 预设

添加目标时一键填入(用户可后续编辑):

| 客户端 | bundle id |
|---|---|
| Microsoft Remote Desktop | `com.microsoft.rdc.macos` |
| Parsec | `com.parsecgaming.parsec` |
| Moonlight | `com.moonlight-stream.Moonlight` |
| AnyDesk | `com.anydesk.anydesk` |
| TeamViewer | `com.teamviewer.TeamViewer` |
| Apple 屏幕共享 | `com.apple.ScreenSharingViewer` |
| ToDesk | `com.todesk.todesk-osx` |
| RustDesk | `com.carriez.RustDesk` |

**不内置**浏览器 bundle id:web 版 RDP 罕用,而浏览器是高频本机应用,会把所有 web 输入误路由到远程。

### 3.7 用户 override(menu bar)

菜单栏图标顶部新增:

```
Output
  ⦿ Auto (currently: Win-PC)
  ○ Local Mac
  ○ Win-PC
  ○ Mac-Studio
```

- override 持久化在 UserDefaults,默认 `.auto`
- 非 `.auto` 时菜单栏图标加角标提醒
- `.auto` 显示当前自动模式匹配到哪台

## 4. 协议

### 4.1 HTTP API

| Method | Path | Auth | 入参 | 返回 |
|---|---|---|---|---|
| `GET` | `/ping` | 无 | — | `{ok, name, platform, version}` |
| `POST` | `/inject` | Bearer token | `{text, request_id?, preserve_clipboard?}` | `{ok, request_id?, outcome}` |
| `GET` | `/info` | Bearer token | — | `{name, platform, hostname}` |

**`POST /inject` 详细**

请求:
```http
POST /inject HTTP/1.1
Authorization: Bearer <token>
Content-Type: application/json
X-Request-ID: <uuid>

{
  "text": "你好世界",
  "request_id": "uuid-xxx",
  "preserve_clipboard": true
}
```

响应矩阵:

| HTTP | body | 含义 |
|---|---|---|
| `200` | `{ok:true, outcome:{pasted:true}}` | 已粘贴 |
| `200` | `{ok:false, outcome:{pasted:false, reason:"no-focus"}}` | OS 提示无焦点;Mac 端弹 toast 提示点窗口重试 |
| `401` | `{error:"invalid_token"}` | token 错;Mac 端标红 |
| `413` | `{error:"text_too_large", max_bytes:8192}` | 超长护栏 |
| `423` | `{error:"concurrent_inject"}` | 上一次 inject 未结束;Mac 端 200ms 后单次重试 |
| `500` | `{error:"platform_error", detail:"..."}` | 平台 API 异常 |

- `text` 上限 8 KB
- 默认监听 `0.0.0.0:47318`,可改

### 4.2 Token

- **生成**:32 字节 `crypto/rand` → `base64.RawURLEncoding` → 43 字符
- **存储**(接收端):macOS Keychain(service `com.type4me.receiver`,account `auth-token`)/ Windows DPAPI 加密后存文件
- **存储**(Mac 端):沿用现有 hybrid —— token 进 Keychain `com.type4me.grouped`,account `remote-target-<targetId>`;其它字段进 `credentials.json` 的 `tf_remote_targets` 节
- **传输**:`Authorization: Bearer <token>` header,不放 URL query
- **服务端比较**:`subtle.ConstantTimeCompare`
- **轮换**:接收端托盘菜单"Regenerate token"立即失效旧的;Mac 端下次 inject 收到 401 后 UI 标红提示重新配对
- **不做**:软过期、设备绑定、多 token

### 4.3 配对流程

**接收端**首次启动 / 点击托盘"显示配对信息"弹一个小窗:

```
┌──────────────────────────────────┐
│ 配对信息                          │
│                                  │
│ 名称:   Win-PC                    │
│ 地址:   192.168.1.42:47318        │
│ Token:  abc123...xyz              │
│                                  │
│ [ 复制为 type4me:// URL ]         │
│ [ 复制字段(JSON) ]                │
│ [ 显示 QR(v1.5) ]                 │
└──────────────────────────────────┘
```

URL 形式:
```
type4me://pair?host=192.168.1.42&port=47318&token=<base64url>&name=Win-PC&platform=windows
```

**用户操作路径**(三选一):

| 路径 | 适用场景 |
|---|---|
| ① 远程桌面剪贴板同步 → 复制 URL → Mac 粘贴 → 自动唤起 Type4Me | LAN/Tailscale,64 字符 token 短串足以过 |
| ② 远程通过 IM/邮件/笔记同步 URL 到 Mac → Mac 上点 URL | 跨互联网 / 剪贴板同步坏掉 |
| ③ QR 显示 + Mac 摄像头扫(v1.5 兜底) | 完全离线 |

**Mac 端**接收逻辑:`Type4MeApp.application(_:open:)` 已实现 URL Scheme(用于现有 LLM 配置导入)。新增 `pair` path 分支 → 弹"添加远程目标"对话框,字段预填,用户只需补 `matchBundleIds` 后保存。保存前自动 `GET /info` 验证 token。

## 5. 接收端实现细节

### 5.1 平台 inject 抽象

```go
type Injector interface {
    Inject(text string) (Outcome, error)
    Ping() error
}
type Outcome struct {
    Pasted bool
    Reason string  // "" | "no-focus" | "clipboard-locked" | "paste-blocked"
}
```

### 5.2 Windows (`inject_windows.go`)

- 剪贴板:纯 `syscall` 直调 `user32.dll`:`OpenClipboard` → `EmptyClipboard` → `SetClipboardData(CF_UNICODETEXT, ...)` → `CloseClipboard`,无 cgo
- 注入键:`SendInput` 构造 Ctrl down → V down → V up → Ctrl up;比 `keybd_event` 更稳,IME 友好
- 焦点检查:`GetForegroundWindow` 返回 0 时标记 `no-focus` 但仍执行 paste(部分 RDP 客户端窗口 API 返回空,实际可粘)

### 5.3 macOS (`inject_darwin.go`)

- 剪贴板:cgo 调 `NSPasteboard generalPasteboard` 的 `setString:forType:`
- 注入键:cgo 调 `CGEventCreateKeyboardEvent(nil, kVK_ANSI_V, true/false)` 配合 `kCGEventFlagMaskCommand`,逻辑对齐 Type4Me 的 `TextInjectionEngine.simulatePaste`

### 5.4 共通行为

- 全局 `sync.Mutex` 串行化 inject;**抢锁最多等 500 ms**(留 300 ms 给后续步骤,Mac 端 800 ms 总预算来得及收到 `423`);超时即返回 `423 concurrent_inject`
- 每次 inject 前 snapshot 剪贴板(types + bytes),paste 完成后 150 ms 异步恢复
- 失败不抛错,返回 200 + `{ok:false, reason}`,让 Mac 端拿到原因

### 5.5 配置

- 配置文件:macOS `~/Library/Application Support/type4me-receiver/config.json`、Win `%APPDATA%\type4me-receiver\config.json`,权限 0600
- 字段:`port, bind_addr, name, version, created_at`(非密文)
- token 进平台密钥库
- 首次启动:自动生成 token + 默认 name=主机名,托盘弹"显示配对信息"窗口

### 5.6 系统托盘(`getlantern/systray` 或 `fyne.io/systray`,~600 KB)

- 图标状态:常态(已监听)、活动(刚注入,绿色闪 200ms)、错误(红色)
- 菜单:当前监听地址(灰)/ 显示配对信息 / 查看日志 / 复制 token(再生新 token,旧失效)/ 退出
- 不带任何 LLM/ASR 逻辑;二进制目标 ~5 MB

## 6. 错误处理与回退

### 6.1 Mac 端失败矩阵

| 情形 | 行为 |
|---|---|
| 网络超时 / connection refused | **不回落 LocalSink**;弹 toast "无法注入到 Win-PC,文本已复制到剪贴板,可手动粘贴";ASR 文本写到 macOS 剪贴板 |
| `401 invalid_token` | 同上;目标 row 标红,菜单栏图标加红点 |
| `reason:"no-focus"` | 短 toast "远程窗口未取得焦点,点窗口后重试";**不**自动重试(避免打到下一次切换的窗口) |
| `423 concurrent_inject` | 200 ms 单次重试,再失败按超时处理 |
| 用户 override 选远程但接收端未启动 | 同超时;菜单栏弹一次性提醒"Output = Win-PC,但接收端未响应" |

**关键原则**:语音内容可能私密(密码、私人聊天)。"想发到远程结果发到本机"比"丢失一次输入"糟糕得多 —— 后者只是浪费时间,前者是隐私泄漏。回落只能到剪贴板(用户自己控制粘贴位置),绝不偷换 sink。

### 6.2 网络层超时

Mac 端 HTTP client 超时 800 ms(与现 `TextInjectionEngine` 同步路径延时预算对齐)。超时归类 `transport_error`。

## 7. 安全权衡(显式声明)

| 风险 | v1 缓解 | 何时补强 |
|---|---|---|
| token 泄漏 | 仅放 Authorization header;接收端不打日志;Mac 端日志脱敏前 12 字符 | — |
| 中间人(LAN) | 假设 LAN 可信;Tailscale/VPN 已 e2e | 用户要求时加自签 TLS + cert pinning |
| 重放 | 不防;重放只会重粘相同文字,损失可控 | 加 `X-Timestamp` + `X-Nonce` + 5s 窗口,仅在用户要求时做 |
| port 被扫到 | 鉴权失败仅 401 + 通用 message;`/info` 需 token,`/ping` 默认开 | `/ping` 可改为需 token,trade-off 是 LAN 探测略麻烦 |

## 8. 切片计划(MVP)

| 阶段 | 范围 | 验收信号 |
|---|---|---|
| **S0 — 重构地基** | Mac 端 `OutputSink` 抽象 + `LocalTextSink` 包装 + `RecognitionSession` 走 sink;无行为变化 | 现有测试全过 + 手动冒烟 = main 分支 |
| **S1 — MVP 端到端(macOS→macOS,无 UI)** | Go 接收端骨架(`/ping` + `/inject`,token 硬编码,console only);Mac 端 JSON 配单远程目标 | 同机 Mac 起接收端 127.0.0.1:47318;开 Microsoft RD 窗口(空 RD 即可)识别后文字经 HTTP 落到接收端再到 Finder |
| **S2 — 设置 UI + 配对 URL** | Settings "Output Targets" tab;menu bar Override;`type4me://pair` handler;接收端 console 启动打印 URL | 删配置,从接收端拷 URL,Mac 点开,字段预填,保存后路由正常 |
| **S3 — Windows 接收端** | port `inject_darwin.go` → `inject_windows.go`(纯 win32 syscall);Makefile 加 windows-amd64 cross-build;真 Windows 机冒烟 | Mac → Win 接收端 → 远程窗口中文字成功 |
| **S4 — Tray + 错误 toast + history.target_id** | Go `systray`;Mac toast + 剪贴板兜底;`HistoryStore` 加列 + 迁移 | 主观体验完整 |
| **S5 — v1.5 留口** | mDNS / QR / 多窗口路由 / 自启动 / mTLS | 不在 v1 |

**先做 S0–S1,在本机端到端打通后再做剩下**。

## 9. 测试策略

### 9.1 Mac 端

| 目标 | 测试类型 | 关键 case |
|---|---|---|
| `OutputTarget` 序列化 | 单元 | round-trip JSON;token 不入 JSON 只查 Keychain;`matchBundleIds` 空数组合法 |
| `OutputRouter.resolve` | 单元(纯逻辑) | ① override=.local 永远本机;② frontmost ∈ target.bundleIds → target;③ 多 target 命中按优先级;④ frontmost=nil → local;⑤ disabled 跳过 |
| `RemoteHTTPSink` | 单元 + 本地 HTTP server stub | 200 ok / 200 ok=false / 401 / 413 / 423 / 500 / connection refused / 800 ms 超时 → outcome 映射正确 |
| `RecognitionSession.lockedSink` | 集成 | 录音中切前台 app,inject 仍打到 record-start 时锁定的 sink |
| URL Scheme `pair` 解析 | 单元 | 合法 URL 全字段;缺 token 拒绝;非 base64url 拒绝;port 越界拒绝 |
| `HistoryStore` 迁移 | 集成 | 旧 schema 打开后 `target_id` 列存在且旧行 = `"local"` |
| 剪贴板兜底 | 手测 | 关接收端,触发识别,剪贴板有文字,toast 显示 |

### 9.2 接收端(Go)

| 目标 | 测试类型 | 关键 case |
|---|---|---|
| HTTP 路由 | `httptest` | `/ping` 无 auth 通过;`/inject` 无/错 token 401(constant-time);超长 413;并发第二个 423 |
| Inject 平台层抽象 | 单元 | 接口 `Injector` 用 fake 验证业务编排顺序(snapshot → set → paste → restore;失败标 reason) |
| token 生成 | 单元 | 32 字节 → 43 字符 base64url;两次不同 |
| 配置持久化 | 集成 | 首次生成 token 写 Keychain/DPAPI;二次读出同值 |
| 平台 inject 真机 | 手测 | macOS:跑接收端 `curl /inject` 文 = "你好",Finder 地址栏拿到;Windows 同样 |

### 9.3 跨进程端到端冒烟

`scripts/test_remote_input.sh`(macOS 自检):
1. 后台起接收端 loopback 127.0.0.1:47318,token=test-token
2. `curl POST /inject` text="你好世界"
3. `pbpaste` 验证剪贴板内容
4. 杀进程

不依赖真实远程桌面客户端,纯打通 HTTP → 剪贴板 → Cmd+V → 当前窗口链路。给 S1 提供回归保护。

### 9.4 CI 矩阵

- Mac 端 Swift 测试:`swift test`,macOS runner
- 接收端 Go 测试:`go test ./...` ubuntu runner(平台无关层 + httptest);macOS runner 额外跑 `go build` for windows + darwin 验证 cross-compile
- 平台 inject 真机:**不上 CI**,作为 release checklist 条目

### 9.5 不做的测试(YAGNI)

- 大流量压测(语音输入 10 QPM 量级)
- fuzz(协议字段就 3 个)
- 跨平台 UI 自动化(SwiftUI 走单元 + 手测)
- token rotation 时序竞态(人为操作不会到秒级)

## 10. 开放问题 / 后续

- **同一 RD 客户端多窗口区分**:Microsoft RD 一个 app 开三台机时,目前整体路由到一个 target。后续读 `kAXTitleAttribute` 按 title 包含的机器名决定。AX 权限已具备
- **基于焦点输入框类型的判定**:焦点是 secure text field(密码框)时不走远程,防止把密码意外打到非 secure 字段。v1 不做,代码注释里留 hook
- **网络可达性预筛**:Mac 后台周期 `/ping` 标记 reachable;auto-route 解析时跳过 unreachable。v1 用户自己看 UI 状态点
- **多任务并发**:`RecognitionSession` 现在单实例,本设计沿用串行约束
