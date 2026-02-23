BINARY      = memvra
MODULE      = github.com/memvra/memvra
BUILD_DIR   = dist
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE       ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS     = -s -w \
              -X main.version=$(VERSION) \
              -X main.commit=$(COMMIT) \
              -X main.date=$(DATE)

.PHONY: all build install test lint fmt vet clean release snapshot tidy

all: build

## build: Compile the binary for the current OS/arch.
build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/memvra

## install: Install memvra to $GOPATH/bin.
install:
	CGO_ENABLED=1 go install -ldflags="$(LDFLAGS)" ./cmd/memvra

## test: Run all tests.
test:
	CGO_ENABLED=1 go test -v ./...

## lint: Run golangci-lint.
lint:
	golangci-lint run ./...

## fmt: Format all Go source files.
fmt:
	gofmt -w -s .

## vet: Run go vet.
vet:
	go vet ./...

## tidy: Tidy go.mod and go.sum.
tidy:
	go mod tidy

## clean: Remove build artifacts.
clean:
	rm -rf $(BUILD_DIR)

## release: Run GoReleaser for a full release.
release:
	goreleaser release --clean

## snapshot: Build a local snapshot with GoReleaser (no publish).
snapshot:
	goreleaser release --snapshot --clean

## help: Print this help message.
help:
	@echo "Usage: make <target>"
	@sed -n 's/^## //p' $(MAKEFILE_LIST)
