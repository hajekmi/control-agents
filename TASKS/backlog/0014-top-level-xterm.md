# TASKS-03 — Top-level xterm frontend, ttyd backend zůstává

Status: backlog

Dependencies: `0013-history-testing-benchmarks.md`

## Cíl

Odstranit iframe jako UX překážku, ale dočasně zachovat ttyd jako PTY/backend vrstvu.

## Schválená migrační rozhodnutí

- Iframe se v této etapě definitivně odstraní. Ttyd protocol adapter je pouze
  dočasný backend krok a po ověření vlastního bridge se odstraní také.
- Vendorované xterm assets musí být připnuté, lokální a vložené přes `go:embed`;
  produkční build ani VM nesmí vyžadovat Node/npm nebo CDN.
- Live transport state zůstane oddělený od immutable History UI state.

## Úkoly

### CA-0301 — Vendorovat frontend assets
- Priorita: P1

- [ ] Připnout konkrétní verzi xterm.js a potřebných addonů.
- [ ] Nepoužívat CDN.
- [ ] Release assets vložit do Go binary přes `go:embed`.
- [ ] Produkční VM nesmí vyžadovat Node/npm ani frontend build.
- [ ] Zapsat postup aktualizace vendorovaných assets.

**Acceptance criteria**
- Čistý deployment Go binárky obsahuje celý frontend.

### CA-0302 — Implementovat ttyd protocol adapter
- Priorita: P1

- [ ] Zdokumentovat používané ttyd message types.
- [ ] Implementovat klienta pro input, output, resize a flow-control zprávy.
- [ ] Připnout podporovanou ttyd verzi.
- [ ] Přidat integrační contract test proti konkrétní ttyd binárce.
- [ ] Při neznámé verzi odmítnout připojení místo tichého pokračování.

**Acceptance criteria**
- Upgrade ttyd, který změní protokol, selže v CI contract testu.

### CA-0303 — Přesunout xterm do application shell
- Priorita: P1

- [ ] Vytvořit top-level xterm container.
- [ ] Napojit jej přes Go reverse proxy na privátní Unix socket ttyd.
- [ ] Sdílet toolbar, History overlay a focus management bez iframe boundary.
- [ ] Minimalizovat live xterm scrollback; dlouhá historie patří History vrstvě.

**Acceptance criteria**
- History → Live focus a první keypress fungují konzistentně na iOS Safari.

### CA-0304 — Live output activity
- Priorita: P1

- [ ] Získat activity signal přímo z přijímaných ttyd output frames.
- [ ] Obsah frame nezapisovat ani neuchovávat pro History.
- [ ] Coalescovat activity eventy.
- [ ] Aktualizovat badge bez DOM změny snapshotu.

**Acceptance criteria**
- Activity badge reaguje bez periodického capture-pane.

### CA-0305 — Reconnect a reset terminálu
- Priorita: P0

- [ ] Při reconnectu vytvořit nový ttyd/tmux attach.
- [ ] Před přijetím nového redraw resetovat xterm state.
- [ ] Nereplayovat starý Live byte stream.
- [ ] History snapshot ponechat čitelný během reconnectu.
- [ ] Po úspěchu nabídnout explicitní návrat do Live.

**Acceptance criteria**
- Po výpadku nevznikne smíšený escape state ze starého a nového spojení.

### CA-0306 — Writer a resize lease
- Priorita: P1

- [ ] Přidat read-only viewer mode.
- [ ] Zavést maximálně jeden browser writer lease na pane/session podle zvoleného modelu.
- [ ] Přidat časově omezený resize lease.
- [ ] SSH klienta nezamykat; zobrazit upozornění na souběžné připojení.
- [ ] Lease obnovovat heartbeat mechanismem.

**Acceptance criteria**
- Dva browsery nemění velikost tmux window současně.

## Exit criteria

- Iframe lze odstranit bez ztráty funkčnosti ttyd.
- Live a History sdílí jeden top-level UX model.
- Ttyd backend je stále snadno rollbackovatelný.
