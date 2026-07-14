# Control Agents

Control Agents manages durable tmux sessions from SSH and exposes those same
sessions through one password-protected web app. One **managed session** is one
Control Agents registry record plus one tmux session; it appears as one SSH
selector item and one top-level web tab. Tmux windows and panes remain inside
that managed session. A per-session `ttyd` process is only a recoverable web
bridge on a private Unix socket, not the session's identity.

The Go client and server share the same lifecycle implementation for registry
persistence, per-session locking, tmux creation and termination, bridge
recovery, and reconciliation. The SSH login, tmux server, `ttyd` processes,
client, and `systemd --user` service must all run as the same Unix account and
use the same state directory.

It is optimized for mobile touch displays, especially iOS Safari and iPadOS browsers: the web UI includes immutable local History with native selection and Copy, Paste support, special-key buttons, and viewport handling for the software keyboard.

## Quick Install

Linux user-local install from the latest GitHub Release:

```sh
sudo apt-get update
sudo apt-get install -y bison build-essential ca-certificates curl libevent-dev libncurses-dev pkg-config ttyd
curl -fsSL https://raw.githubusercontent.com/hajekmi/control-agents/main/install-tmux.sh | sh
export PATH="$HOME/.local/bin:$PATH"
curl -fsSL https://raw.githubusercontent.com/hajekmi/control-agents/main/install.sh | sh
systemctl --user enable control-agents.service
systemctl --user restart control-agents.service
control-agents
```

Then open:

```text
https://<vm-host-or-ip>:8080
```

To install a specific release, pass `VERSION` to the shell that runs the installer:

```sh
curl -fsSL https://raw.githubusercontent.com/hajekmi/control-agents/main/install.sh | VERSION=2026.5.21 sh
```

`install-tmux.sh` downloads the pinned tmux 3.7b source archive, verifies its
published SHA-256 checksum, builds and verifies it in a temporary prefix, and
atomically replaces `~/.local/bin/tmux` without `sudo`. The explicit `PATH`
export makes that executable current for the rest of the shell; add the same
line to the shell profile when `~/.local/bin` is not already present. The
Control Agents installer also prepends its selected user-local `bin` directory
for its own verification, requires exact tmux 3.7b in that directory before
writing Control Agents files, and warns when the caller's shell needs the PATH
update. The installed user service always searches the user-local directory
first. Set the same absolute `BIN_DIR` for both installers to use a custom
destination; `PREFIX` is not a tmux-installer destination setting.

The Control Agents installer downloads GitHub Release binaries, installs them
under `~/.local/bin`, creates `~/.config/control-agents/env` with a generated
`CONTROL_AGENTS_PASSWORD` when the file does not already exist, installs the
user systemd unit under `~/.config/systemd/user/control-agents.service`, and
runs `systemctl --user daemon-reload` when available.

Review the generated config before exposing the service:

```sh
chmod 600 ~/.config/control-agents/env
sed -n '1,80p' ~/.config/control-agents/env
```

If the service should start after boot before the user logs in, enable lingering for that user:

```sh
loginctl enable-linger "$USER"
```

Uninstall the release-installed binaries and user systemd unit:

```sh
systemctl --user disable --now control-agents.service
rm -f ~/.config/systemd/user/control-agents.service
rm -f ~/.local/bin/control-agents ~/.local/bin/control-agents-server
systemctl --user daemon-reload
```

The uninstall commands above keep `~/.config/control-agents/env` and `~/.local/state/control-agents`. Remove them only when you want to delete the generated password, auth secret, TLS certificate, session registry, sockets, and logs:

```sh
rm -rf ~/.config/control-agents ~/.local/state/control-agents
```

## Requirements

Runtime:

- Linux `amd64` or `arm64`. Release installs select architecture-matched Go
  client and server binaries and verify both against the published checksum
  manifest.
- Exactly `tmux` 3.7b for shared terminal sessions. Newer releases are not
  accepted until they are added to the tested runtime contract. In particular, the
  Ubuntu 24.04 package is tmux 3.4 and is too old for managed-pane history
  reconciliation and the opaque topology format used by Control Agents.
- `ttyd` for browser terminal I/O.
- A Linux kernel with pidfd support (5.3 or newer); lifecycle process cleanup
  fails closed rather than signaling through a reusable numeric PID.
- systemd user services for the default service setup.
- One Unix account for the SSH client, user service, tmux sessions, `ttyd`
  bridges, and private lifecycle state.

The tmux source installer requires `bison`, a C compiler and build toolchain,
`libevent` and ncurses development headers, `make`, `pkg-config`, `tar`, and
`sha256sum`. On Ubuntu 24.04 the Quick Install command above provides them.
The Control Agents release installer additionally requires `sha256sum` before
it writes either downloaded binary.

Development:

- Go 1.25 or newer; release and security builds should use the latest stable Go
  toolchain compatible with the module.
- `make` for the provided workflow.
- Node.js 20, 22, or 24 plus npm for Playwright browser E2E tests.
- Linux `unshare`, `setpriv`, and `ip` (normally provided by `util-linux` and
  `iproute2`) for the private loopback-only Playwright network boundary.
- Ubuntu's `aa-exec` and loaded vendor `chrome` AppArmor profile (provided by
  `apparmor-utils` and `apparmor`) when the boundary uses `sudo`.

Playwright is a project-local dev dependency. Install JavaScript dependencies from the repo root:

```sh
npm install
npx playwright install chromium
```

Install `firefox` and `webkit` as well before running
`make test-browser-matrix`; CI installs all three engines and their host
dependencies with Playwright's `--with-deps` option.

On AlmaLinux/RHEL-like hosts, Chromium also needs system libraries. If `make test-browser` reports missing browser dependencies, install the matching packages:

```sh
sudo dnf install -y nspr nss atk at-spi2-atk at-spi2-core cups-libs libxcb libxkbcommon alsa-lib mesa-libgbm libX11 libXext cairo pango libXcomposite libXdamage libXfixes libXrandr
```

