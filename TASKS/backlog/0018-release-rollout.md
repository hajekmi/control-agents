# TASKS-09 — Release, rollout a provozní připravenost

Status: backlog

Dependencies: `0017-ttyd-decommission.md`

## Cíl

Nasazovat rizikové změny po malých etapách s měřitelným rollbackem, bez observability úniku terminálového obsahu.

## Schválená migrační rozhodnutí

- Release obsahuje pouze cílovou lokální History/Copy cestu a vlastní Go
  WebSocket/PTTY Live bridge. Legacy scroll/capture UI, iframe a ttyd nejsou
  dlouhodobá rollback cesta.
- Cohort rollout může použít předchozí release artifact jako provozní rollback,
  nikoliv zachovaný dead code v cílovém release.
- Skutečný production deploy se provede až po CI, security, SSH isolation a
  fyzických iOS/iPadOS gates a po výslovném pokynu operátora.

## Úkoly

### CA-0901 — Feature flags
- Priorita: P0

- [ ] `history_v1`.
- [ ] `history_reflow`.
- [ ] `top_level_xterm`.
- [ ] `control_mode_sidecar`.
- [ ] `go_pty_bridge`.
- [ ] `resize_lease`.
- [ ] `browser_writer_lease`.
- [ ] Flags musí být per user nebo per session a auditované bez obsahu.

### CA-0902 — Rollout cohorts
- Priorita: P1

- [ ] Interní vývojáři.
- [ ] Testovací účty a non-critical sessions.
- [ ] iOS-heavy pilot.
- [ ] 10 % uživatelů.
- [ ] 50 % uživatelů.
- [ ] 100 % uživatelů.
- [ ] Mezi cohortami vyhodnotit definované health gates.

### CA-0903 — Health gates
- Priorita: P0

- [ ] Snapshot error rate.
- [ ] Snapshot latency percentily.
- [ ] Memory usage a rejection count.
- [ ] Reconnect rate.
- [ ] Slow-consumer disconnects.
- [ ] Paste failures podle reason code.
- [ ] Frontend error rate bez terminal payloadu.
- [ ] User rollback count.

### CA-0904 — Runbook
- Priorita: P0

- [ ] History API degradace.
- [ ] Memory pressure.
- [ ] Tmux server restart.
- [ ] Ttyd socket failure.
- [ ] Go bridge reconnect storm.
- [ ] Pane generation mismatch.
- [ ] Emergency disable Paste.
- [ ] Emergency revert na ttyd.

### CA-0905 — Dokumentace pro uživatele
- Priorita: P1

- [ ] Vysvětlit rozdíl Live a History.
- [ ] Vysvětlit `Nový výstup` a návrat na Live bottom.
- [ ] Vysvětlit Reflow a Original width.
- [ ] Vysvětlit, že resize sdílené tmux session ovlivní další klienty.
- [ ] Vysvětlit explicitní bezpečný Paste.
- [ ] Vysvětlit omezení alternate-screen historie.

### CA-0906 — Release checklist
- Priorita: P0

- [ ] CI green.
- [ ] Security canary test green.
- [ ] Reálný iPhone/iPad smoke test.
- [ ] SSH isolation test green.
- [ ] Backup/rollback artifact dostupný.
- [ ] Ttyd bez veřejného TCP listeneru.
- [ ] Žádné private SSH keys na VM.
- [ ] Operátor zná aktuální feature flag stav.

The real iPhone/iPad Safari smoke test is an explicit pending operator gate
carried from `0011-history-ios-desktop-ux.md`. Automated browser emulation does
not satisfy it. Record device model, iOS/iPadOS and Safari versions, native
momentum scrolling, long-press selection/Copy, Clipboard and visible-textarea
Paste flows, software-keyboard viewport/focus behavior, orientation change,
and absence of terminal resize/SIGWINCH before any production rollout.

Task `0012-history-security-performance.md` also leaves production-only
content-free evidence as a pending operator gate: before rollout, verify the
terminal/Paste canary is absent from the user-service journal, host traces and
metric labels, core/diagnostic exports, and backups; confirm `LimitCORE=0`, the
chosen encrypted-or-disabled swap policy, private `0600` ttyd sockets with no
TCP listener, and absence of private SSH keys in the service account and backup
set. Do not generate a production core dump to perform this check.

## Pending Safari and physical-device evidence from task 0013

Playwright Chromium, Firefox, and WebKit jobs are engine automation only. They
do not complete any row below and must never be recorded as Safari or physical
device evidence. Every row remains `pending` until an operator attaches the
named artifact and records the complete evidence object.

Evidence object schema:

```json
{
  "gateId": "history-ios-current-portrait-software-keyboard",
  "deviceModel": "operator supplied",
  "osVersion": "operator supplied",
  "safariVersion": "operator supplied",
  "orientation": "portrait|landscape",
  "windowMode": "full-screen|split-view|stage-manager",
  "keyboardType": "software|external|none",
  "expectedResult": "gate-specific expectation",
  "actualResult": "operator observation",
  "artifactReference": "immutable screenshot, video, or test-record URI",
  "operator": "operator identity",
  "observedAt": "RFC3339 timestamp",
  "result": "pass|fail"
}
```

Pending evidence rows:

| Gate | Device/browser | Window and input modes | Required native evidence | Status |
| --- | --- | --- | --- | --- |
| `history-desktop-safari` | Real macOS Safari | Desktop window, external keyboard | Wheel/trackpad inertia, selection and system Copy, Paste review, Escape ownership, Live focus | pending |
| `history-ios-current` | Current supported iPhone and iOS Safari | Portrait and landscape; software and external keyboard | Native swipe inertia, long-press handles/system Copy, Clipboard Paste, visible-textarea Paste, return-to-Live focus | pending |
| `history-ios-oldest` | Oldest supported iPhone/iOS combination | Portrait and landscape; software and external keyboard | Same native selection, Copy, Paste, swipe, focus, and reload/background gates | pending |
| `history-ipad-fullscreen` | Supported iPad and iPadOS Safari | Full-screen portrait and landscape; software and external keyboard | Native swipe/selection/Copy/Paste, focus restoration, orientation without unintended tmux resize | pending |
| `history-ipad-split-view` | Supported iPad and iPadOS Safari | Split View widths in both orientations | Header and History layout, native selection, Paste fallback, focus, no unintended tmux resize | pending |
| `history-ipad-stage-manager` | Supported iPad and iPadOS Safari | Stage Manager-sized window in both orientations | Window resize/layout, native selection and swipe, Paste, focus, no unintended tmux resize | pending |

Each physical row must also record that History scrolling caused no
`copy-mode`, `send-keys -X`, viewport-scroll command, terminal input, or tmux
resize; orientation and software-keyboard changes caused no unrequested
SIGWINCH; transport loss reconnected without mutating the open immutable
snapshot; and no wall-clock performance gate regressed relative to the named
reference device baseline. Long-press handles and the native system Copy menu
must be visible in the artifact rather than inferred from DOM selection state.
