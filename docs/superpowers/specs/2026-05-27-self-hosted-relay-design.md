# Self-Hosted Relay — 设计文档

- **日期**: 2026-05-27
- **作者**: dsy (with Claude Code)
- **状态**: Draft(待 review)
- **关联**: 是 [2026-05-27 Remote Voice Input](2026-05-27-remote-voice-input-design.md) spec 的延伸,跟它的 S0+S1+S3 已实现部分共存

## 1. 问题陈述

S0+S1+S3 的"远程语音输入"假设 Mac 能直接 HTTP POST 到远程机器(同 LAN / Tailscale / 公网 IP)。**实际 Mac 和远程 Windows 经常不在同一网络**:Mac 在咖啡馆,Win 在家里;Win 在公司内网无公网 IP;两者跨地理位置。Tailscale 是最干净的解法,但用户希望走"完全自控"的中继方案,理由是不引入第三方 mesh-VPN(metadata 私密性、设备数量限制、信任边界等)。

**目标**:让 Mac → 远程机器的文字投递不再依赖网络可达性,改为两边都连 outbound 到用户自己 VPS 上的中继服务。

## 2. 范围 (v1)

**做**

- 新增独立 Go 服务 `type4me-relay`,部署在用户自己的 VPS(Linux,有公网 IP + 域名 + Caddy/nginx 反代 TLS)
- SSE-based PubSub:receiver 订阅,sender POST,relay 在内存路由,**不持久化消息**
- account → device 两层模型,每个 device 一个 bcrypt-hashed token;预留多人扩展但 v1 只有一个 admin
- Mac 端 `OutputTarget` 加 `mode` 字段,支持 `direct`(老 LAN 模式,完全保留)+ `relay`(新模式)
- Win 端 receiver 加 `mode` 字段,支持 `listener`(老 LAN 模式,完全保留)+ `relay-subscriber`(新模式)
- 同一用户 credentials.json 可以混搭:Win-PC 走 relay,家里 Mac-Studio 走 direct
- 部署 artifacts:systemd unit、Caddyfile 模板、部署文档
- 4 层测试金字塔(单元 + httptest + transport + 本地端到端脚本)+ 真机跨网手测

**显式不做(YAGNI / 留 v2+)**

- 消息持久化与重放(`Last-Event-ID` 当前只 echo,不真重放)
- 反向通信(Win → Mac 报告 paste outcome,目前 202 accepted 就够)
- 集群化 / 多 relay 节点 / Redis Streams
- gRPC / WebSocket / 浏览器扩展接收
- 端到端加密(relay 是用户自己的 VPS,信任边界已在用户手里)
- OAuth / OIDC / mTLS / 多 scope
- Web admin dashboard(curl 命令够用)
- 自动 CLI 子命令(`type4me-relay admin add-device …`),v1.5 再加
- Prometheus / OTel / 多地域 / 灾备
- SaaS 化(注册/计费/隐私政策等)

## 3. 架构

### 3.1 总览

```
              ┌─────────────────────────────────┐
   Mac        │     VPS (relay.your-domain.com)   │       Windows
              │                                   │
  Type4Me ──POST /v1/dispatch───►  relay ──SSE──►   receiver
  RelayHTTPSink                    (~400 LOC                (relay-subscriber
   (新 transport)                   新组件)                   mode, ~150 LOC 重写)
                                    │
                                    └─ Caddy 反代 TLS + Let's Encrypt
```

**关键拓扑改变**:Windows 那一头从"HTTP 服务器"变成"HTTP 客户端"。两边都 outbound 到 VPS,中间 NAT/防火墙不再是问题。

### 3.2 Relay 内部架构

无 DB,纯内存 PubSub,围绕 `Hub` 数据结构:

```go
type Hub struct {
    mu       sync.RWMutex
    accounts map[string]*Account
    devices  map[string]*Device           // deviceID → device
    subs     map[string]chan *Message     // deviceID → SSE 推送 channel
}

type Account struct {
    ID, Name string
    CreatedAt time.Time
}

type Device struct {
    ID, AccountID, Label, Role string  // role: sender / receiver / either(v1 全 either)
    TokenHash                  string  // bcrypt,不存明文
    CreatedAt, LastSeen        time.Time
}

type Message struct {
    ID, Text, FromDevice, RequestID string
    PreserveClipboard               bool
    CreatedAt                       time.Time
}
```

