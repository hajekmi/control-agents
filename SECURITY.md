# Security Policy

Control Agents exposes authenticated browser access to live `tmux` sessions. Treat every deployment as remote shell access to the account running the managed sessions.

## Supported Versions

Security fixes are provided for the latest tagged release only. Upgrade to the newest release before reporting an issue unless the problem prevents upgrading.

## Reporting A Vulnerability

Please report security issues privately first.

- If GitHub private vulnerability reporting is enabled for this repository, use that channel.
- Otherwise, contact the maintainer out of band before opening a public issue.
- Include the affected version, deployment mode, relevant configuration, reproduction steps, and impact.

Do not include live passwords, cookies, private keys, terminal output containing secrets, or public exploit links in an initial report.

## Security Model

- The web service is password protected and serves HTTPS by default.
- `ttyd` instances are proxied through private Unix domain sockets, not exposed
  directly as TCP ports by the shared lifecycle.
- Authenticated mutating API routes and terminal WebSocket upgrades require
  exact same-origin requests. Every authenticated HTTP mutation also requires
  a token bound to the concrete signed login. Authentication or a token alone
  does not bypass either check.
- Authenticated mutation bodies are byte-bounded and strictly decoded as one
  known-field JSON object. Resize viewer identifiers, dimensions, retained
  client metadata, and per-session/process viewer counts are bounded; capacity
  rejection never evicts an active viewer entry.
- Authenticated session creation and termination are remote shell lifecycle
  operations. Browser creation can start a new shell as the service user.
  Confirmed termination is destructive: it kills the tmux session and
  disconnects every attached SSH and browser client, not only the requester.
- Managed names must match `[A-Za-z0-9][A-Za-z0-9._-]{0,63}` exactly. Creation
  accepts no arbitrary command, shell, environment, tmux arguments, or working
  directory and always starts in the service user's home directory.
- `CONTROL_AGENTS_MAX_SESSIONS` limits web-created sessions (32 by default) to
  reduce accidental resource exhaustion. Existing sessions remain visible and
  terminable, and operators must still apply host-level CPU, memory, process,
  and storage limits appropriate to their deployment.
- Public session, window, pane, and viewer identities are opaque references.
  API objects omit canonical tmux targets, raw pane/window IDs, window indexes,
  bridge PIDs, Unix socket paths, and other process internals.
- Every terminal mutation resolves its opaque references server-side and
  verifies the active pane generation from the tmux server start time,
  validated server PID, and pane ID. Cross-session, foreign-pane, and
  stale-pane references are rejected.
- History snapshots exist only in server memory and are bound to the concrete
  authenticated login, opaque viewer ID, session ref, and verified pane
  generation. They have idle expiry plus per-viewer, per-login, process-count,
  process-memory, process-node, capture-byte, page-line, page-byte, and ANSI-run
  limits. Capture concurrency is bounded per concrete login and process, and
  identical authorization scopes have a bounded coalesced-waiter count.
- ANSI parsing is server-side. History responses contain structured text/style
  runs, omit OSC/DCS/APC/PM control strings and OSC 8 links, and are rendered
  with browser text nodes rather than HTML insertion.
- The browser UI can send input to the active terminal, paste clipboard text, and trigger allowlisted tmux controls. Clipboard reads require an explicit user action; text is staged with UTF-8 byte and logical-line counts, multiline or control-character content requires confirmation, fallback textarea paste events never send directly, and failed Paste requests are not retried automatically.
- Paste additionally uses a short-lived, single-use token bound to the concrete
  login, opaque session and pane generation, and SHA-256 digest plus warning
  metadata for the staged action. The server consumes it before terminal
  mutation. Paste text reaches `tmux load-buffer` only through stdin; the
  cryptographically named temporary buffer is deleted on every later path.
- Terminal audit logs contain metadata only: opaque session reference, status,
  byte count, duration, and a reason code. History/Paste bodies, terminal
  output, and WebSocket frames are not logged, and production tmux is not run
  with verbose logging.
