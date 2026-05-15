.PHONY: build run test test-e2e clean prepare-cache

BINARY := bin/server
GOCACHE ?= $(CURDIR)/.cache/go-build
GOTMPDIR ?= $(CURDIR)/.cache/go-tmp
TMPDIR ?= $(CURDIR)/.cache/tmp
TMUX_TMPDIR ?= $(CURDIR)/.cache/tmux
CGO_ENABLED ?= 0
GOFLAGS ?= -buildvcs=false

export GOCACHE
export GOTMPDIR
export TMPDIR
export TMUX_TMPDIR
export CGO_ENABLED
export GOFLAGS

prepare-cache:
	mkdir -p $(GOCACHE) $(GOTMPDIR) $(TMPDIR) $(TMUX_TMPDIR)
	chmod 700 $(TMUX_TMPDIR)

build: prepare-cache
	go build -o $(BINARY) ./cmd/server

run: prepare-cache
	go run ./cmd/server

test: prepare-cache
	go test ./...

test-e2e: prepare-cache
	RUN_E2E=1 go test -count=1 ./test/e2e

clean:
	rm -f $(BINARY)
