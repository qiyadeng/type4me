# 账号系统(自助登录取代手工配置)— 整体设计

日期:2026-05-29
分支:feature/account-system

## 背景与问题

当前要用 relay 远程输入,用户需要在两端手工配置一堆参数:

- **Mac(Type4Me)**:手改 `credentials.json` 的 `tf_remote_targets`,填 `relay_url`、`device_id`、`device_token`、`target_device_id`、`matchBundleIds`(见 `OutputTarget.swift`)。
- **Windows(receiver)**:改 `config.json` 或设 env(`TYPE4ME_RELAY_URL`/`DEVICE_ID`/`DEVICE_TOKEN`),纯命令行,无界面。
- **Relay**:账号/设备只能由持有 `TYPE4ME_RELAY_ADMIN_TOKEN` 的管理员通过 admin API 创建。

这些 device id/token 都得从 admin API 响应里手工复制粘贴,易用性差。

目标:**用一个账号系统把这些参数全部自动化**。用户注册/登录账号,两端登录后自动完成配置,只需选择目标设备即可使用,全程不接触 relay 地址、device id/token 等参数。

## 现状:已有的可复用骨架

relay 后端已具备账号系统的"数据/通信内核"(见 `relay/internal/hub/`):

- `Account{ID, Name, CreatedAt}`、`Device{ID, AccountID, Label, Role, TokenHash, ...}`、`Message`。
- device token:32 字节随机 + bcrypt 哈希 + 热路径缓存(`token.go`);Bearer 鉴权(`auth.go`)。
- 账号级隔离:`Dispatch` 拒绝跨账号(`hub.go`)。
- 接口:`/v1/dispatch`、`/v1/subscribe`(SSE)、`/v1/ping`(device 鉴权);`/v1/admin/{accounts,devices,...}`(admin 鉴权)。
- 状态持久化:`state.json` 原子写,存 Account/Device(只存 token 哈希)。
- 客户端传输层:Mac `RelayTransport`(POST /v1/dispatch)、Windows `Subscriber`(GET /v1/subscribe SSE + 自动重连)。

**缺口**:面向终端用户的自助层——注册、登录(密码/会话)、"列出我的设备"、客户端自助登记本机为设备;以及 Mac/Windows 两端的登录界面。

## 已确定的设计决策

1. **推进方式**:先出本整体设计 spec,再按 后端 → Mac → Windows 分期,每块各自 spec→计划→实现。
2. **认证**:用户名 + 密码;注册需**邀请码**(自托管防滥注册)。无邮箱/短信。
3. **设备登记**:登录时**自动登记本机**为设备(默认用主机名做标签),客户端本地保存返回的 device token;之后复用,用户看不到 id/token。
4. **目标选择**:登录后只**手动从账号设备列表里选激活设备**(复用已实现的「激活目标」选择器);**丢弃 bundle-id 自动路由**,不再有 `matchBundleIds` 配置。
5. **集成方式**:**方案 A · 登录即合成目标**——不改动现有路由内核,账号层登录后拉设备列表并在内存中合成 OutputTarget(relay 模式)。登录本质是自动配置器。

## 架构

### 集成方式(方案 A,已选)

保持现有 `OutputSink` / `OutputRouter` / `RemoteHTTPSink` / `RelayTransport` 路由内核不变。账号层登录后:

- 从 relay 拉取账号设备列表;
- 在内存中为每个"其他设备"合成一个 relay 模式的 `OutputTarget`(`relayURL` 取固化常量、`deviceID`/`deviceToken` 取本机登记凭据、`targetDeviceID` 取该设备 id、`matchBundleIds` 为空);
- 「激活目标」选择器列出 `本机 + 这些合成目标`,用户手动选其一。

（被否的方案 B:用新会话/设备模型全面替换 OutputTarget/credentials.json relay 配置并改写路由——概念更干净但要重写已能跑的代码,不采纳。）

### 1. Relay 后端 · 自助账号层

**数据模型变更**

- `Account` 增加 `Username string`、`PasswordHash string`(bcrypt)。一个用户 = 一个账号(1:1)。
- 复用现有 `Device`/token/隔离,无需改动。
- `state.json` schema 版本号递增,加载时兼容旧记录(无 Username/PasswordHash 的旧账号仍可读)。

**邀请码**

- relay 启动时通过 env(如 `TYPE4ME_RELAY_INVITE_CODES`,逗号分隔)配置一组有效邀请码。
- 注册时校验邀请码在集合内方可建账号。最小实现:邀请码可重复使用(自托管个人/小团队足够);是否一次性消费留待实现期权衡(YAGNI:先做可重复使用 + 校验)。

**两类凭据**

