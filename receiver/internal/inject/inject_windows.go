//go:build windows

package inject

import "errors"

type winInjector struct{}

// NewPlatform returns the Windows platform injector.
func NewPlatform() Injector { return &winInjector{} }

func (w *winInjector) Inject(text string) (Outcome, error) {
	if text == "" {
		return Outcome{Pasted: false, Reason: "empty"}, nil
	}
	return Outcome{}, errors.New("not implemented")
}

func (w *winInjector) Ping() error { return nil }
