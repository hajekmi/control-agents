# TASKS-02 — Desktop a iOS/iPadOS UX

Status: done

Dependencies: `0010-local-history-copy-mvp.md`

## Cíl

Zajistit přirozené scrollování, selection, Copy, Paste, focus a resize chování v Safari včetně dotykových zařízení.

## Schválená migrační rozhodnutí

- History vrstva používá nativní browser scrolling a selection; nepřidá vlastní
  momentum engine ani vzdálené tmux scrollování.
- Explicitní Paste je vzdálená shellová akce a zůstává oddělený od nativního
  Copy. Bez potvrzení se nesmí odeslat multiline ani control-character obsah.
- Fyzický iPhone/iPad smoke test je release gate. Automatizované testy musí být
  dokončeny v repozitáři i v případě, že hardware gate čeká na operátora.
- Starý Copy panel, vzdálený scroll a gesture-scroll controller už odstranil
  task `0010`; tato etapa je nesmí znovu zavést ani skrýt jako fallback.
- Per-viewer scroll-gesture preference is stored in the current browser tab's
  `sessionStorage`; it is not a server-side resize mode and never changes tmux.
- Paste confirmation shows UTF-8 byte and logical line counts. Any multiline
  value or C0/C1/DEL control character requires an explicit confirmation;
  permission denial, cancel, and network failure keep the user in History and
  never retry automatically.
- The visible textarea fallback is an explicit user-operated control in the
  History/Paste flow. Its `paste` event only stages text for the same
  confirmation dialog and never sends terminal input directly.
- No installed service restart or deployment is part of this task.

## Úkoly

### CA-0201 — Desktop vstup do History
- Priorita: P0

- [x] Wheel nahoru v Live otevře History.
- [x] `PageUp` a `Shift+PageUp` otevřou History.
- [x] Menu action otevře History.
- [x] První wheel delta po otevření aplikovat na lokální scroller, pokud je to bezpečné.
- [x] Přidat per-viewer volbu `Scroll gesture: History | Application`.

**Acceptance criteria**
- Trackpad inertia probíhá čistě lokálně.
- Mouse-reporting aplikace lze explicitně přepnout do režimu Application.

### CA-0202 — iOS nativní scroll container
- Priorita: P0

- [x] History vrstva je skutečný `overflow-y: auto` element.
- [x] Nepoužívat globální `preventDefault()` na `touchmove`.
- [x] Nastavit `touch-action: pan-y pinch-zoom`.
- [x] Respektovat safe-area inset.
- [x] Nepřekrývat text transparentním elementem.
- [x] Nepoužívat vlastní momentum engine.

**Acceptance criteria**
- Swipe nahoru/dolů má nativní inerciální scroll.
- Dlouhý stisk otevře systémové selection UI.

### CA-0203 — Nativní Copy
- Priorita: P0

- [x] Povolit `user-select: text` a `-webkit-user-select: text`.
- [x] Povolit `-webkit-touch-callout: default`.
- [x] Nevytvářet vlastní kontextové menu v MVP.
- [x] Při selection pozastavit DOM prepend a jiné mutace.
- [x] Po zrušení selection bezpečně obnovit lazy loading.

**Acceptance criteria**
- Copy funguje přes systémové menu na reálném iPhonu a iPadu.

### CA-0204 — Explicitní Paste button
- Priorita: P0

- [x] Paste lze spustit pouze explicitní user action.
- [x] Použít Clipboard API jen v rámci user activation.
- [x] Při odmítnutí permission zůstat v History.
- [x] Po načtení ukázat byte count a line count.
- [x] Pro multiline nebo control characters vyžadovat potvrzení.
- [x] Teprve po potvrzení přejít do Live a odeslat Paste API.

**Acceptance criteria**
- Paste se nikdy nespustí pouhým návratem do Live.
- Síťová chyba nezpůsobí automatický retry.

