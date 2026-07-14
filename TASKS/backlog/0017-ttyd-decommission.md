# TASKS-08 — Rozhodnutí a odstranění ttyd

Status: backlog

Dependencies: `0016-go-pty-websocket-bridge.md`

## Cíl

Odstranit ttyd pouze po prokázané funkční a provozní paritě vlastního Go bridge.

## Schválená migrační rozhodnutí

- Konečný stav tohoto tasku neobsahuje ttyd procesy, ttyd Unix sockety, ttyd
  reverse proxy, ttyd protocol adapter, iframe ani ttyd instalační závislost.
- Rollback window je dočasná validační fáze uvnitř tasku; po splnění exit gates
  se starý kód a feature flag odstraní podle decommission kroků.
- Pokud parity nebo security gate selže, task zůstane `in-progress` a ttyd se
  neodstraní napůl.

## Rozhodnutí

- V první fázi ttyd **ponechat** pouze pro Live terminál.
- Po History MVP odstranit nejprve **iframe**, nikoliv ttyd backend.
- Ttyd odstranit až po splnění všech exit criteria níže.

## Exit criteria před odstraněním ttyd

### Funkční parita
- [ ] Shell input/output.
- [ ] Function keys a control keys.
- [ ] Unicode, CJK, emoji a IME.
- [ ] Alternate screen.
- [ ] `mc`, `sngrep`, `vim`, `less`, `top`.
- [ ] Mouse reporting.
- [ ] Bracketed Paste.
- [ ] Resize a SIGWINCH.
- [ ] Reconnect a full redraw.
- [ ] Multi-viewer behavior.

### Safari/iOS parita
- [ ] Focus a softwarová klávesnice.
- [ ] První keypress po History.
- [ ] Background/foreground.
- [ ] Orientation change.
- [ ] iPad Split View a Stage Manager.
- [ ] Nativní Copy/Paste workflow.

### Provozní parita
- [ ] Bounded backpressure.
- [ ] Slow-consumer reconnect.
- [ ] Per-user feature flag.
- [ ] Rollback na ttyd.
- [ ] Metriky bez obsahu.
- [ ] Stabilní soak test.
- [ ] Incident runbook.

### Security gate
- [ ] WebSocket Origin a auth audit.
- [ ] Paste security audit.
- [ ] Žádné veřejné neautorizované PTY endpointy.
- [ ] Žádné logování terminal frames.
- [ ] Závislostní a fuzz test bridge parseru/protokolu.

## Decommission tasks

### CA-0801 — Zastavit nové ttyd procesy
- Priorita: P1

- [ ] Vypnout ttyd pro pilotní cohort.
- [ ] Sledovat error rate, reconnect a user rollback.
- [ ] Rozšířit rollout po etapách.

### CA-0802 — Odstranit ttyd proxy cestu
- Priorita: P2

- [ ] Odstranit Unix socket proxy pouze po plném rollout.
- [ ] Odstranit ttyd protocol adapter.
- [ ] Odstranit ttyd binary z image/package.
- [ ] Aktualizovat systemd sandbox a dependency list.

### CA-0803 — Odstranit dead code a dokumentaci
- Priorita: P2

- [ ] Odstranit feature flag až po rollback window.
- [ ] Odstranit staré integrační testy ttyd protokolu.
- [ ] Aktualizovat threat model a provozní runbook.
- [ ] Potvrdit, že History cesta zůstává nezávislá na Live bridge.

## Stop conditions

Odstranění ttyd se pozastaví při:

- VT corruption nebo ztrátě inputu;
- iOS focus/keyboard regresi;
- neřešeném slow-consumer chování;
- růstu reconnect nebo crash rate;
- nemožnosti rychlého rollbacku;
- bezpečnostním nálezu v Go bridge.
