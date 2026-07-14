.PHONY: build release-assets run test test-e2e test-browser test-browser-matrix test-browser-network-boundary test-browser-artifacts test-benchmarks clean prepare-cache prepare-playwright install uninstall restart

SERVER_BINARY := bin/control-agents-server
CLIENT_BINARY := bin/control-agents
DIST_DIR ?= dist
RELEASE_ASSET_NAMES := \
	control-agents-server-linux-amd64 \
	control-agents-linux-amd64 \
	control-agents-server-linux-arm64 \
	control-agents-linux-arm64 \
	sha256sums.txt
RELEASE_ASSETS := $(addprefix $(DIST_DIR)/,$(RELEASE_ASSET_NAMES))
VERSION_PKG := control-agents/internal/version
VERSION ?= $(shell git describe --tags --dirty --always --match 'v[0-9]*' 2>/dev/null | sed 's/^v//' || printf dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDVARS := -X $(VERSION_PKG).Version=$(VERSION) -X $(VERSION_PKG).Commit=$(COMMIT) -X $(VERSION_PKG).BuildDate=$(BUILD_DATE)
SERVER_INSTALL ?= $(HOME)/.local/bin/control-agents-server
CLIENT_INSTALL ?= $(HOME)/.local/bin/control-agents
XDG_CONFIG_HOME ?= $(HOME)/.config
SYSTEMD_USER_DIR ?= $(XDG_CONFIG_HOME)/systemd/user
APP_CONFIG_DIR ?= $(XDG_CONFIG_HOME)/control-agents
ENV_FILE ?= $(APP_CONFIG_DIR)/env
SERVICE_UNIT ?= control-agents.service
SYSTEMCTL ?= systemctl
INSTALL ?= install
GOCACHE ?= $(CURDIR)/.cache/go-build
GOTMPDIR ?= $(CURDIR)/.cache/go-tmp
TMPDIR ?= $(CURDIR)/.cache/tmp
TMUX_TMPDIR ?= $(CURDIR)/.cache/tmux
CGO_ENABLED ?= 0
GOFLAGS ?= -buildvcs=false
PLAYWRIGHT_PROFILE_RUNNER := node test/playwright/run_profile.js
CONTROL_AGENTS_UTF8_LOCALE ?= C.UTF-8
E2E_TERM ?= xterm-256color

export GOCACHE
export GOTMPDIR
export TMPDIR
export TMUX_TMPDIR
export CGO_ENABLED
export GOFLAGS
export LANG := $(CONTROL_AGENTS_UTF8_LOCALE)
export LC_ALL := $(CONTROL_AGENTS_UTF8_LOCALE)

prepare-cache:
	mkdir -p $(GOCACHE) $(GOTMPDIR) $(TMPDIR) $(TMUX_TMPDIR)
	chmod 700 $(TMUX_TMPDIR)

prepare-playwright: prepare-cache
	rm -rf test-results playwright-report

build: prepare-cache
	go build -ldflags "$(LDVARS)" -o $(SERVER_BINARY) ./cmd/server
	go build -ldflags "$(LDVARS)" -o $(CLIENT_BINARY) ./cmd/client

release-assets: prepare-cache
	mkdir -p "$(DIST_DIR)"
	rm -f $(RELEASE_ASSETS)
	@set -eu; \
	for arch in amd64 arm64; do \
		GOOS=linux GOARCH="$$arch" CGO_ENABLED=0 go build -ldflags "$(LDVARS)" -o "$(DIST_DIR)/control-agents-server-linux-$$arch" ./cmd/server; \
		GOOS=linux GOARCH="$$arch" CGO_ENABLED=0 go build -ldflags "$(LDVARS)" -o "$(DIST_DIR)/control-agents-linux-$$arch" ./cmd/client; \
	done
	@cd "$(DIST_DIR)" && sha256sum control-agents-server-linux-amd64 control-agents-linux-amd64 control-agents-server-linux-arm64 control-agents-linux-arm64 > sha256sums.txt

run: prepare-cache
	go run -ldflags "$(LDVARS)" ./cmd/server

test: prepare-cache
	go test ./...

test-e2e: build
	TERM=$(E2E_TERM) RUN_E2E=1 go test -count=1 ./test/e2e

test-browser-network-boundary: prepare-playwright
	node --test test/playwright/network_boundary_test.js
	$(PLAYWRIGHT_PROFILE_RUNNER) --network-boundary-probe

test-browser: build test-browser-network-boundary
	$(PLAYWRIGHT_PROFILE_RUNNER) --project=chromium --grep-invert '@benchmark|@isolated'
	$(PLAYWRIGHT_PROFILE_RUNNER) --project=chromium --grep '@isolated-lifecycle'
	$(PLAYWRIGHT_PROFILE_RUNNER) --project=chromium --grep '@isolated-secondary-lifecycle'
	$(PLAYWRIGHT_PROFILE_RUNNER) --project=chromium --grep '@isolated-mobile'
	$(PLAYWRIGHT_PROFILE_RUNNER) --project=chromium --grep '@isolated-network-failure'
	$(MAKE) test-browser-artifacts

test-browser-matrix: build test-browser-network-boundary
	$(PLAYWRIGHT_PROFILE_RUNNER) --grep-invert '@benchmark|@isolated'
	$(PLAYWRIGHT_PROFILE_RUNNER) --grep '@isolated-lifecycle'
	$(PLAYWRIGHT_PROFILE_RUNNER) --grep '@isolated-secondary-lifecycle'
	$(PLAYWRIGHT_PROFILE_RUNNER) --grep '@isolated-mobile'
	$(PLAYWRIGHT_PROFILE_RUNNER) --grep '@isolated-network-failure'
	$(MAKE) test-browser-artifacts

test-browser-artifacts: prepare-playwright
	node test/playwright/validate_failure_artifacts.js

test-benchmarks: build test-browser-network-boundary
	mkdir -p .cache/benchmarks
	CONTROL_AGENTS_BENCHMARK_REPORT="$(CURDIR)/.cache/benchmarks/server-report.json" go test -count=1 -run '^TestHistoryBenchmarkReport$$' ./internal/server
	$(PLAYWRIGHT_PROFILE_RUNNER) --project=chromium --grep '@benchmark'
	node test/benchmarks/validate_reports.js

install: build
	$(INSTALL) -d $(dir $(SERVER_INSTALL)) $(dir $(CLIENT_INSTALL)) $(SYSTEMD_USER_DIR) $(APP_CONFIG_DIR)
	@if [ ! -f "$(ENV_FILE)" ]; then \
		umask 077; \
		password="$$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')"; \
		printf '%s\n' \
			"CONTROL_AGENTS_PASSWORD=$$password" \
			'CONTROL_AGENTS_BIND_ADDR=0.0.0.0' \
			'CONTROL_AGENTS_PORT=8080' > "$(ENV_FILE)"; \
		printf '%s\n' "Created $(ENV_FILE) with a generated CONTROL_AGENTS_PASSWORD."; \
	fi
	$(INSTALL) -m 0755 $(SERVER_BINARY) $(SERVER_INSTALL)
	$(INSTALL) -m 0755 $(CLIENT_BINARY) "$(CLIENT_INSTALL)"
	@printf '%s\n' "Installed $(CLIENT_INSTALL)."
	$(INSTALL) -m 0644 systemd/user/$(SERVICE_UNIT) $(SYSTEMD_USER_DIR)/$(SERVICE_UNIT)
	$(SYSTEMCTL) --user daemon-reload

restart:
	$(SYSTEMCTL) --user restart $(SERVICE_UNIT)

uninstall:
	$(SYSTEMCTL) --user disable --now $(SERVICE_UNIT) >/dev/null 2>&1 || true
	rm -f $(SYSTEMD_USER_DIR)/$(SERVICE_UNIT) $(SERVER_INSTALL) $(CLIENT_INSTALL)
	$(SYSTEMCTL) --user daemon-reload

clean:
	rm -f $(SERVER_BINARY) $(CLIENT_BINARY)
	rm -f $(RELEASE_ASSETS)
	rmdir "$(DIST_DIR)" >/dev/null 2>&1 || true