## Source Install

Use this path when developing locally or installing without release binaries. Install the runtime and build prerequisites on the target host:

- Go 1.25 or newer
- `make`
- `tmux`
- `ttyd`
- systemd user services

Control Agents currently requires exactly tmux 3.7b. The supported release
path pins and verifies that version because Ubuntu 24.04 packages incompatible
tmux 3.4 and untested newer releases are outside the runtime contract. For a
user-local source prerequisite, run `./install-tmux.sh`, export
`PATH="$HOME/.local/bin:$PATH"`, and verify that `tmux -V` prints `tmux 3.7b`.

Clone the repository, build, and install the server and client binaries, user systemd unit, and first-run environment file:

```sh
git clone <repo-url> control-agents
cd control-agents
make install
```

Enable and start:

```sh
systemctl --user enable control-agents.service
systemctl --user restart control-agents.service
```

Open the managed-session selector:

```sh
control-agents
```

Choose `New session`, enter a canonical name, and the client creates it in the
account's `$HOME`, starts its private `ttyd` bridge, writes its durable registry
record, and attaches the current terminal. A direct named shortcut remains
available:

```sh
control-agents main
```

For scripts that should create or select the managed session and exit, use:

```sh
control-agents --no-attach main
```

## Build And Test

```sh
make test
make build
```

The default Makefile uses local Go cache directories under `.cache/` and disables cgo. This keeps tests working in restricted environments and produces simple Linux server and client binaries.
Repository build and test targets force `LANG=C.UTF-8` and `LC_ALL=C.UTF-8`.
`make test-e2e` also supplies `TERM=xterm-256color` to its real PTY fixtures,
so noninteractive callers with `TERM=dumb` exercise the same attach scenarios.

Stage the same release assets used by CI, including both commands for Linux
`amd64` and `arm64` plus their checksum manifest:

```sh
make release-assets VERSION=2026.5.1
(cd dist && sha256sum -c sha256sums.txt)
```

Build metadata is injected from git by default. Override it explicitly when needed:

```sh
make build VERSION=2026.5.1
bin/control-agents-server --version
bin/control-agents --version
```

Run real tmux/ttyd E2E checks explicitly:

```sh
make test-e2e
```

The E2E test is opt-in because it starts real processes. It skips only when `RUN_E2E` is not set or required tools are unavailable.

Run browser E2E checks with Playwright:

```sh
make test-browser
```

This target runs the Chromium engine project. Run the complete automated engine
matrix with:

```sh
make test-browser-matrix
```

The matrix runs Chromium, Firefox, and WebKit against the same real Go,
tmux, and ttyd fixture while isolating browser state. The managed-lifecycle,
secondary-viewer lifecycle, and mobile/two-viewer profiles each run in their
own Playwright invocation with a fresh server, tmux, and browser fixture so
preceding scenarios cannot carry transport state into them. Both browser
targets preserve this split, including every engine in the matrix. The
synthetic request-failure profile runs in one final isolated invocation so its
browser-network transition cannot affect a later functional mutation. A
bounded profile runner owns each complete process group, uses the already-built
server binary, and verifies that the browser network process, server listener,
ttyd children, tmux fixtures, and private state have stopped before starting
the next invocation. On Linux, every complete profile runs in a fresh private
loopback-only network namespace so unrelated host link, route, or dynamic IPv6
notifications cannot abort an exactly-once local History or lifecycle mutation
with Chromium's `ERR_NETWORK_CHANGED`. The deterministic boundary gate also
proves one local mutation completes exactly once while a separate nested
namespace emits link notifications. The original non-root launcher, not the
capability-free browser child, owns that one selected-mode churn attempt and
returns only a fixed request/result signal. Before Playwright starts, the
launcher verifies a distinct namespace, one UP loopback interface, no IPv4 or
IPv6 default route, no route through a non-loopback interface, the original
non-root identity, `NoNewPrivs: 1`, and zero process capabilities. Chromium is
launched with its Linux sandbox explicitly enabled. The suite and standalone
boundary probe inspect every matching Chromium browser owned by the runner and
every renderer below each browser. They reject `--no-sandbox`, root or changed
browser UID, GID, or supplementary groups, group 0, any nonzero browser
capability set including the bounding set, and missing `NoNewPrivs`. Every
renderer must have non-root mapped UID/GID values, no group 0, zero
inheritable, permitted, effective, and ambient capabilities, `NoNewPrivs: 1`,
seccomp mode 2, and distinct user, PID, and network namespaces. A renderer's
nonzero capability bounding mask is accepted only with that distinct user
namespace plus zero-held-capability and no-new-privileges proof; it is a
namespace-local ceiling, not a capability held against the host. Unrelated
host Chromium process trees are outside the ownership check.
The fixed bootstrap retains network capability only long enough to bring up
loopback and then uses `setpriv` to drop it before Node or a browser starts.
Within a shared functional invocation,
each scenario also uses a test-scoped browser process instead of carrying one
browser network service across independent contexts. WebKit
is engine coverage; it is not evidence that desktop Safari, iOS Safari, or
iPadOS Safari was tested. The suite includes
deterministic current/oldest-supported iPhone viewport profiles plus iPad
portrait, landscape, Split View, and Stage Manager-sized profiles. Physical
Safari selection handles, native system menus, swipe inertia, and device
performance remain operator release gates documented in
`TASKS/backlog/0018-release-rollout.md`.

The browser tests verify session lifecycle, terminal iframes, special keys,
resize sources, visual viewport tracking, T-Control, local History state and
focus, two-viewer isolation, background/foreground and reload behavior, and
wheel/touch routing. The History fixture rolls a real managed pane through more
than 50,000 shell-log lines, traverses immutable pages while another tmux client
is attached, and traces that no copy-mode, viewport-scroll, input, or resize
mutation is emitted by local scrolling.

`CONTROL_AGENTS_PLAYWRIGHT_NETWORK_MODE` selects the fail-closed Linux launcher:

- `auto` (default) tries the current-user namespace mapping first. It may try
  the fixed `sudo -n` bootstrap once only when the rootless child exits before
  the verified readiness handshake; cancellation, a signaled child, and every
  failure after readiness are terminal and never relaunch a browser action.
- `unprivileged` requires the current-user mapping and never invokes `sudo`.
- `sudo` invokes only the repository bootstrap through non-interactive sudo.
  The bootstrap creates the network namespace, brings up loopback, restores the
  caller's nonzero UID, GID, and supplementary groups, clears inheritable,
  ambient, effective, and bounding capabilities, sets `no_new_privs`, and then
  directly executes Node without evaluating browser arguments in a privileged
  shell. On Ubuntu it enters the already loaded vendor `chrome` AppArmor
  profile before the privilege drop so Chromium retains only the profile's
  explicit user-namespace permission needed by its own sandbox.

Ubuntu 24.04 commonly rejects the rootless user-namespace path through its
default AppArmor policy. Use `sudo -v` before a local explicit `sudo` run (the
launcher itself always uses `sudo -n`). Both `auto` and `sudo` fail closed when
non-interactive sudo is unavailable. CI is pinned to Ubuntu 24.04 and selects
`sudo`. The tests do not change the AppArmor sysctl, weaken the browser sandbox,
install or broaden an AppArmor profile, or run the server, tmux, ttyd, Node, or
browser as root. The bootstrap process starts with only fixed `PATH`, `LANG`,
and `LC_ALL` values; the bounded caller environment is restored after the
privilege drop solely from stdin. The standalone boundary probe verifies that
a real `sudo -n` attempt from the ready child cannot regain privilege; normal
browser profiles never invoke sudo after readiness.
Each launcher attempt owns its private handshake directory and child process
from allocation through normal exit, failure, timeout, or cancellation; both
are removed before a fallback or target completion is reported.

Browser failure diagnostics use a content-free reporter that emits only fixed
test titles, projects, and status. Automatic page-text failure context, trace,
screenshots, video, HTML reports, and network artifacts are disabled because
they can capture terminal output, Paste bodies, auth cookies, or credentials.
Each browser compatibility target finishes with an intentional-failure canary
check that scans the diagnostics and a deliberately retained sanitized error
context; run that check by itself with `make test-browser-artifacts`.

Generate the synthetic dataset/server report and real-browser History report:

```sh
make test-benchmarks
```

The timing-tagged browser benchmark runs only through this target; the browser
compatibility targets exclude it so transient timing/resource noise cannot
contaminate engine correctness results.

Reports are written under `.cache/benchmarks/`. They contain only fixed dataset
IDs, axis labels, support statuses, numeric measurements, units, and reason
codes. They never include terminal text, commands, managed-session names,
opaque live references, cookies, or credentials. Server measurements cover
ANSI parse duration, estimated snapshot RAM, and encoded response bytes;
Chromium adds real-tmux capture duration, first paint, prepend, scrolling,
scroll-interval long-task count and maximum, DOM/heap support, anchor drift,
and current ttyd disconnect/reconnect correctness. The current ttyd harness marks
input-to-paint, reconnect-to-redraw timing, and slow-consumer/backpressure
measurement as unsupported because ttyd/xterm exposes neither deterministic
paint/redraw completion signals nor a bounded application queue; task 0016 must
replace those statuses when the native bridge adds the required signals.
Hosted-runner wall-clock values are recorded baselines, not release thresholds;
reference-device thresholds are evaluated only with the pending
physical-device evidence matrix.

## Versioning

Releases use calendar versioning: `YYYY.M.REVISION`.

- `YYYY` is the release year.
- `M` is the release month without a leading zero.
- `REVISION` starts at `1` each month and increments for each release in that month.

Git release tags use a `v` prefix, for example `v2026.5.1`. Runtime output omits the prefix and includes commit/build metadata through both binaries' `--version` output, server startup logs, and `GET /api/version`.

Breaking changes are called out in `CHANGELOG.md` with `BREAKING:` because compatibility is not encoded in the version number.

Release checklist:

```sh
make test
make test-e2e
make test-browser
node --check internal/server/static/app.js
make release-assets VERSION=2026.5.1
(cd dist && sha256sum -c sha256sums.txt)
git tag -a v2026.5.1 -m "Release 2026.5.1"
git push origin v2026.5.1
```

Tag pushes run the release workflow, build architecture-matched Go client and
server assets for Linux `amd64` and `arm64`, verify their ELF architectures and
checksums, and upload them with `sha256sums.txt` for `install.sh`.

## Run Locally

Start the web service:

```sh
export CONTROL_AGENTS_PASSWORD='change-me'
export CONTROL_AGENTS_BIND_ADDR='0.0.0.0'
export CONTROL_AGENTS_PORT='8080'
make run
```

Open the managed-session selector in an interactive terminal:

```sh
bin/control-agents
```

Or create or reuse one named session directly:

```sh
bin/control-agents codex-main
```

The selector lists only live Control Agents managed sessions. Selecting or creating one attaches the current terminal; `Ctrl-b d` returns to the refreshed selector without terminating the session. Use `q` to return to the caller. Add `--no-attach` before an explicit name when you only want to register the web session, print its canonical ID, and exit.

Open:

```text
https://<vm-host-or-ip>:8080
```

On first start the server generates a self-signed ECDSA P-256 certificate under the state directory. Browsers will show a certificate warning until you trust that certificate or provide your own TLS certificate.

New sessions started with `bin/control-agents <name>` appear as tabs automatically. Names must match `[A-Za-z0-9][A-Za-z0-9._-]{0,63}`. Unregistered tmux sessions are never adopted automatically.

## Managed Session Lifecycle

Both the selector and web tab row show only sessions with a valid Control
Agents registry record. They never import an arbitrary tmux session. A
top-level selector item or browser tab is a whole managed tmux session; use
tmux or T-Control for windows and panes inside it.

