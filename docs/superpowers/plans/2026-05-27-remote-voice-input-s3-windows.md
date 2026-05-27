# Remote Voice Input — S3 Windows 接收端实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在已有的 Go `receiver/` 项目里增加 Windows 平台的 `Injector` 实现,纯 syscall 调 user32/kernel32(无 cgo),把 Type4Me 的语音识别结果通过同一套 HTTP 接收端协议落到 Windows 远程机器的焦点输入框。

**Architecture:** 新增 `inject_windows.go`(build tag `windows`),与现有 `inject_darwin.go` 对等实现 `Injector` 接口。剪贴板用 `OpenClipboard` + `GlobalAlloc(GMEM_MOVEABLE)` + `SetClipboardData(CF_UNICODETEXT)` 写 UTF-16,paste 用 `SendInput` 发 4 个 keyboard INPUT events(Ctrl down → V down → V up → Ctrl up),`GetForegroundWindow` 返回 0 时标 `no-focus` 但仍执行 paste(部分 RDP 客户端 API 返回空)。

**Tech Stack:** Go 1.21+,`syscall.NewLazyDLL`(`user32.dll`、`kernel32.dll`),无外部依赖,无 cgo。

**Spec:** `docs/superpowers/specs/2026-05-27-remote-voice-input-design.md` § 5.2

**前置:** S0+S1 已完成(branch `feature/remote-voice-input-s0-s1` 23+ commits),`Injector` 接口在 `receiver/internal/inject/inject.go` 已定义,`Fake` 测试已存在,Makefile 已有 `build-windows` cross-compile target。

---

## File Structure

- Create: `receiver/internal/inject/inject_windows.go` — Windows 实现,build tag `//go:build windows`
- Create: `receiver/internal/inject/inject_windows_test.go` — Windows-only 单元测试,build tag `//go:build windows`
- Modify: `receiver/Makefile` — `test` target 加上 cross-compile 验证,确保 windows-amd64 build 永远不挂

测试在 Mac 上**只能跨编译验证 syntax + 链接**;真实行为验证(剪贴板写入、SendInput 触发)需要在真 Windows 机器上手动 smoke。

---

## Task W1: Scaffold `inject_windows.go` 满足 `Injector` 接口

**Files:**
- Create: `receiver/internal/inject/inject_windows.go`

目标:产出可跨编译的最小 Windows 实现,`Inject` 返回 stub outcome,`Ping` 返回 nil。后续 task 把内部填上。这一步先保证 `GOOS=windows go build` 成功 + Mac 端编译不被 build tag 干扰。

- [ ] **Step 1: 创建文件**

`receiver/internal/inject/inject_windows.go`:

```go
//go:build windows

package inject

import "errors"

type winInjector struct{}

// NewPlatform returns the Windows platform injector.
func NewPlatform() Injector { return &winInjector{} }

func (w *winInjector) Inject(text string) (Outcome, error) {
	if text == "" {
		return Outcome{Pasted: false, Reason: "empty"}, nil
	}
	return Outcome{}, errors.New("not implemented")
}

func (w *winInjector) Ping() error { return nil }
```

- [ ] **Step 2: 跨编译验证**

```bash
cd receiver && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...
```

预期:无输出(成功)。

- [ ] **Step 3: 顺便验证 macOS 还能 build(确认 build tag 不串)**

```bash
cd receiver && go build ./...
```

预期:无输出。

- [ ] **Step 4: Commit**

