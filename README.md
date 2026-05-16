# Terminal Mirror

Terminal Mirror exposes wrapper-started tmux sessions through one password-protected web app. Each terminal session runs its own `ttyd` instance on a private Unix domain socket, while the Go service provides login, tab discovery, and reverse proxying.

## Requirements

- Go 1.25 or newer can build the module, but release/security builds should use the latest stable Go toolchain. As of 2026-05-16, the official stable release is Go 1.26.3.
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

Build metadata is injected from git by default. Override it explicitly when needed:

```sh
make build VERSION=2026.5.1
bin/control-agents-server --version
```

Run real tmux/ttyd E2E checks explicitly:

```sh
make test-e2e
```

The E2E test is opt-in because it starts real processes. It skips only when `RUN_E2E` is not set or required tools are unavailable.

## Versioning

Releases use calendar versioning: `YYYY.M.REVISION`.

- `YYYY` is the release year.
- `M` is the release month without a leading zero.
- `REVISION` starts at `1` each month and increments for each release in that month.

Git release tags use a `v` prefix, for example `v2026.5.1`. Runtime output omits the prefix and includes commit/build metadata through `control-agents-server --version`, startup logs, and `GET /api/version`.

Breaking changes are called out in `CHANGELOG.md` with `BREAKING:` because compatibility is not encoded in the version number.

Release checklist:

```sh
make test
git tag -a v2026.5.1 -m "Release 2026.5.1"
make build
```

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
bin/control-agents codex-main
```

When no name is passed, `control-agents` uses the current directory name. For example, running it from `/home/bestie/codex/control-agents` registers the session as `control-agents`.

Open:

```text
https://<vm-host-or-ip>:8080
```

On first start the server generates a self-signed ECDSA P-256 certificate under the state directory. Browsers will show a certificate warning until you trust that certificate or provide your own TLS certificate.

New sessions started with `bin/control-agents <name>` appear as tabs automatically. Only wrapper-started sessions are registered.

## Configuration

The Go service reads:

- `MIRROR_BIND_ADDR`, default `0.0.0.0`
- `MIRROR_PORT`, default `8080`
- `MIRROR_PASSWORD`, required unless `MIRROR_PASSWORD_FILE` is set
- `MIRROR_PASSWORD_FILE`, optional newline-trimmed password file
- `MIRROR_STATE_DIR`, default `$HOME/.local/state/terminal-mirror`
- `MIRROR_TLS_CERT_FILE`, default `$MIRROR_STATE_DIR/certs/server.crt`
- `MIRROR_TLS_KEY_FILE`, default `$MIRROR_STATE_DIR/certs/server.key`
- `MIRROR_AUTH_SECRET_FILE`, default `$MIRROR_STATE_DIR/auth/session.key`
- `MIRROR_COOKIE_SECURE`, default `true` for HTTPS
- `MIRROR_COOKIE_TTL_SECONDS`, default `172800`

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
- `auth/session.key` persistent cookie signing secret
- `certs/server.crt` and `certs/server.key` when the default generated TLS files are used

Keep `MIRROR_STATE_DIR` reasonably short. Unix domain socket paths have a small system limit, and the wrapper fails early when the generated socket path is too long.

`MIRROR_WEB_SCROLLBACK_LINES` controls browser-side terminal history retained by `ttyd`/xterm.js while the web tab is connected. It does not replay tmux output that happened before the browser connected.

Because the browser is attached to tmux, mouse wheel history is primarily tmux pane history, not xterm.js scrollback. `control-agents` enables `MIRROR_TMUX_MOUSE=on` by default so the wheel scrolls tmux history instead of sending arrow-key events to the shell prompt. Disable it with `MIRROR_TMUX_MOUSE=off` if you prefer the old tmux behavior.

`control-agents` also sets a compact tmux status line for managed sessions. The left side shows the session label, for example `[ahoj]` when started with `bin/control-agents ahoj`, and the right side shows the current pane directory through `#{pane_current_path}` without hostname, date, or time. Override the label with `MIRROR_APP_NAME`.

## API

Unauthenticated routes:

- `GET /login`: login page.
- `POST /login`: form login. Expects `password` in an `application/x-www-form-urlencoded` body. Success sets the auth cookie and redirects to `/`; failure redirects to `/login?error=1`.
- `GET /app.js` and `GET /styles.css`: static UI assets used by the login and app pages.

Authenticated routes:

- `GET /`: tabbed web UI.
- `POST /logout`: clears the auth cookie and redirects to `/login`.
- `GET /api/version`: returns server build metadata.
- `GET /api/sessions`: returns active wrapper-registered sessions.
- `GET /api/sessions/{session}/scroll`: returns tmux history scrollbar state for the active pane.
- `POST /api/sessions/{session}/scroll`: scrolls tmux history. Body actions are `line-up`, `line-down`, `page-up`, `page-down`, `top`, `bottom`, or `set`.
- `POST /api/sessions/{session}/keys`: sends a special key to the active tmux pane. Body key values include `ctrl-c`, `ctrl-d`, `ctrl-z`, `ctrl-l`, `escape`, `tab`, `enter`, arrows, `home`, `end`, `page-up`, and `page-down`.
- `GET /terminal/{session}/...`: reverse proxies HTTP and WebSocket traffic to the matching `ttyd` Unix socket.

The browser UI uses regular HTTPS requests for login, static assets, and JSON API calls. `/api/*` endpoints return `401 unauthorized` when the auth cookie is missing or expired, so `app.js` can redirect the browser back to `/login` without receiving an HTML login page as an API response.

