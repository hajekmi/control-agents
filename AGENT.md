# Control Agents Agent Notes

Use `README.md` for product, API, install, deploy, and operations
documentation. Keep this file focused on working rules for future agents.

## Code Style

- Write code, comments, identifiers, function names, variable names, and commit
  text in English.
- Keep implementation scoped and structured by the existing packages:
  `internal/config`, `internal/auth`, `internal/cert`, `internal/registry`,
  `internal/proxy`, and `internal/server`.
- Do not introduce a frontend build step unless the user explicitly asks for it.
  The UI is plain embedded HTML/CSS/JS.
- Keep the UI compact and operational. This is a terminal tool, not a marketing
  page.
- Keep the app header compact: brand, session tabs, and controls belong in the
  same top row where practical.

## Runtime Boundaries

- `control-agents-server` owns auth, API, static UI, session discovery, and
  proxying.
- `control-agents` owns tmux session creation/attach, `ttyd` startup, and
  registry file creation.
- The right-side web scrollbar and touch swipe history scrolling are owned by
  the parent app, not by ttyd. They call server tmux-scroll APIs and should keep
  working even when iframe wheel events are unreliable on mobile browsers. The
  scrollbar state must account for both tmux pane history and tmux client window
  offset when a non-default window-size mode makes the tmux window larger than a
  browser viewport. When status is enabled, subtract the tmux status line from
  client height before calculating bottom offsets, otherwise the prompt can end
  up hidden behind the status line.
- The main menu `Resize` action opens the resize-source panel. Preserve the
  explicit modes: `off`, `smallest`, `web`, and `primary`. Browser tabs report
  distinct `sessionStorage` viewer IDs through `/resize/viewer` heartbeats, and
  the panel should identify viewers by browser/IP, size, and last-seen time.
  `web` follows the selected browser viewer; `primary` follows the primary
  SSH/tmux client when one is available.
- Do not reintroduce one-shot resize behavior or `window-size latest` from the
  Resize menu. Explicit `web` and `primary` modes should use tmux manual
  sizing, `smallest` should set tmux `window-size smallest`, and `off` should
  store the setting without applying a resize.
- iOS software-keyboard handling is local viewport behavior, not a tmux resize
  mode. Keep `visualViewport` tracking in the web shell so the active iframe
  shrinks above the keyboard and sends transient viewer heartbeats; do not apply
  tmux resize settings just because the keyboard opened.
- The parent app also captures vertical wheel and single-finger touch swipe
  events over the same-origin ttyd iframe and routes them through the same
  tmux-scroll API. `Copy mode` in the menu disables this gesture routing and
  opens a selectable text overlay from `/capture` rather than forcing browser
  selection inside xterm's DOM. `Paste` reads the browser clipboard from an
  explicit click and sends it through the bounded `/paste` endpoint. Keep text
  selection working by leaving tmux mouse mode off by default.
- The T-Control panel exposes tmux window and pane actions through a server-side
  allowlist. Do not add arbitrary tmux command execution from browser input
  without an explicit security review.
- Session tabs may show a tiny tmux-window count badge, but only for sessions
  with more than one internal tmux window so the iOS/Safari header stays compact.
- Do not expose per-session `ttyd` TCP ports. `ttyd` must stay behind Unix
  sockets in the shared state directory.
- Only wrapper-started sessions should appear in the UI unless the user changes
  that requirement.
- Preserve the default tmux `window-size smallest` behavior unless changing it
  is the purpose of the task. It keeps fullscreen terminal apps visible in both
  browser and SSH clients attached to the same tmux session.
- Preserve the default tmux `mouse off` behavior unless changing it is the
  purpose of the task. It keeps normal terminal text selection from being
  intercepted by tmux; users can set `CONTROL_AGENTS_TMUX_MOUSE=on` only when they
  prefer tmux to own all mouse handling.
- Preserve the managed tmux status line shape: `status-left` session label,
  `status-right` current pane path, no hostname/date/time.
- Preserve `CONTROL_AGENTS_WEB_SCROLLBACK_LINES` as the browser scrollback control.
  This is xterm.js history while the web tab is connected, not replay of past
  tmux history.

## Security Rules

- Do not log passwords, auth cookies, terminal input, or terminal output.
- Do not log or expose the persistent auth secret stored in the state
  directory.
- Keep state directory permissions private: directories `0700`, registry files
  `0600`, auth secret `0600`.
- HTTPS is the default server mode. Keep `CONTROL_AGENTS_COOKIE_SECURE=true` as the
  default unless the user explicitly needs an HTTP-only test/development path.
- Preserve the in-memory login rate limiter unless the user explicitly changes
  the security model. It is intentionally per direct client IP and reset on
  daemon restart.
- Preserve same-origin checks for authenticated mutating routes and terminal
  WebSocket upgrades. Terminal input is a remote shell action.
- Keep security headers on Go-served responses. Avoid applying CSP to
  `/terminal/` proxy responses unless ttyd compatibility has been tested.
- Keep gzip middleware limited to Go-served HTTP responses. Do not wrap
  `/terminal/` proxy traffic or WebSocket upgrades unless that is explicitly
  requested and tested.

## Tests

- Run `make test` after code changes.
- Run `make test-e2e` when changing `bin/control-agents`, tmux behavior, `ttyd`
  startup, registry compatibility, or proxy routing.
- Run `make test-browser` when changing browser UI behavior, iframe terminal
  interaction, authentication flows, special key controls, T-Control actions,
  resize-source behavior, or scrollbar/wheel behavior.
- The Makefile intentionally sets workspace-local Go caches, `TMUX_TMPDIR`, cgo
  off, and `GOFLAGS=-buildvcs=false` for restricted execution environments.

## Versioning

- Releases use calendar versioning: `YYYY.M.REVISION`.
- `YYYY` is the release year, `M` is the release month without a leading zero,
  and `REVISION` starts at `1` each month and increments for each release in
  that month.
- Git release tags use a `v` prefix, for example `v2026.5.2`.
- Runtime output omits the prefix, for example `2026.5.2`.
- Breaking changes are called out in `CHANGELOG.md` with `BREAKING:` because
  compatibility is not encoded in the version number.
- Before tagging a release, move relevant entries from `Unreleased` in
  `CHANGELOG.md` into a dated release section.

## Repository Notes

- The execution environment may not be a usable git checkout. Do not assume
  `git status` or VCS stamping works.
- Do not remove `.cache/` handling from the Makefile; it exists because default
  home and `/tmp` paths may be read-only.
- Keep README and AGENT synchronized when renaming commands or changing runtime
  behavior.