```bash
git add receiver/internal/inject/inject_windows.go
git commit -m "feat(receiver): Windows Injector 骨架 (build tag windows)

Inject 暂时返回 not-implemented;后续 task 填实现。
此步只验证 cross-compile 成功 + macOS build 不受影响。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task W2: 实现完整 Windows Injector(剪贴板 + SendInput + 焦点检查)

**Files:**
- Modify: `receiver/internal/inject/inject_windows.go`

整体实现作为一块提交。剪贴板、SendInput、GetForegroundWindow 三个 syscall 路径在 `Inject()` 里是串联调用,分开提交会有不可编译的中间态。

### 实现思路

**syscall 模式**(标准库 `syscall.NewLazyDLL` + `LazyProc.Call`):

```go
var (
    user32   = syscall.NewLazyDLL("user32.dll")
    kernel32 = syscall.NewLazyDLL("kernel32.dll")

    procOpenClipboard       = user32.NewProc("OpenClipboard")
    procEmptyClipboard      = user32.NewProc("EmptyClipboard")
    procSetClipboardData    = user32.NewProc("SetClipboardData")
    procCloseClipboard      = user32.NewProc("CloseClipboard")
    procSendInput           = user32.NewProc("SendInput")
    procGetForegroundWindow = user32.NewProc("GetForegroundWindow")

    procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
    procGlobalLock   = kernel32.NewProc("GlobalLock")
    procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
    procRtlMoveMemory = kernel32.NewProc("RtlMoveMemory")
)
```

**关键常量**:

| 名称 | 值 | 用途 |
|---|---|---|
| `CF_UNICODETEXT` | `13` | 剪贴板格式(UTF-16LE,以 NUL 结尾) |
| `GMEM_MOVEABLE` | `0x0002` | GlobalAlloc 标志,SetClipboardData 要求 |
| `INPUT_KEYBOARD` | `1` | SendInput 第一个字段 |
| `KEYEVENTF_KEYUP` | `0x0002` | KEYBDINPUT.Flags 中表示松开 |
| `VK_CONTROL` | `0x11` | Virtual-Key Code: Ctrl |
| `VK_V` | `0x56` | Virtual-Key Code: V |

**`kbdInput` 结构布局**(amd64,总大小 40 字节):

```go
type kbdInput struct {
    typ   uint32   // @0,  size 4 — INPUT_KEYBOARD
    _     uint32   // @4,  size 4 — align padding for amd64 union
    vk    uint16   // @8
    scan  uint16   // @10
    flags uint32   // @12
    time  uint32   // @16
    // @20: implicit 4-byte pad — Go aligns uintptr to 8
    extra uintptr  // @24
    _     [8]byte  // @32, size 8 — pad to fill MOUSEINPUT union slot
    // total = 40
}
```

`unsafe.Sizeof(kbdInput{})` 必须等于 40;Task W3 的测试会断言这一点。

### Step 1: 替换文件全部内容

`receiver/internal/inject/inject_windows.go`:

```go
//go:build windows

package inject

import (
	"errors"
	"fmt"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

const (
	cfUnicodeText = 13

	gmemMoveable = 0x0002

	inputKeyboard   = 1
	keyEventfKeyUp  = 0x0002

	vkControl = 0x11
	vkV       = 0x56
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procOpenClipboard       = user32.NewProc("OpenClipboard")
	procEmptyClipboard      = user32.NewProc("EmptyClipboard")
	procSetClipboardData    = user32.NewProc("SetClipboardData")
	procCloseClipboard      = user32.NewProc("CloseClipboard")
	procSendInput           = user32.NewProc("SendInput")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")

	procGlobalAlloc   = kernel32.NewProc("GlobalAlloc")
	procGlobalLock    = kernel32.NewProc("GlobalLock")
	procGlobalUnlock  = kernel32.NewProc("GlobalUnlock")
	procRtlMoveMemory = kernel32.NewProc("RtlMoveMemory")
)

// kbdInput is INPUT { type = INPUT_KEYBOARD; ki = KEYBDINPUT } on amd64.
// Total size 40 bytes — the MOUSEINPUT union slot is larger than KEYBDINPUT
// so we pad to match. The implicit Go pad between `time` and `extra` is
// 4 bytes (uintptr is 8-byte aligned).
type kbdInput struct {
	typ   uint32
	_     uint32
	vk    uint16
	scan  uint16
	flags uint32
	time  uint32
	extra uintptr
	_     [8]byte
}

type winInjector struct{}

// NewPlatform returns the Windows platform injector.
func NewPlatform() Injector { return &winInjector{} }

func (w *winInjector) Inject(text string) (Outcome, error) {
	if text == "" {
		return Outcome{Pasted: false, Reason: "empty"}, nil
	}
	if err := setClipboardUTF16(text); err != nil {
		return Outcome{}, fmt.Errorf("set clipboard: %w", err)
	}
	// Check foreground window — empty result is recorded but does not stop paste,
	// because some RDP clients return 0 when their window structure isn't
	// what GetForegroundWindow expects, while paste still routes correctly.
	hwnd, _, _ := procGetForegroundWindow.Call()
	reason := ""
	if hwnd == 0 {
		reason = "no-focus"
	}
	if err := sendCtrlV(); err != nil {
		return Outcome{Pasted: false, Reason: "paste-blocked"}, nil
	}
	if reason != "" {
		return Outcome{Pasted: false, Reason: reason}, nil
	}
	return Outcome{Pasted: true}, nil
}

func (w *winInjector) Ping() error { return nil }

// setClipboardUTF16 writes `s` to the Windows clipboard as CF_UNICODETEXT.
func setClipboardUTF16(s string) error {
	// Encode as UTF-16LE with trailing NUL (CF_UNICODETEXT requires this).
	u16 := utf16.Encode([]rune(s))
	u16 = append(u16, 0)
	byteLen := uintptr(len(u16) * 2)

	if ret, _, _ := procOpenClipboard.Call(0); ret == 0 {
		return errors.New("OpenClipboard failed")
	}
	defer procCloseClipboard.Call()

	procEmptyClipboard.Call()

	hMem, _, _ := procGlobalAlloc.Call(gmemMoveable, byteLen)
	if hMem == 0 {
		return errors.New("GlobalAlloc failed")
	}

	dst, _, _ := procGlobalLock.Call(hMem)
	if dst == 0 {
		return errors.New("GlobalLock failed")
	}
	// Copy UTF-16 buffer to the locked memory.
	procRtlMoveMemory.Call(dst, uintptr(unsafe.Pointer(&u16[0])), byteLen)
	procGlobalUnlock.Call(hMem)

	if ret, _, _ := procSetClipboardData.Call(cfUnicodeText, hMem); ret == 0 {
		// Ownership of hMem transfers to the system on success; on failure we
		// would normally GlobalFree, but the system has already consumed it
		// from EmptyClipboard's perspective. Leave it — single-shot per inject.
		return errors.New("SetClipboardData failed")
	}
	return nil
}

// sendCtrlV synthesizes the keyboard sequence Ctrl down → V down → V up → Ctrl up
// via SendInput. Using SendInput (not keybd_event) is more IME-friendly and
// gives us atomic injection per the spec § 5.2.
func sendCtrlV() error {
	inputs := []kbdInput{
		{typ: inputKeyboard, vk: vkControl},                       // Ctrl down
		{typ: inputKeyboard, vk: vkV},                             // V    down
		{typ: inputKeyboard, vk: vkV, flags: keyEventfKeyUp},      // V    up
		{typ: inputKeyboard, vk: vkControl, flags: keyEventfKeyUp},// Ctrl up
	}
	n, _, _ := procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		unsafe.Sizeof(inputs[0]),
	)
	if int(n) != len(inputs) {
		return fmt.Errorf("SendInput: wrote %d of %d", n, len(inputs))
	}
	return nil
}
```

### Step 2: 跨编译

```bash
cd receiver && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...
```

预期:无输出。如果编译失败,常见原因:
- `unsafe.Pointer` 使用错误
- `syscall.LazyProc.Call` 签名不匹配 — `Call(args ...uintptr)` 返回 `(r1, r2 uintptr, err error)`,所有参数转 `uintptr`
- 包导入冲突 — 文件内只在 windows 上编译,所以 darwin 视角 `_test.go` 看不到这些 symbol(后续测试也带 build tag)

### Step 3: 顺便 macOS 端检查

```bash
cd receiver && go build ./... && go test ./...
```

预期:macOS 上 build 通过,fake test 仍过(`fake_test.go` 没 build tag,跨平台跑)。

### Step 4: Commit

```bash
git add receiver/internal/inject/inject_windows.go
git commit -m "feat(receiver): Windows Injector 完整实现

