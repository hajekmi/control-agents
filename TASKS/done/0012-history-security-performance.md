# TASKS-06 — Security hardening a výkonové limity

Status: done

Dependencies: `0009-terminal-identity-foundation.md`, `0010-local-history-copy-mvp.md`, `0011-history-ios-desktop-ux.md`

## Cíl

Omezit XSS, command injection, memory exhaustion, CSRF, IDOR a únik citlivého terminálového obsahu.

## Schválená migrační rozhodnutí

- Bezpečnostní limity se vztahují na cílové API, nikoliv na zachování legacy
  endpointů. Legacy scroll/capture cesty mají být odstraněny po migraci UI.
- Observability smí obsahovat pouze opaque identity, počty bajtů, duration a
  reason codes; nikdy session names, commands ani terminal input/output.
- Hardening nesmí znemožnit privátní Unix sockety, tmux, SSH agent forwarding
  ani user-local systemd deployment.
- CSRF protection applies to every current authenticated HTTP mutation,
  including lifecycle, History, Paste/token, keys, T-Control, and resize APIs.
  Terminal WebSocket upgrades continue to require an exact same-origin
  `Origin`; future lease endpoints must reuse the CSRF mechanism when added.
- Go-served application pages and assets use a self-only CSP without inline
  scripts. While ttyd remains, its proxied HTML must at least enforce
  `frame-ancestors 'self'`; replace the task-0011 inline transport observer with
  a same-origin external asset if required for the compatible policy and test
  the real ttyd page before applying additional terminal CSP directives.
- `PrivateTmp=true` is not compatible with the current shared tmux model: it
  would put the user service's tmux socket in a different `/tmp` namespace from
  SSH clients. Record this explicit exception and enable only hardening that
  preserves shared SSH/tmux attachment and normal managed-shell operation.
- Snapshot create rate limiting and concurrent request coalescing are scoped by
  concrete login, viewer, opaque pane/generation, and mode. Never share capture
  output across an authorization boundary; a rejected/failed leader releases
  waiters with the same content-free status and does not cache terminal data.
- Paste obtains a short-lived, single-use token bound to the concrete login,
  session ref, pane generation, and staged action. Consume it before the
  terminal mutation so a retry cannot repeat Paste. Terminal content is loaded
  through stdin, never argv, environment, logs, metrics, or error text; any
  created tmux buffer is deleted on every success/failure path.
- No installed service restart or deployment is part of this task. Production
  journal/core/diagnostic evidence remains a release gate; repository tests
  must provide equivalent content-free canary coverage now.

## Úkoly

### CA-0601 — Same-origin a session ochrany
- Priorita: P0

- [x] Secure, HttpOnly a vhodná SameSite session cookie.
- [x] Přesná kontrola `Origin` pro WebSocket.
- [x] CSRF token pro create snapshot, delete snapshot, Paste, resize a lease.
- [x] CSP bez CDN a inline scriptů, kde je to praktické.
- [x] `frame-ancestors 'self'` po dobu existence ttyd iframe.
- [x] `Cache-Control: no-store` pro terminálová API.

### CA-0602 — Unix socket a service sandbox
- Priorita: P0

- [x] Ttyd socket umístit do privátního runtime adresáře s `0700`.
- [x] Socket permissions maximálně `0600`.
- [x] Ověřit, že ttyd nemá veřejný TCP listener.
- [x] Přidat systemd hardening podle kompatibility: `NoNewPrivileges`, `PrivateTmp`, `ProtectSystem`, `RestrictAddressFamilies`.
- [x] Nastavit `LimitCORE=0`.

### CA-0603 — Terminal content sanitization
- Priorita: P0

- [x] Zakázat raw `innerHTML` pro capture data.
- [x] Allowlist ANSI atributů.
- [x] Zahodit OSC clipboard a title sekvence.
- [x] Vypnout auto-linkification v první fázi.
- [x] Ošetřit bidi control characters alespoň vizuálním warning režimem pro copy.
- [x] Omezit maximální délku jednoho logického řádku.

### CA-0604 — Snapshot resource budgets
- Priorita: P0

- [x] Max bytes per snapshot.
- [x] Max ANSI runs per line a snapshot.
- [x] Max active snapshots per viewer/user/process.
- [x] Capture timeout a cancellation přes context.
- [x] Rate limit create snapshot.
- [x] Coalescing identických současných create požadavků.
- [x] Měřit parse duration, bytes a node estimate bez obsahu.

