.PHONY: build build-linux build-darwin build-windows test clean

VERSION ?= 0.1.0
DIST := dist

build: build-darwin

build-linux:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -ldflags "-X main.version=$(VERSION)" \
		-o $(DIST)/type4me-relay-linux-amd64 ./cmd/type4me-relay

build-darwin:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 \
		go build -ldflags "-X main.version=$(VERSION)" \
		-o $(DIST)/type4me-relay-darwin-arm64 ./cmd/type4me-relay

build-windows:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
		go build -ldflags "-X main.version=$(VERSION)" \
		-o $(DIST)/type4me-relay-windows-amd64.exe ./cmd/type4me-relay

test:
	go test ./...
	@echo "--- verifying linux cross-compile ---"
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go vet ./...
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/type4me-relay
	@echo "linux cross-compile OK"

clean:
	rm -rf $(DIST)
