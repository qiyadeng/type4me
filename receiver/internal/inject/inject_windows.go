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

	inputKeyboard  = 1
	keyEventfKeyUp = 0x0002

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
	procGlobalFree    = kernel32.NewProc("GlobalFree")
	procGlobalLock    = kernel32.NewProc("GlobalLock")
	procGlobalUnlock  = kernel32.NewProc("GlobalUnlock")
	procRtlMoveMemory = kernel32.NewProc("RtlMoveMemory")
)

// kbdInput is INPUT { type = INPUT_KEYBOARD; ki = KEYBDINPUT } on amd64.
// Total size 40 bytes — the MOUSEINPUT union slot is larger than KEYBDINPUT
// so we pad to match. The implicit Go pad between `time` and `extra` is
// 4 bytes (uintptr is 8-byte aligned on amd64).
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

// Inject sets the Windows clipboard to `text` (CF_UNICODETEXT, UTF-16LE)
// and synthesizes Ctrl+V to paste into the foreground window.
//
// TODO (S2): per spec § 5.4 we should snapshot the user's clipboard before
// paste and restore it ~150ms after the keystroke. Currently the previous
// clipboard contents are permanently overwritten. Tracked for S2 when the
// receiver also gains preserve_clipboard plumbing.
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
		// Ownership of hMem only transfers to the system on success. We
		// must GlobalFree it on failure to avoid a slow leak in this
		// long-running receiver daemon.
		procGlobalFree.Call(hMem)
		return errors.New("SetClipboardData failed")
	}
	return nil
}

// sendCtrlV synthesizes the keyboard sequence Ctrl down → V down → V up → Ctrl up
// via SendInput. Using SendInput (not keybd_event) is more IME-friendly and
// gives us atomic injection per the spec § 5.2.
func sendCtrlV() error {
	inputs := []kbdInput{
		{typ: inputKeyboard, vk: vkControl},                        // Ctrl down
		{typ: inputKeyboard, vk: vkV},                              // V    down
		{typ: inputKeyboard, vk: vkV, flags: keyEventfKeyUp},       // V    up
		{typ: inputKeyboard, vk: vkControl, flags: keyEventfKeyUp}, // Ctrl up
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
