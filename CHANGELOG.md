# Changelog

Versions use calendar numbering: `YYYY.M.REVISION`.

## Unreleased

- Pin CI to the checksum-verified tmux 3.7b upstream release instead of
  Ubuntu 24.04's incompatible tmux 3.4 package through the same user-local
  installer used by Quick Install, with selected executable/version checks
  before Control Agents installation. Enforce `C.UTF-8` for the service, Go
  client, bridge, managed tmux commands, installers, and tests so tmux format
  delimiters remain stable even when the caller uses `LANG=C`. Give
  pseudo-terminal E2E fixtures an explicit capable terminal type instead of
  inheriting a noninteractive runner's `dumb` terminal.
- Enforce the exact tmux 3.7b executable and `C.UTF-8` locale after preserved
  user-service environment files, and make the server and SSH client prefer
  their co-installed verified tmux even when operator `PATH` conflicts. Build
  tmux into a temporary prefix, verify it before one atomic `BIN_DIR`
  replacement, and support repeated custom-destination installs without a
  second independent prefix contract.
- Replace a registered bridge from the immediately previous relative `tmux`
  argv with one using the resolved absolute verified tmux path during upgrade,
  while rejecting unregistered or non-exact process shapes. Keep both Ubuntu
  installation-plan examples on the shared checksum-verified exact-3.7b flow.
- Add repository-owned History parser/snapshot/Paste matrices, real-tmux
  50,000-line SSH-isolation and alternate-screen fixtures, Chromium/Firefox/
  WebKit engine coverage, mobile/tablet viewport automation, and bounded
  content-free server/browser benchmark reports with explicit unsupported
  metrics and pending physical-Safari release gates. Run the managed-lifecycle
  secondary-viewer lifecycle, and mobile/two-viewer profiles in separate fresh
  fixture invocations, isolate the synthetic request-failure probe after
  functional mutations, give each invocation one bounded process owner that
  proves browser/server/ttyd/tmux teardown before the next profile, use a fresh
  browser process for each independent scenario, isolate complete Linux
  profiles from ambient host network-change notifications in a private
  loopback-only namespace with an exactly-once boundary gate, and keep failure
  diagnostics content-free with disabled rich artifacts plus an
  intentional-failure canary scanner.
- Harden browser terminal access with login-bound CSRF tokens, exact WebSocket
  origin checks, self-only application CSP, an external ttyd transport observer,
  and terminal framing restrictions.
- Strictly bound and decode authenticated mutation bodies, reject malformed
  serialized origins, cap retained resize-viewer metadata and state, and
  reconcile existing authentication-secret permissions before use.
- Add snapshot capture timeout, scoped create-rate, per-login/process capture
  concurrency, bounded coalesced waiters, and process node-estimate budgets;
  also add visible bidi-control markers, content-free parser measurements,
  private ttyd socket modes, and compatible user-service sandboxing/core limits.
- Add digest-bound, short-lived single-use Paste tokens and stdin-only random
  tmux buffers with unconditional cleanup and literal bracketed-paste framing;
  reject invalid UTF-8, NUL, oversize, replayed, and stale Paste actions.
- Add Safari-first native History scrolling and selection, upward-wheel and
  PageUp entry with local trackpad inertia, and a tab-local
  `History`/`Application` scroll-gesture preference for mouse-reporting apps.
- Add a reviewed Paste flow with UTF-8 byte and logical-line counts, explicit
  multiline/control-character confirmation, a visible iOS system-Paste
  textarea fallback, and no automatic retry after denial, cancel, or failure.
- Separate transient `visualViewport` keyboard changes from stable layout and
  orientation resize dispatch so opening or closing the iOS keyboard does not
  resize the tmux grid or signal the terminal application.
- BREAKING: Replace remote tmux copy-mode scrolling and the standalone capture
  panel with bounded immutable local History snapshots, structured server-side
  ANSI parsing, progressive browser materialization, native selection/Copy,
  and a new-output indicator while the ttyd Live stream remains connected.
- BREAKING: Remove the legacy `/scroll` and `/capture` routes. Add opaque,
  login/viewer/generation-scoped History snapshot create/page/delete APIs with
  idle expiry and count, memory, line, byte, and ANSI-run limits.