**消息流转**:
1. Mac POST `/v1/dispatch` → relay 校验 sender token、查 target 是否同 account、构造 Message → `subs[targetID] <- msg`(非阻塞 select)
2. SSE handler 在 receiver 订阅时 `subs[receiverID] = make(chan *Message, 16)`,然后 `for msg := range ch { writeSSE(w, msg); flusher.Flush() }`
3. receiver 断开 → handler 退出循环 → defer 里 `delete(subs, …)` + `close(ch)`

**不变量**:
- 消息**不持久化**(receiver 离线时 relay 返回 503 `receiver_offline`,sender 端落剪贴板兜底)
- channel buffer = 16,满了 → 503 `receiver_backpressure`(防止接收方卡死时无限堆积)
- LastSeen 每次收/发更新,后台 goroutine 每分钟扫尸,清 30+ 分钟没动静的 sub channel
- 持久化的只有 account/device 配置(`state.json`,原子写,0600 权限)

**为什么不上 Redis / Postgres**:单用户、单节点、几千行 Go + sync.RWMutex + channel 足够。引入 DB 把"一 binary + 一反代"的部署变成"binary + DB + 反代",运维成本三倍。v2 真做集群化再上 Redis Streams。

### 3.3 Mac 端改造

抽出 `RemoteTransport` protocol,封装"把文字发到远端"的细节:

```swift
protocol RemoteTransport {
    func dispatch(text: String, requestID: String, preserveClipboard: Bool) -> Bool
}
```

两个实现:
- **`DirectTransport`** —— 从现有 `RemoteHTTPSink` 提取的 HTTP POST 逻辑,POST `http://host:port/inject` 带 Bearer token
- **`RelayTransport`**(新) —— POST `https://<relay_url>/v1/dispatch` 带 Bearer device_token,body `{target_device_id, text, request_id, preserve_clipboard}`

`RemoteHTTPSink` 变成纯 Outcome 映射 + 剪贴板兜底,真传输委托给 transport,`init` 时根据 `target.mode` 选择哪个 transport。

`OutputTarget` 扩展:

```swift
struct OutputTarget: Codable, Equatable, Identifiable, Sendable {
    enum Mode: String, Codable, Sendable { case direct, relay }

    let id: String
    var name: String, enabled: Bool
    var matchBundleIds: [String]
    var mode: Mode = .direct           // 老 JSON 缺此字段 → .direct

    // mode == .direct:
    var host: String?
    var port: Int?
    var token: String?

    // mode == .relay:
    var relayURL: URL?
    var deviceID: String?
    var deviceToken: String?
    var targetDeviceID: String?
}
```

自定义 `Codable` decoder 实现"缺 mode 当作 direct";`OutputTargetStore.load()` 按 mode 验证必填字段,不满足跳过该 entry。

**关键不变量**:`OutputRouter` 不动 —— 只看 enabled + matchBundleIds,sink 内部走 direct 还是 relay 不关心。

### 3.4 Win 端改造

新增 `receiver/internal/relay/subscriber.go`:SSE 客户端,自动重连,带 `Last-Event-ID` header(为未来 replay 留口)。

`config.Config` 加 `Mode` 字段(`""` / `"listener"` → 老逻辑;`"relay-subscriber"` → 新)。env vars 加 `TYPE4ME_MODE / TYPE4ME_RELAY_URL / TYPE4ME_DEVICE_ID / TYPE4ME_DEVICE_TOKEN`。

`main.go` 分叉:

```go
switch cfg.Mode {
case "", "listener":
    runListener(ctx, cfg, inj)           // S0+S1+S3 老路径
case "relay-subscriber":
    runRelaySubscriber(ctx, cfg, inj)    // 新路径
default:
    log.Fatalf("unknown mode: %s", cfg.Mode)
}
```

**兼容矩阵**:

| Mac mode | Win mode | 工作模式 |
|---|---|---|
| 缺 / `direct` | 缺 / `listener` | S3 LAN 直连(完全保留) |
| `relay` | `relay-subscriber` | 新 relay 模式 |
| 不匹配组合 | | 不通(Mac 直连 Win 不订阅,反之亦然) |

允许同一 credentials.json 里不同 target 用不同 mode。

## 4. HTTP API

### 4.1 路径表

