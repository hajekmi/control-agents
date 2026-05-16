# Changelog

Versions use calendar numbering: `YYYY.M.REVISION`.

## Unreleased

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