- BREAKING: Replace canonical session names, raw tmux targets, and window
  indexes in browser routes and mutation payloads with opaque session, window,
  pane, and viewer references backed by pane-generation verification.
- BREAKING: Change the managed tmux window-size default from `smallest` to
  `manual` and replace the previous resize-source modes with `Fixed`, one-shot
  `Fit once`, and a disabled future `Follow this device` capability. Existing
  managed windows are reconciled without changing dimensions, and a
  session-local hook makes every newly linked managed window manual immediately.
- Set the exact managed tmux session history default to 50,000 lines before its
  durable user pane is created and during reconciliation without changing the
  global default used by unmanaged sessions. Bound History captures to 32
  MiB by default; increasing an existing pane limit does not restore discarded
  output.
- Add metadata-only terminal audit records and regression coverage proving that
  terminal/Paste canaries do not enter application or ttyd logging output.
- BREAKING: Make no-argument `control-agents` open an interactive managed-session selector instead of deriving a session name from the current directory.
- BREAKING: Restrict authenticated session JSON to browser-required fields;
  bridge PIDs, Unix socket paths, and tmux internals are no longer returned.
- BREAKING: Make managed registry identity canonical across the record ID,
  name, tmux session, and state-owned socket. Safe legacy display-name records
  migrate in place; records with unsafe identity mismatches are ignored rather
  than adopted.
- Replace the Bash SSH client with an architecture-specific Go binary that shares the managed-session lifecycle, supports direct named and register-only modes, and returns to the selector after tmux detach.
- Add authenticated same-origin managed-session create and terminate APIs with
  strict confirmation, bounded inputs, lifecycle serialization, and a
  configurable web creation limit.
- Add compact web Menu dialogs for creating `$HOME` sessions and explicitly
  confirming destructive termination, with deterministic tab/iframe
  reconciliation across clients.
- Recover missing `ttyd` bridges and migrate compatible legacy registry records
  during server startup/list reconciliation without recreating live tmux
  sessions or importing unmanaged sessions.
- Add a private stable forwarded SSH agent socket for managed tmux sessions,
  with atomic reconnect refresh, initial and future-pane inheritance, concise
  CLI availability status, and fake-socket regression coverage.
- Stage and verify architecture-matched Linux `amd64` and `arm64` Go client and
  server release assets with mandatory per-asset checksum verification in the
  user-local installer.

## 2026.5.21 - 2026-05-18

- Make `control-agents <name>` attach to the tmux session by default again; use `--no-attach` for register-and-exit scripts.

## 2026.5.20 - 2026-05-18

- Add user-service journal logging for stale session cleanup reasons.
- Include Homebrew/Linuxbrew binary paths in the managed systemd service `PATH`.

## 2026.5.19 - 2026-05-18

- Make `control-agents <name>` register web sessions and exit by default; use `control-agents --attach <name>` for local tmux attachment.

## 2026.5.18 - 2026-05-18

- Keep managed tmux sessions from being destroyed while unattached and clean stale `ttyd` processes before replacing a session socket.

## 2026.5.17 - 2026-05-18

- Change install/start instructions to restart the user service after install so existing running services pick up the new unit, binary, and state directory.
- Fix `install.sh` checksum verification by preserving release asset filenames until after `sha256sum` validation.
- Keep session registry entries alive based on the `ttyd` Unix socket and tmux session instead of relying on the stored `ttyd` PID.

## 2026.5.16 - 2026-05-18

- Publish the install simplification release from the fixed GitHub Actions workflow.
- Use Node 24-native GitHub Actions and disable empty Go cache restore warnings.

## 2026.5.15 - 2026-05-18

- BREAKING: Rename the public runtime namespace from Control Agents' earlier mirror naming to `control-agents`, including config/state paths and `CONTROL_AGENTS_*` environment variables.
- Add a user-local `install.sh` that installs GitHub Release binaries, writes `~/.config/control-agents/env`, and installs the user systemd service.
- Add a GitHub Actions release workflow that publishes Linux `amd64` and `arm64` binaries plus checksums for the installer.