New sessions created from SSH, direct named mode, register-only mode, or the
web Menu always start in the service account's `$HOME`. Browser requests accept
only the strict canonical name; they cannot provide a working directory,
command, environment, shell, or tmux arguments.

Detaching is nondestructive. `Ctrl-b d` detaches an SSH tmux client and returns
to the selector, quitting the selector leaves every session running, and
closing a browser or losing its connection only disconnects that browser. The
web Menu's `Terminate session` action is different: after explicit named
confirmation, it destroys the selected tmux session, disconnects every SSH and
web client attached to it, stops the bridge, and removes its registry, socket,
log, resize, and viewer state.

The registry records desired managed-session state, while the stored `ttyd` PID
and socket are replaceable runtime metadata. Server startup and session listing
reconcile every valid record. If tmux is still live but its bridge or socket is
gone, Control Agents starts a new bridge without recreating tmux. Restarting
the web server therefore preserves tmux identity and terminal contents. If the
tmux session is gone, reconciliation removes stale managed artifacts. Safe
records written by the earlier Bash client are migrated in place; unsafe or
invalid records are ignored and never cause an unmanaged tmux session to be
adopted or terminated.

During the exact tmux 3.7b path upgrade, reconciliation recognizes the
immediately previous bridge only when its exact former relative `tmux
attach-session -E -t <managed-session>` command belongs to the PID already
stored in that valid managed record. It terminates that bridge and replaces its
socket before starting a bridge with the resolved absolute tmux path. Other
relative ttyd/tmux processes are neither adopted nor terminated.

## Configuration

The Go service reads:

- `CONTROL_AGENTS_BIND_ADDR`, default `0.0.0.0`
- `CONTROL_AGENTS_PORT`, default `8080`
- `CONTROL_AGENTS_PASSWORD`, required unless `CONTROL_AGENTS_PASSWORD_FILE` is set
- `CONTROL_AGENTS_PASSWORD_FILE`, optional newline-trimmed password file
- `CONTROL_AGENTS_STATE_DIR`, default `$HOME/.local/state/control-agents`
- `CONTROL_AGENTS_TLS_CERT_FILE`, default `$CONTROL_AGENTS_STATE_DIR/certs/server.crt`
- `CONTROL_AGENTS_TLS_KEY_FILE`, default `$CONTROL_AGENTS_STATE_DIR/certs/server.key`
- `CONTROL_AGENTS_AUTH_SECRET_FILE`, default `$CONTROL_AGENTS_STATE_DIR/auth/session.key`
- `CONTROL_AGENTS_COOKIE_SECURE`, default `true` for HTTPS
- `CONTROL_AGENTS_COOKIE_TTL_SECONDS`, default `172800`
- `CONTROL_AGENTS_SNAPSHOT_MAX_BYTES`, default `33554432` (32 MiB); maximum
  terminal capture used to create one local History snapshot
- `CONTROL_AGENTS_MAX_SESSIONS`, default `32`; a positive integer limiting new
  sessions created through the web API. Existing sessions above the limit
  remain listable, attachable, and terminable.

The shared managed-session lifecycle used by both the Go server and Go client
reads:

- `CONTROL_AGENTS_APP_NAME`, optional override for tmux status-left; default is the canonical session name
- `CONTROL_AGENTS_TMUX_WINDOW_SIZE`, default `manual`
- `CONTROL_AGENTS_TMUX_MOUSE`, default `off`
- `CONTROL_AGENTS_WEB_SCROLLBACK_LINES`, default `10000`

Control Agents resolves and verifies exact tmux 3.7b beside the installed
server/client binary, then in the default user-local directory, before using
ambient `PATH` only as a development fallback. It executes managed tmux
commands and ttyd bridge processes with
`LANG=C.UTF-8` and `LC_ALL=C.UTF-8`, independent of the invoking SSH shell or
server environment. The installed user service applies its managed `PATH` and
locale at `ExecStart`, after loading the preserved operator environment file,
so `PATH=/usr/bin:/bin`, `LANG=C`, or `LC_ALL=C` entries cannot replace these
runtime invariants. This is part of the tmux topology contract: tmux 3.7b under
the plain `C` locale can rewrite the format delimiters used to resolve opaque
window and pane identity.

The server and client must use the same `CONTROL_AGENTS_STATE_DIR`; its default
is `$HOME/.local/state/control-agents` for both. The Go client additionally
reads:

- `CONTROL_AGENTS_ATTACH`, default `1`; set to `0` with an explicit name to register the web session, print its ID, and exit
- `CONTROL_AGENTS_NO_ATTACH=1`, force register-and-exit mode with an explicit name; kept for tests and scripts

Explicit `--attach` and `--no-attach` flags override these compatibility environment settings. Non-interactive mode always requires an explicit session name; no-argument invocation is the interactive selector.

The shared state directory contains:

- `sessions/*.json` registry files
- `sockets/*.sock` private `ttyd` Unix sockets
- `logs/*.log` per-session `ttyd` logs
- `locks/*.lock` per-session lifecycle locks shared by the server and Go client
- `agent/forwarded.sock` stable link to the most recently refreshed forwarded SSH agent socket
- `auth/session.key` persistent cookie signing secret
- `certs/server.crt` and `certs/server.key` when the default generated TLS files are used

Registry files are durable desired-state records. Their canonical `id`,
`name`, and tmux name identify one managed tmux session; `publicRef` is the
opaque identity used by browser routes. The bridge PID and socket fields are
internal runtime metadata that reconciliation may replace; losing them does
not make a live tmux session unmanaged. Directories are kept at mode `0700`,
and registry records, logs, resize records, the auth secret, and generated
private key material are private files.

Keep `CONTROL_AGENTS_STATE_DIR` reasonably short. Unix domain socket paths have a small system limit, and the lifecycle fails early when the generated socket path is too long.

