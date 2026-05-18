# Control Agents

Control Agents exposes wrapper-started tmux sessions through one password-protected web app. Each terminal session runs its own `ttyd` instance on a private Unix domain socket, while the Go service provides login, tab discovery, and reverse proxying.

It is optimized for mobile touch displays, especially iOS Safari and iPadOS browsers: the web UI includes touch history scrolling, a selectable Copy mode, Paste support, special-key buttons, and viewport handling for the software keyboard.

## Quick Install

Linux user-local install:

```sh
sudo apt install tmux ttyd
curl -fsSL https://raw.githubusercontent.com/hajekmi/control-agents/main/install.sh | sh
systemctl --user enable control-agents.service
systemctl --user restart control-agents.service
control-agents main
```

Then open:

```text
https://<vm-host-or-ip>:8080
```

The installer downloads the latest GitHub Release binaries, installs them under `~/.local/bin`, creates `~/.config/control-agents/env` with a generated `CONTROL_AGENTS_PASSWORD`, installs the user systemd unit, and runs `systemctl --user daemon-reload` when available.

Review the generated config before exposing the service:

```sh
chmod 600 ~/.config/control-agents/env
sed -n '1,80p' ~/.config/control-agents/env
```

If the service should start after boot before the user logs in, enable lingering for that user:

```sh
loginctl enable-linger "$USER"
```

## Requirements

Runtime:

- `tmux` for shared terminal sessions.
- `ttyd` for browser terminal I/O.
- systemd user services for the default service setup.

Development:

- Go 1.25 or newer can build the module, but release/security builds should use the latest stable Go toolchain. As of 2026-05-16, the official stable release is Go 1.26.3.
- `make` for the provided workflow.
- Node.js 20, 22, or 24 plus npm for Playwright browser E2E tests.

Playwright is a project-local dev dependency. Install JavaScript dependencies from the repo root:

```sh
npm install
npx playwright install chromium
```

On AlmaLinux/RHEL-like hosts, Chromium also needs system libraries. If `make test-browser` reports missing browser dependencies, install the matching packages:

```sh
sudo dnf install -y nspr nss atk at-spi2-atk at-spi2-core cups-libs libxcb libxkbcommon alsa-lib mesa-libgbm libX11 libXext cairo pango libXcomposite libXdamage libXfixes libXrandr
```

## Source Install

Use this path when developing locally or installing without release binaries. Install the runtime and build prerequisites on the target host:

- Go 1.25 or newer
- `make`
- `tmux`
- `ttyd`
- systemd user services

Clone the repository, build, and install the server, wrapper client, user systemd unit, and first-run environment file:

```sh
git clone <repo-url> control-agents
cd control-agents
make install
```

Enable and start:

```sh
systemctl --user enable control-agents.service
systemctl --user restart control-agents.service
```

Register a mirrored terminal session from any working directory:

```sh
control-agents main
```

This starts or reuses a tmux session, starts the private `ttyd` bridge, writes the session registry entry, prints the session ID, and exits. To attach the current terminal to the same tmux session too, use:

```sh
control-agents --attach main
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

Run browser E2E checks with Playwright:

```sh
make test-browser
```

These tests start the Go server, create a real tmux/ttyd session through `bin/control-agents`, log in through Chromium, and verify the tabbed UI, terminal iframe, special keys panel, configurable resize-source panel, local visual viewport tracking, logout flow, right-side history controls, and wheel scrolling over the terminal iframe.
They also cover the T-Control panel for listing tmux windows, creating/selecting a tmux window through the web UI, and showing the compact tmux-window count badge on session tabs when a session has more than one tmux window.

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
git push origin v2026.5.1
```

Tag pushes run the release workflow, build Linux `amd64` and `arm64` assets, and upload checksums for `install.sh`.

## Run Locally

Start the web service:

```sh
export CONTROL_AGENTS_PASSWORD='change-me'
export CONTROL_AGENTS_BIND_ADDR='0.0.0.0'
export CONTROL_AGENTS_PORT='8080'
make run
```

Register a mirrored terminal:

```sh
bin/control-agents codex-main
```

When no name is passed, `control-agents` uses the current directory name. For example, running it from `/home/bestie/codex/control-agents` registers the session as `control-agents`. Add `--attach` before the name when you also want the current terminal to attach to the tmux session.

Open:

```text
https://<vm-host-or-ip>:8080
```

On first start the server generates a self-signed ECDSA P-256 certificate under the state directory. Browsers will show a certificate warning until you trust that certificate or provide your own TLS certificate.

New sessions started with `bin/control-agents <name>` appear as tabs automatically. Only wrapper-started sessions are registered.

