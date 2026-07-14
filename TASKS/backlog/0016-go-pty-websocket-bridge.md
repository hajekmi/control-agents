# TASKS-05 — Vlastní Go WebSocket/PTTY bridge

Status: backlog

Dependencies: `0014-top-level-xterm.md`, `0015-tmux-control-mode-sidecar.md`

## Cíl

Nahradit ttyd vlastním, omezeným a auditovatelným Live transportem při zachování tmux jako vlastníka persistentní session.

## Schválená migrační rozhodnutí

- WebSocket/PTTY bridge je cílový Live transport. QUIC/WebTransport není v
  rozsahu.
- Ttyd rollback může existovat pouze během implementace a parity ověření; není
  součástí konečného stavu po tasku `0017-ttyd-decommission.md`.
- Slow consumer se řízeně odpojí a po reconnectu dostane nový tmux redraw;
  terminal bytes se nikdy náhodně nezahazují ani nelogují.
- Persistentní procesy vlastní tmux; disconnect browseru ukončí pouze jeho PTY
  client.

## Úkoly

### CA-0501 — PTY lifecycle
- Priorita: P1

- [ ] Pro každý browser viewer vytvořit vlastní PTY.
- [ ] Před spuštěním tmux klienta nastavit počáteční rows/columns.
- [ ] Spouštět přesný `tmux attach-session` bez shell interpolation.
- [ ] Při WebSocket disconnectu ukončit pouze příslušný tmux client.
- [ ] Persistentní pane a procesy musí pokračovat.

**Acceptance criteria**
- Zavření browseru pouze detachne viewer a nezabije session.

### CA-0502 — Verzovaný WebSocket protokol
- Priorita: P1

- [ ] Binární frames použít pro terminálový input/output.
- [ ] JSON nebo malý binární control frame použít pro resize, ping, error a reset.
- [ ] Přidat protocol version negotiation.
- [ ] Kontrolovat maximální frame size.
- [ ] Ověřovat Origin a session auth při handshake.

**Acceptance criteria**
- Neznámá protokolová verze skončí řízeným odmítnutím.

### CA-0503 — Backpressure a slow consumer
- Priorita: P0

- [ ] Každý viewer má omezenou output queue.
- [ ] Nikdy náhodně nezahazovat terminal bytes.
- [ ] Při překročení limitu uzavřít spojení s důvodem `slow-consumer`.
- [ ] Při reconnectu resetovat xterm a vytvořit nový PTY/tmux attach.
- [ ] Měřit pouze queue size a reason code, nikoliv obsah.

**Acceptance criteria**
- Pomalý klient dostane kompletní redraw po reconnectu, ne poškozený VT state.

### CA-0504 — Input, IME a Unicode
- Priorita: P0

- [ ] Otestovat UTF-8 split přes WebSocket frames.
- [ ] Podporovat IME composition bez duplicitního vstupu.
- [ ] Správně zpracovat control keys, function keys a bracketed paste.
- [ ] Nevytvářet serverovou frontu inputu během reconnectu.

**Acceptance criteria**
- CJK/emoji/combining input funguje na desktopu i iOS bez duplikace.

### CA-0505 — Resize lease
- Priorita: P1

- [ ] Pouze aktivní lease holder může měnit tmux window.
- [ ] Debounce resize eventů.
- [ ] Ignorovat změny způsobené pouze softwarovou klávesnicí.
- [ ] `Fit once` je oddělená explicitní operace.
- [ ] Resize audit obsahuje pouze staré/nové rozměry a opaque ID.

**Acceptance criteria**
- Souběžné browsery nevytvářejí resize oscillation.

### CA-0506 — Paste přes bridge/API
- Priorita: P0

- [ ] Zachovat explicitní Paste API, nikoliv transparentní browser clipboard bridge.
- [ ] Limit 64 KiB po UTF-8 encodingu.
- [ ] Odmítnout NUL a nevalidní UTF-8.
- [ ] Použít náhodný tmux buffer, stdin a `paste-buffer -p -d`.
- [ ] Žádný automatický retry.
- [ ] Ověřit pane generation těsně před provedením.

**Acceptance criteria**
- Multiline paste nemůže být proveden bez explicitního potvrzení.

### CA-0507 — Feature flag a rollback
- Priorita: P0

- [ ] Bridge zapínat per user/session feature flagem.
- [ ] Zachovat ttyd cestu jako rollback.
- [ ] Přidat provozní metriky bez obsahu: connect rate, disconnect reason, queue pressure, latency.
- [ ] Automaticky přepnout pouze po explicitní politice, ne podle neznámé chyby.

**Acceptance criteria**
- Operátor může vrátit konkrétního usera na ttyd bez restartu celé služby.

## Exit criteria

- Funkční parita ttyd na podporovaných browser/device kombinacích.
- Stabilní dlouhodobý provoz bez VT corruption.
- Bezpečnostní audit a benchmarky splněny.