## 2026.5.14 - 2026-05-18

- Add the GNU AGPL-3.0 license and security policy for public repository use.
- Document mobile touch display positioning and an installation simplification roadmap.

## 2026.5.13 - 2026-05-18

- Add a menu `Copy mode` toggle that opens a selectable tmux pane text capture and disables touch/wheel history gesture routing while it is open.
- Add a menu `Paste` action that reads clipboard text from an explicit click and pastes it into the active tmux pane through a bounded API.

## 2026.5.12 - 2026-05-17

- Add single-finger touch swipe history scrolling over the terminal iframe for iOS and other touch devices.
- Hide the right-side history scrollbar on coarse-touch devices while preserving horizontal terminal panning.
- Extend browser E2E coverage so terminal wheel, touch, and scrollbar controls all route through the tmux scroll API.

## 2026.5.11 - 2026-05-17

- Restrict the HTTPS server to TLS 1.3 only and add a regression test for the TLS version policy.
- Add a top-level installation guide for new systems, including `make install`, user systemd startup, lingering, and first mirrored session startup.
- Remove Makefile cleanup of legacy install paths so install, uninstall, and clean targets only remove currently managed files.

## 2026.5.10 - 2026-05-16

- Add a `Resize` management panel with Off, Automatic smallest, Follow web window, and Follow primary SSH/tmux modes.
- Add resize viewer heartbeats and state/API documentation so browser tabs can be selected by viewer ID, browser/IP, size, and last-seen time.
- Document and test that explicit web and primary resize modes use tmux manual sizing, smallest uses `window-size smallest`, and off avoids applying resize.
- Track the mobile visual viewport so the web terminal refits above the iOS software keyboard and sends transient heartbeats without changing tmux resize mode.

## 2026.5.9 - 2026-05-16

- Add a T-Control menu panel with tmux window listing and allowlisted window/pane control actions.
- Show a compact tmux window-count badge on session tabs only when a session has multiple internal tmux windows.

## 2026.5.8 - 2026-05-16

- Prefer the registered ttyd tmux client throughout web history scroll APIs, including copy-mode cleanup, so browser scrolling does not accidentally pan or repaint a smaller SSH tmux client.
- Default managed tmux windows to `window-size smallest` so fullscreen terminal apps render the same complete screen in web and SSH clients.
- Add Playwright coverage for mixed browser plus SSH-sized tmux clients to prevent fullscreen app top rows from being clipped.

## 2026.5.7 - 2026-05-16

- Keep terminal text selection working with tmux mouse mode off while routing browser wheel scrolling through the tmux history scrollbar API.
- Add Playwright browser E2E coverage for login, registered terminal tabs, special keys, logout, history buttons, and terminal iframe wheel scrolling.

## 2026.5.6 - 2026-05-16

- Fix repaint, copy-mode cleanup, and tmux status-line height handling when returning the terminal scrollbar to live bottom.

## 2026.5.5 - 2026-05-16

- Improve the right-side terminal scrollbar on small displays by including tmux client window overflow, not only tmux pane history.

## 2026.5.4 - 2026-05-16

- Added in-memory per-IP login rate limiting.
- Added same-origin protection for mutating routes and terminal WebSocket upgrades.
- Added security headers for Go-served web UI and API responses.

## 2026.5.3 - 2026-05-16

- Compact the web UI topbar for iOS Safari by moving `Keys` and `Sign out` into one menu.
- Keep login sessions valid across server restarts with a persistent auth secret and a 48-hour default cookie TTL.

## 2026.5.2 - 2026-05-16

- Display the running server version in the web UI.
- Rename the installed server binary to `control-agents-server` and the wrapper client to `control-agents`.
- Fix terminal tab switching when multiple session iframes are present.

## 2026.5.1 - 2026-05-16

- Added mobile-friendly special keys panel for terminal controls such as `Ctrl+C`, `Esc`, arrows, and `Enter`.
- Added `POST /api/sessions/{session}/keys` for sending allowlisted tmux keys server-side.
- Added calendar-versioning infrastructure with build metadata, `--version`, and `GET /api/version`.
