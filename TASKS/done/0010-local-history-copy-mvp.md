# TASKS-01 — Lokální History / Copy MVP

Status: done

Dependencies: `0009-terminal-identity-foundation.md`

## Cíl

Nahradit skokové ovládání tmux copy-mode lokálním immutable History snapshotem. Ttyd zůstává pouze pro Live terminál.

## Schválená migrační rozhodnutí

- Lokální History je nová jediná cesta pro prohlížení historie a Copy; staré
  REST scroll příkazy a samostatný capture panel se po dosažení parity
  odstraní, nikoliv dlouhodobě zachovají.
- Ttyd v této etapě zůstává jen proto, aby Live terminál běžel během History;
  jeho konečné odstranění je součástí tasku `0017-ttyd-decommission.md`.
- Snapshot je immutable, autorizovaný přes opaque references a nesmí měnit
  tmux copy-mode, aktivní pane/window ani sdílenou velikost SSH klienta.
- Implementace nepřidá frontend build krok ani CDN závislost.
- V současném single-user deploymentu znamená user scope konkrétní
  autentizované přihlášení, nikoliv canonical session name. Snapshot se navíc
  váže na opaque viewer ID, session ref a ověřenou pane generation; jiný login
  nebo browser viewer jej nesmí číst.
- ANSI parsing a resource enforcement probíhají server-side a API vrací pouze
  strukturované text/style runy renderované přes text nodes. Browser nikdy
  nevkládá capture data přes `innerHTML`.
- Po zelené paritě v rámci tohoto tasku se odstraní legacy `/scroll` route,
  gesture-scroll controller, tmux copy-mode history ovládání a samostatný
  `/capture` Copy panel. Nezůstane skrytá dlouhodobá fallback cesta.

## Úkoly

### CA-0101 — SnapshotManager v Go
- Priorita: P0
- Závislosti: CA-0001, CA-0002, CA-0003

- [x] Implementovat in-memory `SnapshotManager`.
- [x] Vynutit vazbu snapshotu na usera, viewer a pane generation.
- [x] Přidat idle TTL a explicitní delete.
- [x] Omezit počet snapshotů per viewer, user a proces.
- [x] Při memory pressure odmítnout nový snapshot namísto tichého odstranění aktivního snapshotu.

**Acceptance criteria**
- Snapshot po restartu serveru vrací `410 Gone`.
- Jiný viewer ani user snapshot nepřečte.

### CA-0102 — Atomická capture operace na úrovni aplikace
- Priorita: P0

- [x] Před capture zaznamenat `outputEpochBefore`.
- [x] Jednou spustit `tmux capture-pane` pro celý snapshot.
- [x] Po capture zaznamenat `outputEpochAfter`.
- [x] Uložit capture columns, rows, history size, history limit a alternate-screen flag.
- [x] Nikdy znovu nevolat `capture-pane` při stránkování stejného snapshotu.

**Doporučený baseline**

```text
tmux capture-pane -p -e -J -S - -E - -t <pane-id>
```

**Acceptance criteria**
- Všechny stránky snapshotu jsou odvozené z jediného capture blobu.
- Při výstupu během capture je snapshot označen jako potenciálně následovaný novým výstupem.

### CA-0103 — ANSI parser s allowlistem
- Priorita: P0

- [x] Parsovat SGR 16/256/truecolor.
- [x] Podporovat bold, faint, italic, underline, inverse a strike.
- [x] Koalescovat sousední runy se stejným stylem.
- [x] Zahodit OSC, DCS, APC, PM a clipboard sekvence.
- [x] Nevytvářet aktivní OSC 8 odkazy v MVP.
- [x] Renderovat text pouze přes `textContent` nebo text nodes.
- [x] Omezit maximální počet ANSI runů na snapshot a řádek.

**Acceptance criteria**
- `<script>` ani ANSI payload nevytvoří aktivní HTML nebo JavaScript.
- Parser zvládne neúplnou escape sekvenci bez panic.

### CA-0104 — Snapshot a page API
- Priorita: P0

- [x] `POST /api/v1/panes/{paneRef}/history-snapshots`.
- [x] `GET /api/v1/history-snapshots/{snapshotId}/pages?before={cursor}`.
- [x] `DELETE /api/v1/history-snapshots/{snapshotId}`.
- [x] Přidat `Cache-Control: no-store`.
- [x] Cursor musí být opaque a podepsaný nebo serverově mapovaný.
- [x] Stránkovat podle maximálního počtu řádků i bajtů.

