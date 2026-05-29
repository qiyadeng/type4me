# 激活目标(Active Output Target)开关 — 设计文档

日期:2026-05-29
分支:feature/remote-voice-input-s0-s1

## 背景与问题

Type4Me 的语音转写文本可路由到本机或经 relay 发送到远程 Windows 上的 receiver。
路由由 `OutputRouter.resolve(frontmostBundleId:override:)` 决定,支持三种模式:

- `.auto`:取 Mac 当前前台 App 的 bundle id,匹配某个 target 的 `matchBundleIds`,命中第一个即发往该 target;否则本机。
- `.local`:强制本机注入。
- `.remote(id)`:强制发往指定 target,**不看 bundle id**。

问题:用户用**同一个远程桌面客户端**(如 ToDesk)同时打开**多台** Windows 时,这些窗口共享同一个 bundle id。`.auto` 无法区分,只会发往 `matchBundleIds` 命中的第一个 target,与用户当前焦点在哪个远程窗口无关——会一直串到同一台。

关键现状:`OutputOverride` 枚举、`resolve` 逻辑、`outputOverride` 的持久化(UserDefaults `tf_output_override`)与录音起始快照消费逻辑**均已实现**,但 `outputOverride` 在整个代码库中**从未被任何 UI 赋值**,实际恒为 `.auto`。即"手动锁定单一目标"的机制已造好,只缺界面入口。

## 目标

1. 让用户能手动**锁定唯一输出目标**(本机 / 某台具体远程),使同客户端多窗口场景下文本只进选定那台。
2. 保留 `.auto` 作为可选项,默认仍为 `.auto`,不改变现有默认行为。
3. 锁定到的远程发送失败时,**兜底落到本机注入**(而非现有的直接写剪贴板)。

## 非目标

- 不修改 relay 服务、receiver、账号/设备模型或寻址协议。
- 不引入热键切换(远程桌面客户端会抢占键盘快捷键,见项目记忆);切换入口走非键盘的设置界面。
- 不做远程在线状态的实时探测/显示。

## 设计

纯 Mac 端(Type4Me)改动,复用既有路由机制。

### 1. UI:RemoteSettingsTab 顶部「激活目标」单选区

在 `Type4Me/UI/Settings/RemoteSettingsTab.swift` 顶部新增一个分区,单选控件,选项依次为:

- `自动(按前台 App 匹配)` → `.auto`
- `本机 Mac` → `.local`
- 每个 `enabled == true` 的远程 target,按其 `name` 显示 → `.remote(target.id)`

行为:

- 选中即写入 `appState.outputOverride`(setter 已自动持久化到 UserDefaults)。
- 已禁用(`enabled == false`)的 target 不出现在列表中。
- 默认选中 `自动`。
- 若当前 `outputOverride` 指向的 target id 已不存在于启用列表(被删/禁用),控件回退显示为 `自动`(展示层回退;实际路由的回退见第 3 节)。

### 2. 生效时机

不变。`RecognitionSession` 在录音开始时通过 fetcher 快照 `(remoteTargets, outputOverride)`,该次录音锁定 sink,中途不改路由。切换激活目标在**下一次录音**生效。

### 3. 失败兜底:落到本机注入

当前 `RemoteHTTPSink.inject` 在 transport 失败时直接写剪贴板。改为可注入一个本机兜底 sink:

- `RemoteHTTPSink` 新增可选属性 `fallback: OutputSink?`(默认 `nil`)。
- `inject`:transport `dispatch` 成功 → `.inserted`;失败 → 若 `fallback != nil` 则返回 `fallback.inject(text)`(本机键盘注入,`LocalTextSink` 内部仍有自己的剪贴板保底);否则维持现有 `copyToClipboardFallback`。
- `OutputRouter.resolve` 构造 `RemoteHTTPSink` 时,把自身持有的 `localSink` 作为 `fallback` 传入(`.remote` 与 `.auto` 两条远程路径都传)。

效果:远程发不出去时,文本以键盘模拟打入 Mac 前台的远程桌面窗口,经客户端转发多半仍能到对的那台;若本机注入也失败,`LocalTextSink`/引擎自身的剪贴板保底兜住,绝不丢字。

### 4. 边界

- 选中 target 后被删/禁用:`resolve` 既有逻辑 `targets.first(where: { $0.id == id && $0.enabled })` 未命中即回退 `localSink`,绝不静默改选另一台远程。
- 兜底改动仅影响"远程发送失败"路径,正常路由与本机路由行为不变。

## 受影响文件

| 文件 | 改动 |
|---|---|
| `Type4Me/Injection/RemoteHTTPSink.swift` | 新增可选 `fallback: OutputSink?`,失败时优先委派本机注入 |
| `Type4Me/Injection/OutputRouter.swift` | 构造 `RemoteHTTPSink` 时传入 `localSink` 作为 fallback(两处远程分支) |
| `Type4Me/UI/Settings/RemoteSettingsTab.swift` | 新增「激活目标」单选区,绑定 `appState.outputOverride` |

## 测试

- `OutputRouter`/`RemoteHTTPSink`:远程 transport 失败时,fallback sink 的 `inject` 被调用并返回其结果(用假 transport + 记录调用的假 sink)。
- `RemoteHTTPSink`:无 fallback 时维持剪贴板行为(回归)。
- `AppState`:`outputOverride` 存取往返(`.auto`/`.local`/`.remote(id)` ↔ `tf_output_override` 字符串)。

## 实现顺序(TDD)

1. `RemoteHTTPSink` fallback:先写测试(失败→fallback 被调用 / 无 fallback→剪贴板),再实现。
2. `OutputRouter` 传入 fallback:更新/新增 resolve 测试。
3. `AppState` override 往返测试(若尚无)。
4. `RemoteSettingsTab` UI 单选区(手动验证为主)。
