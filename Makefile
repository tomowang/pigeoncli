MODULE     := github.com/tomowang/pigeoncli
BINARY     := pigeon
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE       := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -X $(MODULE)/internal/buildinfo.Version=$(VERSION) \
              -X $(MODULE)/internal/buildinfo.Commit=$(COMMIT) \
              -X $(MODULE)/internal/buildinfo.Date=$(DATE)

.PHONY: build run test vet lint tidy clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/pigeon

run:
	go run ./cmd/pigeon

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run

tidy:
	go mod tidy

clean:
	rm -rf bin/
