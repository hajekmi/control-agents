# Forwarded SSH agent continuity across tmux detach

Status: done

Dependencies:

- `0001-managed-session-lifecycle.md`
- `0002-interactive-ssh-session-selector.md`

## Goal

Allow managed tmux shells to regain access to a newly forwarded Mac SSH agent
after an SSH disconnect and reconnect, without storing the user's private key
on the VM. Make the limitation explicit: no forwarded agent is available while
the forwarding SSH transport is disconnected.

## Background

SSH agent forwarding delegates signing over a Unix socket tied to one SSH
connection. When that connection closes, the forwarded socket disappears. A
later `ssh -A` connection usually creates a different socket path. Existing
shells inside tmux keep their original `SSH_AUTH_SOCK` environment value, so
tmux detach/reattach alone does not reliably repair agent access in an old
pane.

An agent running permanently on the VM cannot retain the Mac's forwarded
identity after disconnect because forwarding does not transfer the private key.
A persistent VM agent would need a separate private key or credential loaded on
the VM and has a different security model.

## Chosen design

Use one stable, private symlink owned by Control Agents, for example:

```text
$CONTROL_AGENTS_STATE_DIR/agent/forwarded.sock
    -> /path/from/current/forwarded/SSH_AUTH_SOCK
```

Managed tmux sessions receive the stable path as `SSH_AUTH_SOCK`. Whenever the
interactive `control-agents` client runs inside an SSH connection with a valid
forwarded agent socket, it atomically retargets the stable symlink. Existing
managed shells created with the stable environment value therefore work again
after reconnect without changing their process environment.

## Scope

1. Add a small agent-forwarding helper to the shared lifecycle/client code.
2. Atomically refresh the stable socket link from valid SSH invocations.
3. Ensure every newly created managed tmux session and its future windows/panes
   inherit the stable `SSH_AUTH_SOCK` path.
4. Expose concise CLI status when forwarding is present, absent, or invalid.
5. Document security properties, reconnect behavior, multiple-client behavior,
   and migration limitations.
6. Add unit and tmux E2E coverage without requiring a real private key.

## Required behavior

### Detecting a forwarded agent

- Consider refreshing the link only when the client is running in an SSH
  context, indicated by standard SSH connection/TTY environment metadata, and
  `SSH_AUTH_SOCK` names an existing Unix socket.
- Do not treat a regular file, directory, missing path, or the stable symlink
  itself as a valid forwarded socket target.
- Do not fail selector/session use merely because SSH agent forwarding is not
  enabled. Report agent unavailability concisely and continue.
- Direct named interactive invocation refreshes the link before creating or
  attaching. The no-argument selector refreshes it before listing/attaching.
- Non-interactive `--no-attach` may refresh only when the same SSH-context and
  socket checks pass; it must not assume automation has an agent.

### Stable link

- Place the link under a dedicated mode-`0700` directory inside the private
  state directory.
- Replace it atomically so concurrent readers see either the previous valid
  link or the new valid link, never a partially written text path.
- Do not copy, proxy, inspect, or persist agent protocol traffic.
- Do not log the agent protocol, identities, key material, signatures, or raw
  socket contents. Avoid logging the transient target path at normal levels.
- Multiple simultaneous forwarded SSH connections use last-successful-refresh
  semantics. Document that one VM user has one current forwarded identity for
  all managed sessions.
- When the SSH connection closes, the link may temporarily point to a missing
  socket. This is expected and must not delete or terminate any session.

### Tmux environment

- Set the managed session's `SSH_AUTH_SOCK` to the stable link path before its
  initial shell starts.
- Store the same value in the tmux session environment so later tmux windows
  and panes, including those created by T-Control, inherit it.
- Do not change the global environment of unrelated/unmanaged tmux sessions.
- Do not overwrite unrelated user environment variables.
- Refreshing the symlink must be enough for already-running shells that were
  originally created with the stable path; do not inject commands or keystrokes
  into a user's shell.
- Existing pre-feature panes whose process environment contains an old direct
  forwarded path cannot be repaired safely in place. Document that new panes
  inherit the stable path and that users may export the documented stable path
  manually in an old pane if needed.

### Availability semantics

Document and test these states:

1. While the forwarding SSH connection is alive, managed shells can use the
   forwarded agent through the stable link.
2. `Ctrl-b d` alone does not break forwarding if the owning SSH transport stays
   alive.
3. After SSH logout or transport loss, agent operations fail until another
   valid forwarded connection refreshes the link.
4. After reconnect and `control-agents` invocation, existing managed shells
   that use the stable path regain agent access.
5. A web-created session can be configured with the stable path even when no
   forwarding socket currently exists; it gains access after a later valid SSH
   refresh.

## Security requirements

- Do not start a persistent `ssh-agent` service on the VM.
- Do not copy or generate a private SSH key for the user.
- Do not attempt to extract keys from a forwarded agent; the protocol does not
  provide private key export.
- Explain that any process running as the same VM user can request signatures
  from the forwarded agent while forwarding is connected. Control Agents does
  not create isolation from the service user's own processes.
- Recommend a separately managed, narrowly scoped deploy key, workload
  identity, or token when detached background jobs must authenticate while the
  Mac is offline.
- Keep all state permissions aligned with `AGENTS.md` and `SECURITY.md`.

## Material decisions

- Agent forwarding continuity means recovery after reconnect, not continuous
  availability while disconnected.
