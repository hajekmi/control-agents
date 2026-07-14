# Interactive SSH session selector

Status: done

Dependencies: `0001-managed-session-lifecycle.md`

## Goal

Replace the current no-argument, current-directory behavior with an
interactive SSH workflow that lists Control Agents managed tmux sessions,
allows selecting or creating one, attaches the current terminal, and returns to
the selector after the user detaches. Preserve explicit non-interactive entry
points for tests and automation.

## User experience

Running `control-agents` in an interactive SSH terminal shows a compact numbered
selector similar to:

```text
Control Agents sessions

1) backend
2) main
n) New session
q) Quit

Select:
```

Selecting a session attaches the current terminal to it. `Ctrl-b d` detaches
only that SSH tmux client; the tmux session and web terminal remain alive. After
tmux returns, refresh and show the selector again so one SSH connection can
move among multiple managed sessions. `q` returns to the normal SSH shell.

## Scope

1. Add a Go client command, such as `cmd/client`, which uses the shared session
   lifecycle from task 0001.
2. Replace the installed Bash client with the built Go `control-agents` binary.
3. Implement the interactive selector, new-session prompt, attach/detach loop,
   and explicit quit behavior.
4. Preserve direct named and non-interactive compatibility where specified
   below.
5. Update build, install, release, and test paths so both server and client are
   real architecture-specific Go binaries.

## Required behavior

### Interactive invocation

- `control-agents` with no positional name requires an interactive terminal and
  opens the selector.
- List only live Control Agents managed sessions. Do not list or adopt arbitrary
  tmux sessions.
- Refresh the list before every prompt and after every tmux attach command
  returns.
- Use stable, deterministic ordering. Prefer the same order returned by the
  shared lifecycle unless the implementation documents a single common sort
  order for both CLI and web.
- Numbered selection attaches the chosen tmux session with inherited stdin,
  stdout, stderr, terminal size, and signals.
- The client process must wait for tmux rather than replacing itself, so tmux
  detach returns to the selector loop.
- If the session is terminated by another client while attached, report a
  concise message, refresh the list, and keep the selector usable.
- Show a short `Ctrl-b d` detach hint before attaching. Do not modify the user's
  global tmux prefix or configuration.
- `q`, `quit`, EOF, or an interrupt at the selector exits cleanly without
  terminating any managed session.

### New session flow

- `n` or `new` prompts for a session name.
- Apply the shared strict name rule
  `[A-Za-z0-9][A-Za-z0-9._-]{0,63}` and show a clear validation error without
  exiting the selector.
- An empty name cancels creation and returns to the selector.
- A newly created session starts in the configured user's `$HOME`, not in the
  caller's current directory.
- After successful creation, attach it immediately.
- If the name already identifies a healthy managed session, select and attach
  that session.
- If an unmanaged tmux session owns the name, show the lifecycle conflict and
  return to the selector without modifying it.

### Direct and non-interactive invocation

- `control-agents NAME` creates or reuses the named managed session and attaches
  directly. After detach it exits to the caller rather than opening the
  selector; this preserves a useful explicit shortcut.
- `control-agents --no-attach NAME` creates or reuses the managed session,
  ensures its bridge, prints only its canonical ID to stdout, and exits.
- Preserve `CONTROL_AGENTS_NO_ATTACH=1` with an explicit `NAME` for existing
  tests and scripts.
- Preserve `CONTROL_AGENTS_ATTACH=0` as a compatibility alias for
  non-interactive mode if it remains documented. Do not allow contradictory
  flags silently; explicit CLI flags take precedence and invalid combinations
  produce usage errors.
- A non-interactive creation invocation requires an explicit name. The old
  behavior that derives a name from the current directory is removed.
- Calling no-argument selector mode without a usable TTY fails promptly with a
  concise usage message instead of blocking on stdin.
- Provide `--help` and `--version` output. Reuse the project's version package
  and release metadata.

### Build and installation

- `make build` builds both `bin/control-agents-server` and the new
  `bin/control-agents` binary.
- `make install` installs both built binaries.
- The release workflow cross-compiles the client for each supported Linux
  architecture instead of copying the current architecture-independent Bash
  script.
- `install.sh` continues to download matching server and client assets and
  checksum them.
- Do not add a third-party TUI library. A small line-oriented selector using the
  Go standard library is sufficient.

### Safety and compatibility

- Selector actions may call only typed lifecycle operations and `tmux
  attach-session`; user input must not be interpolated into a shell command.
- Detaching or quitting never kills tmux, `ttyd`, registry, resize, or socket
  state.
- Preserve private state permissions, Unix-socket-only bridges, tmux mouse
  defaults, window-size defaults, status shape, and web scrollback settings.
- Do not print passwords, cookies, terminal content, agent socket contents, or
  auth secrets.

