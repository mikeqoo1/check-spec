.PHONY: build test test-race lint fmt verify clean install help

BIN     := check-spec
PKG     := github.com/mikeqoo1/check-spec
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X '$(PKG)/internal/version.Version=$(VERSION)' \
	-X '$(PKG)/internal/version.Commit=$(COMMIT)' \
	-X '$(PKG)/internal/version.Date=$(DATE)'

help:
	@echo "Targets:"
	@echo "  build      - build $(BIN) binary"
	@echo "  test       - run unit tests"
	@echo "  test-race  - run tests with race detector"
	@echo "  lint       - run golangci-lint"
	@echo "  fmt        - run gofmt + goimports"
	@echo "  verify     - lint + test-race + build"
	@echo "  install    - go install to GOBIN"
	@echo "  clean      - remove built artifacts"

build:
	go build -ldflags="$(LDFLAGS)" -o $(BIN) ./cmd/check-spec

install:
	go install -ldflags="$(LDFLAGS)" ./cmd/check-spec

test:
	go test ./... -count=1

test-race:
	go test ./... -race -count=1

lint:
	golangci-lint run

fmt:
	gofmt -s -w .
	@command -v goimports >/dev/null 2>&1 && goimports -w -local $(PKG) . || true

verify: lint test-race build

clean:
	rm -f $(BIN)
	rm -rf dist/