- One stable forwarded agent socket is shared by all managed sessions for the
  same Unix account.
- The most recently refreshed valid SSH connection wins when several clients
  forward different agents.
- Persistent VM credentials are explicitly outside Control Agents core
  behavior.

## Out of scope

- Running or supervising a permanent VM `ssh-agent`.
- Loading private keys, prompting for key passphrases, or managing deploy keys.
- Supporting separate forwarded identities per managed session.
- Agent forwarding across different Unix users.
- Keeping the Mac SSH transport alive through ControlMaster or another tunnel.

## References

- `TASKS/backlog/0001-managed-session-lifecycle.md`.
- `TASKS/backlog/0002-interactive-ssh-session-selector.md`.
- `AGENTS.md` Security Rules and Runtime Boundaries.
- `README.md` configuration, security, and operations sections.
- `SECURITY.md`.
- Shared lifecycle tmux creation code introduced by task 0001.
- Interactive client code introduced by task 0002.
- `internal/tmux/tmux.go`, especially session/window/pane creation behavior.

## Acceptance criteria

- A managed session's initial shell and newly created windows/panes expose the
  stable Control Agents `SSH_AUTH_SOCK` value.
- A valid forwarded socket is linked atomically when `control-agents` runs in an
  SSH context.
- Invalid, absent, non-socket, and self-referential values do not replace a
  previously valid link and do not block session selection.
- Repointing the stable link makes a test agent socket reachable from an
  already-running managed shell whose environment has not changed.
- Link refresh does not mutate unrelated tmux sessions or inject terminal
  commands.
- Concurrent refresh tests demonstrate a valid final symlink without partial
  state.
- State permissions remain private and logs contain no agent/key material.
- Documentation clearly states that forwarding is unavailable after SSH logout
  until reconnect, and recommends scoped VM credentials for unattended work.
- `make test` passes.
- `make test-e2e` covers stable-link inheritance and reconnect-style retargeting
  using a fake Unix socket when tmux is available.

## Validation

Run:

```sh
make test
make test-e2e
```

Record exact commands and results in the implementation summary.

## Implementation summary

- Added a shared forwarded-agent helper under `internal/session` that recognizes
  standard SSH connection metadata, accepts only an existing Unix
  `SSH_AUTH_SOCK`, rejects missing, non-socket, direct stable-link-self values,
  and aliases whose symlink chain passes through the stable link, and atomically
  replaces the private
  `$CONTROL_AGENTS_STATE_DIR/agent/forwarded.sock` symlink. Invalid or absent
  forwarding leaves the previous link unchanged, concurrent valid refreshes
  retain a complete valid link, and no agent protocol or transient socket path
  is logged.
- Updated managed tmux creation to pass the stable path into the initial shell
  and store it in the session-scoped tmux environment for future windows and
  panes. Reconciliation or selection of an existing managed session updates
  only that session's `SSH_AUTH_SOCK`; it does not modify tmux global state,
  unmanaged sessions, or running pane process environments.
- Added tmux `attach-session -E` to both direct SSH client and ttyd/browser
  bridge attachments so tmux cannot overwrite or unset the managed session
  environment from an attaching client's transient environment. Bridge
  reconciliation distinguishes the current form from a legacy command that
  exactly matches the managed ttyd name, socket, base path, and tmux target;
  it uses the existing pidfd verification to stop the legacy process before
  replacing its socket, never treats a mismatched process as owned, and leaves
  exactly one current bridge. Reconciliation restores the stable tmux
  environment after retiring a legacy bridge that could update it once during
  its final readiness check.
- Made the Go client refresh forwarding before direct creation/attachment or
  selector listing and report concise `available`, `unavailable`, or `invalid`
  status on stderr. Register-only stdout remains the canonical session ID, and
  forwarding failures do not block managed-session use.
- Added unit coverage for SSH-context detection, invalid-input preservation,
  private permissions, stable-link retargeting and reachability, concurrent
  refreshes, indirect self-reference preservation, CLI
  ordering/status/redaction, environment-preserving direct and bridge attach
  commands, and targeted tmux environment commands. Added real tmux/ttyd E2E
  coverage using two fake Unix sockets for initial inheritance, direct attach
  preservation, new window/pane inheritance while attached, disconnect
  unavailability, and reconnect retargeting while an existing pane remains
  alive. Real-process migration coverage starts a legacy ttyd, verifies its PID
  exits during reconciliation, confirms exactly one `-E` bridge remains, and
  checks that a later pane inherits the restored stable path. Web-created
  sessions are also verified to use the stable path before any forwarding link
  exists.
- Documented transport-bound availability, `Ctrl-b d` behavior, reconnect
  recovery, last-successful-refresh semantics across multiple SSH clients,
  pre-feature pane migration, same-user signing access, absence of persistent
  key storage, and scoped credentials for unattended work in `README.md`,
  `SECURITY.md`, `AGENTS.md`, and `CHANGELOG.md`.
- Final validation completed on 2026-07-13:
  - `make test` — passed for all Go packages.
  - `make test-e2e` — passed with real tmux/ttyd and fake-socket inheritance,
    environment-preserving attach, later window/pane creation, disconnect, and
    reconnect-retarget coverage, plus verified legacy bridge migration to one
    current bridge.
  - `git diff --check` — passed.
  - `make clean` — removed generated client and server binaries after
    validation.
