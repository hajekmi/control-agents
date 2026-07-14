# Session lifecycle integration, migration, and documentation

Status: done

Dependencies:

- `0001-managed-session-lifecycle.md`
- `0002-interactive-ssh-session-selector.md`
- `0003-web-session-lifecycle-api.md`
- `0004-web-session-lifecycle-ui.md`
- `0005-forwarded-ssh-agent-continuity.md`

## Goal

Validate the complete SSH-to-web managed-session lifecycle as one product,
close migration and release gaps left by the preceding scoped tasks, and make
all user, operator, security, and future-agent documentation describe the new
behavior consistently.

This task is integration and hardening work. It must not redesign the lifecycle
contracts approved in tasks 0001–0005.

## End-to-end product contract

After installation on a VM under one Unix user:

1. The user's `systemd --user` service runs `control-agents-server`.
2. An SSH login as that same Unix user can run `control-agents` and see only
   Control Agents managed tmux sessions.
3. The user can select an existing session or create a named session that starts
   in `$HOME`.
4. Detaching with `Ctrl-b d` returns to the selector and leaves the session
   running.
5. Logging out of SSH leaves tmux and its registered managed-session state
   running.
6. The authenticated web UI shows one top-level tab per managed tmux session.
7. The web Menu can create a `$HOME` session and can terminate the currently
   selected session only after explicit confirmation.
8. Termination destroys that tmux session, disconnects all SSH/web clients,
   stops its bridge, removes its state, and removes its web tab.
9. Losing or restarting a `ttyd` bridge or the web server does not destroy a
   still-live managed tmux session; reconciliation restores web access.
10. A forwarded Mac SSH agent can be reacquired by stable socket path after a
    later forwarded reconnect, but is unavailable while no forwarding SSH
    connection exists.

## Scope

1. Add or consolidate full lifecycle E2E coverage across CLI, server, API, web,
   tmux, and `ttyd`.
2. Test migration from registry records written by the pre-selector Bash
   wrapper.
3. Verify service restart and bridge recovery behavior.
4. Verify release assets, installer behavior, and both binary architectures.
5. Update all product, API, security, operations, workflow, and changelog text.
6. Remove obsolete tests and documentation only when superseded by the approved
   contracts.

## Required integration tests

### SSH/CLI lifecycle

- Start with no managed sessions and verify the selector empty state offers New
  session and Quit.
- Create two managed sessions through the client and verify both appear in a
  refreshed selector.
- Attach, detach, return to the selector, and attach the other session using one
  interactive terminal where a reliable pseudo-terminal helper is available.
- Verify both sessions survive selector quit and the end of the client process.
- Verify direct named and `--no-attach` modes still satisfy automation needs.
- Verify both first panes started in the configured home rather than the test
  process working directory.

### Web lifecycle

- Start the real Go server, authenticate, and observe SSH-created sessions as
  tabs.
- Create a session through the web Menu, verify it becomes active, and verify a
  real terminal iframe connects through the private `ttyd` socket.
- Verify an SSH client can attach to the web-created tmux session.
- Cancel termination and confirm every process and tab remains.
- Confirm termination and verify tmux, bridge, registry, socket, resize state,
  iframe, and tab are removed and attached clients are disconnected.
- Verify another remaining tab becomes active deterministically.

### Recovery and migration

- Create a managed session, kill only its `ttyd` process, reconcile/restart the
  server, and verify the same tmux session receives a new usable bridge.
- Restart the server while managed sessions remain live and verify tabs and
  terminal proxy access recover without recreating tmux sessions.
- Load a representative old registry file containing the prior PID/socket
  fields and verify it is accepted, reconciled, and migrated safely.
- Kill a tmux session externally and verify reconciliation removes stale bridge
  and registry artifacts.
- Ensure an unmanaged tmux session never appears or gets terminated during any
  migration/reconciliation path.

### Forwarded agent contract

- Use fake Unix sockets to verify stable-link inheritance and atomic retargeting
  without real keys.
- Verify the documented unavailable-while-disconnected state does not terminate
  or corrupt managed sessions.

## Documentation requirements

### `README.md`

Update at least:

- product overview and terminology,
- Quick Install next command (`control-agents`, not an obligatory named session),
- Source Install and Run Locally workflows,
- runtime/development requirements,
- client invocation and selector behavior,
- direct named and non-interactive compatibility,
- `$HOME` start-directory rule,
- managed-session versus tmux-window distinction,
- detach versus terminate semantics,
- server/client/shared-lifecycle ownership,
- state directory and revised registry semantics,
- create/list/terminate API contracts and public response fields,
- `CONTROL_AGENTS_MAX_SESSIONS`,
- startup reconciliation and bridge recovery,
- SSH agent forwarding continuity and limitations,
- operations and troubleshooting,
- the same-Unix-user assumption for SSH, tmux, `ttyd`, and the user service.

