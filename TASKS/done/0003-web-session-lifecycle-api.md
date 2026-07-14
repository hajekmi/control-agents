# Web session create and terminate API

Status: done

Dependencies:

- `0001-managed-session-lifecycle.md`
- `0002-interactive-ssh-session-selector.md`

## Goal

Expose authenticated, same-origin API operations that create a managed session
in the service user's home directory and terminate an explicitly selected
managed session. Use the shared lifecycle implementation; never execute a
browser-provided command or working directory.

## Scope

1. Extend the authenticated sessions API with create and terminate operations.
2. Inject the shared lifecycle behind an interface so server unit tests can use
   fakes without starting tmux or `ttyd`.
3. Restrict public session JSON to browser-required fields rather than exposing
   bridge PIDs or Unix socket paths.
4. Add configuration and enforcement for a bounded number of web-created
   sessions.
5. Clear session-owned resize state when a managed session is terminated.
6. Add server, security, concurrency, and lifecycle integration tests.

## API contract

### List managed sessions

Keep:

```text
GET /api/sessions
```

The response continues to contain a `sessions` array. Each public session item
contains only fields needed by the web client:

- `id`
- `name`
- `cwd`
- `createdAt`
- optional `tmuxWindowCount` when greater than one

Do not expose `pid`, `socket`, or other bridge/process internals. Document this
as a breaking cleanup of the authenticated API response.

### Create or select a managed session

Add:

```text
POST /api/sessions
Content-Type: application/json

{"name":"backend"}
```

Behavior:

- Authenticate with the existing session cookie and enforce the existing
  same-origin policy.
- Accept a bounded JSON body, reject unknown fields, and require exactly one
  non-empty `name` string.
- Validate the canonical name with
  `[A-Za-z0-9][A-Za-z0-9._-]{0,63}`.
- Ignore any attempted client-provided command, shell, environment, tmux
  arguments, or working directory by rejecting unknown fields.
- Create new sessions in the configured service-user home directory.
- Return `201 Created` with `{ "created": true, "session": ... }` for a new
  managed session.
- Return `200 OK` with `{ "created": false, "session": ... }` when the same
  healthy managed session already exists. This lets the UI select it safely.
- Return `400 Bad Request` for malformed JSON or invalid names.
- Return `409 Conflict` when the name is owned by an unmanaged tmux session or
  when the configured web creation limit is reached.
- Return `502 Bad Gateway` or `503 Service Unavailable`, consistently
  documented and tested, when required local lifecycle dependencies cannot
  create a usable session/bridge.
- Do not return a success response until the managed record and terminal bridge
  are usable.

### Terminate a managed session

Add:

```text
DELETE /api/sessions/{sessionID}
Content-Type: application/json

{"confirmName":"backend"}
```

Behavior:

- Authenticate and enforce same-origin protection.
- Accept a bounded JSON body and reject unknown fields.
- Require `confirmName` to exactly match the canonical managed session ID in
  the path. The web UI confirmation remains mandatory, while this check also
  makes accidental generic DELETE calls less likely.
- Resolve only a managed session. Never terminate an unmanaged tmux session.
- Invoke the shared lifecycle termination primitive. This intentionally:
  - disconnects every SSH and web client attached to that tmux session,
  - kills the tmux session,
  - stops its verified `ttyd` bridge,
  - removes its registry and socket artifacts,
  - removes its persisted resize mode and active viewer records.
- Return `204 No Content` only when termination and safe cleanup have completed.
- Return `400 Bad Request` for an invalid ID/body or mismatched confirmation.
- Return `404 Not Found` when the managed session does not exist.
- Return a consistent `502`/`503` error for a local lifecycle failure that
  leaves work to reconciliation. Do not claim success while the tmux session is
  still live.
- A repeated DELETE after successful termination returns `404` and causes no
  side effects.

## Configuration

Add:

```text
CONTROL_AGENTS_MAX_SESSIONS
```

- Default: `32`.
- Accept only positive integers.
- It bounds creation through the web API to prevent accidental resource
  exhaustion. Existing sessions above the limit remain listable, attachable,
  and terminable.
- Direct local CLI behavior remains governed by the lifecycle and is not
  silently blocked by a web-only policy unless the implementation deliberately
  promotes this to one documented common limit.

## Security requirements

- Treat create and terminate as remote-shell administration actions.
- Preserve login rate limiting, authenticated API behavior, strict same-origin
  checks, security headers, TLS defaults, and secure cookie defaults.
- Never pass the browser name through a shell. Use typed lifecycle calls and
  argument-vector process execution only.
- Do not add arbitrary tmux commands, arbitrary cwd selection, environment
  injection, or a general process API.
- Do not log terminal input/output, request cookies, passwords, auth secrets,
  clipboard text, or confirmation bodies. Metadata logs may include the
  canonical session ID, action, result, and safe error.
- Preserve the terminal proxy's authenticated WebSocket and same-origin
  protections.
- Keep `ttyd` sockets private and do not expose their paths in the public API.

## Concurrency and consistency

- Simultaneous creates for the same name return one new session plus the same
  existing session result, never two tmux sessions or bridges.
- Create racing with terminate for the same name is serialized by the shared
  lifecycle and produces a documented deterministic result.
- Terminating the active session invalidates any in-flight session-specific API
  calls safely; they may return `404` but must not operate on another session.
- Session list calls during creation or termination never return a partially
  written registry record.
- Window count lookup failures must not make otherwise healthy sessions vanish.

## Material decisions

- The destructive operation is named **Terminate session**, not detach. Closing
  a browser, losing a WebSocket, or using `Ctrl-b d` remains nondestructive.
- Web creation accepts only a session name and always starts in `$HOME`.
- Session identity remains the strict canonical name; a separate display-name
  model is not introduced.
