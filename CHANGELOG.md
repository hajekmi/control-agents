# Changelog

Versions use calendar numbering: `YYYY.M.REVISION`.

## 2026.5.1 - 2026-05-16

- Added mobile-friendly special keys panel for terminal controls such as `Ctrl+C`, `Esc`, arrows, and `Enter`.
- Added `POST /api/sessions/{session}/keys` for sending allowlisted tmux keys server-side.
- Added calendar-versioning infrastructure with build metadata, `--version`, and `GET /api/version`.
