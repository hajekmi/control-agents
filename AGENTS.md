# Agent workflow

- Always write code and code comments in English.
- Do not use Czech for function names, variable names, identifiers, or comments.

- For task work, read `TASKS/README.md`, the selected task, and its referenced documents.
- Treat the task as the approved implementation plan. Escalate material conflicts instead of silently redesigning it.
- Work on one task at a time. Move it from `TASKS/backlog/` to `TASKS/in-progress/` before code changes.
- Only the main agent changes task state.

The main agent coordinates, integrates, checks the complete diff, runs final validation, verifies acceptance criteria, and moves a fully completed task to `TASKS/done/`.

Each task requires a fresh implementer and reviewer with no inherited conversation context; pass only the selected task path. Use an explorer only when investigation is needed.
- `explorer`: optional read-only investigation when the task or code path is unclear.
- `implementer`: the single write agent; implements the task, adds tests, and runs targeted validation.
- `reviewer`: read-only independent review after implementation.

Use only one write agent at a time. After spawning a required subagent, explicitly wait for its final report before continuing. Do not mark a task done while any required subagent is active, failed, or unresolved.

# Control Agents Project Notes

Use `README.md` for product, API, install, deploy, and operations
documentation. Keep this file focused on working rules for future agents.

## Code Style

- Write code, comments, identifiers, function names, variable names, and commit
  text in English.
- Keep implementation scoped and structured by the existing packages:
  `internal/auth`, `internal/cert`, `internal/compress`, `internal/config`,
  `internal/proxy`, `internal/registry`, `internal/server`, `internal/session`,
  `internal/tmux`, and `internal/version`.
- Do not introduce a frontend build step unless the user explicitly asks for it.
  The UI is plain embedded HTML/CSS/JS.
- Keep the UI compact and operational. This is a terminal tool, not a marketing
  page.
- Keep the app header compact: brand, session tabs, and controls belong in the
  same top row where practical.

## Runtime Boundaries

- One selector item or top-level web tab is one Control Agents managed tmux
  session, not a tmux window. Tmux windows and panes stay inside that session.
  Only valid registry-backed managed sessions are visible; arbitrary tmux
  sessions must remain invisible and untouched.
- `internal/session` owns the durable managed-session lifecycle shared by the
  server and Control Agents clients: registry persistence, per-session locking,
  tmux creation and termination, terminal bridge recovery, and reconciliation.
- `control-agents-server` owns auth, API, static UI, proxying, and startup/list
  reconciliation through the shared lifecycle. The Go `control-agents` client
  uses the same lifecycle and per-session locks as the SSH entry point. It
  opens the interactive managed-session selector without a positional name and
  attaches directly when a name is provided. Non-interactive
  register-and-exit mode must stay available through `--no-attach` or
  `CONTROL_AGENTS_NO_ATTACH=1` for tests and scripts.
- All CLI and web session creation starts in the configured service user's
  `$HOME`. Never reintroduce current-directory derivation or browser-provided
  commands, environments, tmux arguments, shells, or working directories.
- SSH `Ctrl-b d`, selector quit, browser close, and connection loss are
  nondestructive detach behavior. The web Menu may create a session and may
  terminate only the captured active managed session after explicit named
  confirmation. Termination destroys tmux, disconnects every SSH/web client,
  stops the bridge, and removes persistent and transient session state.
- History and Copy use one immutable, in-memory snapshot captured once from the
  verified active pane. Wheel, touch, progressive paging, native selection, and
  Copy are local browser operations and must never enter tmux copy-mode, pan a
  tmux client viewport, or call a scroll endpoint. The ttyd iframe remains
  connected and full-sized behind the opaque History layer.
- The main menu `Resize` action opens the resize-source panel. Preserve the
  explicit modes: `fixed`, `fit-once`, and the disabled future capability
  `follow-device`. Browser tabs report distinct opaque `sessionStorage` viewer
  IDs through `/resize/viewer` heartbeats, and the panel should identify
  viewers by browser/IP, size, and last-seen time. `fit-once` applies the
  selected viewer dimensions once, then returns to fixed behavior.
- Do not reintroduce `window-size latest`, automatic smallest-client sizing, or
  continuous resize following from viewer heartbeats. `fixed` and `fit-once`
  use tmux manual sizing; `follow-device` remains explicitly unsupported until
  a later task implements it.
- iOS software-keyboard handling is local viewport behavior, not a tmux resize
  mode. Keep `visualViewport` tracking in the web shell so the active iframe
  shrinks above the keyboard and sends transient viewer heartbeats; do not apply
  tmux resize settings just because the keyboard opened.
