# TASKS-00 — Základní invarianty, tmux model a interní identita

Status: done

Dependencies: none

## Cíl

Připravit bezpečný základ pro všechny další etapy bez změny současného Live terminálu.

## Schválená migrační rozhodnutí

- Tato etapa smí dočasně ponechat ttyd pouze jako Live transport; konečný stav
  roadmapy odstraní starý vzdálený tmux-scroll, samostatný Copy mode, iframe i
  ttyd.
- Veřejné webové API přestane používat canonical session names, raw tmux
  targets a window indexes jako autorizační identitu. Uživatelské názvy smějí
  zůstat pouze jako display data a explicitní potvrzení destruktivní akce.
- Nové opaque reference se po restartu serveru mohou změnit; browser musí
  topologii znovu načíst. Mutace musí vždy znovu ověřit aktuální pane
  generation.
- Změna defaultní resize politiky je výslovným účelem této etapy a nahrazuje
  dosavadní default `window-size smallest`.
- Implementace nesmí restartovat současný deploy; deployment patří až do
  release/rollout etapy.

## Úkoly

### CA-0001 — Zavést opaque identifikátory
- Priorita: P0
- Závislosti: žádné

- [ ] Vytvořit interní typy `SessionRef`, `WindowRef`, `PaneRef`, `ViewerID` a `PaneGeneration`.
- [ ] Browseru nikdy neposílat shell command, raw tmux target ani uživatelsky sestavený target string.
- [ ] Mapovat opaque reference na přesný tmux ID, například `%42`.
- [ ] Do `PaneGeneration` zahrnout minimálně start tmux serveru a `pane_id`.
- [ ] Před každou mutující akcí ověřit, že generation stále odpovídá.

**Acceptance criteria**
- Změnou URL nebo JSON payloadu nelze přistoupit k cizímu pane.
- Zaniklý pane nelze zaměnit za nově vytvořený pane se stejným názvem.

### CA-0002 — Zrušit shell interpolation
- Priorita: P0

- [ ] Auditovat všechna volání tmux a ttyd.
- [ ] Nahradit `/bin/sh -c` přímým `exec.CommandContext` s pevnými argumenty.
- [ ] Zakázat předávání uživatelských hodnot do tmux format stringů bez validace.
- [ ] Přidat testy pro mezery, uvozovky, středník, newline a shell metaznaky.

**Acceptance criteria**
- Security test nedokáže spustit vedlejší příkaz přes session name, pane target ani paste metadata.

### CA-0003 — Nastavit historii tmuxu
- Priorita: P0

- [ ] Nastavit `history-limit 50000` před vytvářením nových pane/window.
- [ ] Ověřit chování již existujících session.
- [ ] Zdokumentovat, že změna limitu není retroaktivní pro již vytvořené pane podle použité tmux verze.
- [ ] Přidat serverový byte limit snapshotu, výchozí hodnota 32 MiB.

**Acceptance criteria**
- Nově vytvořený pane reportuje history limit 50 000.
- Extrémně dlouhý řádek nepřekročí serverový memory budget.

### CA-0004 — Přepnout resize politiku na explicitní režim
- Priorita: P0

- [ ] Přestat používat `window-size smallest` jako dlouhodobý default.
- [ ] Zavést default `window-size manual`.
- [ ] Přidat metadata aktuální velikosti tmux window.
- [ ] Připravit UI/API koncept pro `Fixed`, `Fit once` a pozdější `Follow this device`.
- [ ] Zajistit, aby otevření klávesnice na iOS neprovádělo tmux resize.

**Acceptance criteria**
- Připojení úzkého iPhonu samo nezmenší SSH klienta.
- History resize nikdy nevolá `resize-window`.

### CA-0005 — Definovat audit bez obsahu
- Priorita: P0

- [ ] Definovat povolené audit fields: opaque ID, status, počet bajtů, duration, reason code.
- [ ] Zakázat logování request/response body pro History a Paste endpointy.
- [ ] Zakázat WebSocket frame logging.
- [ ] Zakázat tmux verbose logging v produkci.
- [ ] Přidat canary test proti `journalctl`, access logům, traces a metrics.

**Acceptance criteria**
- Unikátní canary text z terminálu ani Paste není nalezen v žádném produkčním logovacím výstupu.

## Výstup etapy

- Bezpečné interní identity.
- Stabilní tmux resize model.
- Definovaný 50k history limit.
- Prokazatelné nulové logování terminálového obsahu.

## Reference

- `AGENTS.md`.
- `TASKS/README.md`.
- `README.md` and `SECURITY.md`.
- `internal/registry` for durable managed-session records.
- `internal/session` for lifecycle, locking, bridge startup and reconciliation.
- `internal/server` and `internal/server/static` for public API and browser UI.
- `internal/tmux` for direct tmux execution and topology metadata.
- `test/e2e` and `test/playwright` for lifecycle, tmux and browser coverage.

## Validace

Run at minimum:

```sh
gofmt -w <changed Go files>
node --check internal/server/static/app.js
make test
make test-e2e
make test-browser
git diff --check
```

The implementation summary must record the public API break, opaque-reference
negative tests, pane-generation mutation checks, history-limit behavior for
new and existing panes, resize behavior, content-free audit coverage and all
validation results. Do not deploy or restart the installed service.

## Implementation summary

- Introduced typed opaque `SessionRef`, `WindowRef`, `PaneRef`, `ViewerID`, and
  `PaneGeneration` identities. The public API and terminal proxy now use opaque
  refs instead of canonical names, raw tmux targets, or window indexes. Every
  mutation refreshes topology, resolves an exact internal tmux ID, and verifies
  the active pane generation using tmux server start time, validated server PID,
  and pane ID.
- Added negative coverage for canonical/cross-session URL substitution, foreign
  pane payloads, stale refs after pane replacement, and generation invalidation
  after a tmux server restart. Browser coverage also verifies that iframe URLs
  and T-Control payloads expose only opaque refs.
- Audited tmux and ttyd startup paths and kept production execution on direct
  `exec.CommandContext` argument vectors. Regression tests cover spaces,
  quotes, semicolons, newlines, command substitution, and other shell
  metacharacters without secondary command execution.
- Set `history-limit 50000` before first managed-pane creation and reconcile it
  across existing managed windows. Integration coverage proves that new panes
  inherit 50,000 lines and that output already discarded under an older limit
  is not restored. Copy snapshots have a configurable 32 MiB default byte cap,
  including an extreme-line limit test.
- Changed the default resize policy to `window-size manual`, exposed current
  opaque window identity/dimensions, and replaced automatic resize sources with
  `Fixed`, one-shot `Fit once`, and disabled future `Follow this device` UI/API
  capabilities. Browser tests prove that a narrow attached client does not
  shrink the fixed window and that local/mobile viewport changes do not apply a
  tmux resize; history operations remain resize-free.
- Added content-free terminal audit records restricted to opaque ID, status,
  byte count, duration, and reason code. Unit and end-to-end canary tests verify
  terminal and Paste content is absent from application/journal-style and ttyd
  log output; request/response bodies and WebSocket frames are not logged, and
  tmux is not started with verbose logging.
- Documented the breaking public API, history and snapshot limits, explicit
  resize behavior, opaque identity rules, and audit policy in `README.md`,
  `SECURITY.md`, `AGENTS.md`, and `CHANGELOG.md`.
- Validation passed: Go formatting, `node --check
  internal/server/static/app.js`, `node --check test/playwright/app.spec.js`,
  `make test`, `make build`, `make test-e2e`, `make test-browser` (7/7), and
  `git diff --check`.
- No installed service was restarted and no deployment was performed.
- Independent-review corrections strengthen `PaneGeneration` with the validated
  tmux server PID while retaining server start time and pane ID; equal
  start-time/pane-ID reuse under a changed server incarnation now invalidates
  the old opaque reference. Unit coverage rejects malformed incarnation data
  and exercises the same-start/same-pane/different-PID case at both tmux and
  identity-store boundaries.
- Existing managed-session reconciliation now sets every registered tmux
  window to `window-size manual` without invoking `resize-window`. Unit coverage
  checks every resolved raw window ID, and the real-tmux E2E migration starts in
  legacy `smallest`, proves reconciliation changes it to `manual`, and proves
  width and height remain unchanged.
- Capture and scroll current-pane topology failures now keep their metadata-only
  audit status and reason synchronized with the emitted HTTP 502 response;
  regression coverage asserts `status=502` and `dependency_failure`, with no
  stale 409 audit status.
- Review-correction validation passed: focused `go test ./internal/tmux
  ./internal/server ./internal/session`; the focused real-tmux sizing migration;
  `node --check internal/server/static/app.js`; `node --check
  test/playwright/app.spec.js`; `make test`; `make build`; `make test-e2e`; and
  `git diff --check`. `make test-browser` passed 7/7 on the full rerun; an
  earlier full run had one transient lifecycle response timeout, and that exact
  scenario subsequently passed alone 1/1 before the successful full rerun.
- No installed service was restarted and no deployment was performed while
  resolving the review findings.
- Second-round review correction now creates a short internal bootstrap pane
  that directly executes `tmux wait-for`, sets `history-limit 50000` on the
  exact managed session, installs its window policy, creates the durable user
  window, and removes the bootstrap in one tmux command queue. Production no
  longer writes the global history option, so unrelated tmux sessions retain
  their own effective limit while new and reconciled managed panes use 50,000.
- A session-local indexed `window-linked` hook makes every later managed window
  manual synchronously, including windows created directly through tmux/SSH.
  Reconciliation installs the hook before enumerating existing windows, so a
  concurrently created window is covered by either the hook or the migration;
  no `resize-window` call or periodic-reconciliation gap is involved.
