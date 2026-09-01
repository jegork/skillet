BIN := skillet
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build test fmt lint install

build:
	go build $(LDFLAGS) -o $(BIN) ./cmd/skillet

test:
	go test ./...

fmt:
	gofmt -w .

lint:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt:"; echo "$$out"; exit 1; fi
	go vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run; fi

install:
	go build $(LDFLAGS) -o $(HOME)/.local/bin/$(BIN) ./cmd/skillet