Authenticated mutating routes require a same-origin `Origin` header, with `Referer` as a fallback for older clients. Terminal WebSocket upgrades under `/terminal/{session}/...` use the same origin check. This is intentionally strict because terminal actions are remote shell input.

Go-served responses are gzip-compressed when the client sends `Accept-Encoding: gzip`. This includes `/login`, `/app.js`, `/styles.css`, and `/api/*` JSON responses. The `/terminal/{session}/...` ttyd proxy is excluded from this middleware, including both ttyd HTTP traffic and WebSocket upgrades.

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

Successful login sets the `terminal_mirror_session` cookie. The cookie is signed with a persistent secret stored under the state directory, so sessions remain valid across server restarts until `MIRROR_COOKIE_TTL_SECONDS` expires or the auth secret file is removed.

Failed logins are rate-limited in server memory per direct client IP: 10 failed attempts in 5 minutes returns `429 Too Many Requests` with `Retry-After`. A successful login clears that IP's failures, and restarting the daemon resets the limiter.

Example scroll command:

```json
{
  "action": "set",
  "value": 120
}
```

`value` is the scrollbar offset from the top of tmux pane history. The bottom position returns to live output.

Example special key command:

```json
{
  "key": "ctrl-c"
}
```

## systemd User Service

Install the binary and user systemd unit:

```sh
make install
```

`make install` also creates `~/.config/terminal-mirror/env` with a generated password when it does not already exist. Edit it if you want a custom password or bind address:

```sh
MIRROR_PASSWORD=<generated>
MIRROR_BIND_ADDR=0.0.0.0
MIRROR_PORT=8080
```

The same target installs the server as `~/.local/bin/control-agents-server` and the wrapper client as `/usr/local/bin/control-agents`, using `sudo` for the client when `/usr/local/bin` is not writable. Override paths with `SERVER_INSTALL=/path/to/control-agents-server` and `CLIENT_INSTALL=/path/to/control-agents` if needed.

Enable and start:

```sh
systemctl --user enable --now control-agents.service
```

After later updates, rebuild, reinstall, and restart the service:

```sh
make install restart
```

Check logs:

```sh
journalctl --user -u control-agents.service -f
```

Uninstall the binary and user systemd unit:

```sh
make uninstall
```

`make uninstall` does not remove `~/.config/terminal-mirror/env` or the state directory.

## Podman Notes

Systemd host deployment is the primary v1 path. Podman can work later for the Go app if the host state directory is bind-mounted into the container, because the app must access `ttyd` Unix sockets created by host-side wrapper sessions.

Example build:

```sh
podman build -t control-agents-server .
```

Example run shape:

```sh
podman run --rm \
  -p 8080:8080 \
  -e MIRROR_PASSWORD=change-me \
  -e MIRROR_BIND_ADDR=0.0.0.0 \
  -e MIRROR_STATE_DIR=/state \
  -v "$HOME/.local/state/terminal-mirror:/state:Z" \
  control-agents-server
```

On hosts where SELinux relabeling breaks Unix socket access, use the appropriate local policy or mount option for that host. The wrapper still runs on the host.

In the container path only the Go server belongs in the container. `control-agents`, tmux, and `ttyd` still run on the host, and the shared state directory must be bind-mounted into the container.

## Security

The service uses HTTPS by default with an automatically generated self-signed ECC certificate. The password, cookies, terminal output, and terminal input are encrypted on the wire, but the browser cannot verify a self-signed certificate until you trust it locally or configure `MIRROR_TLS_CERT_FILE` and `MIRROR_TLS_KEY_FILE` with a certificate from a trusted authority.

Go-served pages and API responses include security headers: CSP for the app shell, `X-Frame-Options: SAMEORIGIN`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`, and a restrictive `Permissions-Policy`. CSP is not applied to `/terminal/` proxy responses so embedded `ttyd` assets keep working.

For local-only access, bind to `127.0.0.1` and use SSH port forwarding:

```sh
ssh -L 8080:127.0.0.1:8080 user@vm
```

## Troubleshooting

- No tabs appear: start sessions through `bin/control-agents <name>` and confirm service and wrapper use the same `MIRROR_STATE_DIR`.
- No tabs appear but `<state-dir>/sockets/<session>.sock` exists: reinstall and restart the systemd unit so the service gets the managed `PATH` that includes Homebrew `tmux`.
- Tab opens but terminal is unavailable: check `<state-dir>/logs/<session>.log` for `ttyd` errors.
- Session disappears: the service removes stale registry files when the `ttyd` PID, tmux session, or Unix socket is gone.
- Browser and SSH sizes differ: both clients attach to the same tmux session. The wrapper sets tmux `window-size` to `largest` by default so browser activity does not constantly resize a larger SSH client. Override with `MIRROR_TMUX_WINDOW_SIZE=latest`, `smallest`, or `manual` if needed.
- Browser history is too short while the web tab is connected: increase `MIRROR_WEB_SCROLLBACK_LINES` before starting `bin/control-agents <name>`, then reconnect the web tab.
- Mouse wheel cycles shell command history: make sure the session was started or refreshed with `MIRROR_TMUX_MOUSE=on bin/control-agents <name>`.
- Use the right-side web scrollbar to scroll tmux pane history on browsers where iframe wheel handling is unreliable.
- On narrow mobile screens, the terminal area has horizontal scrolling so the tmux pane can keep a usable width without rotating the device.