Remove or replace stale claims that:

- no-argument client names a session from the current directory,
- only the Bash wrapper can create managed sessions,
- missing `ttyd` PID/socket permanently makes a live tmux session disappear,
- release client assets are architecture-independent scripts.

### `SECURITY.md`

Document:

- browser session creation as remote shell creation,
- confirmed termination as a destructive remote shell lifecycle action,
- strict managed-name validation and lack of arbitrary command/cwd input,
- authenticated same-origin requirements,
- resource/session limits,
- public API omission of bridge internals,
- forwarded-agent exposure while connected,
- absence of forwarded-agent availability while disconnected,
- why Control Agents does not install or populate a persistent VM SSH agent,
- recommendation for separately managed scoped credentials for unattended jobs.

### `AGENTS.md`

Synchronize future-agent rules with the final architecture:

- shared Go lifecycle ownership,
- server reconciliation responsibility,
- Go client selector behavior,
- managed-only visibility,
- `$HOME` creation rule,
- detach and terminate distinction,
- web Menu create/terminate actions and confirmation,
- stable forwarded-agent path constraints,
- required validation commands for lifecycle, CLI, API, and browser changes.

Keep the existing security, terminal proxy, tmux resize, Copy/Paste, touch,
T-Control, and UI compactness invariants unless the completed tasks explicitly
changed them.

### `CHANGELOG.md`

Add concise `Unreleased` entries. Mark at least the no-argument/current-directory
client change and any registry/public API incompatibility with `BREAKING:`.

### Install, service, container, and release notes

- Ensure `install.sh`, `Makefile`, and the release workflow install/build matching
  Go client and server assets for Linux `amd64` and `arm64`.
- Ensure generated install guidance starts with the selector workflow.
- Keep `systemd --user` as the default service model and document that it must
  run as the same Unix account that owns the managed tmux sessions.
- Review `Containerfile`. Either make its lifecycle limitations explicit or
  update it consistently; do not imply that a server-only container can manage
  host SSH/tmux sessions without shared process/session namespaces and state.
- Keep default installation user-local and preserve private config/state modes.

## Material decisions

- One top-level Control Agents tab/selector item equals one managed tmux
  session, not a tmux window.
- Only Control Agents managed sessions are visible; there is no automatic tmux
  import.
- New sessions start in `$HOME` from both CLI and web.
- SSH/browser detach is nondestructive. `Terminate session` is destructive and
  confirmation-gated.
- The web termination action affects all clients attached to the selected
  session.
- The shared forwarded-agent symlink restores access after reconnect only; it
  does not provide offline credentials.

## Out of scope

- Importing arbitrary tmux sessions.
- Per-session Unix users or authorization roles.
- Session rename, scheduled termination, snapshots, or persistence across VM
  reboot beyond normal tmux process lifetime.
- A persistent VM SSH agent, private-key provisioning, deploy-key management,
  or secret manager integration.
- A frontend framework or build pipeline.
- Making a containerized server transparently control host tmux without an
  explicit future deployment design.

## References

- Tasks `0001` through `0005` in `TASKS/backlog/` or their later workflow
  locations.
- `TASKS/README.md`.
- `AGENTS.md`.
- `README.md`.
- `SECURITY.md`.
- `CHANGELOG.md`.
- `Makefile`.
- `install.sh`.
- `systemd/user/control-agents.service`.
- `Containerfile`.
- `.github/workflows/release.yml`.
- `test/e2e/e2e_test.go`.
- `test/playwright/app.spec.js`.
- All lifecycle, registry, tmux, server, proxy, API, and browser tests introduced
  or changed by the preceding tasks.

## Acceptance criteria

- The complete end-to-end product contract above is covered by automated tests
  where practical and by explicitly recorded manual verification only where a
  real interactive SSH transport is indispensable.
- Old registry records migrate without losing a live managed tmux session.
- Server/bridge restart recovery preserves session identity and terminal
  contents maintained by tmux.
- Create/detach/reattach/terminate behavior is consistent across CLI, API, and
  web UI.
- Unmanaged tmux sessions remain invisible and untouched.
- Installer and release workflows produce and install the architecture-matched
  Go client and server with valid checksums.
- README, SECURITY, AGENTS, CHANGELOG, service/install guidance, and container
  notes contain no material contradictions with the implemented behavior.
- No old documentation tells users that no-argument mode derives the current
  directory name.
- No documentation promises forwarded-agent availability while disconnected.
- Existing authentication, security headers, TLS, gzip boundaries, proxy,
  resize, scrollbar/touch, Copy/Paste, keys, and T-Control suites remain green.
- `make test`, `make test-e2e`, and `make test-browser` pass in an environment
  with their documented dependencies.

## Validation

Run:

```sh
make test
make build
make test-e2e
make test-browser
node --check internal/server/static/app.js
```

Inspect the built and release-staged client/server binaries for both supported
architectures. Record every exact command, result, skip, and unavoidable manual
check in the implementation summary.

## Implementation summary

- Consolidated the lifecycle integration coverage around the complete product
  contract. The real tmux/ttyd E2E suite now verifies the empty selector,
  creation of two `$HOME` sessions through one PTY client, detach-to-selector,
  switching to the other session, nondestructive quit, and survival of both
  sessions. The web lifecycle flow now keeps a real PTY Go client attached to a
  web-created session, proves that an incorrect confirmation leaves it
  attached, and proves that confirmed termination disconnects it and removes
  tmux, the bridge, registry, socket, log, and resize state.
- Added a real server restart and migration scenario using a representative raw
  pre-selector registry record and legacy ttyd attach command. Startup migrates
  the record and bridge, authenticated listing exposes only the managed
  session, and terminal proxy access succeeds. Killing the replacement bridge
  and restarting the server preserves the tmux creation identity and captured
  terminal content while creating a new usable bridge. Externally killing tmux
  removes stale artifacts, while an unmanaged tmux session remains invisible,
  live, and untouched throughout migration and reconciliation.
- Added installer integration tests for Linux `amd64`/`x86_64` and
  `arm64`/`aarch64` asset selection, matching client/server installation,
  selector-first generated guidance, private service configuration, and
  fail-closed checksum mismatch handling. Release downloads now require both
  selected assets to exist in the checksum manifest and verify successfully
  before either binary is installed.
- Added `make release-assets` as the common Linux `amd64` and `arm64` Go client
  and server staging path. The release workflow uses that target, verifies each
  staged ELF architecture and all four checksums, and uploads the matching
  assets. The installed and generated user service now uses `UMask=0077`, and
  installer guidance begins with the no-argument selector workflow.
- Updated `README.md`, `SECURITY.md`, `AGENTS.md`, `CHANGELOG.md`,
  `INSTALL_SIMPLIFICATION_PLAN.md`, the service guidance, and `Containerfile`.
  The documentation now consistently defines managed sessions versus tmux
  windows, `$HOME` creation, detach versus destructive confirmed termination,
  shared lifecycle ownership, durable registry and recoverable bridge
  semantics, public API fields, limits, restart recovery, same-Unix-user
  operation, forwarded-agent disconnect limitations, and the server-only
  container boundary. The changelog records the no-argument, public response,
  and canonical registry changes with `BREAKING:` markers.
- Review follow-up hardened bridge process ownership and cleanup. Every ttyd
  child started by the Go lifecycle now has a goroutine calling `Cmd.Wait`, so
  a long-running server does not retain exited bridge children as zombies.
  Stop paths for Go-owned children wait for that goroutine and report an
  unreaped child as a cleanup failure rather than success. A verified legacy
  bridge owned by another process is considered stopped once it has
  definitively exited, including while it remains a zombie that the Go server
  cannot reap. E2E process-exit assertions for Go-owned children likewise
  require `/proc/<pid>` to disappear instead of accepting process state `Z`.
- Bridge ownership verification now requires `/proc/<pid>/exe` to identify the
  same executable inode as the configured ttyd binary, in addition to the
  existing pidfd, exact ttyd argv, managed Unix socket, terminal base path, and
  tmux target checks. A process that spoofs `argv[0]` and every managed bridge
  argument but runs another executable is classified as unrelated and is not
  signaled. Regression tests cover spoofed argv, deliberate unreaped-zombie
  rejection, and reaping of an early-exiting managed child. Legacy and current
  real ttyd migration/recovery remain covered and passing.
- Corrected the README configuration ownership: `CONTROL_AGENTS_APP_NAME`,
  `CONTROL_AGENTS_TMUX_WINDOW_SIZE`, `CONTROL_AGENTS_TMUX_MOUSE`, and
  `CONTROL_AGENTS_WEB_SCROLLBACK_LINES` are shared lifecycle settings read by
  both server and client, while attach/no-attach controls remain client-only.