## Material decisions

- The no-argument command is intentionally changed and is a breaking CLI
  behavior: it opens the selector instead of deriving a session name from the
  current directory.
- All newly created managed sessions start in `$HOME`, including direct named
  CLI creation.
- The selector operates on tmux sessions. Internal tmux windows remain managed
  inside the selected session through tmux or T-Control.
- SSH detach is nondestructive. Session termination is a separate lifecycle
  action exposed by later web work.

## Out of scope

- Web API or UI changes.
- Terminating a session from the SSH selector.
- Importing unmanaged tmux sessions.
- A full-screen terminal UI, fuzzy finder, or external selector dependency.
- SSH agent socket continuity, which is covered by task 0005.

## References

- `TASKS/backlog/0001-managed-session-lifecycle.md`.
- `AGENTS.md`.
- `README.md` sections Quick Install, Source Install, Run Locally,
  Configuration, and Troubleshooting.
- `bin/control-agents`.
- `cmd/server/main.go` and `internal/version/version.go` for command/version
  conventions.
- `Makefile`.
- `install.sh`.
- `.github/workflows/release.yml`.
- `test/e2e/e2e_test.go`.

## Acceptance criteria

- In an interactive terminal, `control-agents` lists every live managed session
  and no unmanaged tmux session.
- The user can create a valid named session, is attached immediately, and the
  first pane starts in `$HOME`.
- Selecting an existing session attaches it without creating another bridge.
- `Ctrl-b d` returns to a refreshed selector and leaves the session usable from
  web and subsequent SSH attachments.
- `q`, EOF, and interrupt leave every session running.
- Invalid and conflicting names produce clear messages without shell execution
  or accidental adoption.
- `control-agents NAME`, `--no-attach NAME`, and
  `CONTROL_AGENTS_NO_ATTACH=1 control-agents NAME` behave as specified.
- No-argument non-TTY invocation fails rather than waiting indefinitely.
- Both client and server build as Linux `amd64` and `arm64` Go binaries through
  the release workflow.
- Tests no longer assert the removed current-directory default.
- `CHANGELOG.md` records the no-argument change under `Unreleased` with a
  `BREAKING:` marker.
- `make test` passes.
- `make test-e2e` covers direct creation and, where a pseudo-terminal helper is
  available, selector attach/detach behavior.

## Validation

Run:

```sh
make test
make build
make test-e2e
```

Also verify that the release build commands produce executable client and
server binaries for both supported architectures. Record exact results in the
implementation summary.

## Implementation summary

- Added the architecture-specific Go `control-agents` client with a compact
  managed-session selector, new-session prompt, direct named mode,
  register-and-exit compatibility modes, terminal inheritance, signal-aware
  prompt handling, detach-to-selector looping, help, and version output. The
  client uses only the shared lifecycle and argument-vector `tmux
  attach-session` execution; selector quit, EOF, interrupt, and detach paths do
  not terminate managed state.
- Removed the tracked Bash client and made `make build`, `make install`, E2E and
  browser prerequisites, cleanup, and the release workflow build or consume the
  Go client binary. Release assets now cross-compile both server and client for
  Linux `amd64` and `arm64`, while `install.sh` retains matching asset download
  and checksum behavior without obsolete Bash-client dependencies.
- Added client unit coverage for flag/environment precedence, direct mode,
  selector refresh, validation, immediate attachment, terminated-session
  recovery, EOF, and interrupt behavior. Replaced the removed current-directory
  E2E expectation with explicit `$HOME` creation, compatibility-mode, non-TTY,
  conflict, and util-linux `script` PTY coverage for create, attach, `Ctrl-b d`,
  refreshed selection, and nondestructive quit.
- Updated `README.md`, `AGENTS.md`, and `CHANGELOG.md`, including the required
  `Unreleased` `BREAKING:` entry.
- Validation completed on 2026-07-13:
  - `make test` — passed for all Go packages.
  - `make build` — passed; produced statically linked Go client and server
    executables, and client `--help` and `--version` succeeded.
  - `make test-e2e` — passed with real tmux/ttyd direct creation, bridge
    recovery, invalid/unmanaged-name rejection, non-TTY failure, and PTY
    selector attach/detach coverage.
  - `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ... ./cmd/server` and the
    equivalent `./cmd/client` command — passed and produced executable x86-64
    ELF binaries.
  - `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ... ./cmd/server` and the
    equivalent `./cmd/client` command — passed and produced executable AArch64
    ELF binaries.
  - Isolated `make install` with workspace-local install paths — passed and
    installed both executable binaries.
  - `LOCAL_BIN_DIR="$PWD/bin" sh install.sh` with workspace-local HOME, prefix,
    and configuration paths — passed and installed both executable binaries.
  - `sh -n install.sh` — passed.
