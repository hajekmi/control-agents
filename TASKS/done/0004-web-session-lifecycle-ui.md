# Web session create and terminate UI

Status: done

Dependencies: `0003-web-session-lifecycle-api.md`

## Goal

Add compact web controls for creating a managed session and terminating the
currently selected managed session. A created session becomes an active
top-level tab immediately. Termination requires an explicit confirmation and
removes the session for every SSH and web client.

## User experience

The existing main Menu gains two actions:

- `New session`
- `Terminate session`

`New session` opens a small accessible dialog containing a session-name field,
validation/error text, Cancel, and Create. Successful creation closes the
dialog, refreshes the tab row, and activates the created or already-existing
managed session.

`Terminate session` is enabled only when a tab is active. It opens a destructive
confirmation dialog that names the session and explains that all SSH and web
clients attached to it will be disconnected. Only the final `Terminate` button
calls the API.

Closing the browser, switching tabs, losing a network connection, or using
`Ctrl-b d` over SSH remains nondestructive detach behavior and never invokes
termination.

## Scope

1. Add both actions to the existing compact Menu.
2. Add accessible create and terminate dialogs to the embedded plain HTML/CSS
   shell.
3. Call the task 0003 create and terminate APIs with clear loading, validation,
   conflict, and failure states.
4. Keep tabs and iframes consistent when sessions are created, selected,
   terminated, or concurrently changed by another browser/SSH client.
5. Add browser E2E coverage using real tmux and `ttyd` sessions.

## Required behavior

### Create dialog

- Open from an explicit `New session` Menu click.
- Close the Menu popover when the dialog opens.
- Present one text field labeled `Session name` and concise help for the allowed
  canonical form: letters, digits, dot, underscore, and hyphen; maximum 64
  characters; first character alphanumeric.
- Do not offer cwd, command, shell, environment, tmux argument, or SSH-agent
  fields.
- Autofocus the name field where browser behavior permits.
- Enter submits; Escape and Cancel close without side effects.
- Perform matching client-side validation for immediate feedback, while still
  treating server validation as authoritative.
- Disable repeated submission while the request is in flight.
- On `201`, refresh/render sessions and activate the new session.
- On `200` for an existing managed session, activate its existing tab and show
  no duplicate iframe or tab.
- Keep the dialog open with a useful message for invalid names, unmanaged-name
  conflicts, session limits, dependency failures, and network errors.
- Do not place the requested name into HTML with `innerHTML`; use safe DOM text
  properties.

### Terminate confirmation

- `Terminate session` is disabled when no active managed session exists.
- Capture the active session ID when opening the confirmation. Do not silently
  retarget the dialog if polling or a tab click changes the active tab.
- Show the exact session name and a warning that the tmux session will be
  destroyed and all attached SSH/web clients will be disconnected.
- Cancel, Escape, clicking a non-confirming control, or closing the dialog does
  not call DELETE and leaves the session intact.
- The final destructive button is visibly distinct and labeled `Terminate`.
- Disable repeated actions while DELETE is in flight.
- Send `confirmName` matching the captured canonical session ID.
- On `204`, close and remove that session's iframe, refresh the tab list, and
  select a deterministic remaining session. Prefer the next tab at the same
  position, then the previous tab; show the existing empty state when none
  remain.
- If another client already terminated the session and DELETE returns `404`,
  reconcile the local UI as terminated and show at most a concise nonblocking
  notice.
- For confirmation mismatch or lifecycle failure, keep a clear error visible
  and refresh the session list without claiming termination succeeded.
- A terminated iframe or stale async callback must not become active again.

### Tab and polling consistency

- Continue to represent one managed tmux session per top-level tab.
- Keep session polling so sessions created by SSH or another browser appear
  automatically.
- Preserve the active session when unrelated sessions are added or removed.
- If the active session disappears externally, clean up its iframe and select a
  deterministic fallback.
- Do not duplicate iframe instances during immediate API refresh followed by a
  polling refresh.
- Preserve compact multi-window count badges for sessions with more than one
  internal tmux window.

### Existing terminal behavior

Do not regress:

- authenticated ttyd iframes and same-origin WebSocket checks,
- terminal focus and repaint behavior,
- right-side tmux history scrollbar,
- iframe wheel and single-touch history routing,
- Copy mode and bounded Paste,
- special key controls,
- T-Control actions,
- resize-source modes and viewer heartbeats,
- `visualViewport` handling for the iOS software keyboard,
- compact mobile/Safari header layout.

## UI constraints

- Continue using embedded plain HTML, CSS, and JavaScript.
- Do not add npm runtime dependencies, a frontend framework, or a frontend
  build step.
- Reuse the existing visual language and panel/dialog patterns where practical.
- Keep the top row compact. Both lifecycle actions belong inside Menu rather
  than as permanent header buttons.
- Dialogs must have labels, keyboard navigation, initial focus, focus return,
  and an appropriate dialog/alertdialog role.
- Error/status text should use an accessible live region without exposing
  sensitive server details.

## Security requirements

- Calls use same-origin credentials and JSON only.
- Never build terminal, tmux, or shell commands in browser JavaScript.
- Never assume client validation provides security.
- Do not render server error bodies as HTML.
- Do not store termination confirmation or session lifecycle requests in
  `localStorage`.