**Acceptance criteria**
- Změnou cursoru nelze číst jiný snapshot.
- Page response nikdy neobsahuje neomezené množství dat.

### CA-0105 — History overlay
- Priorita: P0

- [x] Vytvořit top-level neprůhlednou History vrstvu nad iframe.
- [x] Live iframe neodstraňovat, nezmenšovat na nulu a nepoužívat `display:none`.
- [x] Při History nastavit Live vrstvě `pointer-events: none`, odebrat focus a použít `inert`, kde je podporováno.
- [x] History použít jako jediný scroll container.
- [x] Při návratu History vrstvu odstranit a obnovit focus Live terminálu.

**Acceptance criteria**
- Live terminál během History stále přijímá výstup.
- Návrat do Live nevyžaduje replay snapshotu.

### CA-0106 — Progressive materialization bez recyklace
- Priorita: P0

- [x] První response načte posledních přibližně 2 000–4 000 řádků.
- [x] Starší stránky načítat před dosažením horního okraje.
- [x] Po prependu zachovat scroll anchor.
- [x] Jednou vložené DOM uzly v otevřeném snapshotu nerecyklovat.
- [x] Při aktivní native selection neměnit DOM.
- [x] Po zavření History DOM i snapshot uvolnit.

**Acceptance criteria**
- Prepend neposune zvolený referenční řádek o více než 1 px.
- Selection přes již načtené stránky se při scrollování nezruší.

### CA-0107 — Stavový automat Live / History / Copy
- Priorita: P0

- [x] Implementovat `LIVE`, `HISTORY_LOADING`, `HISTORY`, `COPY`, `PASTE_PENDING`, `LIVE_RECONNECTING`.
- [x] Transport state držet odděleně od UI state.
- [x] `Escape` v History přejde do Live a neposílá Escape aplikaci.
- [x] Print key, Enter nebo Backspace přejde do Live a až potom odešle první input.
- [x] `Ctrl/Cmd+C` při aktivní selection patří browseru.
- [x] V History nikdy neposílat tmux copy-mode ani scroll command.

**Acceptance criteria**
- Automatizovaný test prokáže, že History scroll nevytvoří žádné tmux commandy.

### CA-0108 — Indikace nového výstupu
- Priorita: P1

- [x] Přidat `outputEpoch` nebo ekvivalentní monotonní activity counter.
- [x] Při změně po vytvoření snapshotu zobrazit badge `Nový výstup`.
- [x] Badge nepřidává text do snapshotu.
- [x] Kliknutí na badge bezpečně přejde na Live bottom.
- [x] Nezobrazovat nepřesný počet nových řádků u TUI aplikací.

**Acceptance criteria**
- Snapshot se po novém Live výstupu byteově ani DOM strukturou nezmění.

### CA-0109 — Reflow a Fixed grid
- Priorita: P1

- [x] Reflow snapshot používat pro běžný shellový výstup.
- [x] Fixed grid snapshot použít pro alternate screen nebo explicitní volbu uživatele.
- [x] Přepnutí režimu vytvoří nový snapshot.
- [x] Fixed grid zachová původní šířku a umožní horizontální pan.
- [x] U alternate screen zobrazit vysvětlení omezení historie redrawů.

**Acceptance criteria**
- Změna orientace zařízení nepřecapturuje otevřený snapshot.
- Reflow snapshot se lokálně přelomí bez změny tmux window.

## Výstup etapy

- Plynulý History scroll bez REST requestu na každý wheel/touch krok.
- Nativní selection a Copy.
- Stabilní snapshot s indikací nového Live výstupu.
- Ttyd zůstává beze změny jako Live backend.

## Reference

- `AGENTS.md`.
- `TASKS/README.md`.
- `TASKS/done/0009-terminal-identity-foundation.md`.
- `README.md`, `SECURITY.md`, and `CHANGELOG.md`.
- `internal/auth` for authenticated login scope.
- `internal/server`, `internal/server/static`, and `internal/tmux` for API,
  browser state, topology, capture and Paste behavior.
- `test/e2e` and `test/playwright` for real tmux/ttyd and browser coverage.

## Validace

Run at minimum:

```sh
node --check internal/server/static/app.js
node --check test/playwright/app.spec.js
make test
make test-e2e
make test-browser
CGO_ENABLED=1 go test -race -count=1 ./...
go vet ./...
git diff --check
```