## Configuration

The Go service reads:

- `CONTROL_AGENTS_BIND_ADDR`, default `0.0.0.0`
- `CONTROL_AGENTS_PORT`, default `8080`
- `CONTROL_AGENTS_PASSWORD`, required unless `CONTROL_AGENTS_PASSWORD_FILE` is set
- `CONTROL_AGENTS_PASSWORD_FILE`, optional newline-trimmed password file
- `CONTROL_AGENTS_STATE_DIR`, default `$HOME/.local/state/control-agents`
- `CONTROL_AGENTS_TLS_CERT_FILE`, default `$CONTROL_AGENTS_STATE_DIR/certs/server.crt`
- `CONTROL_AGENTS_TLS_KEY_FILE`, default `$CONTROL_AGENTS_STATE_DIR/certs/server.key`
- `CONTROL_AGENTS_AUTH_SECRET_FILE`, default `$CONTROL_AGENTS_STATE_DIR/auth/session.key`
- `CONTROL_AGENTS_COOKIE_SECURE`, default `true` for HTTPS
- `CONTROL_AGENTS_COOKIE_TTL_SECONDS`, default `172800`

The wrapper reads:

- `CONTROL_AGENTS_STATE_DIR`, same default as the service
- `CONTROL_AGENTS_DISPLAY_NAME`, optional label for the browser tab
- `CONTROL_AGENTS_APP_NAME`, optional override for tmux status-left; default is the session display name
- `CONTROL_AGENTS_TMUX_WINDOW_SIZE`, default `smallest`
- `CONTROL_AGENTS_TMUX_MOUSE`, default `off`
- `CONTROL_AGENTS_WEB_SCROLLBACK_LINES`, default `10000`
- `CONTROL_AGENTS_ATTACH=1`, attach the current terminal after registering the web session
- `CONTROL_AGENTS_NO_ATTACH=1`, force register-and-exit mode; kept for tests and scripts

The shared state directory contains:

- `sessions/*.json` registry files
- `sockets/*.sock` private `ttyd` Unix sockets
- `logs/*.log` per-session `ttyd` logs
- `auth/session.key` persistent cookie signing secret
- `certs/server.crt` and `certs/server.key` when the default generated TLS files are used

Keep `CONTROL_AGENTS_STATE_DIR` reasonably short. Unix domain socket paths have a small system limit, and the wrapper fails early when the generated socket path is too long.

`CONTROL_AGENTS_WEB_SCROLLBACK_LINES` controls browser-side terminal history retained by `ttyd`/xterm.js while the web tab is connected. It does not replay tmux output that happened before the browser connected.

Because the browser is attached to tmux, `control-agents` keeps `CONTROL_AGENTS_TMUX_MOUSE=off` by default so normal terminal text selection works without tmux intercepting mouse drag. The parent web app captures vertical wheel events and single-finger touch swipes over the terminal iframe and sends them to the tmux scroll API, so history scrolls without sending arrow-key events to the shell prompt. On touch devices, use `Menu` -> `Copy mode` to open a selectable text capture of the active terminal instead of swipe history scrolling, then `Menu` -> `Paste` to paste clipboard text back into the active pane. Start or refresh a session with `CONTROL_AGENTS_TMUX_MOUSE=on` only if you prefer tmux to own all mouse handling.

The default `CONTROL_AGENTS_TMUX_WINDOW_SIZE=smallest` keeps browser and SSH clients on the same full tmux screen, which is important for fullscreen terminal apps such as Midnight Commander. The web Resize panel can override the active session's live resize source without changing this startup default. If you override the wrapper default to `largest`, `latest`, or `manual`, the right-side web scrollbar also accounts for tmux client window offset when a smaller browser or SSH client is panned inside a taller tmux window. The server accounts for tmux's status line when calculating the visible pane height so returning the scrollbar to the bottom lands back on the live prompt, not behind the status line.

