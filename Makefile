GO ?= go
BIN ?= syncd

.PHONY: build test tidy

build:
	CGO_ENABLED=0 $(GO) build -o $(BIN) ./cmd/syncd

test:
	CGO_ENABLED=0 $(GO) test ./...

tidy:
	$(GO) mod tidy
