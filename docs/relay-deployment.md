# Self-Hosted Relay 部署指南

从空 VPS 到生产可用的 step-by-step。前提:已有一台 Linux VPS,带公网 IP +
域名 + Caddy(本指南假设你用 Caddy 反代;nginx 类似)。

## 一、上传二进制

本地构建:

```bash
make -C relay build-linux
ls -la relay/dist/type4me-relay-linux-amd64
```

scp 到 VPS:

```bash
scp relay/dist/type4me-relay-linux-amd64 vps:/tmp/
```

## 二、VPS 上一次性 setup

```bash
# 1. 创建专用 user
sudo useradd --system --no-create-home --shell /usr/sbin/nologin type4me

# 2. 创建目录
sudo mkdir -p /etc/type4me-relay /var/lib/type4me-relay
sudo chown type4me:type4me /var/lib/type4me-relay
sudo chmod 0700 /var/lib/type4me-relay

# 3. 装二进制
sudo install -m 0755 /tmp/type4me-relay-linux-amd64 /usr/local/bin/type4me-relay

# 4. 写 env 文件 (replace TOKEN with output of: openssl rand -base64 48 | tr -d '=' | head -c 64)
sudo cp deploy/env.example /etc/type4me-relay/env
sudo vim /etc/type4me-relay/env           # 改 TYPE4ME_RELAY_ADMIN_TOKEN
sudo chown root:type4me /etc/type4me-relay/env
sudo chmod 0640 /etc/type4me-relay/env

# 5. 装 systemd unit
sudo cp deploy/type4me-relay.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now type4me-relay
sudo systemctl status type4me-relay --no-pager
```

## 三、Caddy 配置

把 `deploy/Caddyfile.example` 的内容 append 到 `/etc/caddy/Caddyfile`(改 hostname 为你的实际域名),然后:

```bash
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

第一次 reload 时 Caddy 会自动通过 HTTP-01 challenge 拿 Let's Encrypt cert。等几秒看到 `certificate obtained`。

## 四、防火墙

```bash
sudo ufw allow 22/tcp 80/tcp 443/tcp
sudo ufw enable
```

## 五、健康检查

```bash
curl https://relay.your-domain.com/healthz
# {"ok":true,"uptime_sec":N,"version":"..."}
```

## 六、创建 account + 2 device

```bash
RELAY=https://relay.your-domain.com
ADMIN="Bearer YOUR_ADMIN_TOKEN"

# 创 account
curl -X POST $RELAY/v1/admin/accounts \
  -H "Authorization: $ADMIN" -H "Content-Type: application/json" \
  -d '{"name":"Personal"}'
# → 记 account_id

ACCT=acct-XXX

# 创 Mac device
curl -X POST $RELAY/v1/admin/devices \
  -H "Authorization: $ADMIN" -H "Content-Type: application/json" \
  -d "{\"account_id\":\"$ACCT\",\"label\":\"My-Mac\"}"
# → 立刻记 device_token (43 字符,仅此一次显示)

# 创 Win device
curl -X POST $RELAY/v1/admin/devices \
  -H "Authorization: $ADMIN" -H "Content-Type: application/json" \
  -d "{\"account_id\":\"$ACCT\",\"label\":\"Win-PC\"}"
# → 同上,记 device_token
```

## 七、配置 Mac 端

编辑 `~/Library/Application Support/Type4Me/credentials.json`,加 `tf_remote_targets`:

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
    "matchBundleIds": ["com.youqu.todesk.mac"],
    "enabled": true
  }]
}
```

退出并重启 Type4Me dist(让它重读 credentials.json)。

## 八、配置 Win 端

在 Windows 上,`%APPDATA%\type4me-receiver\config.json`:

```json
{
  "mode": "relay-subscriber",
  "relay_url": "https://relay.your-domain.com",
  "device_id": "dev-Win01",
  "device_token": "BBB..."
}
```

或 PowerShell env vars:

```powershell
$env:TYPE4ME_MODE = "relay-subscriber"
$env:TYPE4ME_RELAY_URL = "https://relay.your-domain.com"
$env:TYPE4ME_DEVICE_ID = "dev-Win01"
$env:TYPE4ME_DEVICE_TOKEN = "BBB..."
.\type4me-receiver-windows-amd64.exe
```

启动后看到 `subscribing to https://relay.your-domain.com as dev-Win01` 即成功。

## 九、验证端到端

Mac 上:

1. 打开 ToDesk 连到 Windows
2. Windows 端聚焦某个输入框(Notepad / 浏览器都行)
3. ToDesk 窗口为 Mac 前台时按 Type4Me hotkey(Fn / Right Option / 你配的那个)说话
4. 文字应当出现在 Windows 输入框

Windows receiver 控制台应有:

```
inject ok=true reason="" text-len=NN req=<uuid> event=msg-<id>
```

## 十、备份

```bash
# 加到 crontab
0 3 * * * type4me cp /var/lib/type4me-relay/state.json /backup/type4me-relay-$(date +\%F).json
```