### CA-0605 — Paste hardening
- Priorita: P0

- [x] Limit 64 KiB po UTF-8 encodingu.
- [x] NUL a invalid UTF-8 odmítnout.
- [x] Content nikdy nevložit do argv nebo logů.
- [x] Náhodný buffer name.
- [x] Buffer vždy odstranit i při chybě, pokud byl vytvořen.
- [x] Zobrazit varování pro multiline, control chars a trailing newline.
- [x] Idempotency řešit tak, aby se Paste neopakoval; preferovat single-use request token.

### CA-0606 — SSH keys a credentials
- Priorita: P0

- [x] Ověřit, že aplikace neukládá privátní SSH klíče na VM.
- [x] Dokumentovat podporovaný model SSH agent forwarding nebo uživatelských credentials mimo VM.
- [x] Zakázat upload privátních klíčů přes webové API.
- [x] Přidat kontrolu secrets v deployment adresářích a backup policy.

### CA-0607 — Memory dump a observability policy
- Priorita: P1

- [x] Production pprof chránit nebo vypnout.
- [x] Zakázat automatické heap dumpy obsahující snapshoty.
- [x] Zvážit encrypted nebo disabled swap podle threat modelu.
- [x] Redigovat panic context.
- [x] Metriky nesmí používat command, title ani session name jako label.

## Acceptance criteria

- Security test suite projde pro XSS, CSRF, IDOR, command injection a resource exhaustion.
- Terminálový canary není v logu, trace, metric labelu, core dumpu ani diagnostickém exportu.

## Reference

- `AGENTS.md` and `TASKS/README.md`.
- `TASKS/done/0009-terminal-identity-foundation.md`,
  `TASKS/done/0010-local-history-copy-mvp.md`, and
  `TASKS/done/0011-history-ios-desktop-ux.md`.
- `README.md`, `SECURITY.md`, `CHANGELOG.md`, and
  `systemd/user/control-agents.service`.
- `internal/auth`, `internal/server`, `internal/server/static`, `internal/proxy`,
  `internal/session`, and `internal/tmux` for cookies, CSRF, APIs, CSP, socket
  lifecycle, content parsing, rate/coalescing budgets, Paste, and audit data.
- `test/e2e`, `test/playwright`, and `test/install` for real tmux/ttyd, browser,
  service-unit, secret-scan, and content-free canary coverage.

## Validace

Run at minimum:

```sh
node --check internal/server/static/app.js
node --check test/playwright/app.spec.js
make test
make build
make test-e2e
make test-browser
CGO_ENABLED=1 go test -race -count=1 ./...
go vet ./...
git diff --check
```

Also validate the service unit with `systemd-analyze verify` when that command
is available. Tests must cover cookie flags/expiry, exact Origin and CSRF
failure/success, CSP on app and terminal HTML, no inline injected observer,
private socket modes/no TCP listener, compatible unit hardening and the
documented PrivateTmp exception, XSS/OSC/bidi/line and parser-storm handling,
snapshot timeout/cancellation/rate/coalescing/budgets with cross-scope negative
cases, content-free parse measurements, Paste UTF-8/NUL/size/trailing-newline
warnings, stdin-only buffer loading, unconditional buffer cleanup and
single-use tokens, absence of key-upload/pprof/heap-dump surfaces, deployment
secret scanning, and terminal/Paste canaries absent from every captured log,
metric label, diagnostic, error, and command argument.

Do not read live production logs, generate a core dump, deploy, or restart the
installed service in this task. Record any physical/operator evidence still
required in `0018-release-rollout.md`.

## Implementation summary

Implemented the approved security and resource-boundary migration without
deploying or restarting the installed service. All authenticated HTTP
mutations now use a concrete-login-bound CSRF token and same-origin validation;
terminal WebSocket upgrades require an exact serialized Origin. Go-served app
responses use a self-only CSP with external scripts, ttyd HTML keeps
`frame-ancestors 'self'`, and terminal APIs are non-cacheable.

The managed ttyd bridge remains Unix-socket-only under private `0700` state
directories and reconciles direct socket permissions to `0600`. The user unit
adds compatible privilege, filesystem, address-family, and core-dump limits.
`PrivateTmp=false` is the documented compatibility exception because SSH and
the service must share the tmux socket namespace under `/tmp`.