`CONTROL_AGENTS_WEB_SCROLLBACK_LINES` controls browser-side terminal history retained by `ttyd`/xterm.js while the web tab is connected. It does not replay tmux output that happened before the browser connected.

Managed tmux windows and panes use `history-limit 50000`. Control Agents sets
the exact managed session default before creating its durable user pane and
reapplies that session option during reconciliation. It never changes the
global tmux history default, so unmanaged sessions keep their own retention.
Raising the option on an existing pane is not retroactive: history already
discarded under its former limit cannot be recovered (including with the
supported tmux 3.7b behavior). New panes and windows inherit the 50,000-line
limit. History snapshots are independently bounded by
`CONTROL_AGENTS_SNAPSHOT_MAX_BYTES`.

Because the browser is attached to tmux, `control-agents` keeps
`CONTROL_AGENTS_TMUX_MOUSE=off` by default so normal terminal text selection
works without tmux intercepting mouse drag. `Menu` -> `History` creates one
bounded, immutable active-pane snapshot and opens it above the still-connected
Live iframe. Wheel, touch, selection, and Copy are then entirely local browser
operations: they never enter tmux copy-mode or issue a tmux scroll command.
An upward wheel gesture, `PageUp`, or `Shift+PageUp` can open History directly;
the first wheel delta and later trackpad inertia are applied only to the local
History scroller. Each browser tab can change `Scroll gesture` to `Application`
when a mouse-reporting terminal application should receive wheel and PageUp
input instead. The preference is tab-local `sessionStorage` state and never
changes tmux.
Closing History returns immediately to the continuously updated Live terminal.
`New output` indicates Live activity without changing the snapshot. Start or
refresh a session with `CONTROL_AGENTS_TMUX_MOUSE=on` only if you prefer tmux to
own mouse handling inside Live.

History uses the browser's native scrolling, selection, long-press callout,
and Copy behavior on Safari and iOS; there is no custom momentum or remote
scroll controller. Its explicit Paste action reads the Clipboard API only from
the click that requested it, shows UTF-8 byte and logical-line counts, and
stages the value for review. Multiline text and C0, C1, or DEL control
characters receive an explicit warning and require confirmation. If Clipboard
API access is unavailable, the visible History textarea supports the system
Paste callout and stages its `paste` event through the same review dialog. A
denial, cancel, or failed request remains in History and is never retried
automatically. Confirmed text reaches tmux through stdin and preserves its
literal UTF-8 bytes and newlines, including bracketed-paste framing when the
active application enables that terminal mode.

The default `CONTROL_AGENTS_TMUX_WINDOW_SIZE=manual` keeps tmux dimensions
fixed when another browser or a narrower SSH client attaches. The web Resize
panel can keep the current dimensions with `Fixed` or apply one selected web
viewer size once with `Fit once`; continuous `Follow this device` behavior is
reserved for a later stage. Reconciliation migrates every existing managed
window from legacy automatic sizing to `manual` without changing its current
dimensions. A session-local tmux hook makes every later managed window manual
synchronously when it is linked, including windows created directly from SSH;
periodic reconciliation is not required to close a sizing race. History
reflows locally by default, while Fixed grid preserves the captured pane width
and permits horizontal panning. Neither History mode resizes the shared tmux
window.

`control-agents` also sets a compact tmux status line for managed sessions. The left side shows the session label, for example `[ahoj]` when started with `bin/control-agents ahoj`, and the right side shows the current pane directory through `#{pane_current_path}` without hostname, date, or time. Override the label with `CONTROL_AGENTS_APP_NAME`.

### Forwarded SSH Agent Continuity

Connect with agent forwarding and invoke the client to make the current
forwarded agent available through the stable managed-session path:

```sh
ssh -A user@vm
control-agents main
```

The client refreshes `$CONTROL_AGENTS_STATE_DIR/agent/forwarded.sock` only when
standard SSH connection metadata is present and `SSH_AUTH_SOCK` is an existing
Unix socket. It reports `available`, `unavailable`, or `invalid` on stderr and
continues session selection when forwarding is absent. The symlink lives in a
mode-`0700` directory and is replaced atomically. Control Agents does not copy,
inspect, or persist agent traffic, identities, keys, or signatures.

Managed sessions always use the stable path as `SSH_AUTH_SOCK`, including a
web-created session for which no forwarding socket exists yet. The initial
shell and later tmux windows and panes inherit that value. Availability then
follows the SSH transport:

- While the forwarding SSH connection is alive, managed shells can use its
  agent. Detaching from tmux with `Ctrl-b d` does not break forwarding when the
  SSH transport remains connected.
- After SSH logout or transport loss, the forwarded socket disappears and
  agent operations fail. Control Agents leaves the stable link and managed
  sessions in place; it does not provide agent access while disconnected.
- After reconnecting with `ssh -A`, invoke `control-agents` again. The client
  retargets the stable link, so existing panes that already use the stable path
  regain access without environment injection or terminal input.
- One Unix account has one current forwarded identity shared by all managed
  sessions. If several SSH clients forward different agents, the most recent
  successful refresh wins for every managed session.

Panes created before this feature may still contain an old direct forwarded
socket path. Control Agents does not rewrite a running process environment.
New panes inherit the stable path after lifecycle reconciliation; an old pane
can opt in manually:

```sh
export SSH_AUTH_SOCK="${CONTROL_AGENTS_STATE_DIR:-$HOME/.local/state/control-agents}/agent/forwarded.sock"
```

Agent forwarding delegates signing to the connected Mac; it does not transfer
the private key to the VM. Any process running as the same VM user can request
signatures from that agent while forwarding is connected, and Control Agents
does not isolate the service user's processes from one another. For detached
background jobs that must authenticate while the Mac is offline, use a
separately managed, narrowly scoped deploy key, workload identity, or token.

## API

Unauthenticated routes:

- `GET /login`: login page.
- `POST /login`: form login. Expects `password` in an `application/x-www-form-urlencoded` body. Success sets the auth cookie and redirects to `/`; failure redirects to `/login?error=1`.
- `GET /app.js` and `GET /styles.css`: static UI assets used by the login and app pages.