- **会话 token**:注册/登录后下发,短期有效(如 24h)。用途:登记设备、拉设备列表、设备管理。实现用带签名的 token(HMAC 签名,内含 account_id + 过期时间)或不透明 token + 服务端存储;最小实现用 HMAC 签名 token(无需新存储)。
- **device token**:长期,实际收发用,已存在。客户端本地存它实现"保持登录";会话过期只影响管理类操作。

**新增接口**

| 方法 | 路径 | 鉴权 | 作用 |
|---|---|---|---|
| POST | `/v1/auth/register` | 无(需邀请码) | `{username, password, invite_code}` → 建账号,返回会话 token |
| POST | `/v1/auth/login` | 无 | `{username, password}` → 返回会话 token |
| POST | `/v1/devices` | 会话 token | `{label, role}` → 复用 `AddDevice`,返回 `{device_id, device_token}` |
| GET | `/v1/devices` | 会话 token | 列出本账号设备(`id, label, role, last_seen, online`),给目标选择器 |

现有 `/v1/dispatch`、`/v1/subscribe`、`/v1/ping`(device 鉴权)**不变**。

**鉴权中间件**:在现有 `requireAdmin`/`requireDevice` 旁新增 `requireSession`(校验会话 token 签名+过期,解析出 account_id)。

**限流**:对 `/v1/auth/*` 加基本限流(按 IP),防暴力破解/滥注册。

### 2. Mac 端 · 登录与设备选择

- 设置中新增**账号**区(或登录态视图):未登录显示 用户名/密码/邀请码 的登录+注册;已登录显示账号名、本机设备名、设备列表、退出登录。
- 登录流程:`login` 拿会话 token → 若本地无本机 device token 则 `POST /v1/devices` 自动登记(label = 主机名,role = either)→ 存 device token → `GET /v1/devices` 拉列表。
- 「激活目标」选择器来源改为 `本机 + 合成的账号设备目标`(方案 A);`relayURL` 用固化常量;用户只选目标,不碰参数。
- 凭据(会话 token、device token)存 **Keychain**(落实 `OutputTarget.swift` 里记的迁移 TODO),不再明文进 credentials.json。
- LAN 直连模式(`OutputTarget` direct)保持原样,留给高级用户,不在本次重点。

### 3. Windows 端 · 登录界面取代命令行

- 把纯 CLI 换成**最小登录窗口 + 系统托盘**:输入 用户名/密码(注册可选)→ `login` → 自动登记本机为设备 → 写入本地配置 → 起 `Subscriber` 开始接收;登录后常驻托盘,显示连接状态。
- 凭据存 Windows 凭据库或 0600 配置文件。
- **GUI 技术选型(Go 原生 GUI 库 / 轻量 webview / 托盘+对话框)留到 Windows 分期 spec 决定**;整体设计在此只锁定"登录→自动登记→订阅"流程与所需后端接口(同第 1 节,无额外接口)。

## 固化参数

用户全程不接触:relay 地址(build 常量)、device id/token(自动生成)、消息协议、目标设备 id(从列表选)。唯一手工输入:用户名、密码、邀请码(仅注册一次)、选哪台目标设备。

## 安全

- 密码 bcrypt 哈希,与 device token 同算法。
- 会话 token 有过期;HMAC 签名密钥由 relay env 配置。
- 邀请码门槛防公网滥注册。
- 客户端 token 进系统密钥库(Mac Keychain / Windows 凭据库)。
- `/v1/auth/*` 基本限流。
- 跨账号隔离沿用现有 `Dispatch` 检查。

## 错误处理

- 邀请码无效 / 用户名已存在 / 密码错误 → 明确的 4xx + 客户端可读提示。
- 会话过期 → 客户端提示重新登录;但已存的 device token 仍可继续收发(管理操作才需会话)。
- 设备离线(目标未订阅)→ 沿用现有 `/v1/dispatch` 503 + Mac 端「失败落本机」兜底(已实现)。

## 测试策略

- **后端**:hub 层 User/Account 注册/登录(密码校验、邀请码、重复用户名);会话 token 签名/过期;`requireSession` 中间件;`/v1/devices` 列表只返回本账号;端到端冒烟脚本扩展(注册→登记设备→列表→dispatch)。
- **Mac**:会话/设备凭据的 Keychain 存取;登录后设备列表→合成 OutputTarget 的映射;登录态状态机(未登录/登录中/已登录/会话过期)。
- **Windows**:登录→自动登记→订阅 的流程(GUI 之外的逻辑单测);凭据存取。

## 分期实现顺序

1. **后端**(本设计第 1 节):User/Account + 邀请码 + auth/devices 接口 + 会话中间件 + 限流。其余两端依赖它。
2. **Mac**(第 2 节):登录 UI + 自动登记 + 设备列表→激活目标 + Keychain。
3. **Windows**(第 3 节):登录 GUI(技术选型另定)+ 自动登记 + 订阅 + 凭据存储。

每块单独 spec→计划→实现。