History capture now has bounded bytes, parser lines/runs/structured nodes,
snapshot counts and process memory/nodes, context timeout/cancellation,
per-key rate limits, process/login concurrency limits, and bounded exact-key
coalescing. Logs contain only opaque references, byte/node counts, durations,
exact HTTP status, and reason codes. ANSI rendering remains structured DOM text,
strips non-SGR controls including OSC clipboard/title data, replaces bidi
controls with visible warnings, and does not auto-linkify terminal content.

Paste now stages a short-lived single-use token bound to login, session, pane
generation, and exact action metadata. UTF-8, NUL, and 64 KiB limits are
enforced; text reaches tmux only over stdin into a random named buffer, which is
cleaned on every path. Mutation bodies and retained viewer metadata are bounded,
viewer counts are capped, existing auth permissions are reconciled, startup and
panic logs are content-free, and no key-upload, pprof, heap-dump, or persistent
VM private-key surface was introduced.

All ten independent findings were resolved. The final browser correction keeps
T-Control window DOM nodes stable by opaque `windowRef` during background
refreshes and separates refresh from mutation-pending state, with a
pointer-down/refresh/pointer-up regression proving the select action is not
lost.

Main-agent validation passed `make test`, `make build`, `make test-e2e`,
`make test-browser` (9/9), `CGO_ENABLED=1 go test -race -count=1 ./...`,
`go vet ./...`, JavaScript syntax checks, changed-Go formatting checks,
`git diff --check`, secret-pattern scanning, and `systemd-analyze --user
verify`. Independent review repeated the targeted T-Control test five times and
the full browser suite with no findings. Production logs, core evidence, and
physical Safari checks remain operator release gates in task 0018.

## Independent review history (resolved)

1. Add process-wide and concrete-login capture concurrency limits plus a
   bounded number of coalesced waiters. Exact-key rate limiting alone is
   bypassable by rotating valid viewer IDs and permits many simultaneous
   captures before snapshot memory limits apply. Preserve exact
   login/viewer/pane-generation/mode coalescing and add adversarial concurrent
   cross-viewer and waiter-bound regressions.
2. Make the application-shell CSP genuinely self-only. Do not allow arbitrary
   `ws:`, `wss:`, or `data:` sources; the separately tested terminal iframe CSP
   owns ttyd compatibility. Strengthen exact policy assertions.
3. Consume the content-free `NodeEstimate` measurement in a bounded budget,
   audit record, or metric with the approved label schema. A calculated but
   otherwise dead value does not satisfy CA-0604; add behavior/observability
   coverage without terminal content.
4. Restore the exact HTTP status field in terminal audit records while keeping
   the allowlist content-free. Add status-accuracy and canary regressions.
5. Sanitize startup reconciliation failures. Do not log the verbatim lifecycle
   error because it can contain canonical session names and state paths; emit
   only an approved reason code and add a startup-log canary regression.
6. Bound and strictly decode every authenticated mutation body, including
   Keys, Resize, viewer heartbeat, and T-Control. Limit retained viewer
   metadata such as User-Agent, enforce per-session and process viewer
   capacity, reject excess state without evicting active trusted entries, and
   add oversized-body/string plus viewer-flood regressions.
7. Exact Origin validation must accept only a serialized origin. Reject
   userinfo, non-origin path, query, and fragment components for both HTTP
   mutations and terminal WebSocket upgrades; add malformed-origin negatives.
8. Reconcile an existing authentication-secret directory to `0700` and secret
   file to `0600`, including the concurrent `O_EXCL` race branch, before using
   the secret. Add permissive-mode migration tests.
9. Reject an empty fragment delimiter in a raw Origin. Go URL parsing does not
   preserve a trailing `#`, so `https://control.test#` must be rejected before
   or alongside parsed-component validation for both HTTP mutations and
   terminal WebSocket upgrades. Add exact empty-fragment regressions.
10. Fix the T-Control refresh race exposed by the full browser validation. A
    concurrent topology refresh can replace a window button between pointer
    down and click, so the visible click is lost and no `select-window`
    mutation is sent. Preserve live refresh without replacing an unchanged
    actionable window DOM node during the interaction, add a deterministic
    browser regression for the concurrent refresh/select case, and keep the
    existing T-Control API and CSRF contract unchanged.
