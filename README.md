# Terminal Mirror

Terminal Mirror exposes wrapper-started tmux sessions through one password-protected web app. Each terminal session runs its own `ttyd` instance on a private Unix domain socket, while the Go service provides login, tab discovery, and reverse proxying.

## Requirements

- Go 1.25 or newer for building.
- `tmux` for shared terminal sessions.
- `ttyd` for browser terminal I/O.
- `make` for the provided workflow.

On this VM, `tmux` and `ttyd` are expected to come from Homebrew:

```sh
brew install tmux ttyd
```

## Build And Test

```sh
make test
make build
```

The default Makefile uses local Go cache directories under `.cache/` and disables cgo. This keeps tests working in restricted environments and produces a simple Linux binary.

Run real tmux/ttyd E2E checks explicitly:

```sh
make test-e2e
```

The E2E test is opt-in because it starts real processes. It skips only when `RUN_E2E` is not set or required tools are unavailable.

## Run Locally

Start the web service:

```sh
export MIRROR_PASSWORD='change-me'
export MIRROR_BIND_ADDR='0.0.0.0'
export MIRROR_PORT='8080'
make run
```

Start a mirrored SSH terminal:

```sh
bin/client_mirror codex-main
```

Open:

```text
http://<vm-host-or-ip>:8080
```

New sessions started with `bin/client_mirror <name>` appear as tabs automatically. Only wrapper-started sessions are registered.

## Configuration

The Go service reads:

- `MIRROR_BIND_ADDR`, default `0.0.0.0`
- `MIRROR_PORT`, default `8080`
- `MIRROR_PASSWORD`, required unless `MIRROR_PASSWORD_FILE` is set
- `MIRROR_PASSWORD_FILE`, optional newline-trimmed password file
- `MIRROR_STATE_DIR`, default `$HOME/.local/state/terminal-mirror`
- `MIRROR_COOKIE_SECURE`, default `false` for HTTP
- `MIRROR_COOKIE_TTL_SECONDS`, default `43200`

The wrapper reads:

- `MIRROR_STATE_DIR`, same default as the service
- `MIRROR_DISPLAY_NAME`, optional label for the browser tab
- `MIRROR_APP_NAME`, optional override for tmux status-left; default is the session display name
- `MIRROR_TMUX_WINDOW_SIZE`, default `largest`
- `MIRROR_TMUX_MOUSE`, default `on`
- `MIRROR_WEB_SCROLLBACK_LINES`, default `10000`
- `MIRROR_NO_ATTACH=1`, test/support mode that registers the session without attaching the current terminal

The shared state directory contains:

- `sessions/*.json` registry files
- `sockets/*.sock` private `ttyd` Unix sockets
- `logs/*.log` per-session `ttyd` logs

Keep `MIRROR_STATE_DIR` reasonably short. Unix domain socket paths have a small system limit, and the wrapper fails early when the generated socket path is too long.

`MIRROR_WEB_SCROLLBACK_LINES` controls browser-side terminal history retained by `ttyd`/xterm.js while the web tab is connected. It does not replay tmux output that happened before the browser connected.

Because the browser is attached to tmux, mouse wheel history is primarily tmux pane history, not xterm.js scrollback. `client_mirror` enables `MIRROR_TMUX_MOUSE=on` by default so the wheel scrolls tmux history instead of sending arrow-key events to the shell prompt. Disable it with `MIRROR_TMUX_MOUSE=off` if you prefer the old tmux behavior.

`client_mirror` also sets a compact tmux status line for managed sessions. The left side shows the session label, for example `[ahoj]` when started with `bin/client_mirror ahoj`, and the right side shows the current pane directory through `#{pane_current_path}` without hostname, date, or time. Override the label with `MIRROR_APP_NAME`.

## API

Unauthenticated routes:

- `GET /login`: login page.
- `POST /login`: form login. Expects `password` in an `application/x-www-form-urlencoded` body. Success sets the auth cookie and redirects to `/`; failure redirects to `/login?error=1`.

Authenticated routes:

- `GET /`: tabbed web UI.
- `POST /logout`: clears the auth cookie and redirects to `/login`.
- `GET /api/sessions`: returns active wrapper-registered sessions.
- `GET /api/sessions/{session}/scroll`: returns tmux history scrollbar state for the active pane.
- `POST /api/sessions/{session}/scroll`: scrolls tmux history. Body actions are `line-up`, `line-down`, `page-up`, `page-down`, `top`, `bottom`, or `set`.
- `GET /terminal/{session}/...`: reverse proxies HTTP and WebSocket traffic to the matching `ttyd` Unix socket.
- `GET /app.js` and `GET /styles.css`: static UI assets.

