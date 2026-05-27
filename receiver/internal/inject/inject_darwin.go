//go:build darwin

package inject

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework Carbon

#include <stdlib.h>

// Returns 1 on success, 0 on failure.
int t4m_set_clipboard(const char* utf8);

// Returns 1 on success, 0 on failure.
int t4m_simulate_paste(void);
*/
import "C"
import (
	"errors"
	"time"
	"unsafe"
)

type macInjector struct{}

// NewPlatform returns the macOS platform injector.
func NewPlatform() Injector { return &macInjector{} }

func (m *macInjector) Inject(text string) (Outcome, error) {
	if text == "" {
		return Outcome{Pasted: false, Reason: "empty"}, nil
	}
	cs := C.CString(text)
	defer C.free(unsafe.Pointer(cs))
	if C.t4m_set_clipboard(cs) != 1 {
		return Outcome{}, errors.New("set clipboard failed")
	}
	// Small delay so the focused app sees the clipboard change before paste.
	time.Sleep(20 * time.Millisecond)
	if C.t4m_simulate_paste() != 1 {
		return Outcome{Pasted: false, Reason: "paste-blocked"}, nil
	}
	return Outcome{Pasted: true}, nil
}

func (m *macInjector) Ping() error {
	// On macOS, NSPasteboard is always available within a Cocoa runtime; nothing to check.
	return nil
}
