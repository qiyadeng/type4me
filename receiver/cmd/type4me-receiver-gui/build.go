//go:build gui

package main

// Build-time overridable values for the GUI binary. Override the relay URL with:
//   go build -tags gui -ldflags "-X main.defaultRelayURL=https://your-relay"
var (
	version         = "dev"
	defaultRelayURL = "https://relay.example.com"
)
