MODULE     := github.com/tomowang/pigeoncli
BINARY     := pigeon
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE       := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -X $(MODULE)/internal/buildinfo.Version=$(VERSION) \
              -X $(MODULE)/internal/buildinfo.Commit=$(COMMIT) \
              -X $(MODULE)/internal/buildinfo.Date=$(DATE)

.PHONY: build run test vet lint tidy clean release-check release-dry-run hooks

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

# hooks points git at the repo's tracked .githooks/ dir (run once per clone) so
# `make lint` runs automatically before every commit.
hooks:
	git config core.hooksPath .githooks

clean:
	rm -rf bin/

# release-check/release-dry-run require goreleaser (https://goreleaser.com)
# on PATH; the release workflow itself only runs in CI on a pushed tag.
release-check:
	goreleaser check

release-dry-run:
	goreleaser release --snapshot --clean