### CA-0205 — iOS fallback přes viditelný textarea
- Priorita: P1

- [x] Přidat viditelný, focusovatelný `<textarea>` pro nativní `Vložit` callout.
- [x] Po `paste` eventu přesunout obsah do potvrzovacího dialogu.
- [x] Textarea po dokončení bezpečně vyčistit.
- [x] Nevkládat obsah přímo do terminálu bez potvrzení.

**Acceptance criteria**
- Fallback funguje i při nedostupném `navigator.clipboard.readText()`.

### CA-0206 — Focus a první klávesový vstup
- Priorita: P0

- [x] Při přechodu History → Live obnovit focus terminálu.
- [x] První tisknutelnou klávesu neztratit ani neodeslat dvakrát.
- [x] Při reconnectu vstup nefrontovat.
- [x] V History zachovat klávesy pro scroll a browser Copy.

**Acceptance criteria**
- Jeden Enter po History vykoná právě jeden Enter v Live aplikaci.

### CA-0207 — VisualViewport a softwarová klávesnice
- Priorita: P0

- [x] `visualViewport` použít pouze pro umístění UI.
- [x] Otevření klávesnice nesmí automaticky měnit tmux rows/columns.
- [x] Resize debounce oddělit od keyboard viewport změn.
- [x] Detekovat stabilní změnu orientace/layout viewportu.

**Acceptance criteria**
- Otevření a zavření iOS keyboard neposílá SIGWINCH do TUI aplikace.

### CA-0208 — Přístupnost
- Priorita: P1

- [x] Všechny toolbar actions mají textový accessible name.
- [x] Badge nového výstupu používá vhodné `aria-live`, ale nezahlcuje čtečku.
- [x] Focus order je stabilní.
- [x] Kontrast toolbaru a selection indikátorů splňuje WCAG AA.
- [x] Reduced motion neovlivní funkčnost.

## Výstup etapy

- Safari-first History UX.
- Nativní selection a systémové Copy/Paste interakce.
- Žádná regresní změna velikosti tmux při otevření klávesnice.

## Reference

- `AGENTS.md` and `TASKS/README.md`.
- `TASKS/done/0010-local-history-copy-mvp.md`.
- `README.md`, `SECURITY.md`, and `CHANGELOG.md`.
- `internal/server/static/index.html`, `styles.css`, and `app.js` for the
  top-level History layer, state machine, focus, gestures, viewport handling,
  accessibility, and Paste flow.
- `internal/server/server.go` and `internal/tmux` for the bounded Paste/input
  APIs and negative resize/scroll guarantees.
- `test/playwright/app.spec.js` for desktop, touch, keyboard, selection,
  clipboard, accessibility, and viewport automation.

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

Browser tests must cover wheel, PageUp, Shift+PageUp, Menu, the first local
wheel delta, both per-viewer gesture modes, native touch scrolling without a
custom momentum loop, selection-safe lazy loading, Clipboard API denial,
single-line and confirmed multiline/control-character Paste, visible-textarea
fallback, exactly-once first input, reconnect input rejection, stable focus,
safe-area/visualViewport behavior, no keyboard-driven tmux resize/SIGWINCH,
accessible names/live regions/focus order, and reduced-motion behavior. Use
command-level assertions for forbidden tmux scroll/copy/resize mutations.

Record the physical Safari smoke test as pending operator evidence for
`0018-release-rollout.md`; do not claim that real iPhone/iPad hardware was
tested from this execution environment. Do not deploy or restart the installed
service.

## Implementation summary

- Added tab-local `Scroll gesture: History | Application` behavior. Upward
  wheel, `PageUp`, `Shift+PageUp`, and Menu enter History in History mode; the
  first wheel delta and subsequent inertia are routed only to the local native
  scroller. Application mode leaves terminal mouse/PageUp input untouched and
  never changes tmux state.
