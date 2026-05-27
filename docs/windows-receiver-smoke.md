# Windows 接收端手动 Smoke

S3 完成后,Mac 端跨编译 verify 通过不代表 Windows 上行为对。这份文档列出真机
smoke 步骤。

## 前置

- Windows 10 或 11(amd64)
- 与 Mac 在同一 LAN,或 Mac/Windows 都在 Tailscale 网内(知道 Windows 的可达
  hostname/IP)
- Mac 上 receiver/ 已 build 出 windows-amd64 二进制

## 步骤

### 1. 在 Mac 上 build Windows 二进制

```bash
cd receiver && make build-windows
ls -la dist/type4me-receiver-windows-amd64.exe
```

预期看到 `dist/type4me-receiver-windows-amd64.exe`,大小约 5-7 MB。

### 2. scp 到 Windows 机器

```bash
scp receiver/dist/type4me-receiver-windows-amd64.exe \
    <user>@<windows-host>:C:/Users/<user>/Desktop/type4me-receiver.exe
```

或者用任何文件同步手段把 `.exe` 拷到 Windows。

### 3. 在 Windows 上启动 receiver

打开 PowerShell 或 cmd,进入 `.exe` 所在目录:

```powershell
$env:TYPE4ME_TOKEN = "test-token-win"
$env:TYPE4ME_PORT  = "47318"
.\type4me-receiver.exe
```

应看到 pairing 信息:

```
================ type4me-receiver pairing ================
  Name:    <hostname>
  Addr:    0.0.0.0:47318
  Token:   test-token-win
  URL:     type4me://pair?host=127.0.0.1&port=47318&token=test-token-win&...
==========================================================
2026/XX/XX XX:XX:XX listening on 0.0.0.0:47318
```

### 4. Windows 防火墙允许

第一次启动 Windows 可能弹防火墙提示,**勾选"专用网络"**(LAN)然后允许。

### 5. 在 Mac 上从命令行打一次 /inject(基本连通性)

```bash
# 替换 <windows-host> 为 Windows 机器的 IP/hostname
curl -s -X POST http://<windows-host>:47318/inject \
    -H "Authorization: Bearer test-token-win" \
    -H "Content-Type: application/json" \
    -d '{"text":"hello from mac"}'
```

预期返回:

```json
{"ok":true,"outcome":{"pasted":true},"request_id":""}
```

并且 Windows 这一头:
- 系统剪贴板里有 `hello from mac`
- Cmd+V(在 Windows 是 Ctrl+V)被发送,如果你在 Windows 上有任何焦点文本框,字会进去

### 6. 真实场景:Mac Type4Me → Windows

在 Mac 的 `~/Library/Application Support/Type4Me/credentials.json` 加:

```json
{
  "tf_remote_targets": [
    {
      "id": "win-prod",
      "name": "Win-PC",
      "host": "<windows-host>",
      "port": 47318,
      "token": "test-token-win",
      "matchBundleIds": [
        "com.microsoft.rdc.macos",
        "com.parsecgaming.parsec",
        "com.moonlight-stream.Moonlight"
      ],
      "enabled": true
    }
  ]
}
```

启动 Type4Me dist,打开你的远程桌面客户端(Microsoft Remote Desktop /
Parsec / Moonlight),连到 Windows,在远程的某个文本框聚焦,Mac 上按
Type4Me 录音快捷键说一句话。

**预期:文字出现在 Windows 远程焦点框里**,链路是
`Mac mic → ASR → Mac OutputRouter (前台命中 RDP bundle id) →
Mac POST /inject → Windows receiver → SetClipboardData + Ctrl+V →
Windows 焦点框`。

### 7. Type4Me Mac 端如何确认走了 remote 而不是 local

查看 Windows 上 receiver 的 stdout,每次成功 inject 都会打一行:

```
2026/XX/XX XX:XX:XX /inject ok=true reason="" text-len=NN req=...
```

如果 Mac 录音后这行没出现,说明:
- 路由没匹配 RDP bundle id(检查 matchBundleIds 是不是真的命中你的客户端)
- 或者 HTTP 连不通(防火墙、IP 错、端口错)
- 或者 Mac 端 fallback 到了剪贴板兜底,文字进剪贴板而非远程

## 验收清单

- [ ] curl 直接打 receiver,Windows 剪贴板有文字、Ctrl+V 在 Notepad 里
      看到了文字
- [ ] Mac Type4Me 录音 + 远程桌面前台,文字出现在 Windows 远程焦点框
- [ ] Mac Type4Me 录音 + Mac 本地 app 前台(不命中 RDP bundle id),
      文字落到 Mac 本地剪贴板/Cmd+V 路径,Windows receiver 没有 /inject
      日志
