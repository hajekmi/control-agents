# Terminal Mirror Agent Notes

Use `README.md` for product, API, install, deploy, and operations
documentation. Keep this file focused on working rules for future agents.

## Code Style

- Write code, comments, identifiers, function names, variable names, and commit
  text in English.
- Keep implementation scoped and structured by the existing packages:
  `internal/config`, `internal/auth`, `internal/registry`, `internal/proxy`, and
  `internal/server`.
- Do not introduce a frontend build step unless the user explicitly asks for it.
  The UI is plain embedded HTML/CSS/JS.
- Keep the UI compact and operational. This is a terminal tool, not a marketing
  page.

## Runtime Boundaries

- `server` owns auth, API, static UI, session discovery, and proxying.
- `client_mirror` owns tmux session creation/attach, `ttyd` startup, and
  registry file creation.
- Do not expose per-session `ttyd` TCP ports. `ttyd` must stay behind Unix
  sockets in the shared state directory.
- Only wrapper-started sessions should appear in the UI unless the user changes
  that requirement.
- Preserve the default tmux `window-size largest` behavior unless changing it is
  the purpose of the task.

## Security Rules

- Do not log passwords, auth cookies, terminal input, or terminal output.
- Keep state directory permissions private: directories `0700`, registry files
  `0600`.
- Public HTTP/password-only mode is an accepted v1 product decision, but do not
  weaken the code further around auth, cookies, proxying, or socket exposure.
- If adding HTTPS support later, set or document `MIRROR_COOKIE_SECURE=true`.

## Tests

- Run `make test` after code changes.
- Run `make test-e2e` when changing `bin/client_mirror`, tmux behavior, `ttyd`
  startup, registry compatibility, or proxy routing.
- The Makefile intentionally sets workspace-local Go caches, `TMUX_TMPDIR`, cgo
  off, and `GOFLAGS=-buildvcs=false` for restricted execution environments.

## Repository Notes

- The execution environment may not be a usable git checkout. Do not assume
  `git status` or VCS stamping works.
- Do not remove `.cache/` handling from the Makefile; it exists because default
  home and `/tmp` paths may be read-only.
- Keep README and AGENT synchronized when renaming commands or changing runtime
  behavior.