state.json < 10 KB。100 个 device 都不到几十 KB。

## 十一、升级

```bash
make -C relay build-linux
scp relay/dist/type4me-relay-linux-amd64 vps:/tmp/
ssh vps 'sudo install -m 0755 /tmp/type4me-relay-linux-amd64 /usr/local/bin/type4me-relay && sudo systemctl restart type4me-relay'
curl https://relay.your-domain.com/healthz
```

restart 约 5 秒,期间 receiver 会断开 + 自动重连。

## 十二、轮换 / 撤销 device token

```bash
# Rotate (生成新 token,老 token 立刻 401)
curl -X POST $RELAY/v1/admin/devices/dev-Win01/rotate \
  -H "Authorization: $ADMIN"
# → {"device_token":"NEW..."} 更新 Win config + 重启 receiver

# Delete (该 device 完全注销,token 永久失效)
curl -X DELETE $RELAY/v1/admin/devices/dev-Win01 \
  -H "Authorization: $ADMIN"
```

## 十三、卸载

```bash
sudo systemctl disable --now type4me-relay
sudo rm -rf /usr/local/bin/type4me-relay /etc/type4me-relay /var/lib/type4me-relay
sudo userdel type4me
# 从 Caddyfile 删 relay.your-domain.com 那段,reload Caddy
```

---

## 十四、生产实例(实际部署)

> **凭据不入库**:SSH 连接信息、`TYPE4ME_RELAY_ADMIN_TOKEN`、`TYPE4ME_RELAY_SESSION_KEY`、邀请码等敏感信息**不提交到本仓库**,由维护者私存(本地 agent 记忆 `relay-production-deploy`)。本节只记录运行手册与非敏感坐标。

- **公网地址**:`https://oc10.gouruicm.com`(`/healthz` 应返回 `{"ok":true,...}`)。前置 TLS/反代层**不是** Caddy,已存在并把 443 → 本机 `9010`,升级时不用动。
- **relay 绑定**:`0.0.0.0:9010`(`TYPE4ME_RELAY_BIND`)。
- **systemd**:`type4me-relay.service`(`User=type4me`,enabled),`EnvironmentFile=/etc/type4me-relay/env`,`ExecStart=/usr/local/bin/type4me-relay serve`。
- **路径**:二进制 `/usr/local/bin/type4me-relay`;env `/etc/type4me-relay/env`(root:type4me 0640);状态 `/var/lib/type4me-relay/state.json`(type4me:type4me 0600)。
- **客户端固化地址**:Mac `Type4Me/Services/RelayConfig.swift`、Windows `receiver/cmd/type4me-receiver-gui/build.go`(及 `receiver/Makefile` 的 `RELAY_URL`)均指向 `https://oc10.gouruicm.com`。

### 升级 / 重新部署(就地换二进制,保留账号数据)

账号系统(`/v1/auth/*`、会话鉴权的 `/v1/devices`)于 2026-05-30 上线。`state.json` 由 v1 平滑升到 v2,旧的 admin 建账号/设备继续可用。

```bash
# 1. 本地构建 linux 二进制
make -C relay build-linux
# 2. 上传(端口/用户见私存记录)
scp -P <port> relay/dist/type4me-relay-linux-amd64 <user>@<host>:/tmp/type4me-relay-new
# 3. 服务器上(sudo):校验 sha → 备份 → 安装 → 重启
ssh -p <port> <user>@<host>
sha256sum /tmp/type4me-relay-new          # 与本地 shasum -a 256 比对一致
DATE=$(date +%Y%m%d-%H%M%S)
sudo cp -a /usr/local/bin/type4me-relay /usr/local/bin/type4me-relay.bak-$DATE
sudo cp -a /var/lib/type4me-relay/state.json /var/lib/type4me-relay/state.json.bak-$DATE
sudo install -m 0755 -o root -g root /tmp/type4me-relay-new /usr/local/bin/type4me-relay
sudo systemctl restart type4me-relay
# 4. 验证(本地或服务器)
curl https://oc10.gouruicm.com/healthz
curl -X POST https://oc10.gouruicm.com/v1/auth/login -H 'Content-Type: application/json' -d '{"username":"x","password":"y"}'   # 期望 401 invalid_credentials(旧版会 404)
```

首次启用账号层时,需在 `/etc/type4me-relay/env` **追加**(不要覆盖已有行):

```
TYPE4ME_RELAY_SESSION_KEY=<openssl rand -base64 48 | tr -dc A-Za-z0-9 | head -c 64>   # 勿轻易更换:换了所有已登录会话失效(device token 不受影响)
TYPE4ME_RELAY_INVITE_CODES=<逗号分隔的邀请码>                                          # 客户端注册账号时填;空则关闭注册
```

回滚:`sudo install -m0755 /usr/local/bin/type4me-relay.bak-<DATE> /usr/local/bin/type4me-relay && sudo systemctl restart type4me-relay`。
