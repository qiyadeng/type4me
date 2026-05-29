.PHONY: build build-darwin build-darwin-arm64 build-darwin-amd64 build-windows build-windows-gui test clean

VERSION ?= 0.1.0
RELAY_URL ?= https://relay.example.com
DIST := dist

build: build-darwin

build-darwin: build-darwin-arm64 build-darwin-amd64

build-darwin-arm64:
	mkdir -p $(DIST)
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
		go build -ldflags "-X main.version=$(VERSION)" \
		-o $(DIST)/type4me-receiver-darwin-arm64 ./cmd/type4me-receiver

build-darwin-amd64:
	mkdir -p $(DIST)
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 \
		go build -ldflags "-X main.version=$(VERSION)" \
		-o $(DIST)/type4me-receiver-darwin-amd64 ./cmd/type4me-receiver

# Windows: cgo not used (pure syscall in inject_windows.go, added in S3).
build-windows:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
		go build -ldflags "-X main.version=$(VERSION)" \
		-o $(DIST)/type4me-receiver-windows-amd64.exe ./cmd/type4me-receiver

# Windows GUI (Fyne + tray). Requires CGO + a Windows C toolchain — typically
# built ON Windows, or via fyne-cross. -H windowsgui suppresses the console.
build-windows-gui:
	mkdir -p $(DIST)
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 \
		go build -tags gui \
		-ldflags "-H windowsgui -X main.version=$(VERSION) -X main.defaultRelayURL=$(RELAY_URL)" \
		-o $(DIST)/type4me-receiver-gui-windows-amd64.exe ./cmd/type4me-receiver-gui

test:
	go test ./...
	@echo "--- verifying windows cross-compile ---"
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet ./...
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /dev/null ./cmd/type4me-receiver
	@echo "windows cross-compile OK"

clean:
	rm -rf $(DIST)
