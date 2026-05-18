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
- `ttyd` instances are proxied through private Unix domain sockets, not exposed directly as TCP ports by the wrapper.
- Mutating API routes and terminal WebSocket upgrades require same-origin requests.
- The browser UI can send input to the active terminal, paste clipboard text, and trigger allowlisted tmux controls.
- Anyone who authenticates to the web UI can effectively operate the exposed shell sessions as the service user.

## Deployment Guidance

- Use a strong, unique `CONTROL_AGENTS_PASSWORD`.
- Prefer binding to a private interface, VPN, SSH tunnel, or trusted reverse proxy instead of exposing the service directly to the public internet.
- Replace the generated self-signed certificate with a trusted TLS certificate for remote use.
- Run the service as a dedicated, least-privileged user.
- Keep `tmux`, `ttyd`, Go, and the host OS patched.
- Review `~/.config/control-agents/env` permissions and keep it readable only by the service user.
- Avoid running highly privileged shells or root sessions through the web UI.

## Known Sensitive Areas

- Authentication, cookie handling, login rate limiting, and same-origin checks.
- `/terminal/` proxying and WebSocket upgrade handling.
- `/api/sessions/{session}/paste`, because it writes clipboard text into a shell.
- `/api/sessions/{session}/tmux-control`, because it triggers allowlisted tmux actions.
- Session registry files and Unix socket path handling.

## Not A Vulnerability

The following are expected when the service is configured this way:

- An authenticated user can read terminal output and send terminal input.
- A user with the configured password can paste text into the active shell.
- A browser may warn about the default self-signed certificate until it is trusted locally or replaced.
