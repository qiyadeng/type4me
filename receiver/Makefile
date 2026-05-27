.PHONY: build build-darwin build-darwin-arm64 build-darwin-amd64 build-windows test clean

VERSION ?= 0.1.0
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

test:
	go test ./...

clean:
	rm -rf $(DIST)