Authenticated routes:

- `GET /`: tabbed web UI.
- `POST /logout`: clears the auth cookie and redirects to `/login`.
- `GET /api/version`: returns server build metadata.
- `GET /api/csrf`: returns the concrete login's CSRF token for authenticated
  browser HTTP mutations.
- `GET /api/sessions`: reconciles and returns active managed sessions.
- `POST /api/sessions`: creates or selects a managed session. Body:
  `{ "name": "backend" }`. New sessions always start in the service user's
  home directory.
- `DELETE /api/sessions/{sessionRef}`: terminates the explicitly confirmed
  managed session. Body:
  `{ "confirmName": "backend", "paneRef": "p_F4jLm9aWx3cTzQ8vBnHsKA2b" }`.
- `POST /api/v1/panes/{paneRef}/history-snapshots`: creates one immutable
  History snapshot. Body: `{ "mode": "reflow|fixed" }`.
- `GET /api/v1/history-snapshots/{snapshotId}/pages?before={cursor}`: returns a
  bounded structured text/style page from the original capture.
- `GET /api/v1/history-snapshots/{snapshotId}`: reports whether the live pane
  has produced output since the immutable capture, for the History badge.
- `DELETE /api/v1/history-snapshots/{snapshotId}`: explicitly releases the
  in-memory snapshot.
- `POST /api/sessions/{sessionRef}/paste/token`: issues a short-lived single-use
  token bound to the concrete login, opaque pane generation, staged SHA-256
  digest, byte/line counts, and control/trailing-newline warnings.
- `POST /api/sessions/{sessionRef}/paste`: pastes text into the active tmux pane. Body: `{ "text": "...", "paneRef": "p_F4jLm9aWx3cTzQ8vBnHsKA2b", "token": "pt_..." }`; invalid UTF-8, NUL bytes, payloads above 64 KiB, token replay, and binding mismatches are rejected. The token is consumed before terminal mutation, text is loaded through stdin, and the temporary tmux buffer is always deleted.
- `POST /api/sessions/{sessionRef}/keys`: sends a special key to the active tmux pane. The body includes `paneRef`; key values include `ctrl-c`, `ctrl-d`, `ctrl-z`, `ctrl-l`, `escape`, `tab`, `enter`, arrows, `home`, `end`, `page-up`, and `page-down`. A one-rune `text` value is used only to forward the first printable key after History returns to Live.
- `POST /api/sessions/{sessionRef}/resize/viewer`: records a browser tab/window resize heartbeat. Body: `{ "viewerId": "viewer-550e8400-e29b-41d4-a716-446655440000", "paneRef": "p_F4jLm9aWx3cTzQ8vBnHsKA2b", "width": 120, "height": 32, "transient": false }`. A transient heartbeat updates viewer liveness but never applies a tmux resize.
- `GET /api/sessions/{sessionRef}/resize`: returns fixed resize state, active browser viewers, current tmux window identity and dimensions, capabilities, and the last one-shot applied size when available.
- `POST /api/sessions/{sessionRef}/resize`: stores or applies an explicit resize choice. Body: `{ "mode": "fixed|fit-once|follow-device", "paneRef": "p_F4jLm9aWx3cTzQ8vBnHsKA2b", "viewerId": "viewer-550e8400-e29b-41d4-a716-446655440000" }`; `viewerId` is required for `fit-once`, while `follow-device` is advertised but currently rejected as unsupported.
- `GET /api/sessions/{sessionRef}/tmux-control`: lists opaque window references and display metadata for the session.
- `POST /api/sessions/{sessionRef}/tmux-control`: runs an allowlisted tmux control action such as `new-window`, `select-window`, `next-window`, `previous-window`, `split-horizontal`, `split-vertical`, pane selection/resizing, `choose-window`, or `command-prompt`. Every action includes `paneRef`; `select-window` additionally uses an opaque `windowRef`.
- `GET /terminal/{sessionRef}/...`: reverse proxies HTTP and WebSocket traffic to the matching `ttyd` Unix socket.

The browser UI uses regular HTTPS requests for login, static assets, and JSON API calls. `/api/sessions` includes `tmuxWindowCount` only when a session has more than one internal tmux window; the tab row renders that value as a compact badge. `/api/*` endpoints return `401 unauthorized` when the auth cookie is missing or expired, so `app.js` can redirect the browser back to `/login` without receiving an HTML login page as an API response.

Every public session object contains opaque `id`, display-only `name`, `cwd`,
`createdAt`, `activeWindowRef`, `activePaneRef`, `windowWidth`, and
`windowHeight`, plus optional `tmuxWindowCount` only when it exceeds one. List
and create responses never expose bridge PIDs, Unix socket paths, canonical
tmux targets, pane IDs, or window indexes. Create responses have this shape:

```json
{
  "created": true,
  "session": {
    "id": "G8qK1mR5tV9xB3nF7jL2pQ6wZ0cH4sYd",
    "name": "backend",
    "cwd": "/home/user",
    "createdAt": "2026-07-13T12:00:00Z",
    "activeWindowRef": "w_PvN7Hk2mQe8sRg4yUa6cDwXy",
    "activePaneRef": "p_F4jLm9aWx3cTzQ8vBnHsKA2b",
    "windowWidth": 120,
    "windowHeight": 32
  }
}
```

Session creation accepts only canonical names matching
`[A-Za-z0-9][A-Za-z0-9._-]{0,63}`. It returns `201 Created` with
`"created": true` for a new session and `200 OK` with `"created": false` when
the same healthy managed session already exists. An unmanaged tmux name or the
configured web creation limit returns `409 Conflict`. Local lifecycle or bridge
failures return `503 Service Unavailable` and are left for safe reconciliation.
Termination requires `confirmName` to exactly match the session display name
and the supplied `paneRef` to still identify its active pane generation. It returns
`204 No Content` after tmux, bridge, registry, socket, and resize cleanup, and
returns `404 Not Found` when the managed session does not exist.

