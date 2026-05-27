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
