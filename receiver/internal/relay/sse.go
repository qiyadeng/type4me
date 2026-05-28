package relay

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

type SSEEvent struct {
	ID    string
	Event string
	Data  string
}

// ParseSSE reads SSE-formatted events from r and invokes onEvent for each
// complete event. Comments (lines starting with ":") are skipped.
// Returns io.EOF or another error when the stream ends.
//
// If onEvent returns a non-nil error, parsing stops with that error.
func ParseSSE(r io.Reader, onEvent func(SSEEvent) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 32*1024), 1024*1024)
	var cur SSEEvent
	var dataLines []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if cur.ID != "" || cur.Event != "" || len(dataLines) > 0 {
				cur.Data = strings.Join(dataLines, "\n")
				if err := onEvent(cur); err != nil {
					return err
				}
			}
			cur = SSEEvent{}
			dataLines = nil
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "id:"):
			cur.ID = strings.TrimSpace(line[3:])
		case strings.HasPrefix(line, "event:"):
			cur.Event = strings.TrimSpace(line[6:])
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(line[5:]))
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
