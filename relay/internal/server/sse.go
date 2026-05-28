package server

import (
	"fmt"
	"io"
	"net/http"
)

type sseWriter struct {
	out     io.Writer
	flusher http.Flusher
}

func newSSEWriter(w http.ResponseWriter) *sseWriter {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	f, _ := w.(http.Flusher)
	return &sseWriter{out: w, flusher: f}
}

func (s *sseWriter) frame(id, event, data string) error {
	if _, err := fmt.Fprintf(s.out, "id: %s\nevent: %s\ndata: %s\n\n", id, event, data); err != nil {
		return err
	}
	s.flushIf()
	return nil
}

func (s *sseWriter) heartbeat() error {
	if _, err := fmt.Fprintf(s.out, ": ping\n\n"); err != nil {
		return err
	}
	s.flushIf()
	return nil
}

func (s *sseWriter) flushIf() {
	if s.flusher != nil {
		s.flusher.Flush()
	}
}
