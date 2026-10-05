VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null)
LDFLAGS = -X main.version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o trinity-installer ./cmd/trinity-installer

test:
	go test ./...

.PHONY: build test
