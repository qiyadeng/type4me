package inject

import "sync"

// Fake is an in-memory Injector for tests.
type Fake struct {
	mu         sync.Mutex
	Calls      []string
	NextReason string // if set, the next Inject returns Pasted=false with this reason
	NextError  error
}

func (f *Fake) Inject(text string) (Outcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, text)
	if f.NextError != nil {
		err := f.NextError
		f.NextError = nil
		return Outcome{}, err
	}
	if f.NextReason != "" {
		r := f.NextReason
		f.NextReason = ""
		return Outcome{Pasted: false, Reason: r}, nil
	}
	return Outcome{Pasted: true}, nil
}

func (f *Fake) Ping() error { return nil }
