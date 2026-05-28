package relay

import (
	"strings"
	"testing"
)

func TestParseSSEStream(t *testing.T) {
	stream := "id: msg-1\nevent: inject\ndata: {\"text\":\"hello\"}\n\n" +
		": ping\n\n" +
		"id: msg-2\nevent: inject\ndata: {\"text\":\"world\"}\n\n"
	var events []SSEEvent
	err := ParseSSE(strings.NewReader(stream), func(e SSEEvent) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(events), events)
	}
	if events[0].ID != "msg-1" || events[0].Event != "inject" {
		t.Errorf("event 0: %+v", events[0])
	}
	if events[0].Data != `{"text":"hello"}` {
		t.Errorf("event 0 data: %q", events[0].Data)
	}
	if events[1].ID != "msg-2" {
		t.Errorf("event 1: %+v", events[1])
	}
}

func TestParseSSESkipsHeartbeatComments(t *testing.T) {
	stream := ": ping\n\n: another comment\n\nid: m\nevent: e\ndata: d\n\n"
	count := 0
	_ = ParseSSE(strings.NewReader(stream), func(e SSEEvent) error {
		count++
		return nil
	})
	if count != 1 {
		t.Errorf("got %d events, want 1", count)
	}
}

func TestParseSSEMultilineDataConcatenates(t *testing.T) {
	stream := "id: m\nevent: e\ndata: line1\ndata: line2\n\n"
	var got SSEEvent
	_ = ParseSSE(strings.NewReader(stream), func(e SSEEvent) error {
		got = e
		return nil
	})
	if got.Data != "line1\nline2" {
		t.Errorf("data = %q, want line1\\nline2", got.Data)
	}
}