| Method | Path | Auth | 入参 | 返回 |
|---|---|---|---|---|
| `GET` | `/healthz` | 无 | — | `{ok:true, version, uptime_sec}` |
| `GET` | `/v1/ping` | device token | — | `{ok:true, device_id, account_id, server_time}` |
| `POST` | `/v1/dispatch` | sender device token | `{target_device_id, text, request_id?, preserve_clipboard?}` | 202 `{accepted:true, message_id}` |
| `GET` | `/v1/subscribe` | receiver device token | `Accept: text/event-stream` | SSE 长连 |
| `POST` | `/v1/admin/accounts` | admin token | `{name}` | 201 `{account_id, name}` |
| `GET` | `/v1/admin/accounts` | admin token | — | `{accounts: [...]}` |
| `POST` | `/v1/admin/devices` | admin token | `{account_id, label, role?}` | 201 `{device_id, device_token, …}` — token 仅返回一次 |
| `GET` | `/v1/admin/devices` | admin token | — | `{devices: [...]}`(无 token 字段) |
| `POST` | `/v1/admin/devices/{id}/rotate` | admin token | — | `{device_token}` |
| `DELETE` | `/v1/admin/devices/{id}` | admin token | — | 204 |

### 4.2 鉴权模型

**Admin token**:env `TYPE4ME_RELAY_ADMIN_TOKEN`(启动时校验非空),全用于 `/v1/admin/*`。无法轮换(改 env 重启)。

**Device token**:32 字节 `crypto/rand` → base64url 43 字符。明文**只在 `POST /v1/admin/devices` 响应里出现一次**。server 只存 bcrypt 哈希。

**校验**:`/v1/dispatch` 等 device 路径解析 Bearer token → 查 hash 匹配的 device,**用 token cache(hash[:16] → device_id)避免每次 bcrypt ~80ms 开销**;cache miss 时走完整 bcrypt 验证 + 缓存结果;token rotate / device delete 失效 cache。

**跨 account 防护**:`/v1/dispatch` 校验 sender.account == target.account,否则 403 `cross_account`。

### 4.3 `POST /v1/dispatch` 详细

请求:
```http
POST /v1/dispatch HTTP/1.1
Authorization: Bearer <sender_device_token>
Content-Type: application/json

{"target_device_id":"dev-win-qiya","text":"你好","request_id":"uuid-xxx","preserve_clipboard":true}
```

响应矩阵:

| HTTP | body | 含义 |
|---|---|---|
| `202` | `{accepted:true, message_id:"msg-uuid"}` | 已投递给 target 的 SSE channel |
| `400` | `{error:"bad_json"}` | body 无法解析 |
| `401` | `{error:"invalid_sender_token"}` | sender token 错 / 已 revoke |
| `403` | `{error:"cross_account"}` | target device 不在 sender 的 account 里 |
| `404` | `{error:"target_not_found"}` | target_device_id 不存在 |
| `413` | `{error:"text_too_large", max_bytes:8192}` | 与 [remote-voice-input-design § 4.1](2026-05-27-remote-voice-input-design.md) 一致 |
| `503` | `{error:"receiver_offline"}` | target 当前没有活的 SSE 订阅 |
| `503` | `{error:"receiver_backpressure"}` | target 的 channel buffer 已满(16 条) |

### 4.4 `GET /v1/subscribe` 详细 (SSE)

请求:
```http
GET /v1/subscribe HTTP/1.1
Authorization: Bearer <receiver_device_token>
Accept: text/event-stream
Cache-Control: no-cache
Last-Event-ID: <optional>
```

响应:`Content-Type: text/event-stream`,`Connection: keep-alive`,`X-Accel-Buffering: no`。

消息帧格式:
```
id: msg-uuid
event: inject
data: {"text":"你好","request_id":"uuid-xxx","from_device":"dev-mac-1","preserve_clipboard":true}

```

(注意空行 `\n\n` 是 SSE 分隔符,必须有)

**心跳**:server 每 25 秒写 `: ping\n\n`(SSE 注释),防中间代理切 idle 连接。

**断开 / 重连**:receiver 收到 EOF → SSE 规范要求自动重连,server 通过 `retry: 5000\n\n` 控制间隔。重连时 `Last-Event-ID` header 当前 server 只 echo log,不真重放。后期接 Redis Streams 才能做真 replay。

## 5. 设备注册 + 配对流程

### 5.1 Bootstrap

VPS 上设环境变量,启动 relay:

