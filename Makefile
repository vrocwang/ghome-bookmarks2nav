BINARY  := bookmarks-to-nav
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD   := CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: help build test race vet fmt check cross clean

help:
	@echo "make build  - build ./$(BINARY) for this machine"
	@echo "make test   - go test ./..."
	@echo "make race   - go test -race ./..."
	@echo "make check  - fmt + vet + test"
	@echo "make cross  - static binaries for linux/darwin into dist/"
	@echo ""
	@echo "Then: ./$(BINARY) ~/Downloads/bookmarks.html > nav.yaml"

build:
	$(BUILD) -o $(BINARY) .

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

check: fmt vet test

cross:
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 $(BUILD) -o dist/$(BINARY)-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux  GOARCH=arm64 $(BUILD) -o dist/$(BINARY)-linux-arm64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(BUILD) -o dist/$(BINARY)-darwin-arm64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(BUILD) -o dist/$(BINARY)-darwin-amd64 .
	@ls -lh dist

clean:
	rm -rf $(BINARY) dist