`control-agents` also sets a compact tmux status line for managed sessions. The left side shows the session label, for example `[ahoj]` when started with `bin/control-agents ahoj`, and the right side shows the current pane directory through `#{pane_current_path}` without hostname, date, or time. Override the label with `CONTROL_AGENTS_APP_NAME`.

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
- `GET /api/sessions/{session}/capture`: returns a bounded text capture of the active tmux pane for Copy mode.
- `POST /api/sessions/{session}/paste`: pastes text into the active tmux pane. Body: `{ "text": "..." }`; NUL bytes and payloads above 64 KiB are rejected.
- `POST /api/sessions/{session}/keys`: sends a special key to the active tmux pane. Body key values include `ctrl-c`, `ctrl-d`, `ctrl-z`, `ctrl-l`, `escape`, `tab`, `enter`, arrows, `home`, `end`, `page-up`, and `page-down`.
- `POST /api/sessions/{session}/resize/viewer`: records a browser tab/window resize heartbeat. Body: `{ "viewerId": "browser-tab-id", "width": 120, "height": 32, "transient": false }`. A transient heartbeat updates viewer liveness but does not auto-apply web-follow resize.
- `GET /api/sessions/{session}/resize`: returns resize state: selected mode, selected browser viewer, active browser viewers, primary tmux client metadata, and the last applied size when available.
- `POST /api/sessions/{session}/resize`: stores and applies the selected resize mode. Body: `{ "mode": "off|smallest|web|primary", "viewerId": "browser-tab-id" }`; `viewerId` is required only for explicit web-viewer mode.
- `GET /api/sessions/{session}/tmux-control`: lists tmux windows for the session.
- `POST /api/sessions/{session}/tmux-control`: runs an allowlisted tmux control action such as `new-window`, `select-window`, `next-window`, `previous-window`, `split-horizontal`, `split-vertical`, pane selection/resizing, `choose-window`, or `command-prompt`.
- `GET /terminal/{session}/...`: reverse proxies HTTP and WebSocket traffic to the matching `ttyd` Unix socket.

The browser UI uses regular HTTPS requests for login, static assets, and JSON API calls. `/api/sessions` includes `tmuxWindowCount` only when a session has more than one internal tmux window; the tab row renders that value as a compact badge. `/api/*` endpoints return `401 unauthorized` when the auth cookie is missing or expired, so `app.js` can redirect the browser back to `/login` without receiving an HTML login page as an API response.

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
      "socket": "/home/user/.local/state/control-agents/sockets/main.sock",
      "pid": 1234,
      "cwd": "/home/user/project",
      "createdAt": "2026-05-15T20:12:14Z"
    }
  ]
}
```

Successful login sets the `control_agents_session` cookie. The cookie is signed with a persistent secret stored under the state directory, so sessions remain valid across server restarts until `CONTROL_AGENTS_COOKIE_TTL_SECONDS` expires or the auth secret file is removed.

Failed logins are rate-limited in server memory per direct client IP: 10 failed attempts in 5 minutes returns `429 Too Many Requests` with `Retry-After`. A successful login clears that IP's failures, and restarting the daemon resets the limiter.

Example scroll command:

```json
{
  "action": "set",
  "value": 120
}
```

`value` is the scrollbar offset from the top of the combined tmux scroll range. The top of the range is tmux pane history, followed by any live-window overflow for small clients. The bottom position returns to live output.

Example special key command:

```json
{
  "key": "ctrl-c"
}
```

Example resize state:

```json
{
  "mode": "web",
  "selectedViewerId": "viewer-7f3d",
  "viewers": [
    {
      "id": "viewer-7f3d",
      "ip": "203.0.113.10",
      "userAgent": "Mozilla/5.0 ...",
      "width": 132,
      "height": 36,
      "lastSeen": "2026-05-16T12:34:56Z",
      "active": true
    }
  ],
  "primaryClient": {
    "name": "/dev/pts/2",
    "width": 100,
    "height": 28,
    "activity": 1778944495,
    "web": false
  },
  "applied": {
    "mode": "web",
    "width": 132,
    "height": 36
  }
}
```

Each browser tab stores its own `viewerId` in `sessionStorage` and sends periodic resize heartbeats with the current terminal size. The Resize panel identifies web viewers by browser/IP, terminal size, and last-seen time so users can choose the intended tab when multiple web windows are open.

On mobile Safari/iOS, the page also tracks `visualViewport` changes from the software keyboard. This is local layout handling only: the web terminal iframe is refit above the keyboard and the tab heartbeat is refreshed as transient, so tmux resize mode is not changed unless the user explicitly applies a Resize panel mode.

Resize modes:

- `Off`: stores the setting and avoids applying a tmux resize.
- `Automatic smallest`: applies tmux `window-size smallest`, letting tmux pick the smallest attached client.
- `Follow web window`: applies manual tmux sizing from the selected browser viewer. This keeps the chosen browser tab authoritative until another mode is selected.
- `Follow primary SSH/tmux`: applies manual tmux sizing from the primary non-web tmux client when one is available, so an SSH/tmux attachment can drive the shared size.

Explicit `web` and `primary` modes use tmux manual sizing. They must not set `window-size latest`. `smallest` is the only resize mode that should set `window-size smallest`, and `off` should not force a resize.

Example T-Control command:

```json
{
  "action": "select-window",
  "windowIndex": 1
}
```

T-Control intentionally uses an action allowlist instead of accepting arbitrary tmux commands from the browser. The web panel shows tmux windows, lets users switch windows, and exposes common window/pane controls.

The main menu also includes `Resize`, which opens the resize-source panel. From there users can turn automatic resize management off, choose tmux's automatic smallest-client behavior, follow a selected web browser viewer, or follow the primary SSH/tmux client.

## systemd User Service Details

Install the binary and user systemd unit:

```sh
make install
```

`make install` also creates `~/.config/control-agents/env` with a generated password when it does not already exist. Edit it if you want a custom password or bind address:

```sh
CONTROL_AGENTS_PASSWORD=<generated>
CONTROL_AGENTS_BIND_ADDR=0.0.0.0
CONTROL_AGENTS_PORT=8080
```

The same target installs the server as `~/.local/bin/control-agents-server` and the wrapper client as `~/.local/bin/control-agents`. Override paths with `SERVER_INSTALL=/path/to/control-agents-server` and `CLIENT_INSTALL=/path/to/control-agents` if needed.

Enable and start:

```sh
systemctl --user enable control-agents.service
systemctl --user restart control-agents.service
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

