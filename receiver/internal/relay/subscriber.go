package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/qiyadeng/type4me/receiver/internal/inject"
)

// Subscriber connects to a relay's /v1/subscribe SSE endpoint, parses incoming
// messages, and forwards them to the local Injector.
//
// Reconnects automatically on disconnect with exponential backoff capped at 30s.
// A 401 from the relay terminates the loop (no auto-reconnect on auth failure).
type Subscriber struct {
	RelayURL    string
	DeviceToken string
	Injector    inject.Injector
	HTTPClient  *http.Client

	// ReconnectMin defaults to 1s if zero. Backoff doubles on each failure,
	// capped at 30s.
	ReconnectMin time.Duration
}

type sseInjectPayload struct {
	Text              string `json:"text"`
	RequestID         string `json:"request_id"`
	FromDevice        string `json:"from_device"`
	PreserveClipboard bool   `json:"preserve_clipboard"`
}

// Run blocks until ctx is canceled or a fatal error (401) occurs.
func (s *Subscriber) Run(ctx context.Context) error {
	min := s.ReconnectMin
	if min == 0 {
		min = time.Second
	}
	backoff := min
	var lastEventID string

	for {
		if ctx.Err() != nil {
			return nil
		}
		err := s.connectAndStream(ctx, &lastEventID)
		if errors.Is(err, errAuth) {
			return fmt.Errorf("subscribe: %w", err)
		}
		if ctx.Err() != nil {
			return nil
		}
		log.Printf("subscribe: %v; reconnecting in %s", err, backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

var errAuth = errors.New("HTTP 401 from relay")

func (s *Subscriber) connectAndStream(ctx context.Context, lastID *string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", s.RelayURL+"/v1/subscribe", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.DeviceToken)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if *lastID != "" {
		req.Header.Set("Last-Event-ID", *lastID)
	}

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return errAuth
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return ParseSSE(resp.Body, func(ev SSEEvent) error {
		if ev.Event != "inject" {
			return nil
		}
		var payload sseInjectPayload
		if err := json.Unmarshal([]byte(ev.Data), &payload); err != nil {
			log.Printf("subscribe: bad data: %v", err)
			return nil
		}
		out, ierr := s.Injector.Inject(payload.Text)
		if ierr != nil {
			log.Printf("inject err: %v (event %s)", ierr, ev.ID)
		} else {
			log.Printf("inject ok=%v reason=%q text-len=%d req=%s event=%s",
				out.Pasted, out.Reason, len(payload.Text), payload.RequestID, ev.ID)
		}
		*lastID = ev.ID
		return nil
	})
}