- 纯 syscall 调 user32/kernel32,无 cgo
- 剪贴板:OpenClipboard + GlobalAlloc(GMEM_MOVEABLE) + SetClipboardData(CF_UNICODETEXT) 写 UTF-16LE
- 键盘:SendInput 发 Ctrl down → V down → V up → Ctrl up 序列
- 焦点:GetForegroundWindow == 0 时标 'no-focus',仍执行 paste(spec § 5.2)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task W3: Windows-only 单元测试(布局 + 编码)

**Files:**
- Create: `receiver/internal/inject/inject_windows_test.go`

只能在 Windows 上跑(build tag `//go:build windows`),不在 Mac CI 跑。但**Mac 端会跑跨编译验证 syntax 不挂**(Task W4)。

测试目标:
1. `kbdInput` 结构 size 一定是 40 字节(amd64 union slot)
2. UTF-16 编码中文字符串得到预期字节序列

### Step 1: 创建测试文件

`receiver/internal/inject/inject_windows_test.go`:

```go
//go:build windows

package inject

import (
	"bytes"
	"testing"
	"unicode/utf16"
	"unsafe"
)

func TestKbdInputSize(t *testing.T) {
	const expected = 40
	got := unsafe.Sizeof(kbdInput{})
	if got != expected {
		t.Errorf("kbdInput size = %d, want %d (mismatch will break SendInput)", got, expected)
	}
}

func TestUTF16EncodeNihao(t *testing.T) {
	u16 := utf16.Encode([]rune("你好"))
	// "你" = U+4F60, "好" = U+597D
	expected := []uint16{0x4F60, 0x597D}
	if len(u16) != len(expected) {
		t.Fatalf("len = %d, want %d", len(u16), len(expected))
	}
	for i := range expected {
		if u16[i] != expected[i] {
			t.Errorf("u16[%d] = 0x%04X, want 0x%04X", i, u16[i], expected[i])
		}
	}
}

func TestUTF16ByteOrderLE(t *testing.T) {
	u16 := utf16.Encode([]rune("AB"))
	// 'A' = 0x0041, 'B' = 0x0042
	// In memory (LE): 41 00 42 00
	buf := make([]byte, len(u16)*2)
	for i, v := range u16 {
		buf[i*2] = byte(v)
		buf[i*2+1] = byte(v >> 8)
	}
	expected := []byte{0x41, 0x00, 0x42, 0x00}
	if !bytes.Equal(buf, expected) {
		t.Errorf("LE bytes = % X, want % X", buf, expected)
	}
}
```

