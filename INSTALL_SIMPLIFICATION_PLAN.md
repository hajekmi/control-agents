# Installation Simplification Plan

This plan tracks work needed to make Control Agents easier to install from a public GitHub repository.

## Goal

Make the first successful install require only:

```sh
sudo apt install tmux ttyd
curl -fsSL https://raw.githubusercontent.com/hajekmi/control-agents/main/install.sh | sh
systemctl --user enable control-agents.service
systemctl --user restart control-agents.service
control-agents main
```

Then open:

```text
https://<host>:8080
```

## Recommended Direction

### 1. Publish Release Binaries

Attach prebuilt binaries to GitHub Releases so users do not need Go installed.

Initial targets:

- `linux-amd64`
- `linux-arm64`

Artifacts:

- `control-agents-server-linux-amd64`
- `control-agents-linux-amd64`
- `control-agents-server-linux-arm64`
- `control-agents-linux-arm64`
- `sha256sums.txt`

Later targets:

- `darwin-arm64`
- `darwin-amd64`

### 2. Add `install.sh`

Create a small installer that:

- Detects OS and architecture.
- Downloads the latest compatible GitHub release.
- Installs binaries under `~/.local/bin`.
- Creates `~/.config/control-agents/env` if missing.
- Generates a strong `CONTROL_AGENTS_PASSWORD`.
- Installs the systemd user unit under `~/.config/systemd/user`.
- Runs `systemctl --user daemon-reload` when systemd is available.
- Prints the generated URL, env file path, and next commands.

Keep it user-local by default. Avoid sudo except for separately installing OS packages.

### 3. Prefer User-Local Install Paths

Public install defaults should avoid `/usr/local/bin`.

Preferred paths:

- Server: `~/.local/bin/control-agents-server`
- Wrapper: `~/.local/bin/control-agents`
- Config: `~/.config/control-agents/env`
- Service: `~/.config/systemd/user/control-agents.service`

The installer should warn if `~/.local/bin` is not in `PATH`.

### 4. Optional Later: Add Server Init Command

Consider adding:

```sh
control-agents-server install-user
```

or:

```sh
control-agents-server init
```

It should perform the same local setup as the shell installer where possible:

- Validate `tmux` and `ttyd` are available.
- Create the env file.
- Generate a password.
- Install or print the systemd user unit.
- Print exact next steps.

This reduces shell-script complexity over time.

### 5. Keep Container Setup Advanced

Do not make containers the primary quickstart path.

The server can run in a container, but `control-agents`, `tmux`, and `ttyd` still need host access to sessions and Unix sockets. Keep container notes as an advanced deployment option.

### 6. Add GitHub Actions Release Workflow

Add CI/release automation that:

- Runs `go test ./...`.
- Runs `node --check internal/server/static/app.js`.
- Optionally runs Playwright browser E2E on supported runners.
- Builds release binaries for supported platforms.
- Uploads artifacts and checksums when a `v*` tag is pushed.

### 7. Package Managers

Package managers are intentionally not part of the initial deployment path. Distro packages add repository, signing, upgrade, and uninstall complexity that is not needed for the first public install path.

Reconsider only after release binaries and `install.sh` are proven.

## README Changes

When the installer exists, move the quickstart near the top:

```sh
sudo apt install tmux ttyd
curl -fsSL https://raw.githubusercontent.com/hajekmi/control-agents/main/install.sh | sh
systemctl --user enable control-agents.service
systemctl --user restart control-agents.service
control-agents main
```

Keep source build instructions as an advanced path.

## Open Decisions

- macOS support remains out of scope for the initial installer.

## Resolved Decisions

- `install.sh` installs the latest release by default and supports `VERSION=...`.
- Public install paths are user-local; the wrapper is not installed to `/usr/local/bin` automatically.
- The generated password is written to `~/.config/control-agents/env`, not printed.
- Legacy `terminal-mirror` paths and `MIRROR_*` variables are not supported as fallbacks.
