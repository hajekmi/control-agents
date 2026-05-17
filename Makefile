.PHONY: build run test test-e2e test-browser clean prepare-cache install uninstall restart

SERVER_BINARY := bin/control-agents-server
CLIENT_BINARY := bin/control-agents
VERSION_PKG := terminal-mirror/internal/version
VERSION ?= $(shell git describe --tags --dirty --always --match 'v[0-9]*' 2>/dev/null | sed 's/^v//' || printf dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDVARS := -X $(VERSION_PKG).Version=$(VERSION) -X $(VERSION_PKG).Commit=$(COMMIT) -X $(VERSION_PKG).BuildDate=$(BUILD_DATE)
SERVER_INSTALL ?= $(HOME)/.local/bin/control-agents-server
CLIENT_INSTALL ?= /usr/local/bin/control-agents
XDG_CONFIG_HOME ?= $(HOME)/.config
SYSTEMD_USER_DIR ?= $(XDG_CONFIG_HOME)/systemd/user
APP_CONFIG_DIR ?= $(XDG_CONFIG_HOME)/terminal-mirror
ENV_FILE ?= $(APP_CONFIG_DIR)/env
SERVICE_UNIT ?= control-agents.service
SYSTEMCTL ?= systemctl
INSTALL ?= install
SUDO ?= sudo
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
	go build -ldflags "$(LDVARS)" -o $(SERVER_BINARY) ./cmd/server

run: prepare-cache
	go run -ldflags "$(LDVARS)" ./cmd/server

test: prepare-cache
	go test ./...

test-e2e: prepare-cache
	RUN_E2E=1 go test -count=1 ./test/e2e

test-browser: prepare-cache
	npx playwright test

install: build
	$(INSTALL) -d $(dir $(SERVER_INSTALL)) $(SYSTEMD_USER_DIR) $(APP_CONFIG_DIR)
	@if [ ! -f "$(ENV_FILE)" ]; then \
		umask 077; \
		password="$$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')"; \
		printf '%s\n' \
			"MIRROR_PASSWORD=$$password" \
			'MIRROR_BIND_ADDR=0.0.0.0' \
			'MIRROR_PORT=8080' > "$(ENV_FILE)"; \
		printf '%s\n' "Created $(ENV_FILE) with a generated MIRROR_PASSWORD."; \
	fi
	$(INSTALL) -m 0755 $(SERVER_BINARY) $(SERVER_INSTALL)
	@if [ -d "$(dir $(CLIENT_INSTALL))" ] && [ -w "$(dir $(CLIENT_INSTALL))" ]; then \
		$(INSTALL) -m 0755 $(CLIENT_BINARY) "$(CLIENT_INSTALL)"; \
	else \
		$(SUDO) $(INSTALL) -d "$(dir $(CLIENT_INSTALL))"; \
		$(SUDO) $(INSTALL) -m 0755 $(CLIENT_BINARY) "$(CLIENT_INSTALL)"; \
	fi
	@printf '%s\n' "Installed $(CLIENT_INSTALL)."
	$(INSTALL) -m 0644 systemd/user/$(SERVICE_UNIT) $(SYSTEMD_USER_DIR)/$(SERVICE_UNIT)
	$(SYSTEMCTL) --user daemon-reload

restart:
	$(SYSTEMCTL) --user restart $(SERVICE_UNIT)

uninstall:
	$(SYSTEMCTL) --user disable --now $(SERVICE_UNIT) >/dev/null 2>&1 || true
	rm -f $(SYSTEMD_USER_DIR)/$(SERVICE_UNIT) $(SERVER_INSTALL)
	@if [ -w "$(dir $(CLIENT_INSTALL))" ]; then \
		rm -f "$(CLIENT_INSTALL)"; \
	else \
		$(SUDO) rm -f "$(CLIENT_INSTALL)"; \
	fi
	$(SYSTEMCTL) --user daemon-reload

clean:
	rm -f $(SERVER_BINARY)