- Repository benchmark reports are content-free and privately written under
  `.cache/benchmarks/`. They contain fixed synthetic dataset IDs, axis labels,
  numeric resource/timing measurements, support statuses, units, and reason
  codes only; they exclude terminal text, commands, session names, live opaque
  references, cookies, and credentials.
- Playwright failure diagnostics emit only fixed test titles, projects, and
  status. They disable automatic page-text context, traces, screenshots,
  videos, HTML reports, and network artifacts. The browser targets run an
  intentional-failure canary that scans diagnostics and a deliberately
  retained sanitized error context for terminal text, Paste content, auth
  cookies, and credentials.
- Linux Playwright profiles fail closed inside a verified loopback-only network
  namespace. The default launcher uses a current-user mapping and may fall back
  once before readiness to a fixed non-interactive sudo bootstrap for hosts
  such as Ubuntu 24.04 that deny unprivileged user namespaces. That bootstrap
  uses Ubuntu's already loaded vendor `chrome` AppArmor profile solely for its
  explicit user-namespace permission, then restores the original non-root UID,
  GID, and groups, clears all process capabilities, and requires
  `NoNewPrivs: 1` before Node, the server, tmux, ttyd, or a browser starts;
  browser arguments are passed as literal argv and are never evaluated by the
  privileged shell. The original non-root launcher owns the one selected-mode
  link-churn probe, so functional `no_new_privs` browser profiles never invoke
  sudo.
  IPv4 and IPv6 routes must remain loopback-only, cancellation never triggers
  fallback, and per-attempt files and children are cleaned on every exit path.
  The privileged spawn receives only fixed locale and path values; caller
  environment values cross the boundary solely through a bounded stdin handoff
  after privilege drop. Chromium's sandbox is explicitly enabled and verified
  over every matching browser owned by the runner and every renderer below
  each browser. Every browser must retain the runner's exact non-root UID, GID,
  and supplementary groups, exclude group 0, have all five capability masks
  (including bounding) clear, and retain `NoNewPrivs`. Every renderer must have
  non-root mapped UID/GID values, exclude group 0, have clear inheritable,
  permitted, effective, and ambient masks, retain `NoNewPrivs`, run under
  seccomp mode 2, and use separate user, PID, and network namespaces. A nonzero
  renderer bounding mask is accepted only under that distinct user namespace
  with zero held capabilities and no-new-privileges; the mask is a
  namespace-local ceiling rather than a held host capability. Unrelated host
  Chromium trees are ignored. One real sudo attempt from the standalone
  ready-state probe proves that privilege cannot be regained.
- Anyone who authenticates to the web UI can effectively operate the exposed shell sessions as the service user.
- Managed shells intentionally match that Unix account's ordinary SSH
  privilege boundary. The user unit sets `NoNewPrivileges=false`, so
  interactive `sudo` remains available when the account's sudoers and
  authentication policy authorize it; Control Agents neither grants nor
  removes that authorization. After deployment, the unit must report
  `NoNewPrivileges=no`, a newly created tmux server must report
  `NoNewPrivs: 0`, and a non-interactive sudo probe may fail only through the
  account's normal password/policy path, not because no-new-privileges blocks
  elevation.
- The SSH client, tmux server, `ttyd` bridges, lifecycle state, and
  `systemd --user` service share one Unix security boundary. Control Agents
  does not isolate sessions from other processes owned by that account.
- Managed sessions use one stable, private symlink to the most recently
  refreshed SSH-forwarded agent socket for the Unix account. Control Agents
  does not run a persistent agent or copy, generate, inspect, or store private
  keys and agent protocol contents.
- Agent forwarding is available only while its owning SSH transport is alive.
  After reconnect, an SSH invocation of `control-agents` can atomically
  retarget the stable link so existing managed panes regain access.