Authenticated mutating routes require a same-origin `Origin` header, with `Referer` as a fallback for older clients, plus `X-Control-Agents-CSRF-Token` from `/api/csrf`. Terminal WebSocket upgrades under `/terminal/{sessionRef}/...` require an exact same-origin `Origin`. This is intentionally strict because terminal actions are remote shell input.

Authenticated JSON mutations accept exactly one bounded object with known,
non-duplicate fields. Ordinary mutation bodies are limited to 4 KiB; Paste has
a larger encoded envelope while its decoded UTF-8 text remains limited to 64
KiB. Resize viewer heartbeats retain at most 512 bytes of User-Agent metadata,
use bounded dimensions, and keep at most 32 viewers per managed session and
256 across the server process. New viewer identities are rejected at capacity
without evicting active entries.

History requests also send the browser tab's opaque
`X-Control-Agents-Viewer-ID`. An in-memory snapshot is bound to that viewer,
the concrete signed login cookie, session ref, pane ref, and verified pane
generation. Cross-login and cross-viewer reads return no content. Snapshots
expire after ten idle minutes, are deleted on close/logout/session termination,
and disappear with `410 Gone` after a server restart. Per-viewer, per-login,
process-count, process-memory, and process node-estimate limits reject new
snapshots instead of evicting an active snapshot. Capture work is additionally
bounded per concrete login and across the server process; only a bounded number
of requests with the exact same login/viewer/pane-generation/mode scope may
wait for one coalesced capture.

Go-served responses are gzip-compressed when the client sends `Accept-Encoding: gzip`. This includes `/login`, `/app.js`, `/styles.css`, and `/api/*` JSON responses. The `/terminal/{sessionRef}/...` ttyd proxy is excluded from this middleware, including both ttyd HTTP traffic and WebSocket upgrades.

Session, window, pane, and viewer references are opaque authorization handles.
Clients must reload the current topology after a server restart or a stale-ref
response and must never construct a tmux target from display data. Before each
mutation, the server resolves the handle to an exact internal tmux ID and
verifies the pane generation from the tmux server start time, validated server
PID, and pane ID.
Changing a URL or JSON ref to one from another session, or reusing a ref after
its pane disappears, returns an error instead of redirecting the operation.

Terminal audit records are content-free. Their allowlist is the opaque session
reference, HTTP status, byte count, duration, and reason code. History and
Paste request/response bodies, terminal WebSocket frames, and terminal output
are never written to application logs, traces, or metrics; production tmux is
also run without verbose logging.

Example `GET /api/sessions` response:

```json
{
  "sessions": [
    {
      "id": "G8qK1mR5tV9xB3nF7jL2pQ6wZ0cH4sYd",
      "name": "main",
      "cwd": "/home/user",
      "createdAt": "2026-05-15T20:12:14Z",
      "activeWindowRef": "w_PvN7Hk2mQe8sRg4yUa6cDwXy",
      "activePaneRef": "p_F4jLm9aWx3cTzQ8vBnHsKA2b",
      "windowWidth": 120,
      "windowHeight": 32
    }
  ]
}
```

The authenticated public session response intentionally excludes canonical
tmux names, raw window/pane IDs, terminal bridge PIDs, Unix socket paths, and
other process internals.

Successful login sets the `control_agents_session` cookie. The cookie is signed with a persistent secret stored under the state directory, so sessions remain valid across server restarts until `CONTROL_AGENTS_COOKIE_TTL_SECONDS` expires or the auth secret file is removed.

Failed logins are rate-limited in server memory per direct client IP: 10 failed attempts in 5 minutes returns `429 Too Many Requests` with `Retry-After`. A successful login clears that IP's failures, and restarting the daemon resets the limiter.

Example special key command:

```json
{
  "key": "ctrl-c",
  "paneRef": "p_F4jLm9aWx3cTzQ8vBnHsKA2b"
}
```

Example resize state:

```json
{
  "mode": "fixed",
  "selectedViewerId": "viewer-550e8400-e29b-41d4-a716-446655440000",
  "viewers": [
    {
      "id": "viewer-550e8400-e29b-41d4-a716-446655440000",
      "ip": "203.0.113.10",
      "userAgent": "Mozilla/5.0 ...",
      "width": 132,
      "height": 36,
      "lastSeen": "2026-05-16T12:34:56Z",
      "active": true
    }
  ],
  "window": {
    "ref": "w_PvN7Hk2mQe8sRg4yUa6cDwXy",
    "width": 132,
    "height": 36
  },
  "capabilities": [
    { "mode": "fixed", "supported": true },
    { "mode": "fit-once", "supported": true },
    { "mode": "follow-device", "supported": false }
  ],
  "applied": {
    "mode": "fit-once",
    "width": 132,
    "height": 36
  }
}
```

Each browser tab stores its own `viewerId` in `sessionStorage` and sends periodic resize heartbeats with the current terminal size. The Resize panel identifies web viewers by browser/IP, terminal size, and last-seen time so users can choose the intended tab when multiple web windows are open.

On mobile Safari/iOS, the page also tracks `visualViewport` changes from the
software keyboard. This is local layout handling only: the web terminal iframe
is refit above the keyboard and the tab heartbeat is refreshed as transient.
Keyboard viewport resize/scroll events are kept separate from the debounced,
stable layout/orientation resize path, so opening or closing the keyboard does
not request a terminal grid resize or deliver SIGWINCH to the tmux application.
Tmux resize mode is not changed unless the user explicitly applies a Resize
panel mode.

Resize modes:

- `Fixed`: keeps `window-size manual` and preserves the current tmux window dimensions.
- `Fit once`: selects one active browser viewer, applies its reported dimensions once with manual sizing, then returns to fixed behavior. Later viewer heartbeats do not resize tmux.
- `Follow this device`: visible as a future capability but disabled until continuous following is implemented.