The browser UI uses normal HTTP for login, static assets, and JSON API calls. `/api/*` endpoints return `401 unauthorized` when the auth cookie is missing or expired, so `app.js` can redirect the browser back to `/login` without receiving an HTML login page as an API response.

Go-served HTTP responses are gzip-compressed when the client sends `Accept-Encoding: gzip`. This includes `/login`, `/app.js`, `/styles.css`, and `/api/*` JSON responses. The `/terminal/{session}/...` ttyd proxy is excluded from this middleware, including both ttyd HTTP traffic and WebSocket upgrades.

Example `GET /api/sessions` response:

```json
{
  "sessions": [
    {
      "id": "main",
      "name": "main",
      "tmuxName": "main",
      "socket": "/home/user/.local/state/terminal-mirror/sockets/main.sock",
      "pid": 1234,
      "cwd": "/home/user/project",
      "createdAt": "2026-05-15T20:12:14Z"
    }
  ]
}
```

Successful login sets the `terminal_mirror_session` cookie. The cookie is signed with an in-memory secret, so sessions are invalidated when the server restarts.

Example scroll command:

```json
{
  "action": "set",
  "value": 120
}
```

`value` is the scrollbar offset from the top of tmux pane history. The bottom position returns to live output.

## systemd User Service

Build and install the binary:

```sh
make build
mkdir -p ~/.local/bin ~/.config/terminal-mirror
cp bin/server ~/.local/bin/server
cp systemd/user/server.service ~/.config/systemd/user/server.service
```

Create `~/.config/terminal-mirror/env`:

```sh
MIRROR_PASSWORD=change-me
MIRROR_BIND_ADDR=0.0.0.0
MIRROR_PORT=8080
MIRROR_COOKIE_SECURE=false
```

Enable and start:

```sh
systemctl --user daemon-reload
systemctl --user enable --now server.service
```

Check logs:

```sh
journalctl --user -u server.service -f
```

## Podman Notes

Systemd host deployment is the primary v1 path. Podman can work later for the Go app if the host state directory is bind-mounted into the container, because the app must access `ttyd` Unix sockets created by host-side wrapper sessions.

Example build:

```sh
podman build -t terminal-mirror-server .
```

Example run shape:

```sh
podman run --rm \
  -p 8080:8080 \
  -e MIRROR_PASSWORD=change-me \
  -e MIRROR_BIND_ADDR=0.0.0.0 \
  -e MIRROR_STATE_DIR=/state \
  -v "$HOME/.local/state/terminal-mirror:/state:Z" \
  terminal-mirror-server
```

On hosts where SELinux relabeling breaks Unix socket access, use the appropriate local policy or mount option for that host. The wrapper still runs on the host.

In the container path only the Go server belongs in the container. `client_mirror`, tmux, and `ttyd` still run on the host, and the shared state directory must be bind-mounted into the container.

## Security

The v1 service can bind publicly, but password-only HTTP is not safe on untrusted networks. The password, cookies, terminal output, and terminal input can be observed without TLS.

Before using this outside a trusted network, put the service behind HTTPS with a reverse proxy such as Caddy or nginx, then set:

```sh
MIRROR_COOKIE_SECURE=true
```

For safer interim access, bind to `127.0.0.1` and use SSH port forwarding:

```sh
ssh -L 8080:127.0.0.1:8080 user@vm
```

## Troubleshooting

- No tabs appear: start sessions through `bin/client_mirror <name>` and confirm service and wrapper use the same `MIRROR_STATE_DIR`.
- Tab opens but terminal is unavailable: check `<state-dir>/logs/<session>.log` for `ttyd` errors.
- Session disappears: the service removes stale registry files when the `ttyd` PID, tmux session, or Unix socket is gone.
- Browser and SSH sizes differ: both clients attach to the same tmux session. The wrapper sets tmux `window-size` to `largest` by default so browser activity does not constantly resize a larger SSH client. Override with `MIRROR_TMUX_WINDOW_SIZE=latest`, `smallest`, or `manual` if needed.
- Browser history is too short while the web tab is connected: increase `MIRROR_WEB_SCROLLBACK_LINES` before starting `bin/client_mirror <name>`, then reconnect the web tab.
- Mouse wheel cycles shell command history: make sure the session was started or refreshed with `MIRROR_TMUX_MOUSE=on bin/client_mirror <name>`.
- Use the right-side web scrollbar to scroll tmux pane history on browsers where iframe wheel handling is unreliable.
- On narrow mobile screens, the terminal area has horizontal scrolling so the tmux pane can keep a usable width without rotating the device.