```bash
export TYPE4ME_RELAY_ADMIN_TOKEN=<pwgen 64 字节随机>
export TYPE4ME_RELAY_BIND=127.0.0.1:8443
export TYPE4ME_RELAY_STATE_DIR=/var/lib/type4me-relay
./type4me-relay serve
```

Caddyfile:

```caddy
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
}
```

### 5.2 创建 account + 两台 device(三个 curl)

```bash
RELAY=https://relay.your-domain.com
ADMIN="Bearer $ADMIN_TOKEN"

# 1. 创 account
curl -X POST $RELAY/v1/admin/accounts \
  -H "Authorization: $ADMIN" -H "Content-Type: application/json" \
  -d '{"name":"Personal"}'
# → {"account_id":"acct-AbCd","name":"Personal","created_at":"..."}
ACCT=acct-AbCd

# 2. 创 Mac sender device
curl -X POST $RELAY/v1/admin/devices \
  -H "Authorization: $ADMIN" -H "Content-Type: application/json" \
  -d "{\"account_id\":\"$ACCT\",\"label\":\"My-Mac\",\"role\":\"either\"}"
# → {"device_id":"dev-Mac01","device_token":"AAA...",...}

# 3. 创 Win receiver device
curl -X POST $RELAY/v1/admin/devices \
  -H "Authorization: $ADMIN" -H "Content-Type: application/json" \
  -d "{\"account_id\":\"$ACCT\",\"label\":\"Win-PC\",\"role\":\"either\"}"
# → {"device_id":"dev-Win01","device_token":"BBB...",...}
```

### 5.3 配到 Mac

`~/Library/Application Support/Type4Me/credentials.json`:

```json
{
  "tf_remote_targets": [{
    "id": "win-via-relay",
    "name": "Win-PC (via relay)",
    "mode": "relay",
    "relay_url": "https://relay.your-domain.com",
    "device_id": "dev-Mac01",
    "device_token": "AAA...",
    "target_device_id": "dev-Win01",
    "matchBundleIds": ["com.youqu.todesk.mac", "com.microsoft.rdc.macos"],
    "enabled": true
  }]
}
```

### 5.4 配到 Win

`%APPDATA%\type4me-receiver\config.json`(或 PowerShell env):

```json
{
  "mode": "relay-subscriber",
  "relay_url": "https://relay.your-domain.com",
  "device_id": "dev-Win01",
  "device_token": "BBB..."
}
```

### 5.5 Token 轮换 / 注销

```bash
curl -X POST $RELAY/v1/admin/devices/dev-Win01/rotate -H "Authorization: $ADMIN"
# → 返回新 token,老 token 立刻 401。更新 Win 配置 + 重启 receiver。

curl -X DELETE $RELAY/v1/admin/devices/dev-Win01 -H "Authorization: $ADMIN"
# → 204,该 device 完全删除。
```

### 5.6 一次性 pair URL(预留,依赖 [[type4me-remote-input-design]] S2 的 URL scheme handler)

`POST /v1/admin/devices` 响应中增字段 `pair_url`:

```
type4me://pair-relay?relay=<urlencoded>&account=<acct>&device=<dev>&token=<tok>
```

Mac 端 URL handler 解析后弹"添加 relay target"对话框,字段预填,用户只补 `matchBundleIds` 即可。**v1 relay spec 里只**保留响应字段,Mac 端 URL 解析等 S2 做。

### 5.7 State 持久化

`/var/lib/type4me-relay/state.json`,原子写,0600,结构:

```json
{
  "version": 1,
  "accounts": [{"id":"acct-AbCd","name":"Personal","created_at":"..."}],
  "devices": [
    {"id":"dev-Mac01","account_id":"acct-AbCd","label":"My-Mac",
     "role":"either","token_hash":"$2a$10$...","created_at":"...","last_seen":"..."}
  ]
}
```

**消息从不落盘**;token_hash 是 bcrypt,丢了也无法反推明文。

## 6. 部署形态

### 6.1 systemd unit

`/etc/systemd/system/type4me-relay.service`:

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

### 6.2 目录权限

```
/usr/local/bin/type4me-relay              root:root,    0755
/etc/type4me-relay/env                    root:type4me, 0640  (admin token + 配置)
/var/lib/type4me-relay/state.json         type4me:type4me, 0600
```

