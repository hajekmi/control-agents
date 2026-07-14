# Managed session lifecycle foundation

Status: done

Dependencies: none

## Goal

Introduce one durable, concurrency-safe lifecycle implementation for Control
Agents managed tmux sessions. The lifecycle must be reusable by the server and
the client work planned in later tasks, and it must make the tmux session—not a
particular `ttyd` process—the persistent unit shown in the SSH selector and in
the web tab row.

## Definitions

- A **managed session** is a tmux session created and recorded by Control
  Agents. One managed session maps to one SSH selector entry and one top-level
  web tab.
- A tmux **window** or **pane** remains an object inside a managed session. It
  does not become a top-level Control Agents tab.
- A **terminal bridge** is the per-session `ttyd` process and private Unix
  socket used by the web proxy.
- **Detach** disconnects a client while leaving the managed tmux session alive.
- **Terminate** destroys the managed tmux session, disconnects all clients,
  stops its terminal bridge, and removes its persistent record.

## Current behavior

`bin/control-agents` currently owns tmux creation, `ttyd` startup, and registry
file creation. A registry record is considered dead when its stored `ttyd` PID
or socket is unavailable, even when the tmux session still exists. The Go
server only discovers registry records and cannot create, recover, or terminate
managed sessions. Concurrent server and CLI operations have no shared lifecycle
lock.

## Scope

1. Add a shared Go lifecycle package, preferably `internal/session`, that
   composes the existing `internal/registry` and `internal/tmux` behavior.
2. Define and implement lifecycle operations for listing, creating, ensuring,
   reconciling, and terminating managed sessions.
3. Change registry semantics so a live managed tmux session survives a missing
   or restarted `ttyd` bridge.
4. Wire server startup and the existing session listing path through the new
   lifecycle sufficiently to reconcile old records without changing the web UI
   or adding new API routes in this task.
5. Add unit and real-process coverage for lifecycle and recovery behavior.
6. Update the runtime-boundary rules in `AGENTS.md` to describe the shared
   lifecycle owner. Do not leave the old wrapper-only ownership rule in place.

## Required behavior

### Managed-session identity

- Managed session names and IDs are the same canonical value.
- Accept only names matching
  `[A-Za-z0-9][A-Za-z0-9._-]{0,63}`.
- Do not silently sanitize an invalid name or allow two user inputs to collapse
  to the same ID.
- Do not automatically expose or adopt tmux sessions that lack a valid Control
  Agents registry record.
- If an unmanaged tmux session already uses a requested name, creation must
  return a distinct conflict error and must not modify or expose that session.

### Creation

- New sessions start in the service user's home directory obtained explicitly
  by the lifecycle configuration. The home directory must be injectable in
  tests; it must not be inferred from the caller's current directory.
- Creation must preserve these managed tmux defaults unless a documented
  existing environment override applies:
  - `destroy-unattached off`
  - window size mode `smallest`
  - mouse mode `off`
  - `status-left-length` of `80`
  - `status-left` containing the managed session label
  - `status-right` containing `#{pane_current_path}`
- Creating an already-existing, healthy managed session is idempotent and
  returns that session rather than creating another tmux session or bridge.
- Registry files are written atomically with mode `0600`. State, socket, log,
  lock, and other private directories use mode `0700`.

### Terminal bridge

- Keep one `ttyd` bridge per managed session.
- Continue using a private Unix socket under the shared state directory. Do not
  expose a per-session TCP port.
- Preserve the `ttyd` base path `/terminal/{sessionID}`, writable terminal mode,
  and `CONTROL_AGENTS_WEB_SCROLLBACK_LINES` behavior.
- Preserve per-session `ttyd` logs without logging terminal input, terminal
  output, cookies, passwords, or authentication secrets through the Go server.
- Starting or replacing a bridge must wait for the Unix socket with a bounded
  timeout and report a useful error when `ttyd` fails.
- A bridge replacement may stop only a process verified to be the bridge for
  that managed session and socket. A stale or reused PID must never cause an
  unrelated process to be killed.

### Durable registry and compatibility

- A registry record represents desired managed-session state. A missing bridge
  PID or socket is a recoverable condition while the tmux session exists.
- Runtime bridge metadata may be updated in the registry, but it must not be
  the sole reason that a managed session is forgotten.
- Read the existing registry JSON format produced by the Bash wrapper. Existing
  valid records and tmux sessions must be reconciled without requiring users to
  recreate them.
- When the tmux session is gone, reconciliation cleans up its stale bridge,
  socket, resize state where available, and registry record.
- Invalid registry files remain ignored safely and produce metadata-only logs;
  their contents and terminal data must not be logged.

### Reconciliation and concurrency

- On server startup, reconcile every valid managed-session record before it is
  returned by `GET /api/sessions`.
- For a live tmux session with a dead or missing bridge, start a replacement
  bridge and update the runtime metadata.
- For a missing tmux session, clean up the stale managed record and bridge
  artifacts.
- Creation, bridge replacement, reconciliation, and termination for the same
  session must be serialized across server and CLI processes, not only between
  goroutines in one process.
- Operations for different sessions should not require one global long-held
  lock.
- Interrupted creation must not leave a registry file that reports a usable
  session when tmux creation failed.