- Unit coverage asserts the exact session history target, absence of a global
  history mutation, bootstrap policy ordering, hook installation before window
  enumeration, and manual migration of every raw managed window ID. A real
  tmux negative test starts with global `history-limit 3456` and `window-size
  latest`, proves both the initial and a directly created later managed window
  are manual with 50,000-line history, and proves an unmanaged session plus its
  later window remain at 3,456/latest with both global defaults unchanged.
- Second-round correction validation passed: focused `go test
  ./internal/tmux ./internal/session`; the focused real-tmux managed/unmanaged
  policy test; Go formatting; `node --check internal/server/static/app.js`;
  `node --check test/playwright/app.spec.js`; `make test`; `make build`; `make
  test-e2e`; `make test-browser` (7/7); and `git diff --check`.
- No installed service was restarted and no deployment was performed while
  resolving the second-round review findings.
- Main-agent race-validation correction makes the shared terminal API test
  runner concurrency-safe: call recording is serialized, assertions consume
  deep-copied call snapshots, and call reset, injected topology failures, and
  bounded-read observations all use the same lock. This preserves the
  production create/terminate concurrency exercise without sleeps or timing
  workarounds and prevents assertions from retaining aliases to mutable call
  storage.
- Race-correction validation passed: the formerly failing
  `TestCreateRacingWithTerminateIsSerializedPerSession` under `-race` for 50
  uncached repetitions; the specified uncached `CGO_ENABLED=1 go test -race
  -count=1 ./internal/tmux ./internal/session ./internal/server
  ./internal/proxy ./internal/registry`; an uncached `CGO_ENABLED=1 go test
  -race -count=1 ./...` adjacent-race sweep; Go formatting; `node --check
  internal/server/static/app.js`; `node --check test/playwright/app.spec.js`;
  `make test`; `make build`; `make test-e2e`; `make test-browser` (7/7); and
  `git diff --check`.
- No installed service was restarted and no deployment was performed while
  resolving the race-validation finding.

## Independent review history (resolved)

All findings below were corrected, regression-tested, and approved by a fresh
final reviewer. They are retained as implementation history.

1. Pane generation currently uses second-resolution tmux `#{start_time}` plus
   `pane_id`. Tmux 3.7b can restart within the same second and reuse `%0`, so a
   stale `PaneRef` can authorize a replacement pane. Add a validated strong
   tmux server-incarnation discriminator, such as server PID, while retaining
   start time and pane ID. Add a regression with equal start time and pane ID
   but a changed server incarnation.
2. Startup/use reconciliation of an existing managed session configures history
   and SSH agent state but does not migrate `window-size smallest` or another
   legacy automatic mode to `manual`. The resize API can report `fixed` while
   tmux remains `smallest`. Reconcile existing managed windows to manual/fixed
   sizing without changing their current dimensions and add an E2E migration
   assertion.
3. When current-pane topology lookup fails, capture and scroll write HTTP 502
   but their metadata-only audit records report 409. Make the audit status match
   the actual response and add status-accuracy coverage.

The first review round above was corrected and independently verified. A second
fresh review found these additional blockers:

4. `ConfigureManagedSession` makes only the initial window manual. A later
   T-Control or SSH-created window can inherit global `window-size latest` until
   periodic reconciliation. Ensure every newly created window in a managed
   session is manual immediately, including windows created outside the web
   action, without a resize race. Add unit and real-tmux coverage proving the
   initial and subsequent managed windows are manual.
5. `history-limit 50000` is currently applied with a global tmux option, which
   changes effective history retention for unmanaged sessions. Scope the
   history default to the exact managed session before creating its durable
   user pane/windows, reconcile existing managed panes as far as tmux permits,
   and prove an unmanaged session remains unchanged. Do not use a temporary
   global option that can race with unrelated window creation.

The second review round above was corrected and independently approved. Main
agent validation found one additional test-infrastructure blocker:

6. `CGO_ENABLED=1 go test -race ./internal/tmux ./internal/session
   ./internal/server ./internal/proxy ./internal/registry` reports concurrent
   reads/writes to `terminalAPIRunner.calls` in
   `TestCreateRacingWithTerminateIsSerializedPerSession`. Make the shared fake
   runner and its assertions concurrency-safe so the race detector can verify
   the production identity/lifecycle synchronization rather than fail inside
   the test double. Add no sleeps or timing-only workaround.

## Main-agent final validation

Completed on 2026-07-13 after all review corrections:

- `CGO_ENABLED=1 go test -race -count=1 ./...` — passed all packages.
- `make test` — passed all packages.
- `make test-e2e` — passed the complete real-process tmux/ttyd suite in 2.546s.
- `make test-browser` — passed all 7 Chromium tests in 22.9s.
- `go vet ./...` — passed.
- Both JavaScript syntax checks and `git diff --check` — passed.
- The validation-created `bin/control-agents` was removed afterward to preserve
  its pre-existing deleted worktree state.
- No installed service was restarted and no deployment was performed.
