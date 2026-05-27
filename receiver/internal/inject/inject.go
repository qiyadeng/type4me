package inject

// Outcome describes what happened during an inject attempt.
type Outcome struct {
	Pasted bool   `json:"pasted"`
	Reason string `json:"reason,omitempty"` // "" | "no-focus" | "clipboard-locked" | "paste-blocked"
}

// Injector is the platform-abstraction for putting `text` into the current
// foreground application's focused control.
type Injector interface {
	// Inject sets the clipboard to `text` and simulates a paste keystroke.
	// Returns Outcome (never partial); err is reserved for internal API failures.
	Inject(text string) (Outcome, error)
	// Ping returns nil if the platform API is available.
	Ping() error
}
