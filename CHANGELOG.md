# Changelog

Versions use calendar numbering: `YYYY.M.REVISION`.

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
