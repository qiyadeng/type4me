package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qiyadeng/type4me/receiver/internal/inject"
)

func newTestServer(t *testing.T, inj inject.Injector, token string) (*httptest.Server, *Server) {
	t.Helper()
	s := New(Options{
		Token:    token,
		Injector: inj,
		Name:     "TestRX",
		Platform: "darwin",
		Version:  "test",
	})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, s
}

func TestPingNoAuth(t *testing.T) {
	ts, _ := newTestServer(t, &inject.Fake{}, "secret")
	resp, err := http.Get(ts.URL + "/ping")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestInjectMissingTokenReturns401(t *testing.T) {
	ts, _ := newTestServer(t, &inject.Fake{}, "secret")
	resp, err := http.Post(ts.URL+"/inject", "application/json",
		strings.NewReader(`{"text":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 401 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestInjectWrongTokenReturns401(t *testing.T) {
	ts, _ := newTestServer(t, &inject.Fake{}, "secret")
	req, _ := http.NewRequest("POST", ts.URL+"/inject",
		strings.NewReader(`{"text":"hi"}`))
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 401 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestInjectOKReturns200(t *testing.T) {
	fake := &inject.Fake{}
	ts, _ := newTestServer(t, fake, "secret")
	body, _ := json.Marshal(map[string]any{"text": "hello"})
	req, _ := http.NewRequest("POST", ts.URL+"/inject", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if len(fake.Calls) != 1 || fake.Calls[0] != "hello" {
		t.Errorf("inject not called as expected: %v", fake.Calls)
	}
}

func TestInjectTooLargeReturns413(t *testing.T) {
	ts, _ := newTestServer(t, &inject.Fake{}, "secret")
	body, _ := json.Marshal(map[string]any{"text": strings.Repeat("x", 9000)})
	req, _ := http.NewRequest("POST", ts.URL+"/inject", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 413 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

// Slow fake to test the 423 path: hold the mutex for longer than the next
// request will wait.
type slowFake struct {
	hold time.Duration
}

func (s *slowFake) Inject(text string) (inject.Outcome, error) {
	time.Sleep(s.hold)
	return inject.Outcome{Pasted: true}, nil
}
func (s *slowFake) Ping() error { return nil }

func TestInjectConcurrentReturns423(t *testing.T) {
	ts, _ := newTestServer(t, &slowFake{hold: 800 * time.Millisecond}, "secret")
	body := []byte(`{"text":"a"}`)
	mkReq := func() *http.Request {
		req, _ := http.NewRequest("POST", ts.URL+"/inject", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		return req
	}
	var wg sync.WaitGroup
	wg.Add(2)
	var statuses [2]int
	for i := 0; i < 2; i++ {
		go func(idx int) {
			defer wg.Done()
			resp, err := http.DefaultClient.Do(mkReq())
			if err != nil {
				t.Error(err)
				return
			}
			statuses[idx] = resp.StatusCode
		}(i)
		time.Sleep(50 * time.Millisecond) // ensure stable ordering
	}
	wg.Wait()
	saw423 := statuses[0] == 423 || statuses[1] == 423
	if !saw423 {
		t.Errorf("expected one 423 among %v", statuses)
	}
}