专用 user:`useradd --system --no-create-home --shell /usr/sbin/nologin type4me`

### 6.3 防火墙

只开 22(SSH)+ 80(Let's Encrypt HTTP-01)+ 443(HTTPS)。relay 自身只 listen `127.0.0.1:8443`,**永远不暴露公网**。

### 6.4 升级流程(零停机)

```bash
scp dist/type4me-relay-linux-amd64 vps:/tmp/
ssh vps 'sudo install -m 0755 /tmp/type4me-relay-linux-amd64 /usr/local/bin/type4me-relay && sudo systemctl restart type4me-relay'
curl -s https://relay.your-domain.com/healthz
```

restart 期间 receiver 会断开 + 自动重连(~5s),期间 Mac POST 会拿到 503 `receiver_offline` → 文字进剪贴板兜底。**用户感知 = 5 秒内说的话要手动 Cmd+V**,不掉。

### 6.5 备份 + 卸载

备份只一个文件:
```bash
0 3 * * * type4me cp /var/lib/type4me-relay/state.json /backup/type4me-relay-$(date +\%F).json
```

卸载:`systemctl disable --now type4me-relay && rm -rf /usr/local/bin/type4me-relay /etc/type4me-relay /var/lib/type4me-relay && userdel type4me`,Caddy 配置删 relay.your-domain.com 那段。

## 7. 测试策略

### 7.1 4 层金字塔

| Layer | 范围 | 在哪跑 |
|---|---|---|
| L1 — Hub 单元 | `Hub.AddDevice/Subscribe/Dispatch/LoadState` + 并发 + token cache 失效 | Go,Linux/macOS 都可,纯 stdlib + bcrypt |
| L2 — Relay HTTP 集成 | httptest + 所有端点的 happy + auth fail + business rule fail;SSE 帧格式;心跳 | Go,任何 OS |
| L3 — 客户端 Transport | Mac 的 `DirectTransportTests` / `RelayTransportTests`;Win 的 `subscriber_test.go` | Mac (Swift),Win 跨编译 (Go) |
| L4 — 端到端冒烟脚本 | `scripts/test_relay_e2e.sh` 起 relay + receiver(loopback subscriber)+ curl dispatch → `pbpaste` 验证 | macOS dev 机 |

### 7.2 关键测试 case 清单

**L1 Hub**:
- 重名 account 拒绝;创建后 state.json 原子写
- AddDevice 父 account 不存在 → 拒绝;token bcrypt 后只存 hash
- Subscribe 注册 channel,后续 Dispatch 投递成功
- 重复 Subscribe(reconnect)覆盖旧 channel + 旧 channel 关闭
- Offline / Backpressure / CrossAccount 三类错误
- 同 device 自发自收允许(用于自检)
- 10 个 goroutine 并发 Dispatch,顺序内一致
- LoadState 后 token_hash 字段不丢
- Token rotate 后老 token cache 命中也走 bcrypt 验证、失败清 cache

**L2 HTTP**:
- 每端点 3 个 case(happy / auth fail / business rule fail)
- SSE 消息帧含 `id:` + `event: inject` + `data: {…}` + 空行
- 心跳每 25 秒一条
- Subscribe 后 1 秒内 Dispatch 可见
- Subscribe 断开后 1 秒内 channel 被清

**L3 Mac**:
- 现有 `RemoteHTTPSinkTests` 改名 `DirectTransportTests`,5 case 沿用
- `RelayTransportTests`:202 / 401 / 403 / 503 / timeout / body shape / Auth header
- `OutputTargetTests` 扩展:mode 缺失 → direct;relay 必填字段缺失抛 decode 错

**L3 Win**:
- subscriber 收 SSE 帧后调 Injector.Inject
- 重连场景:server 主动 close → subscriber 重连成功
- Last-Event-ID:断开重连时 header 出现
- 坏 JSON data 跳过 + 继续
- 心跳注释忽略
- 401 退出 fatal 而非无限重连

**L4 端到端**:
- 跑 `./scripts/test_relay_e2e.sh`,PASS 输出 + 剪贴板验证

### 7.3 显式不做的测试

- TLS 握手单测(Caddy 接管)
- 极端 backpressure 压测(实际 QPS 1-3/min)
- SSE 协议 conformance suite(stdlib 实现 + 简单 protocol)
- bcrypt cost fuzz

## 8. 切片(R0-R5)

| 阶段 | 范围 | 验收信号 | 估时 |
|---|---|---|---|
| **R0** | `relay/` 新目录 + go module;`internal/hub/`(Hub + Account/Device/Message + state.json load/save);Hub 单元测试 | `go test ./internal/hub/...` 全过(L1 完整) | 半天 |
| **R1** | `cmd/type4me-relay/main.go` + `internal/server/` HTTP handlers + auth middleware + SSE + heartbeat;Makefile cross-compile darwin/linux/windows | `make test` 全过(L2 完整);本地 curl 完整跑 dispatch→subscribe→收到消息 | 1 天 |
| **R2** | Mac 端 `OutputTarget` mode 字段;`RemoteTransport` protocol + `DirectTransport` + `RelayTransport`;`RemoteHTTPSink` 重构;测试改名 + 新增 | `swift test` 全过(L3 Mac 完整);现有 LAN 模式行为不变 | 1 天 |
| **R3** | `receiver/internal/relay/subscriber.go` SSE 客户端 + 自动重连;`config.Config` Mode + env vars;`main.go` 分叉;subscriber 单测 | `make test` 全过(L3 Win 完整);本地 relay + subscriber + curl 链路通 | 1 天 |
| **R4** | `scripts/test_relay_e2e.sh`;`deploy/type4me-relay.service`;`deploy/Caddyfile.example`;`docs/relay-deployment.md`;Makefile 加 `build-linux` target | E2E 脚本 PASS;新部署 docs 步骤可执行 | 半天 |
| **R5** | 真 VPS 部署 + 跨网真机手测(Mac 在咖啡馆,Win 在家里)| Type4Me 录音 → 文字落 Win 焦点框,延迟 < 500ms | 0.5-1 天 |

**推荐严格顺序 R0→R5**。R1 完成后**建议停 1-2 天自测**:relay 本身可用,curl 当 driver,确认协议手感再继续 R2/R3。R4 不严格依赖 R3,部署文档可以在 R1 后写 80%。

**分支策略**:整套 `feature/relay` 一个分支,R1/R3/R5 三个里程碑各打 tag(`relay-r1` / `relay-r3` / `relay-r5-shipped`),merge 时机由用户决定。`relay/` 跟 `receiver/` 同级,两个独立 go module。

## 9. 风险与缓解

| 风险 | 缓解 | 何时补强 |
|---|---|---|
| VPS 是 SPOF | Mac 剪贴板兜底;LAN 模式不依赖 relay | 不补强 |
| admin token 泄露 | env file 0640 + 限定 user;不进日志;重启轮换 | 切 SSH key auth |
| device token 泄露 | bcrypt hash 存储;rotate 简单(1 curl);Mac 端 Keychain 在 S2 一并 | S2 配合 |
| bcrypt 验证慢(80ms) | token cache(hash[:16] → device_id),hit O(1) | 不需要 |
| SSE 被中间代理 buffer | Caddy `flush_interval -1` + relay `X-Accel-Buffering: no` 三重防御 | 出问题再针对补 |
| 公司网络封 long-lived HTTP | 已知风险接受 | 真发生切 WebSocket |
| 25s 心跳被 NAT 切 | 自动重连 | 若频繁掉降到 15s |
| state.json 损坏 | 原子写 + 每日 cron 备份 + version 字段 | 备份够 |
| Mac 端 Bug 发错 target | OutputRouter 不静默回退 + relay cross_account 二重防御 | 已包含 |
| DDoS / 扫描 | Caddy 前置 rate limiting(可加);401 不做昂贵操作 | Caddy module |

## 10. Open Questions(留 v2 spec)

1. **token 进 Keychain/DPAPI**:Mac 端 `tf_remote_targets[].device_token` 目前在 JSON,Win 端 `device_token` 在 config.json/env。两边一起在 S2 搬到平台密钥库,还是 receiver 单独做?
2. **本机自检 mode**:relay 是否需要 `--self-check` 子命令,启动后自己 dispatch 一条消息给自己看 round-trip ok?
3. **Mac 端 UI 显示 relay target 的"在线/离线"状态**:Subscriber 在线只有 relay 知道,`/v1/admin/devices/{id}` 加 `online` 字段?S2 UI 设计时考虑
4. **Win 端 Type4Me 出来后,反向 Win → Mac sender**:role: either 已支持,但目前只有"Mac sender → Win receiver"一种方向