- Preserve current Content Security Policy compatibility; do not introduce
  inline scripts or third-party assets.

## Material decisions

- The destructive menu action is `Terminate session`. It is not a web detach
  action.
- A normal browser disconnect is already detach behavior and requires no
  confirmation.
- Termination applies to the captured currently selected managed session and
  affects all clients, not only the current browser tab.
- Session creation always starts in the server user's `$HOME`; the web dialog
  intentionally has no directory picker.

## Out of scope

- Importing, renaming, restarting, archiving, or cloning sessions.
- Termination from the SSH selector.
- Exposing internal tmux windows as top-level tabs.
- Starting a session with a browser-provided command or cwd.
- SSH agent management.

## References

- `TASKS/backlog/0003-web-session-lifecycle-api.md`.
- `AGENTS.md`, especially UI, Runtime Boundaries, Tests, and Security Rules.
- `README.md`, especially API and browser behavior.
- `SECURITY.md`.
- `internal/server/static/index.html`.
- `internal/server/static/styles.css`.
- `internal/server/static/app.js`.
- `internal/server/server.go`.
- `test/playwright/app.spec.js` and `playwright.config.js`.

## Acceptance criteria

- Menu contains `New session` and an active-session-only `Terminate session`
  action without making the header wider.
- A valid create request produces one tab, activates it immediately, and opens
  a usable ttyd terminal whose tmux pane starts in `$HOME`.
- Creating an existing managed name selects its existing tab without
  duplication.
- Invalid, conflicting, limited, and failed creates show actionable dialog
  errors and do not create stale tabs or iframes.
- Termination cannot occur without opening and confirming the named-session
  warning.
- Cancel and Escape leave the tmux session, registry, bridge, tab, and attached
  clients intact.
- Confirmed termination kills the selected tmux session, disconnects clients,
  removes its tab/iframe, and chooses the specified fallback tab.
- A session created or terminated by another client is reconciled by polling
  without duplicate tabs or stale active iframes.
- Existing browser interaction suites remain green on desktop and configured
  mobile viewport cases.
- `node --check internal/server/static/app.js` passes.
- `make test` passes.
- `make test-browser` covers successful create, duplicate selection, validation
  failure, termination cancel, confirmed termination, and tab fallback.

## Validation

Run:

```sh
node --check internal/server/static/app.js
make test
make test-browser
```

Record exact commands and results in the implementation summary.

## Implementation summary

- Added compact `New session` and `Terminate session` actions inside the
  existing Menu without adding permanent header controls. The terminate action
  tracks whether a managed tab is active and remains disabled in the empty
  state.
- Added native accessible create and destructive confirmation dialogs with
  labelled content, live status regions, initial focus, focus return, Enter
  submission, Escape/Cancel handling, and disabled in-flight controls. The
  close path verifies that the original opener is still visible and focusable;
  because Menu actions are hidden when their dialog opens, focus returns to the
  visible Menu button instead of remaining on a hidden action. The
  create dialog applies the canonical-name rule client-side and maps invalid,
  unmanaged-conflict, session-limit, dependency, and network failures to
  concise messages while retaining server authority. Session names are written
  only through DOM text properties.
- Added same-origin JSON create and terminate requests. Successful `201` and
  idempotent `200` creates immediately select exactly one tab and iframe. The
  terminate dialog captures the selected session ID when opened, sends the
  matching `confirmName`, treats `204` as confirmed termination, reconciles
  `404` as already terminated with a bounded notice, and keeps failures visible
  without claiming success.
- Extended session rendering with a normalized one-session/one-frame model,
  active-session preservation, deterministic next-at-the-same-position then
  previous fallback, and lifecycle refresh invalidation so stale polling
  responses cannot recreate a locally terminated iframe or override an
  immediate create selection. A post-create list refresh preserves any newer
  manual tab selection instead of forcing the created session active again.
  Existing multi-window badges and terminal, scrolling, Copy/Paste, key,
  T-Control, resize, heartbeat, and viewport paths remain shared and unchanged.
- Extended the real tmux/ttyd Playwright flow to cover client validation without
  a request, unmanaged conflicts, the web session limit, creation in `$HOME`,
  idempotent duplicate selection without duplicate tabs or iframes,
  cancellation and Escape without DELETE, confirmed termination, both fallback
  directions, and polling reconciliation of creation and termination performed
  from a second browser page. Regression assertions verify the actual focused
  element after create Escape and terminate Cancel/Escape/success closure, while
  successful create remains free to preserve ttyd/xterm terminal focus. The
  test also holds a post-create list response until after a manual tab switch to
  verify selection ordering.
- Validation completed on 2026-07-13:
  - `node --check internal/server/static/app.js` — passed.
  - `node --check test/playwright/app.spec.js` — passed.
  - `make test` — passed for all Go packages.
  - `make test-browser` — passed twice consecutively, all 5 Chromium tests in
    15.8s each; the web lifecycle test, including the focus and delayed-refresh
    regressions, passed in 9.4s and 9.5s.
  - `git diff --check` — passed.