- History snapshots are scoped to the concrete authenticated login, opaque
  viewer ID, session ref, and verified pane generation. ANSI parsing and
  resource limits are server-side; the browser renders only structured runs
  through text nodes. `Paste` reads the browser clipboard from an explicit
  click, stages it with UTF-8 byte and logical-line counts, requires explicit
  confirmation for multiline, control-character, or trailing-newline content,
  obtains a short-lived single-use action token, and sends it through the
  bounded `/paste` endpoint without automatic retry. The visible
  iOS textarea fallback stages its `paste` event through the same review flow
  and never sends terminal input directly. Keep text selection working by
  leaving tmux mouse mode off by default.
- The T-Control panel exposes tmux window and pane actions through a server-side
  allowlist. Do not add arbitrary tmux command execution from browser input
  without an explicit security review.
- Session tabs may show a tiny tmux-window count badge, but only for sessions
  with more than one internal tmux window so the iOS/Safari header stays compact.
- Do not expose per-session `ttyd` TCP ports. `ttyd` must stay behind Unix
  sockets in the shared state directory.
- Only sessions with a valid Control Agents registry record should appear in
  the UI. Never adopt arbitrary tmux sessions automatically.
- Browser routes and mutation payloads use opaque session, window, pane, and
  viewer references. Canonical names and raw tmux IDs are display/internal
  data only; resolve refs server-side and verify the current pane generation
  immediately before every mutation.
- Preserve the default tmux `window-size manual` behavior. A newly attached
  narrow browser or SSH client must not shrink the shared tmux window without
  an explicit `fit-once` action.
- Preserve the default tmux `mouse off` behavior unless changing it is the
  purpose of the task. It keeps normal terminal text selection from being
  intercepted by tmux; users can set `CONTROL_AGENTS_TMUX_MOUSE=on` only when they
  prefer tmux to own all mouse handling.
- Preserve the managed tmux status line shape: `status-left` session label,
  `status-right` current pane path, no hostname/date/time.
- Managed terminal sessions must retain the same account privilege boundary as
  an ordinary SSH shell, including interactive `sudo` when account policy
  authorizes it. Keep the user unit at `NoNewPrivileges=false`; restrict sudo
  through the Unix account policy rather than irreversibly constraining the
  shared tmux server.
- Preserve `LANG=C.UTF-8` and `LC_ALL=C.UTF-8` for the server, Go SSH client,
  ttyd bridge, managed tmux commands, installed user service, and repository
  tests. Tmux 3.7b under the plain C locale corrupts topology delimiters.
- Preserve `CONTROL_AGENTS_WEB_SCROLLBACK_LINES` as the browser scrollback control.
  This is xterm.js history while the web tab is connected, not replay of past
  tmux history.
- Preserve the managed tmux `history-limit 50000` default and reconciliation
  for existing managed windows. Raising an existing pane's limit cannot restore
  history that tmux already discarded.
- Managed tmux sessions use
  `$CONTROL_AGENTS_STATE_DIR/agent/forwarded.sock` as their stable
  `SSH_AUTH_SOCK`. Only a valid SSH client invocation may atomically retarget
  it to an existing Unix socket. Preserve last-successful-refresh semantics,
  per-session tmux environment inheritance, and unavailability while the
  forwarding SSH transport is disconnected; do not introduce a persistent VM
  agent or private-key storage.

## Security Rules

- Do not log passwords, auth cookies, terminal input, or terminal output.
- Terminal audit records are metadata-only: opaque ID, status, byte count,
  duration, and reason code. Do not log History/Paste bodies, WebSocket frames,
  or enable tmux verbose logging in production.
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
- Run `make test-e2e` when changing the lifecycle, Go client/selector, CLI/API
  integration, tmux behavior, `ttyd` startup or recovery, registry
  compatibility, server reconciliation, or proxy routing.
- Run `make test-browser` when changing session lifecycle API/UI behavior,
  iframe terminal interaction, authentication flows, special key controls,
  T-Control actions, resize-source behavior, or History/wheel behavior.
- Run `make test-browser-matrix` for History/browser compatibility changes so
  Chromium, Firefox, and WebKit engine automation stays green. Never describe
  WebKit automation as physical or Safari evidence.
- Keep Linux browser targets inside the verified loopback-only network
  boundary. Launcher modes are `auto`, `unprivileged`, and `sudo`; only the
  fixed bootstrap may run through `sudo -n`, and Node, browsers, the server,
  tmux, and ttyd must run as the original non-root user without capabilities.
- Run `make test-benchmarks` after changing History capture, ANSI parsing,
  snapshot paging/materialization, local scrolling, or reconnect behavior.
  Generated reports must remain bounded and content-free.
- Run `node --check internal/server/static/app.js` after browser JavaScript
  changes. Lifecycle integration tasks require the complete `make test`,
  `make build`, `make test-e2e`, `make test-browser`, and JavaScript syntax
  matrix.
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
- Keep README and AGENTS synchronized when renaming commands or changing runtime
  behavior.
