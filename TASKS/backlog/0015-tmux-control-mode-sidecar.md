# TASKS-04 — Tmux control mode jako metadata sidecar

Status: backlog

Dependencies: `0014-top-level-xterm.md`

## Cíl

Použít tmux control mode pro lifecycle a metadata, nikoliv jako primární Live renderer.

## Schválená migrační rozhodnutí

- Control mode zůstává metadata/lifecycle sidecar. Nesmí být transcript store,
  primární Live renderer ani zdroj History ANSI obsahu.
- Sidecar nesmí měnit tmux client size ani přidat neomezenou output frontu.
- Restart tmux serveru invaliduje všechny reference odvozené od staré server
  epoch.

## Úkoly

### CA-0401 — Persistentní control-mode klient
- Priorita: P2

- [ ] Spustit jeden autorizovaný control-mode klient pro relevantní tmux server.
- [ ] Automaticky jej obnovit po disconnectu.
- [ ] Parser musí zvládnout částečné řádky a neznámé eventy.
- [ ] Neaktivovat změnu client size přes control-mode klienta.

**Acceptance criteria**
- Sidecar připojení nemění velikost žádného tmux window.

### CA-0402 — Lifecycle eventy
- Priorita: P2

- [ ] Zpracovat create/close pane, create/close window a session lifecycle.
- [ ] Aktualizovat interní mapování opaque references.
- [ ] Invalidovat snapshot a writer/resize lease při zániku pane generation.
- [ ] Poslat browseru omezený metadata event.

**Acceptance criteria**
- Paste do zaniklého pane skončí deterministickou chybou a nikdy necílí jinam.

### CA-0403 — Format subscriptions
- Priorita: P2

- [ ] Subscribe na activity metadata, alternate screen, history size a attachment count.
- [ ] Coalescovat vysokofrekvenční změny.
- [ ] Neodesílat raw pane output do logů nebo event storage.
- [ ] U TUI změn zobrazovat obecné `Terminál se změnil`, nikoliv falešný line count.

**Acceptance criteria**
- History badge funguje bez polling burstů a bez ukládání output streamu.

### CA-0404 — Resynchronizace sidecaru
- Priorita: P1

- [ ] Po reconnectu vytvořit kompletní snapshot tmux topologie.
- [ ] Porovnat generation a invalidovat stale references.
- [ ] Eventy přijaté během resync zpracovat deterministicky.
- [ ] Přidat epoch pro celý tmux server.

**Acceptance criteria**
- Restart tmux serveru nezpůsobí použití stale pane mapping.

## Non-goals

- Nepoužívat control mode jako zdroj ANSI historie.
- Nepoužívat `%output` jako dlouhodobý transcript.
- Nepoužívat control mode pro rendering Live terminálu v této etapě.