`make uninstall` does not remove `~/.config/control-agents/env` or the state directory.

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
  -e CONTROL_AGENTS_PASSWORD=change-me \
  -e CONTROL_AGENTS_BIND_ADDR=0.0.0.0 \
  -e CONTROL_AGENTS_STATE_DIR=/state \
  -v "$HOME/.local/state/control-agents:/state:Z" \
  control-agents-server
```

On hosts where SELinux relabeling breaks Unix socket access, use the appropriate local policy or mount option for that host. The wrapper still runs on the host.

In the container path only the Go server belongs in the container. `control-agents`, tmux, and `ttyd` still run on the host, and the shared state directory must be bind-mounted into the container.

## Security

See [`SECURITY.md`](SECURITY.md) for supported versions, vulnerability reporting, threat model notes, and deployment guidance.

The service uses HTTPS by default with an automatically generated self-signed ECC certificate and accepts TLS 1.3 only. Older protocol versions, including TLS 1.2, are disabled. The password, cookies, terminal output, and terminal input are encrypted on the wire, but the browser cannot verify a self-signed certificate until you trust it locally or configure `CONTROL_AGENTS_TLS_CERT_FILE` and `CONTROL_AGENTS_TLS_KEY_FILE` with a certificate from a trusted authority.

Go-served pages and API responses include security headers: CSP for the app shell, `X-Frame-Options: SAMEORIGIN`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`, and a restrictive `Permissions-Policy`. CSP is not applied to `/terminal/` proxy responses so embedded `ttyd` assets keep working.

For local-only access, bind to `127.0.0.1` and use SSH port forwarding:

```sh
ssh -L 8080:127.0.0.1:8080 user@vm
```

## License

Control Agents is licensed under the GNU Affero General Public License v3.0. See [`LICENSE`](LICENSE).

## Troubleshooting

- No tabs appear: start sessions through `bin/control-agents <name>` and confirm service and wrapper use the same `CONTROL_AGENTS_STATE_DIR`.
- No tabs appear but `<state-dir>/sockets/<session>.sock` exists: reinstall and restart the systemd unit so the service gets the managed `PATH` that includes `tmux` and `ttyd`.
- Tab opens but terminal is unavailable: check `<state-dir>/logs/<session>.log` for `ttyd` errors.
- Session disappears: the service removes stale registry files when the `ttyd` PID, tmux session, or Unix socket is gone.
- Browser and SSH sizes differ: both clients attach to the same tmux session. The wrapper sets tmux `window-size` to `smallest` by default so fullscreen apps render the same complete screen in every client. Use Menu -> Resize to choose whether the session should stay off, use automatic smallest-client sizing, follow a browser viewer, or follow the primary SSH/tmux client. Explicit web and primary modes use tmux manual sizing, not `window-size latest`.
- Browser history is too short while the web tab is connected: increase `CONTROL_AGENTS_WEB_SCROLLBACK_LINES` before starting `bin/control-agents <name>`, then reconnect the web tab.
- Mouse wheel cycles shell command history: reinstall and restart the service so the current web UI captures terminal wheel events. Use the right-side web scrollbar on browsers where iframe wheel handling is unreliable.
- Use the right-side web scrollbar to scroll tmux pane history and small-client live-window overflow directly.
- On narrow mobile screens, the terminal area has horizontal scrolling so the tmux pane can keep a usable width without rotating the device.
