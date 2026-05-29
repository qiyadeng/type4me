package relay

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qiyadeng/type4me/receiver/internal/inject"
)

type mockInjector struct {
	mu    sync.Mutex
	calls []string
}

func (m *mockInjector) Inject(text string) (inject.Outcome, error) {
	m.mu.Lock()
	m.calls = append(m.calls, text)
	m.mu.Unlock()
	return inject.Outcome{Pasted: true}, nil
}
func (m *mockInjector) Ping() error { return nil }
func (m *mockInjector) Calls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.calls...)
}

func TestSubscriberReceivesAndInjects(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		f, _ := w.(http.Flusher)
		fmt.Fprintf(w, "id: msg-1\nevent: inject\ndata: {\"text\":\"hello\",\"request_id\":\"r1\"}\n\n")
		f.Flush()
		<-r.Context().Done()
	}))
	defer ts.Close()

	inj := &mockInjector{}
	s := &Subscriber{
		RelayURL:    ts.URL,
		DeviceToken: "tok",
		Injector:    inj,
		HTTPClient:  &http.Client{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.Run(ctx)

	calls := inj.Calls()
	if len(calls) != 1 || calls[0] != "hello" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestSubscriber401ReturnsImmediately(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer ts.Close()
	inj := &mockInjector{}
	s := &Subscriber{
		RelayURL: ts.URL, DeviceToken: "bad", Injector: inj,
		HTTPClient: &http.Client{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := s.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 error, got %v", err)
	}
}

func TestSubscriberReconnectsOnDisconnect(t *testing.T) {
	var connectCount int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&connectCount, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		f, _ := w.(http.Flusher)
		fmt.Fprintf(w, "id: msg-%d\nevent: inject\ndata: {\"text\":\"hello-%d\"}\n\n", count, count)
		f.Flush()
		if count == 1 {
			return
		}
		<-r.Context().Done()
	}))
	defer ts.Close()

	inj := &mockInjector{}
	s := &Subscriber{
		RelayURL: ts.URL, DeviceToken: "tok", Injector: inj,
		HTTPClient:   &http.Client{},
		ReconnectMin: 50 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.Run(ctx)

	calls := inj.Calls()
	if len(calls) < 2 {
		t.Errorf("expected at least 2 calls (reconnect), got %d", len(calls))
	}
}

func TestSubscriberSendsLastEventIDOnReconnect(t *testing.T) {
	var lastIDReceived string
	var mu sync.Mutex
	first := true
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if !first {
			lastIDReceived = r.Header.Get("Last-Event-ID")
		}
		isFirst := first
		first = false
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		f, _ := w.(http.Flusher)
		if isFirst {
			fmt.Fprintf(w, "id: msg-abc\nevent: inject\ndata: {\"text\":\"x\"}\n\n")
			f.Flush()
			return
		}
		<-r.Context().Done()
	}))
	defer ts.Close()

	s := &Subscriber{
		RelayURL: ts.URL, DeviceToken: "tok", Injector: &mockInjector{},
		HTTPClient:   &http.Client{},
		ReconnectMin: 50 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.Run(ctx)

	mu.Lock()
	defer mu.Unlock()
	if lastIDReceived != "msg-abc" {
		t.Errorf("Last-Event-ID on reconnect = %q, want msg-abc", lastIDReceived)
	}
}

func TestSubscriberIgnoresBadJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		f, _ := w.(http.Flusher)
		fmt.Fprintf(w, "id: m1\nevent: inject\ndata: NOT-JSON\n\n")
		f.Flush()
		fmt.Fprintf(w, "id: m2\nevent: inject\ndata: {\"text\":\"good\"}\n\n")
		f.Flush()
		<-r.Context().Done()
	}))
	defer ts.Close()

	inj := &mockInjector{}
	s := &Subscriber{
		RelayURL: ts.URL, DeviceToken: "tok", Injector: inj,
		HTTPClient: &http.Client{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.Run(ctx)

	calls := inj.Calls()
	if len(calls) != 1 || calls[0] != "good" {
		t.Errorf("calls = %+v, expected only 'good'", calls)
	}
}

func TestSubscriberOnStatusLifecycle(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	defer ts.Close()

	var mu sync.Mutex
	var got []Status
	s := &Subscriber{
		RelayURL: ts.URL, DeviceToken: "tok", Injector: &mockInjector{},
		HTTPClient: &http.Client{},
		OnStatus: func(st Status, err error) {
			mu.Lock()
			got = append(got, st)
			mu.Unlock()
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = s.Run(ctx)

	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 || got[0] != StatusConnecting {
		t.Fatalf("statuses = %+v, want first=connecting", got)
	}
	connected := false
	for _, st := range got {
		if st == StatusConnected {
			connected = true
		}
	}
	if !connected {
		t.Errorf("expected a connected status; got %+v", got)
	}
}

func TestSubscriberOnStatusErrorOn401(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer ts.Close()
	var mu sync.Mutex
	var got []Status
	s := &Subscriber{
		RelayURL: ts.URL, DeviceToken: "bad", Injector: &mockInjector{},
		HTTPClient: &http.Client{},
		OnStatus: func(st Status, err error) {
			mu.Lock()
			got = append(got, st)
			mu.Unlock()
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.Run(ctx)
	mu.Lock()
	defer mu.Unlock()
	sawError := false
	for _, st := range got {
		if st == StatusError {
			sawError = true
		}
	}
	if !sawError {
		t.Errorf("expected error status on 401; got %+v", got)
	}
}