Tests must prove that local History wheel/touch/selection produces no tmux
copy-mode, `send-keys -X`, client viewport scroll or resize command; the Live
ttyd stream remains connected in the background; pages come from one immutable
bounded capture; stale/cross-login/cross-viewer references fail; ANSI/XSS and
resource limits fail safely; and removed legacy routes are unavailable. Do not
deploy or restart the installed service.

## Implementation summary

- Added an in-memory snapshot manager with idle expiry, explicit release,
  opaque server-side cursors, login/viewer/session/pane-generation bindings,
  and per-viewer, per-login, process-count, process-memory, line, byte, and ANSI
  run limits. Missing snapshots after restart return `410 Gone`; cross-login,
  cross-viewer, stale-generation, and cursor-substitution tests reject access.
- Added one bounded `tmux capture-pane -p -e -J -S - -E -` operation per
  snapshot. Paging uses only the stored immutable structured representation.
  Pane dimensions, history metadata, alternate-screen state, and a monotonic
  content-free proxy-output byte epoch are sampled around capture so concurrent
  and same-second redraws surface as new Live output without recapture.
- Added a server-side ANSI allowlist parser for 16/256/truecolor SGR and the
  approved attributes. It strips OSC, DCS, APC, PM, clipboard controls, and
  active links; coalesces runs in linear time; enforces limits while parsing;
  and returns only structured text/style runs rendered through browser text
  nodes. Adversarial segment, newline-heavy, malformed-control, aggregate-run,
  XSS, and byte-envelope regressions pass.
- Replaced remote tmux scrolling and the standalone Copy capture with the
  opaque top-level History overlay. The Live ttyd iframe stays connected and
  full-sized behind an inert, pointer-disabled layer. History progressively
  prepends immutable pages, preserves its anchor within one pixel, never
  recycles inserted nodes, defers both pending and in-flight page
  materialization during native selection, and releases DOM and server state
  on close.
- Added separate UI and transport states, native browser Copy behavior,
  Escape/first-input transitions back to Live, Reflow and Fixed-grid snapshots,
  alternate-screen guidance, and a `New output` badge that never mutates the
  snapshot. Command-level browser tracing proves wheel, touch, selection,
  paging, and orientation changes emit no tmux copy-mode, `send-keys -X`,
  client viewport-scroll, or resize command.
- Removed the legacy `/scroll` and `/capture` routes and the old browser
  gesture-scroll/copy-mode controller. Updated `README.md`, `SECURITY.md`,
  `AGENTS.md`, and `CHANGELOG.md` for the breaking API and operational model.
- Independent review findings covering monotonic activity, linear ANSI
  coalescing, command-level negative assertions, byte and aggregate limits,
  newline-heavy structured allocation, and in-flight selection races were all
  corrected and approved by a fresh final reviewer.
- Main-agent validation passed: Go formatting for all relevant packages, both
  JavaScript syntax checks, `make test`, `make test-e2e`, `make test-browser`
  (7/7), `CGO_ENABLED=1 go test -race -count=1 ./...`, `go vet ./...`, and
  `git diff --check`.
- Validation-created client binaries were removed to restore the pre-existing
  deleted worktree state. No installed service was restarted and no deployment
  was performed.

## Independent review history (resolved)

All findings below were corrected, regression-tested, and approved by a fresh
final reviewer. They are retained as implementation history.

1. `outputEpoch` must be a genuinely monotonic output activity counter. The
   current second-resolution `#{window_activity}` plus `history_bytes` can miss
   a same-second alternate-screen redraw, including output that occurs while a
   snapshot is being captured. Add a concurrent/same-second redraw regression
   that proves CA-0102 and CA-0108.
2. ANSI run coalescing must have linear complexity. Repeated immutable string
   concatenation for adjacent segments with the same effective allowlisted
   style can become quadratic on a valid adversarial capture. Replace it with a
   bounded linear approach and add a generated adversarial regression.
3. Strengthen validation so wheel, touch, native selection, and progressive
   paging prove at command level that they emit no tmux `copy-mode`,
   `send-keys -X`, client viewport-scroll, or `resize-window` command. Add
   explicit byte-pagination and aggregate snapshot ANSI-run-limit tests in
   addition to existing line and per-line coverage.
4. Enforce aggregate structured-memory and line-count budgets while parsing,
   before allocating one structured line per newline. A newline-heavy capture
   near the byte limit must fail safely without excessive allocation or CPU;
   add an adversarial regression.
5. Recheck native selection after an older-page request returns and before
   materializing/prepending its DOM nodes. Add a delayed-response browser
   regression where selection begins while the request is in flight and prove
   that the selected DOM is not mutated.