### Step 2: 验证跨编译不挂

```bash
cd receiver && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...
```

注意:`go build` 不编译 `_test.go`。要验证测试源代码也能编译,用:

```bash
cd receiver && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet ./...
```

或:

```bash
cd receiver && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go test -c ./internal/inject/ -o /tmp/inject_test_windows.exe && rm /tmp/inject_test_windows.exe
```

预期:无错误。

### Step 3: Commit

```bash
git add receiver/internal/inject/inject_windows_test.go
git commit -m "test(receiver): Windows Injector 单元测试

- kbdInput size 必须 40 字节(否则 SendInput 会写错偏移)
- UTF-16 编码中文 你好 验证
- UTF-16LE 字节顺序验证(剪贴板 CF_UNICODETEXT 要求 LE)

只在 Windows 跑;Mac 上跨编译验证 syntax 通过即可。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task W4: Makefile `test` target 加上跨编译验证

**Files:**
- Modify: `receiver/Makefile`

目标:`make test` 不仅跑 Mac 端单元测试,还跑 Windows 跨编译以保证未来改动不会偷偷把 windows build 干挂。

### Step 1: 修改 Makefile

把现有的 `test` target 改为:

```makefile
test:
	go test ./...
	@echo "--- verifying windows cross-compile ---"
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet ./...
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /dev/null ./cmd/type4me-receiver
	@echo "windows cross-compile OK"
```

### Step 2: 验证

```bash
cd receiver && make test
```

预期最后看到 `windows cross-compile OK`。如果 macOS 上 `go test ./...` 全过 + windows 跨编译过,这一步就算成功。

### Step 3: Commit

```bash
git add receiver/Makefile
git commit -m "build(receiver): make test 加上 windows cross-compile 验证

go vet + go build 都做,防止未来改动悄悄破坏 windows build。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task W5: Windows 真机手动 smoke 文档

**Files:**
- Create: `docs/windows-receiver-smoke.md`

S3 没法在 Mac 自动化验证真实行为(剪贴板写入、SendInput 触发)。给用户一份步骤化的手动 smoke 文档。

### Step 1: 写文档

`docs/windows-receiver-smoke.md`:

```markdown
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
```

### Step 2: Commit

```bash
git add docs/windows-receiver-smoke.md
git commit -m "docs(receiver): Windows 真机 smoke 步骤文档

Mac 端 cross-compile 不能验证真实行为,这份是给真 Windows 机器上
跑 receiver 时的 step-by-step。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## 完成判据(S3)

- [ ] `cd receiver && make test` 全过(macOS 端单元 + Windows 跨编译)
- [ ] `make build-windows` 产出 `dist/type4me-receiver-windows-amd64.exe`,可拷到 Windows 跑
- [ ] Windows 真机上,curl 直接打 `/inject` 收到 200 + 剪贴板写入 + Ctrl+V 触发(`docs/windows-receiver-smoke.md` 验收清单 1)
- [ ] Mac Type4Me dist + 远程桌面 → Windows 焦点框真实链路通(验收清单 2)
- [ ] 反向(本地 app 前台)Mac 端不路由到 Windows(验收清单 3)

---

## 备注

**spec 中明确不在 S3 做的**(留 S4 / S5):
- 系统托盘 UI(spec § 5.6)
- 接收端 Keychain/DPAPI token 持久化 — S3 复用 S0/S1 已有 config.json 路径
- 配对窗口图形界面(只是 stdout 打印 URL,跟 macOS 一致)
- Windows 上的 GetForegroundWindow → kAXTitleAttribute 类比的窗口标题路由

**已知简化**:
- 剪贴板 snapshot+restore 还没做(spec § 5.4 通用要求,S2 待补)。Windows receiver
  跟 macOS receiver 一样,inject 后用户剪贴板被覆盖。`inject_darwin.go` 已有
  TODO 注释指明 S2 补,Windows 同样情形请在 Task W2 的代码注释里也加一条。

实际写实现时不要遗漏这个 TODO 注释。