These modes never set `window-size latest` or restore automatic
smallest-client sizing. Local History and iOS visual viewport changes also
never call `resize-window`.

Example T-Control command:

```json
{
  "action": "select-window",
  "windowRef": "w_PvN7Hk2mQe8sRg4yUa6cDwXy",
  "paneRef": "p_F4jLm9aWx3cTzQ8vBnHsKA2b"
}
```

T-Control intentionally uses an action allowlist instead of accepting arbitrary tmux commands from the browser. The web panel shows tmux windows, lets users switch windows, and exposes common window/pane controls.

The main menu also includes `Resize`, which opens the explicit sizing panel.
Users can preserve the current fixed dimensions or fit once to a selected
browser viewer; continuous device following is clearly marked as unavailable.

## systemd User Service Details

Install the binary and user systemd unit:

```sh
make install
```

`make install` also creates `~/.config/control-agents/env` with a generated password when it does not already exist. Edit it if you want a custom password or bind address:

```sh
CONTROL_AGENTS_PASSWORD=<generated>
CONTROL_AGENTS_BIND_ADDR=0.0.0.0
CONTROL_AGENTS_PORT=8080
```

The same target installs the server as `~/.local/bin/control-agents-server` and the Go client as `~/.local/bin/control-agents`. Override paths with `SERVER_INSTALL=/path/to/control-agents-server` and `CLIENT_INSTALL=/path/to/control-agents` if needed.

Run the service and invoke the client as the same Unix account. That account
owns the tmux server, `ttyd` bridges, registry and lock files, and forwarded
agent link. A root or system service, another SSH account, or a unit with a
different state directory cannot transparently manage those sessions.

Enable and start:

```sh
systemctl --user enable control-agents.service
systemctl --user restart control-agents.service
```

After later updates, rebuild, reinstall, and restart the service:

```sh
make install restart
```

Check logs:

```sh
journalctl --user -u control-agents.service -f
```

Uninstall the binary and user systemd unit:

```sh
make uninstall
```

`make uninstall` does not remove `~/.config/control-agents/env` or the state directory.

## Container Deployment Limitations

The provided `Containerfile` builds only `control-agents-server`; it is not the
default deployment model and does not include the Go client, tmux, or `ttyd`.
A standalone container cannot see or manage tmux sessions created by SSH on the
host. Doing so would require an explicit deployment design that shares the
same Unix identity, process/session namespaces, lifecycle state directory, tmux
socket, terminal bridge runtime, and Unix sockets. The supported default is the
same-account `systemd --user` service described above.

## Security

See [`SECURITY.md`](SECURITY.md) for supported versions, vulnerability reporting, threat model notes, and deployment guidance.

The service uses HTTPS by default with an automatically generated self-signed ECC certificate and accepts TLS 1.3 only. Older protocol versions, including TLS 1.2, are disabled. The password, cookies, terminal output, and terminal input are encrypted on the wire, but the browser cannot verify a self-signed certificate until you trust it locally or configure `CONTROL_AGENTS_TLS_CERT_FILE` and `CONTROL_AGENTS_TLS_KEY_FILE` with a certificate from a trusted authority.

Go-served pages and API responses include security headers: a self-only CSP
without inline scripts for the app shell, `X-Frame-Options: SAMEORIGIN`,
`X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`, and a
restrictive `Permissions-Policy`. The proxied ttyd page uses the minimal
compatible `frame-ancestors 'self'` policy and loads the Control Agents
transport observer from an external same-origin asset.

For local-only access, bind to `127.0.0.1` and use SSH port forwarding:

```sh
ssh -L 8080:127.0.0.1:8080 user@vm
```

## License

Control Agents is licensed under the GNU Affero General Public License v3.0. See [`LICENSE`](LICENSE).

## Troubleshooting

- Service logs: use `journalctl --user -u control-agents.service -n 100 --no-pager` or follow live with `journalctl --user -u control-agents.service -f`.
- No tabs appear: run `control-agents`, create or select a managed session, and
  confirm the client and user service run as the same Unix account with the
  same `CONTROL_AGENTS_STATE_DIR`. Arbitrary tmux sessions do not appear.
- No tabs appear but `<state-dir>/sockets/<session>.sock` exists: reinstall and
  restart the user unit so the same-account service gets the managed `PATH`
  that includes `tmux` and `ttyd`.
- Tab opens but terminal is unavailable: check `<state-dir>/logs/<session>.log` for `ttyd` errors.
- Tab opens but its bridge was killed: request the session list or restart the
  user service. Reconciliation replaces the bridge and socket while preserving
  the live tmux session and its contents.
- Session disappears: the service removes a managed record only when its tmux
  session is gone or the record is unsafe/invalid. A missing `ttyd` PID or Unix
  socket alone is recovered without recreating tmux.
- Lifecycle cleanup reports `pidfd_open` as unsupported: use Linux kernel 5.3 or newer. Stable process handles are required so a reused numeric PID can never redirect a stop signal to an unrelated process.
- Browser and SSH sizes differ: both clients attach to the same tmux session,
  whose dimensions remain fixed with `window-size manual` by default. Use Menu
  -> Resize -> Fit once to apply one browser viewer's dimensions, or Fixed to
  preserve the current size. Attaching a narrower client does not
  automatically shrink the shared tmux window.
- Browser history is too short while the web tab is connected: increase `CONTROL_AGENTS_WEB_SCROLLBACK_LINES` before starting `bin/control-agents <name>`, then reconnect the web tab.
- Older tmux output is missing after increasing the history limit: discarded
  pane history is not recoverable retroactively. New output and newly created
  panes use the reconciled 50,000-line limit.
- Mouse wheel cycles shell command history: reinstall and restart the service,
  then use Menu -> History. History scroll is local and does not send wheel
  events or tmux copy-mode commands to the shell.
- On narrow mobile screens, the terminal area has horizontal scrolling so the tmux pane can keep a usable width without rotating the device.