### Lifecycle API for later tasks

Expose an internal interface that later server and client tasks can fake in
unit tests. It must cover at least:

- `List`
- `Create`
- `EnsureBridge`
- `Reconcile`
- `Terminate`

Return typed errors for invalid names, unmanaged-name conflicts, missing
managed sessions, dependency failures, and incomplete bridge startup. Do not
make HTTP status codes or interactive prompt behavior part of this package.

### Termination primitive

Implement the internal termination operation, but do not expose it through the
web API or UI in this task.

Termination must:

1. Acquire the session lifecycle lock.
2. Verify that the target is a managed session.
3. Stop only its verified `ttyd` bridge.
4. Kill the tmux session, thereby disconnecting attached SSH and web clients.
5. Remove its socket, registry record, and session-owned transient state.
6. Tolerate components that have already exited while still completing safe
   cleanup.

## Material decisions

- Only Control Agents managed sessions appear in either client. Arbitrary tmux
  sessions are intentionally excluded from this project phase.
- The Go lifecycle package becomes the authoritative implementation. The
  server and the future Go CLI may both call it.
- The server is responsible for startup reconciliation and may own bridges it
  creates. Bridge loss during a service restart is recoverable because the tmux
  session and durable registry remain.
- No frontend framework or build step is introduced.
- Do not add arbitrary tmux command execution or a general process-execution
  API.

## Out of scope

- Interactive SSH selection.
- Web create or terminate routes.
- Web dialogs and menu actions.
- Automatic import of unmanaged tmux sessions.
- Renaming a managed session.
- A persistent SSH agent or forwarded-agent socket handling.
- A session termination button exposed to users.

## References

- `AGENTS.md`, especially Runtime Boundaries and Security Rules.
- `README.md`, especially Requirements, Configuration, API, resize behavior,
  security, and troubleshooting.
- `SECURITY.md`.
- `bin/control-agents`.
- `internal/registry/registry.go` and its tests.
- `internal/tmux/tmux.go` and its tests.
- `internal/server/server.go` and `internal/proxy/proxy.go`.
- `test/e2e/e2e_test.go`.

## Acceptance criteria

- A new lifecycle unit creates a managed tmux session in the configured home
  directory with the required tmux options, bridge, socket, log, and atomic
  registry record.
- Two concurrent creators for one name produce one managed session and one
  usable bridge.
- Repeating create for an existing managed session returns the existing
  session without replacing a healthy bridge.
- An unmanaged tmux session with the same name is left untouched and reported
  as a conflict.
- Killing only the `ttyd` bridge and restarting or reconciling the server
  restores web access without losing or recreating the tmux session.
- Killing the tmux session removes its stale managed record and bridge
  artifacts during reconciliation.
- Existing valid registry files from the Bash wrapper are accepted and
  reconciled.
- Termination disconnects clients, removes the tmux session, stops the verified
  bridge, and removes persistent/transient session state.
- No unrelated process can be killed through stale bridge metadata.
- State permissions and Unix-socket-only exposure remain compliant with
  `AGENTS.md` and `SECURITY.md`.
- `AGENTS.md` describes the new shared lifecycle boundary.
- `make test` passes.
- `make test-e2e` passes when `tmux` and `ttyd` are available.

## Validation

Run:

```sh
make test
make test-e2e
```

Record the exact commands and results in the implementation summary.

## Implementation summary

- Added `internal/session` as the shared lifecycle owner with typed errors,
  per-session cross-process locks, managed tmux creation, bridge ensuring,
  reconciliation, termination, and an interface suitable for later fakes.
- Made registry records atomic and durable, bound canonical records to one ID,
  name, tmux name, and state-owned socket, and added a narrow migration for
  historical display-only `name` values. Unsafe legacy identities remain
  ignored and are never adopted.
- Added Linux pidfd-based verified bridge signaling. Both Go and the existing
  Bash client open a stable process handle before verifying the exact ttyd
  executable, Unix socket, base path, and tmux target, so PID reuse cannot
  redirect TERM or KILL to another process.
- Updated the Bash client to share lifecycle locks, strictly parse canonical
  registry JSON, preserve durable metadata on healthy reuse and PID-only bridge
  recovery updates, reject invalid names without sanitizing, and report
  unmanaged-name conflicts. Every embedded Python helper runs in isolated mode
  so caller-directory modules cannot shadow standard-library imports.
- Wired server startup and `GET /api/sessions` listing through reconciliation,
  while preserving the existing API and UI surface. Failed bridge stops and
  tmux checks or kills retain the durable record for a safe retry.
- Updated `AGENTS.md` and `README.md` for the shared lifecycle boundary,
  canonical managed-session behavior, and runtime dependencies. The installer
  now checks flock, Python 3.9+ pidfd APIs, and kernel pidfd support.
- Validation completed on 2026-07-13:
  - `bash -n bin/control-agents` — passed.
  - `sh -n install.sh` — passed.
  - `make test` — passed for all Go packages.
  - `make test-e2e` — passed with real tmux/ttyd creation, recovery,
    reconciliation, termination, wrapper metadata preservation, malformed
    registry rejection, and unmanaged conflict coverage.