- Validation completed on 2026-07-13:
  - `sh -n install.sh` — passed.
  - `node --check internal/server/static/app.js` — passed.
  - `node --check test/playwright/app.spec.js` — passed.
  - `git diff --check` — passed.
  - `make test` — passed for every Go package, including the new installer
    integration package; the final rerun also passed.
  - `make build` — passed and built both native Go commands.
  - `RUN_E2E=1 go test -count=1 -run 'TestServerRestartMigratesRegistryRecoversBridgeAndLeavesUnmanagedTmuxUntouched|TestClientSelectorCreatesAttachesDetachesAndReturnsToPrompt' -v ./test/e2e`
    — passed both new targeted scenarios (`ok`, 0.696s).
  - `RUN_E2E=1 go test -count=1 -run TestWebLifecycleAPICreatesSelectsLimitsAndTerminatesRealSession -v ./test/e2e`
    — passed the extended web/direct-client scenario (5.036s) before the full
    suite run.
  - The first full `make test-e2e` run failed in
    `TestWebLifecycleAPICreatesSelectsLimitsAndTerminatesRealSession` after 30s
    while waiting for the new PTY client to appear. Early-exit transcript
    diagnostics under the Makefile's exact `TMUX_TMPDIR`, cache, cgo, and
    `GOFLAGS` environment then showed that util-linux `script` inherited EOF,
    logged out of tmux, and exited before the assertion. The fixture was fixed
    by retaining a stdin pipe for the attached client and by reporting any
    premature exit directly.
  - `TMUX_TMPDIR="$PWD/.cache/tmux" GOCACHE="$PWD/.cache/go-build" GOTMPDIR="$PWD/.cache/go-tmp" TMPDIR="$PWD/.cache/tmp" CGO_ENABLED=0 GOFLAGS=-buildvcs=false RUN_E2E=1 go test -count=1 -run TestWebLifecycleAPICreatesSelectsLimitsAndTerminatesRealSession -v ./test/e2e`
    — passed after the PTY fixture fix (`ok`, 0.261s).
  - `make test-e2e` — final full rerun passed with real tmux and ttyd
    (`ok control-agents/test/e2e 1.905s`).
  - `make test-browser` — passed all 5 Chromium tests in 15.3s, including
    authentication, terminal proxying, resize, scroll/touch, Copy/Paste,
    special keys, T-Control, lifecycle dialogs, termination, and tab fallback.
  - `make release-assets VERSION=2026.7.1-test COMMIT=validation BUILD_DATE=2026-07-13T00:00:00Z`
    — passed and staged four binaries plus `sha256sums.txt`.
  - `file dist/control-agents-server-linux-amd64 dist/control-agents-linux-amd64 dist/control-agents-server-linux-arm64 dist/control-agents-linux-arm64`
    — reported statically linked x86-64 ELF files for both `amd64` assets and
    ARM aarch64 ELF files for both `arm64` assets.
  - `(cd dist && sha256sum -c sha256sums.txt)` — verified all four assets as
    `OK`.
  - `make clean` — removed the generated native binaries and release staging
    after validation.
- Review follow-up validation completed on 2026-07-13:
  - `go test -count=1 -run 'TestStopDoesNotKillProcessWithSpoofedBridgeArguments|TestStopRequiresGoOwnedZombieToBeReaped|TestBridgeReportsEarlyTtydExitAsIncompleteStartup|TestBridgeCommandVerificationDistinguishesCurrentAndLegacyCommands' -v ./internal/session`
    — passed all focused process-identity/reaping regressions
    (`ok control-agents/internal/session 1.031s`).
  - `make build >/dev/null && RUN_E2E=1 go test -count=1 -run 'TestReconcileMigratesLegacyBridgeToSingleEnvironmentPreservingProcess|TestLifecycleCreatesAndTerminatesRealManagedSession|TestServerRestartMigratesRegistryRecoversBridgeAndLeavesUnmanagedTmuxUntouched' -v ./test/e2e`
    — passed bridge termination/reaping, legacy migration, server restart,
    terminal-content preservation, and unmanaged-session isolation
    (`ok control-agents/test/e2e 0.680s`). The legacy migration fixture starts
    `ttyd` in the background beneath an exec/long-lived wrapper, verifies the
    parent relationship, and leaves the stopped legacy child as an externally
    owned zombie throughout successful reconciliation. The test reaps only its
    wrapper; it does not reap the legacy child before reconciliation.
  - `make test` — passed all Go packages; `internal/session` completed in
    1.135s and the installer integration package remained green.
  - `make build` — passed for both native Go commands.
  - `node --check internal/server/static/app.js` — passed.
  - `sh -n install.sh` — passed.
  - `git diff --check` — passed.
  - `make test-e2e` — final rerun passed the complete real tmux/ttyd suite with
    ownership-aware process assertions (`ok control-agents/test/e2e 1.976s`).
  - `make test-browser` — final rerun passed all 5 Chromium tests.
  - `node --check test/playwright/app.spec.js` — final rerun passed.
- Explicit manual-check limitations: no installed/current deployment was
  installed, restarted, or modified. A real network SSH transport and a real
  forwarded Mac private key were not used; util-linux PTYs exercise the
  interactive client contract, and fake Unix agent sockets cover inheritance,
  disconnect, and retargeting without keys. The cross-compiled `arm64` binaries
  were inspected as AArch64 ELF files and checksum-verified but were not run on
  physical arm64 hardware. These are the remaining environment-specific manual
  deployment checks; no automated test was skipped in the documented local
  validation environment.
