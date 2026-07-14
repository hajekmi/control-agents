# Keep the iOS Safari header controls inside the viewport

Status: done

Dependencies: `0004-web-session-lifecycle-ui.md`

## Goal

Fix the deployed web shell layout on narrow iOS Safari viewports so the compact
header remains usable after selecting or creating a terminal session. Session
tabs may scroll horizontally inside their own area, but the Menu control must
remain pinned inside the visible right side of the app header and the document
itself must not pan horizontally.

## Reported behavior

After creating a session from the web UI on iOS Safari, the header layout moves
out of place. The Menu control appears on the wrong side/outside the immediately
visible header and the user must horizontally scroll the page to reach the
controls. The terminal itself is otherwise usable.

The first deployed containment fix kept the header inside an unshifted layout
viewport, but physical iOS Safari verification still placed Menu too far to the
right. The remaining case is a visual viewport that is narrower than, or
horizontally offset within, the layout viewport after terminal focus/zoom.

## Requirements

- Reproduce the narrow mobile layout with an iPhone-sized viewport, an active
  terminal iframe, and enough session tabs to overflow the available tab area.
- Keep `.topbar` constrained to the layout and visual viewport width, including
  safe-area insets.
- Track both `visualViewport.width` and `visualViewport.offsetLeft` on initial
  load, resize, and visual-viewport scroll so the header follows the actually
  visible horizontal region instead of only the wider layout viewport.
- Keep `.topbar-actions` and the Menu button non-scrolling and visible at the
  right edge of the header.
- Make only `.tabs` horizontally scrollable when tab labels overflow.
- Prevent the wide terminal iframe and terminal-strip horizontal scrolling from
  expanding or horizontally panning the parent document when Safari focuses the
  iframe.
- Preserve the existing terminal horizontal pan behavior inside
  `.terminal-strip`, the compact single-row header, lifecycle dialogs, keyboard
  viewport handling, and desktop layout.
- Do not introduce a frontend build step.

## Security and runtime boundaries

- Do not change authentication, session lifecycle, terminal proxy, tmux resize,
  scrolling APIs, or terminal input behavior.
- Do not expose terminal content in tests or diagnostics.
- Do not restart or modify the deployed service until the fix has passed review
  and validation.

## References

- `AGENTS.md`.
- `TASKS/README.md`.
- `TASKS/done/0004-web-session-lifecycle-ui.md`.
- `internal/server/static/index.html`.
- `internal/server/static/styles.css`.
- `internal/server/static/app.js`.
- `test/playwright/app.spec.js`.
- `playwright.config.js`.

## Acceptance criteria

- At iPhone-sized portrait and landscape viewports, the parent document reports
  no horizontal overflow or nonzero horizontal scroll after the terminal iframe
  loads and receives focus.
- With a narrower visual viewport and a nonzero horizontal visual-viewport
  offset, the header and Menu bounds follow that visible region after its
  resize/scroll event without changing tmux resize mode.
- The Menu button bounding box stays fully inside the visible viewport and at
  the right side of the header.
- Overflowing session tabs scroll within the tabs element without moving the
  Menu button or the parent document.
- The active terminal iframe remains usable and its own horizontal strip can
  still pan when the terminal minimum width exceeds the available width.
- Existing desktop and mobile lifecycle/browser behavior remains green.
- `node --check internal/server/static/app.js`, `make test`, and
  `make test-browser` pass.

## Validation

Run:

```sh
node --check internal/server/static/app.js
make test
make test-browser
```

Record the exact mobile viewport regression coverage and all validation results
in the implementation summary.

## Implementation summary

- Constrained the document, body, compact header, and workspace to the available
  inline viewport width. The root document now clips horizontal overflow and
  disables horizontal overscroll, while the body uses dynamic viewport width
  bounds and the existing border-box sizing keeps safe-area padding inside the
  header width.
- Made the header and workspace flex items explicitly shrinkable. Session-tab
  overflow remains isolated to `.tabs`, whose touch scrolling is preserved,
  while `.topbar-actions` retains its non-scrolling fixed flex size so the Menu
  control stays at the visible right side of the header.
- Added inline-size containment and width bounds around the terminal pane and
  terminal strip. The terminal iframe keeps its existing minimum width and the
  strip keeps horizontal touch panning, but neither can expand or pan the
  parent document.
- Added a Playwright layout regression using a real loaded ttyd/xterm iframe
  plus ten overflow tab labels. It runs at 390x844 iPhone portrait and 844x390
  iPhone landscape viewports, focuses the xterm helper input, and verifies zero
  window/document/body horizontal scroll or overflow, a fully visible
  right-aligned Menu button, independent tab scrolling without Menu movement,
  and preserved terminal-strip horizontal panning. No terminal content is read
  or exposed.
- Validation completed on 2026-07-13:
  - `node --check internal/server/static/app.js` — passed.
  - `node --check test/playwright/app.spec.js` — passed.
  - Targeted Chromium Playwright mobile-header regression — passed, 1 of 1
    test in 2.5s.
  - `make test` — passed for all Go packages.
  - `make test-browser` — passed all 6 Chromium tests in 16.4s, including the
    new portrait/landscape regression and existing desktop, lifecycle, resize,
    terminal interaction, and scrolling coverage.
  - `git diff --check` — passed.
- Deployment follow-up on physical iOS Safari rejected the first fix because
  Menu remained too far right when Safari's visible visual viewport differed
  from the layout viewport. Task 0007 was reopened to cover horizontal visual
  viewport width and offset explicitly.
- The reopened implementation now records `visualViewport.width` and
  `visualViewport.offsetLeft` as CSS viewport metrics on initial load, window
  resize, visual-viewport resize, and visual-viewport scroll. The topbar uses
  those metrics for its width and horizontal position, with a layout-width
  clamp and border-box safe-area padding, while the workspace and terminal
  overflow containment remain unchanged.
- Expanded the real ttyd/xterm mobile regression with a top-level mock visual
  viewport. At a 390x844 layout viewport it starts with a 320px visual viewport
  at offset 42px before app load, then verifies a width/offset resize event at
  offset 34px and a visual-viewport scroll event to offset 56px. Each state
  checks header and Menu bounds against the visible region, zero parent
  horizontal overflow/scroll, and unchanged tmux resize mode. The same test
  retains real terminal focus, overflowing tabs, terminal-strip panning, and
  390x844 portrait plus 844x390 landscape coverage.
- Reopened-task validation completed on 2026-07-13:
  - `node --check internal/server/static/app.js` — passed.
  - `node --check test/playwright/app.spec.js` — passed.
  - Targeted Chromium mobile-header regression — passed, 1 of 1 test in 2.3s.
  - `make test` — passed for all Go packages.
  - `make test-browser` — passed all 6 Chromium tests in 16.9s.
  - `git diff --check` — passed.
- Main-agent final validation on 2026-07-13:
  - `node --check internal/server/static/app.js`,
    `node --check test/playwright/app.spec.js`, `git diff --check`, and
    `make test` — passed.
  - The first `make test-browser` run encountered an environment-level Chromium
    `net::ERR_NETWORK_CHANGED` during the first test's initial navigation; the
    remaining five tests, including the visual-viewport regression, passed.
  - A clean full `make test-browser` rerun passed all 6 tests in 17.3s.