- Only managed sessions can be listed, created idempotently, or terminated.

## Out of scope

- Web dialogs and menu controls.
- SSH selector termination controls.
- Session rename, archive, restart, or import.
- Selecting a working directory or startup command from the browser.
- Persistent SSH agent behavior.

## References

- `TASKS/backlog/0001-managed-session-lifecycle.md`.
- `TASKS/backlog/0002-interactive-ssh-session-selector.md`.
- `AGENTS.md`, especially Security Rules and Runtime Boundaries.
- `README.md` API, security, resize, and configuration sections.
- `SECURITY.md`.
- `internal/server/server.go`.
- `internal/server/security.go`.
- `internal/server/resize_store.go`.
- `internal/config/config.go`.
- `internal/registry/registry.go`.
- `internal/tmux/tmux.go`.
- Existing server, API, resize, registry, proxy, and tmux tests.

## Acceptance criteria

- Authenticated same-origin clients can create a valid managed session with
  `POST /api/sessions`; its first pane starts in the configured `$HOME`.
- Duplicate managed creation returns the existing session and does not restart
  its healthy bridge.
- Malformed, oversized, unknown-field, invalid-name, unmanaged-conflict, and
  max-session requests return the documented errors without side effects.
- Unauthenticated and cross-origin create/terminate requests are rejected.
- Public list/create responses do not expose PID or socket paths.
- A correctly confirmed DELETE terminates tmux, disconnects attached clients,
  stops only the verified bridge, removes registry/socket/resize state, and
  returns `204`.
- Cancellation or an incorrect confirmation leaves the session untouched at
  the API level by never invoking lifecycle termination.
- Repeated deletion is harmless and returns `404`.
- Concurrent create/create and create/terminate tests demonstrate serialized,
  consistent results.
- Existing login, logout, terminal proxy, scroll, Copy/Paste, resize, key, and
  T-Control API tests remain green.
- `README.md` documents the new API and configuration.
- `SECURITY.md` identifies create and terminate as remote shell lifecycle
  operations.
- `make test` passes.
- `make test-e2e` passes when runtime dependencies are available.

## Validation

Run:

```sh
make test
make test-e2e
```

Record exact commands and results in the implementation summary.

## Implementation summary

- Added authenticated, same-origin managed-session lifecycle routes.
  `POST /api/sessions` strictly accepts one canonical `name`, creates sessions
  in the configured service-user home, returns an atomic created-versus-selected
  result, and maps unmanaged conflicts, web limits, and local lifecycle
  failures to the documented statuses. `DELETE /api/sessions/{sessionID}`
  requires an exact `confirmName`, invokes only the typed managed lifecycle,
  returns `204` after safe cleanup, and returns `404` when repeated.
- Added bounded 4 KiB lifecycle request decoding with malformed, extra-value,
  unknown-field, duplicate-field, wrong-type, oversized, and invalid-name
  rejection. Browser-provided commands, working directories, environment, and
  tmux arguments therefore cannot reach process execution.
- Replaced embedded registry records in list/create responses with an explicit
  public session shape containing only `id`, `name`, `cwd`, `createdAt`, and an
  optional multi-window count. Window-count failures leave healthy managed
  sessions visible.
- Added `CONTROL_AGENTS_MAX_SESSIONS`, default `32`, accepting only positive
  integers. A server-side create decision lock prevents concurrent web creates
  from exceeding the limit, while existing sessions remain selectable even
  above it and direct CLI creation remains governed only by the shared
  lifecycle.
- Extended the lifecycle with an atomic `CreateOrSelect` result and locked
  session-use and termination-cleanup helpers. Session-specific APIs now hold
  the per-session cross-process lifecycle lock so terminate/recreate races
  cannot redirect an in-flight action to a replacement session. Active resize
  viewers are forgotten while that same lock is held, and the lifecycle
  removes persisted resize, registry, socket, log, bridge, and tmux state
  before releasing it.
- Marked structurally invalid registry contents separately from operational
  registry failures. Lifecycle termination now returns `ErrNotFound` only for
  absent or invalid managed records, while filesystem reads and legacy-record
  migration writes return `ErrDependency`, producing the documented `503`
  without invoking bridge, tmux, artifact, or resize cleanup.
- Added fake-injected server coverage for authentication, same-origin policy,
  public JSON fields, strict bodies, typed error mapping, confirmation safety,
  repeated deletion, resize cleanup, creation limits, simultaneous creates,
  create/terminate ordering, and in-flight session serialization. Added
  lifecycle/config tests and a real HTTPS/tmux/ttyd E2E flow covering creation
  in `$HOME`, idempotent selection without bridge replacement, limit
  enforcement, confirmation rejection, full termination, and repeated `404`.
- Updated `README.md` with the API, response cleanup, configuration, and `503`
  behavior; updated `SECURITY.md` to classify create/terminate as remote-shell
  lifecycle operations; and recorded the public response cleanup as a breaking
  change in `CHANGELOG.md`.
- Validation completed on 2026-07-13:
  - `make test` — passed for all Go packages.
  - `make test-e2e` — passed; built both binaries and completed the real
    tmux/ttyd suite, including the new web lifecycle API test
    (`ok control-agents/test/e2e 4.442s`).
  - `make test-browser` — passed all 4 Chromium Playwright tests in 6.8s.
  - `env GOCACHE="$PWD/.cache/go-build" GOTMPDIR="$PWD/.cache/go-tmp" TMPDIR="$PWD/.cache/tmp" TMUX_TMPDIR="$PWD/.cache/tmux" CGO_ENABLED=1 GOFLAGS=-buildvcs=false go test -race ./internal/registry ./internal/session ./internal/server` — passed all three race-enabled packages.