- Made History a Safari-first native `overflow-y: auto` selection surface with
  `pan-y pinch-zoom`, safe-area padding, default touch callout, no global touch
  cancellation, no transparent selection blocker, and no custom momentum
  loop. Lazy prepend pauses for active and in-flight cross-boundary selections
  anywhere touching the History overlay, then resumes after selection clears.
- Replaced direct Paste behavior with an explicit staged flow. Clipboard reads
  happen only from the initiating click; UTF-8 byte and logical-line counts are
  shown; multiline and C0/C1/DEL values display an explicit warning; and only
  the confirmation action transitions to Live and sends one bounded Paste
  request. Denial, cancellation, and network failure stay in History and do
  not retry.
- Added a visible focusable textarea for the iOS system Paste callout. Its
  `paste` event stages and clears text through the same review dialog and never
  sends terminal input directly.
- Added exactly-once first printable/Enter/Backspace input after History and
  reject-without-queue behavior while the actual ttyd WebSocket is reconnecting.
  The terminal proxy injects a pre-bootstrap observer that emits only
  same-origin `CONNECTING`/`CONNECTED` state; it never retains or exposes frame
  payloads, and the parent verifies both origin and iframe source.
- Separated visual-viewport movement from stable layout resize. Horizontal and
  vertical viewport offsets position the shell above iOS keyboard panning;
  only stable layout/orientation changes request a terminal repaint. A real
  pane `WINCH` trap and command tracing prove keyboard open/close emits no tmux
  resize or SIGWINCH.
- Added accessible names, a polite atomic new-output live region, stable focus
  order, reduced-motion behavior, safe-area styling, and verified contrast
  ratios from 5.77:1 to 9.78:1 for the affected toolbar, selection, status, and
  focus colors.
- Browser coverage now has nine real-server scenarios, including first wheel,
  PageUp variants, both gesture modes, native touch CSS, cross-boundary
  selection races, explicit/fallback/failed Paste, one-shot input, a real
  same-document WebSocket reconnect, vertical visual-viewport panning, command
  mutation tracing, and the pane SIGWINCH trap.
- Independent review findings for socket lifecycle state, vertical viewport
  placement, and cross-boundary selection ownership were corrected and
  approved by a fresh final reviewer. The physical iPhone/iPad Safari smoke
  test remains an explicit pending operator gate in
  `0018-release-rollout.md`; no real-hardware claim is made here.
- Main-agent validation passed: Go formatting for the changed proxy files, both
  JavaScript syntax checks, `make test`, `make build`, `make test-e2e`, `make
  test-browser` (9/9), `CGO_ENABLED=1 go test -race -count=1 ./...`, `go vet
  ./...`, numeric WCAG contrast checks, and `git diff --check`.
- Validation-created client binaries were removed to restore the pre-existing
  deleted worktree state. No installed service was restarted and no deployment
  was performed.

## Independent review history (resolved)

All findings below were corrected, regression-tested, and approved by a fresh
final reviewer. They are retained as implementation history.

1. Track the real ttyd/xterm WebSocket transport lifecycle, not only iframe
   `load`, `pagehide`, and `beforeunload`. A socket outage/reconnect without
   document navigation must move the frame out of `CONNECTED`, reject the
   first History input instead of queueing/sending it during the outage, and
   return to Live only after the actual terminal transport reconnects. Add a
   real-WebSocket browser regression rather than an iframe reload surrogate.
2. Apply vertical `visualViewport.offsetTop` to top-level shell placement as
   well as the existing horizontal offset. Add automation where iOS-style
   keyboard panning changes `offsetTop`, proves the header/terminal remain in
   the visible viewport, and proves no tmux resize or pane `SIGWINCH` occurs.
3. Treat a native selection as History-owned whenever either endpoint or an
   intersecting range touches History content, including ranges whose common
   ancestor is the History overlay because they cross into the header, notice,
   or Paste panel. Add a cross-boundary delayed-page regression proving no DOM
   prepend or selection mutation occurs.