- Any process running as the service user can request signatures from the
  forwarded agent while it is connected. The stable link does not isolate
  managed sessions from other processes owned by that Unix account.
- Control Agents deliberately does not install a persistent VM `ssh-agent` or
  load, generate, copy, or store private keys. A forwarded agent cannot remain
  available after its SSH transport disconnects because forwarding delegates
  signing; it does not transfer the private key.

## Deployment Guidance

- Use a strong, unique `CONTROL_AGENTS_PASSWORD`.
- Prefer binding to a private interface, VPN, SSH tunnel, or trusted reverse proxy instead of exposing the service directly to the public internet.
- Replace the generated self-signed certificate with a trusted TLS certificate for remote use.
- Run the service as a dedicated, least-privileged user.
- Keep `ttyd`, Go, and the host OS patched. Control Agents currently supports
  exactly checksum-verified tmux 3.7b; do not substitute an untested newer
  tmux release without updating and validating the explicit runtime contract.
- Review `~/.config/control-agents/env` permissions and keep it readable only by the service user.
- Keep the state directory private: directories use mode `0700`, while registry
  records and the persistent authentication secret use mode `0600`.
- Treat the environment file, authentication secret, generated TLS private key,
  registry, logs, and any separately managed workload credential as backup
  secrets. Prefer excluding runtime sockets and logs; encrypt necessary backups,
  restrict restore access to the service account, and test deletion/rotation.
- Disable swap on dedicated high-sensitivity hosts or use host-managed encrypted
  swap. This is an operator threat-model decision; Control Agents never writes
  History snapshots or Paste staging data to an application dump file.
- The supplied user unit disables core dumps and enables compatible filesystem,
  and address-family restrictions. It deliberately uses
  `NoNewPrivileges=false` for ordinary account-authorized interactive `sudo`;
  `PrivateTmp=true` is intentionally not used because SSH clients and the user
  service must share the same tmux socket namespace under `/tmp`. The private
  umask, read-only system protection, address-family allowlist, and zero core
  limit remain enabled.
- Avoid running highly privileged shells or root sessions through the web UI.
  Prefer an account with no sudo authorization; otherwise restrict sudoers to
  the minimum commands and authentication policy required by the operator.
- Use a separately managed, narrowly scoped deploy key, workload identity, or
  token when unattended jobs must authenticate while the forwarding computer
  is offline.

## Known Sensitive Areas

- Authentication, cookie handling, login rate limiting, and same-origin checks.
- `POST /api/sessions` and `DELETE /api/sessions/{sessionRef}`, because they create
  and terminate remote shell sessions for the service user.
- `/terminal/` proxying and WebSocket upgrade handling.
- `/api/sessions/{sessionRef}/paste`, because it writes clipboard text into a shell.
- `/api/v1/panes/{paneRef}/history-snapshots` and
  `/api/v1/history-snapshots/{snapshotId}` (including `/pages`), because they
  return bounded terminal History content or activity scoped to one login and
  viewer.
- `/api/sessions/{sessionRef}/tmux-control`, because it triggers allowlisted tmux actions.
- Session registry files and Unix socket path handling.
- The private forwarded-agent link directory and SSH-context validation.
- Core dumps, swap, backups, and host diagnostic tooling, which are outside the
  process but can expose in-memory shell content if configured unsafely.

## Not A Vulnerability

The following are expected when the service is configured this way:

- An authenticated user can read terminal output and send terminal input.
- A user with the configured password can paste text into the active shell.
- If the service account is authorized for sudo, an authenticated terminal user
  can invoke interactive `sudo` under the same policy as an SSH shell.
- A browser may warn about the default self-signed certificate until it is trusted locally or replaced.
- A forwarded agent becomes unavailable after SSH logout or transport loss
  until another valid forwarded connection invokes `control-agents`.
- When multiple SSH connections forward different agents for one Unix
  account, the most recently refreshed valid socket becomes current for all
  managed sessions.
