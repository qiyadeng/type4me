package server

import (
	"bytes"
	"strings"
	"testing"
)

type recordingFlusher struct {
	bytes.Buffer
	flushes int
}

func (r *recordingFlusher) Flush() { r.flushes++ }

func TestSSEWriteFrame(t *testing.T) {
	rf := &recordingFlusher{}
	w := &sseWriter{out: rf, flusher: rf}
	if err := w.frame("msg-1", "inject", `{"text":"hello"}`); err != nil {
		t.Fatal(err)
	}
	out := rf.String()
	if !strings.Contains(out, "id: msg-1\n") {
		t.Errorf("missing id: %q", out)
	}
	if !strings.Contains(out, "event: inject\n") {
		t.Errorf("missing event: %q", out)
	}
	if !strings.Contains(out, `data: {"text":"hello"}`+"\n") {
		t.Errorf("missing data: %q", out)
	}
	if !strings.HasSuffix(out, "\n\n") {
		t.Errorf("missing terminating blank line: %q", out)
	}
	if rf.flushes != 1 {
		t.Errorf("flushes = %d, want 1", rf.flushes)
	}
}

func TestSSEHeartbeat(t *testing.T) {
	rf := &recordingFlusher{}
	w := &sseWriter{out: rf, flusher: rf}
	if err := w.heartbeat(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rf.String(), ":") {
		t.Errorf("heartbeat should start with comment colon: %q", rf.String())
	}
	if !strings.HasSuffix(rf.String(), "\n\n") {
		t.Errorf("heartbeat missing terminator: %q", rf.String())
	}
}
