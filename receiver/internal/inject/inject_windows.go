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

	// Modifier VKs we proactively release before paste — see sendCtrlV.
	vkLShift   = 0xA0
	vkRShift   = 0xA1
	vkLControl = 0xA2
	vkRControl = 0xA3
	vkLMenu    = 0xA4 // Left Alt
	vkRMenu    = 0xA5 // Right Alt / AltGr
	vkLWin     = 0x5B
	vkRWin     = 0x5C
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
//
// Before the paste, we issue key-up events for every common modifier (Shift /
// Ctrl / Alt / Win, both left and right). This guards against the remote-
// desktop case where the host machine (e.g. Mac running ToDesk) forwards
// modifier-down events into Windows but the matching up-event is lost or
// raced — leaving the Windows kernel believing a modifier is still held,
// which combines with our synthetic Ctrl+V to produce Ctrl+Alt+V (paste
// special), Ctrl+Shift+V (other shortcut) or even random characters when
// the focused app's IME interprets the stuck-modifier state oddly.
//
// SendInput with KeyUp for a key that's not currently down is a no-op, so
// flushing modifiers does not disturb the user's normal typing on Windows
// — it only matters in the bug case where state was already corrupted.
func sendCtrlV() error {
	// Phase 1: force-release every modifier.
	modifiers := []uint16{
		vkLShift, vkRShift,
		vkLControl, vkRControl, vkControl,
		vkLMenu, vkRMenu,
		vkLWin, vkRWin,
	}
	flush := make([]kbdInput, 0, len(modifiers))
	for _, m := range modifiers {
		flush = append(flush, kbdInput{typ: inputKeyboard, vk: m, flags: keyEventfKeyUp})
	}
	procSendInput.Call(
		uintptr(len(flush)),
		uintptr(unsafe.Pointer(&flush[0])),
		unsafe.Sizeof(flush[0]),
	)

	// Phase 2: actual Ctrl+V.
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

	// Phase 3: defensive — re-flush modifiers after paste in case any timer/
	// RDP event leaked a down-event during the Ctrl+V window. Cheap insurance.
	procSendInput.Call(
		uintptr(len(flush)),
		uintptr(unsafe.Pointer(&flush[0])),
		unsafe.Sizeof(flush[0]),
	)

	return nil
}
