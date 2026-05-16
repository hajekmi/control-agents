.PHONY: build run test test-e2e clean prepare-cache install uninstall restart

BINARY := bin/control-agents
CLIENT_MIRROR := bin/client_mirror
VERSION_PKG := terminal-mirror/internal/version
VERSION ?= $(shell git describe --tags --dirty --always --match 'v[0-9]*' 2>/dev/null | sed 's/^v//' || printf dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDVARS := -X $(VERSION_PKG).Version=$(VERSION) -X $(VERSION_PKG).Commit=$(COMMIT) -X $(VERSION_PKG).BuildDate=$(BUILD_DATE)
INSTALL_BINARY ?= $(HOME)/.local/bin/control-agents
CLIENT_MIRROR_INSTALL ?= /usr/local/bin/client_mirror
XDG_CONFIG_HOME ?= $(HOME)/.config
SYSTEMD_USER_DIR ?= $(XDG_CONFIG_HOME)/systemd/user
APP_CONFIG_DIR ?= $(XDG_CONFIG_HOME)/terminal-mirror
ENV_FILE ?= $(APP_CONFIG_DIR)/env
SERVICE_UNIT ?= control-agents.service
LEGACY_SERVICE_UNIT ?= server.service
LEGACY_INSTALL_BINARY ?= $(dir $(INSTALL_BINARY))server
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
	go build -ldflags "$(LDVARS)" -o $(BINARY) ./cmd/server

run: prepare-cache
	go run -ldflags "$(LDVARS)" ./cmd/server

test: prepare-cache
	go test ./...

test-e2e: prepare-cache
	RUN_E2E=1 go test -count=1 ./test/e2e

install: build
	$(INSTALL) -d $(dir $(INSTALL_BINARY)) $(SYSTEMD_USER_DIR) $(APP_CONFIG_DIR)
	@if [ ! -f "$(ENV_FILE)" ]; then \
		umask 077; \
		password="$$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')"; \
		printf '%s\n' \
			"MIRROR_PASSWORD=$$password" \
			'MIRROR_BIND_ADDR=0.0.0.0' \
			'MIRROR_PORT=8080' > "$(ENV_FILE)"; \
		printf '%s\n' "Created $(ENV_FILE) with a generated MIRROR_PASSWORD."; \
	fi
	$(INSTALL) -m 0755 $(BINARY) $(INSTALL_BINARY)
	@if [ -d "$(dir $(CLIENT_MIRROR_INSTALL))" ] && [ -w "$(dir $(CLIENT_MIRROR_INSTALL))" ]; then \
		$(INSTALL) -m 0755 $(CLIENT_MIRROR) "$(CLIENT_MIRROR_INSTALL)"; \
	else \
		$(SUDO) $(INSTALL) -d "$(dir $(CLIENT_MIRROR_INSTALL))"; \
		$(SUDO) $(INSTALL) -m 0755 $(CLIENT_MIRROR) "$(CLIENT_MIRROR_INSTALL)"; \
	fi
	@printf '%s\n' "Installed $(CLIENT_MIRROR_INSTALL)."
	$(INSTALL) -m 0644 systemd/user/$(SERVICE_UNIT) $(SYSTEMD_USER_DIR)/$(SERVICE_UNIT)
	$(SYSTEMCTL) --user disable --now $(LEGACY_SERVICE_UNIT) >/dev/null 2>&1 || true
	rm -f $(SYSTEMD_USER_DIR)/$(LEGACY_SERVICE_UNIT) $(LEGACY_INSTALL_BINARY)
	$(SYSTEMCTL) --user daemon-reload

restart:
	$(SYSTEMCTL) --user restart $(SERVICE_UNIT)

uninstall:
	$(SYSTEMCTL) --user disable --now $(SERVICE_UNIT) >/dev/null 2>&1 || true
	$(SYSTEMCTL) --user disable --now $(LEGACY_SERVICE_UNIT) >/dev/null 2>&1 || true
	rm -f $(SYSTEMD_USER_DIR)/$(SERVICE_UNIT) $(INSTALL_BINARY) $(SYSTEMD_USER_DIR)/$(LEGACY_SERVICE_UNIT) $(LEGACY_INSTALL_BINARY)
	@if [ -w "$(dir $(CLIENT_MIRROR_INSTALL))" ]; then \
		rm -f "$(CLIENT_MIRROR_INSTALL)"; \
	else \
		$(SUDO) rm -f "$(CLIENT_MIRROR_INSTALL)"; \
	fi
	$(SYSTEMCTL) --user daemon-reload

clean:
	rm -f $(BINARY) bin/server
