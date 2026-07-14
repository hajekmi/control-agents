# TASKS-07 — Testovací a benchmark plán

Status: in-progress

Dependencies: `0010-local-history-copy-mvp.md`, `0011-history-ios-desktop-ux.md`, `0012-history-security-performance.md`

## Cíl

Prokázat korektnost, Safari UX, izolaci SSH klienta a rozumné limity na slabších iPhone/iPad zařízeních.

## Schválená migrační rozhodnutí

- Automatizovatelné unit, Go integration, Playwright a benchmark scénáře musí
  být součástí repozitáře a CI; fyzický Safari hardware matrix se zaznamená
  jako explicitní release gate, ne jako předstíraný lokální výsledek.
- Testy nesmějí zapisovat terminal canary do diagnostických logů. Test může
  canary hledat jen v izolovaném ověřovaném výstupu určeném pro leak test.
- Výkonnostní gate se vztahuje na lokální History scroll, nikoliv na legacy
  tmux copy-mode scroll, který bude odstraněn.
- Playwright `webkit` is automated engine coverage, not a claim that desktop
  or iOS Safari was tested. Chromium, Firefox, and WebKit engine jobs belong in
  CI; the real Safari/device rows and native system-menu interactions remain
  evidence-bearing operator gates in `0018-release-rollout.md`.
- Shared CI enforces deterministic correctness/resource gates and emits
  content-free benchmark measurements. Wall-clock FPS, long-task, JS-heap, and
  device-latency thresholds are recorded as baselines and are release-blocking
  only on the named reference hardware, because hosted-runner timing is not a
  stable performance oracle.
- This task establishes reusable fixtures, benchmarks, and reporting. Later
  transport tasks must update the same harness. Under ttyd, slow-consumer
  coverage proves bounded disconnect/reconnect without snapshot mutation;
  native bridge queue/backpressure gates remain additive acceptance criteria
  for `0016-go-pty-websocket-bridge.md`.
- Optional full-screen application smoke tests must report an explicit
  unsupported dependency when an application is absent. Core alternate-screen,
  redraw, wrap, rollover, pane-generation, and SSH-isolation semantics use
  deterministic fixtures and may never be skipped.
- No installed service restart or deployment is part of this task.

## Úkoly

### CA-0701 — Unit testy ANSI parseru
- Priorita: P0

- [x] 16/256/truecolor.
- [x] Reset a částečný reset.
- [x] Bold, faint, italic, underline, inverse, strike.
- [x] Split a neúplné escape sekvence.
- [x] OSC/DCS stripping.
- [x] HTML/XSS payloady.
- [x] Unicode, CJK, emoji a combining marks.
- [x] ANSI run explosion.

### CA-0702 — Unit testy SnapshotManageru
- Priorita: P0

- [x] Immutable pages.
- [x] Opaque cursors.
- [x] TTL a `410 Gone`.
- [x] Pane generation mismatch.
- [x] 2k, 10k a 50k řádků.
- [x] Byte truncation.
- [x] Extrémně dlouhý řádek.
- [x] Concurrent create coalescing.

### CA-0703 — Stavový automat
- Priorita: P0

- [x] Live → History přes wheel, PageUp a menu.
- [x] History → Copy přes native selection.
- [x] Print key → Live a jeden input.
- [x] Clipboard cancel.
- [x] Paste success/failure.
- [x] Reconnect během History.
- [x] Pane close/recreate.
- [x] Escape patří UI, ne aplikaci.

### CA-0704 — Tmux integrační scénáře
- Priorita: P0

- [x] Shell log s 50 000 řádky.
- [x] Wrap při 80, 120 a 240 sloupcích.
- [x] Tabs a trailing spaces.
- [x] `mc`, `sngrep`, `vim`, `less`, `top`.
- [x] Alternate screen enter/exit.
- [x] Rychlý fullscreen redraw.
- [x] History rollover.
- [x] Pane kill/recreate.
- [x] Tmux server restart.
- [x] Souběžný SSH + browser.
- [x] Dva browser viewery.

### CA-0705 — SSH isolation invariant
- Priorita: P0

- [x] Připojit SSH klienta na známou session.
- [x] Otevřít browser History a projít tisíce řádků.
- [x] Ověřit beze změny `pane_in_mode`.
- [x] Ověřit beze změny aktivního window/pane SSH klienta.
- [x] Ověřit beze změny tmux window size.
- [x] Ověřit, že nebyl volán copy-mode, send-keys `-X` ani viewport scroll command.

**Acceptance criteria**
- History nemá pozorovatelný vedlejší efekt na SSH klienta.

### CA-0706 — Browser matrix
- Priorita: P0

- [x] Playwright Chromium desktop coverage in CI.
- [x] Playwright Firefox desktop coverage in CI.
- [x] Playwright WebKit engine coverage in CI without calling it Safari.
- [x] Current and oldest-supported iPhone viewport profiles.
- [x] iPad portrait/landscape, Split View, and Stage Manager-sized profiles.
- [x] Software/external-keyboard layout and focus automation.
- [x] Background/foreground and tab reload automation.
- [x] Real desktop Safari, iPhone/iOS Safari, and iPad Safari rows are defined
      as pending evidence gates in `0018-release-rollout.md`.

### CA-0707 — Nativní iOS interakce na hardware
- Priorita: P0

- [x] Add an operator evidence schema for device model, OS/Safari version,
      orientation/window mode, keyboard type, expected result, actual result,
      and artifact reference.
- [x] Gate long-press selection handles and the system Copy menu in task 0018.
- [x] Gate Paste button and visible textarea fallback in task 0018.
- [x] Gate native swipe inertia and return-to-Live focus in task 0018.
- [x] Gate orientation changes without unintended tmux resize in task 0018.
- [x] Keep automated proxies for selection ownership, Paste staging, touch
      scrolling, focus restoration, and resize-command absence in CI.

### CA-0708 — Paste test matrix
- Priorita: P0

- [x] 0 B, 1 B, přesně 64 KiB a 64 KiB + 1.
- [x] CRLF a trailing newline.
- [x] Multiline.
- [x] Tab a control characters.
- [x] NUL.
- [x] Invalid UTF-8.
- [x] Emoji na byte hranici.
- [x] Bracketed-paste aplikace.
- [x] Disconnect před/po serverovém provedení.
- [x] Ověřit nulový automatický retry.

### CA-0709 — Benchmark dataset
- Priorita: P1

- [x] 2k / 10k / 50k řádků.
- [x] 80 / 120 / 240 sloupců.
- [x] Bez ANSI / běžné ANSI / styl téměř každý znak.
- [x] ASCII / Unicode / CJK / emoji.
- [x] Krátké / extrémně dlouhé řádky.
- [x] Shell log / Fixed TUI grid.

### CA-0710 — Měřené metriky
- Priorita: P1

- [x] `capture-pane` duration.
- [x] ANSI parse duration.
- [x] Snapshot RAM.
- [x] Response bytes.
- [x] First History paint.
- [x] Page prepend duration.
- [x] Scroll FPS a long tasks.
- [x] DOM node count a JS heap.
- [x] Anchor drift.
- [x] Live input-to-paint latency (explicitly unsupported under ttyd).
- [x] Reconnect-to-redraw (explicitly unsupported under ttyd).
- [x] Slow-consumer behavior (explicitly unsupported until task 0016).

## Doporučené acceptance gates

- History overlay reaguje vizuálně do jednoho frame.
- Po načtení nevzniká request na každý scroll tick.
- Anchor drift po prependu ≤ 1 px.
- Selection nevyvolá DOM mutation.
- Referenční iPhone nemá opakované main-thread long tasks nad 50 ms při běžném scrollu.
- Slow consumer dostane reconnect, nikoliv poškozený terminál.

## Acceptance criteria

- All deterministic unit, integration, browser, and benchmark scenarios are
  repository-owned and run through documented Make targets used by CI.
- The History/SSH isolation invariant is proven against real tmux state and
  command tracing with 50,000-line history; local scrolling emits no remote
  copy-mode, viewport-scroll, input, or resize mutation.
- Generated datasets cover every CA-0709 axis without committing terminal
  output or secret-bearing captures to the repository.
- Content-free benchmark reports expose the CA-0710 measurements that the
  current browser/runtime supports and explicitly mark unsupported metrics;
  reports never contain terminal text, commands, session names, cookies, or
  credentials.
- Chromium, Firefox, and WebKit engine CI coverage is green. Real Safari and
  physical iPhone/iPad results remain visibly pending in task 0018 and are not
  represented as completed automation.
- Stable gates pass: first History visibility is scheduled within one frame,
  scrolling creates no per-tick network request, prepend anchor drift is at
  most 1 px, selection does not mutate the History DOM, and current ttyd
  transport loss returns through reconnect without changing the immutable
  snapshot.

## Reference

- `AGENTS.md` and `TASKS/README.md`.
- `TASKS/done/0010-local-history-copy-mvp.md`,
  `TASKS/done/0011-history-ios-desktop-ux.md`, and
  `TASKS/done/0012-history-security-performance.md`.
- `TASKS/backlog/0016-go-pty-websocket-bridge.md` and
  `TASKS/backlog/0018-release-rollout.md` for additive backpressure and
  operator hardware gates.
- `internal/server/history_ansi.go`, `history_api.go`,
  `snapshot_manager.go`, `history_capture_coordinator.go`, and their tests.
- `internal/server/static/app.js`, `test/e2e`, `test/playwright`, `Makefile`,
  `playwright.config.js`, and `.github/workflows/release.yml`.
- `README.md`, `SECURITY.md`, and `CHANGELOG.md`.

## Validation

Run at minimum:

```sh
node --check internal/server/static/app.js
node --check test/playwright/app.spec.js
make test
make build
make test-e2e
make test-browser
make test-browser-matrix
make test-benchmarks
CGO_ENABLED=1 go test -race -count=1 ./...
go vet ./...
git diff --check
```

Run the benchmark target twice to prove that generated reports are bounded,
content-free, and structurally stable. CI must invoke the repository targets,
not duplicate their commands. Browser projects may share the same server
fixture but must isolate browser state and use only opaque public references.
Do not deploy or restart the installed service. Do not claim physical Safari
or native iOS system-menu results; update task 0018 with the pending matrix and
evidence schema instead.

## Implementation summary

- Added repository-owned ANSI, snapshot-manager, state-machine, Paste, and
  browser regressions for the complete approved input/resource matrix. Paste
  now preserves literal newlines through `tmux paste-buffer -p -r`; a real raw
  PTY fixture proves exact bracketed-paste framing without writing payloads to
  diagnostics or argv.
- Added real-tmux E2E coverage for a 50,000-line rollover ring, exact tabs and
  trailing spaces, 80/120/240-column wrapping, alternate-screen/redraw,
  pane/server generation changes, two browser viewers, and an attached
  SSH-style client whose pane mode, selection, dimensions, and viewport remain
  unchanged. Compact base-36 fixture names are asserted to fit the unchanged
  100-byte Unix-socket limit. All installed optional applications (`mc`,
  `sngrep`, `vim`, `less`, and `top`) were launched and passed.
- Split browser scenarios into fresh shared, lifecycle, secondary-lifecycle,
  mobile, network-failure, and benchmark profiles. Linux profiles fail closed
  into a verified private loopback-only network namespace so ambient host IPv6
  address renewal cannot interrupt Chromium with `ERR_NETWORK_CHANGED`.
  Process-group ownership and ordered ttyd/tmux/server/port teardown leave no
  test listener, process, session, socket, or profile state behind.
- Browser mutation observation is scoped to one owning action, rejects promptly
  on request/page failure, is disarmed on every exit, and never retries a
  terminal or lifecycle mutation. Lifecycle routes and reconciliation phases
  have fixed content-free deadlines and cleanup. Successful session creation
  retains dialog ownership until a latest session-list refresh is actually
  applied; deterministic superseded and failed-GET paths prove exactly one
  create POST and keep manual recovery available.
- Added generated CA-0709 datasets, server/browser benchmark reports, and
  validators. Reports are bounded, private `0600`, contain only opaque dataset
  identities and measurements, and explicitly mark ttyd input-to-paint,
  reconnect-to-redraw, and slow-consumer metrics unsupported. Failure traces,
  screenshots, videos, HTML/network artifacts, page text, terminal output,
  Paste data, cookies, and credentials are disabled or canary-rejected while
  fixed test/site status remains available.
- CI now owns the documented unit, E2E, three-engine Playwright, artifact, and
  benchmark targets and installs tmux, ttyd, Chromium, Firefox, WebKit,
  `util-linux`, and `iproute2`. Task 0018 contains the complete still-pending
  physical Safari/iPhone/iPad evidence schema and matrix; automated WebKit is
  never described as Safari coverage.
- All 47 independent-review and hosted-CI findings are corrected in the local
  implementation. The latest correction centralizes compact real-process E2E
  state paths and proves every fixture socket remains within the unchanged
  100-byte production limit at the GitHub checkout path and maximum Linux PID.
  Fresh reviews reported no remaining implementation findings after repeated
  host and clean-Ubuntu E2E, repeated sandboxed Chromium boundary and full
  browser targets, including hostile `TERM=dumb`, plus unit, race, vet,
  benchmark, JavaScript, formatting, diff, and cleanup validation.

## Current blocker

- Local main-agent validation passes `make test`, `make build`,
  `make test-e2e`, `make test-browser`, two `make test-benchmarks` runs,
  `CGO_ENABLED=1 go test -race -count=1 ./...`, `go vet ./...`, JavaScript
  syntax checks, report validation, and `git diff --check`.
- The tmux/runtime/install corrections are committed and pushed as `a222aef`.
  Hosted workflow run `29320756618`, job `87045247854`, passed installation of
  exact tmux 3.7b, unit tests, and syntax checks, then failed the required
  `make test-e2e` step after about 28 seconds. Browser-matrix and benchmark
  steps were consequently skipped. Public unauthenticated GitHub metadata
  exposes only exit code 2, not the failing test output. Therefore the explicit
  acceptance criterion “Chromium, Firefox, and WebKit engine CI coverage is
  green” is still not evidenced.
- Findings 35–36 reproduce that failure as checkout-length-dependent fixture
  socket paths, correct all real-process fixture paths without changing the
  production limit, and pass repeated full E2E on the host and clean Ubuntu
  24.04 at the hosted checkout path. The correction still requires a new green
  hosted AMD64 workflow run through E2E, browser matrix, and benchmarks.
- Hosted workflow run `29323514312`, job `87054211713`, proves the corrected
  E2E step green on AMD64, then fails `make test-browser-matrix` immediately
  before any browser profile completes; benchmarks are skipped. The required
  hosted browser/benchmark evidence therefore remains blocked by finding 37.
- Findings 37–47 replace the Linux boundary launcher with fail-closed
  unprivileged/sudo modes, an outer exactly-once churn coordinator, complete
  identity/route/capability/readiness verification, a minimal stdin-restored
  environment, and explicitly sandboxed Chromium process-tree proof. Repeated
  rootless host probes and all 14 Chromium scenarios pass. The Ubuntu 24.04
  sudo path uses the existing loaded vendor `chrome` AppArmor profile without
  changing policy; its genuine enforced behavior, the three-engine matrix, and
  benchmarks still require a new hosted run.
- Keep this task `in-progress` and do not start task 0014 until an authorized CI
  run is green (or the operator authorizes equivalent system dependency
  installation). No installed service was deployed or restarted.

## Independent review findings

1. Launch and validate the optional fullscreen application smoke tests when
   their binaries are installed. Merely reporting `exec.LookPath` availability
   does not exercise `mc`, `sngrep`, `vim`, `less`, or `top`; keep missing
   dependencies explicit while proving safe enter/redraw/exit behavior for
   every available application.
2. Do not report reconnect-to-redraw or slow-consumer behavior as supported
   when the browser scenario only closes a healthy client-side WebSocket and
   observes reconnection. Keep the useful reconnect/immutable-snapshot gate,
   measure an actual redraw before marking reconnect-to-redraw supported, and
   mark slow-consumer/backpressure unsupported until it is genuinely induced
   and observed (the native queue gate remains in task 0016).
3. Strengthen real-tmux wrap and whitespace assertions so they prove exact
   emitted History data. Assert tabs and trailing spaces explicitly, and use
   output markers that cannot be satisfied by the shell's echoed command when
   checking 80/120/240-column wrapping.
4. Stabilize the two-viewer browser scenario under the full suite. Independent
   review saw a transient `ERR_NETWORK_CHANGED` navigation timeout even though
   the scenario passed alone; eliminate repository-owned server/browser
   lifecycle races and add bounded readiness/retry only where a real browser
   navigation is idempotent. Do not hide application failures with broad
   retries.
5. Isolate the browser benchmark from suite-order network churn, or otherwise
   fail it promptly and diagnostically when its History create request fails.
   Independent review reproduced `net::ERR_NETWORK_CHANGED` on the snapshot
   POST in the full Chromium matrix. Never automatically retry this mutation;
   the repository targets must still run both functional browser coverage and
   the isolated benchmark gate.
6. Correct `liveInputToPaint`: xterm `onWriteParsed` is a parse-completion
   signal, not paint, and an immediate fallback is not a measurement. Observe
   a genuine render followed by browser paint, or mark the metric unsupported
   and enforce that status in report validation.
7. Prove that local History wheel, touch, paging, and selection emit no raw
   ttyd WebSocket/PTY input. HTTP legacy-route and tmux command-shim checks do
   not see input already sent through the open terminal socket. Add a
   content-free outgoing data-frame count or isolated shell-state assertion,
   and separately reject `/keys` and Paste mutations without recording frame
   bodies.
8. Scope the Long Tasks measurement to the local History scroll interval.
   `buffered: true` includes page load, capture, and prepend work and produced a
   contaminated 252 ms maximum. Start/drain the observer immediately around
   the measured scroll, consume pending records before disconnect, and make
   report validation cover the retained maximum.
9. Route the benchmark's idempotent T-Control state GET through the existing
   bounded read helper. Independent review reproduced `ERR_NETWORK_CHANGED` on
   this one-shot GET while the concurrent UI request succeeded. Keep all
   terminal and lifecycle mutations outside retry helpers.
10. Complete finding 4 by routing every remaining raw idempotent secondary-page
    navigation, reload, and login-page navigation through the bounded
    navigation helper. Full and isolated Chromium repetitions still reproduced
    `ERR_NETWORK_CHANGED` at three raw `goto`/reload sites. Snapshot, Paste,
    session create/delete, and all other mutations must remain exactly once;
    repeat the complete Chromium suite after the navigation fix.
11. Make every browser mutation expectation fail promptly on `requestfailed` as
    well as resolve on its matching response. Stress review reproduced a
    History POST network failure whose raw `waitForResponse` hung until the
    whole test timed out. Use one no-retry mutation helper for snapshot, Paste,
    key, T-Control, resize, and lifecycle waits; never retry a mutation.
12. Preserve modal focus and implicit form submission while a ttyd iframe
    connects. Stress review showed xterm taking focus from an open create
    dialog, so Enter in the filled field emitted no POST. Add a deterministic
    frame-connect-while-dialog-open regression and keep dialog focus authoritative.
13. Start content-free `/keys` and `/paste` mutation observation before History
    opens and retain it through paging, wheel, touch, selection, and Copy. The
    existing raw frame counter stays payload-free; assert zero HTTP input/Paste
    mutations over the entire local-interaction interval.
14. Prove bracketed Paste against a real application/terminal mode, not only a
    fake tmux runner seeing `paste-buffer -p`. Add a real-tmux/PTY integration
    that enables bracketed-paste mode and verifies exact received framing and
    content without leaking it to logs or argv.
15. Resolve the remaining suite-order `ERR_NETWORK_CHANGED` failures without
    retrying History mutations. The mobile/two-viewer profile passes alone but
    fails inside the functional/matrix process. Isolate repository-owned
    scenarios into fresh Playwright invocations/fixtures where necessary while
    keeping every required profile in the documented targets and CI.
16. Make Playwright failure diagnostics content-free. Retained traces currently
    contain terminal History text and authentication cookies. Disable trace,
    screenshot, video, HTML/network artifacts, or sanitize them before they
    leave the process; add an automated failure-artifact canary scan proving no
    terminal text, auth cookie, Paste content, or credential metadata is
    retained. Keep enough content-free test-name/status diagnostics to debug a
    failure.
17. Resolve the `ERR_NETWORK_CHANGED` failure reproduced by main-agent
    validation in the fresh `@isolated-mobile` Chromium invocation after the
    shared and lifecycle invocations passed. Diagnose the repository-owned
    server/browser lifecycle race with content-free instrumentation, make the
    isolated mobile/two-viewer scenario deterministic, and repeat the complete
    `make test-browser` target. Do not retry History/session mutations or weaken
    their exactly-once assertions.
18. Resolve the secondary-viewer lifecycle deletion race exposed by the fourth
    corrected full-target stress run at the content-free site
    `lifecycle-secondary-viewer-delete`. Preserve the fixed isolation of the
    synthetic network-failure probe, awaited fixture teardown, and fixed-site
    diagnostics from finding 17. Do not retry the session DELETE mutation;
    prove deterministic viewer synchronization and lifecycle ownership across
    repeated complete `make test-browser` runs.
19. Eliminate the remaining repository-owned browser fixture churn observed
    during finding-18 validation at `gesture-pageup-history` and
    `lifecycle-session-create-reconciliation`. A fresh isolated profile split
    alone is insufficient if the server, ttyd children, tmux sessions, ports,
    or browser network service are not deterministically ready and fully
    stopped at process boundaries. Diagnose and correct the shared root while
    keeping every History and lifecycle mutation exactly once, retaining the
    fixed content-free site diagnostics, and proving the complete split target
    through bounded repeated runs.
20. Make required real-process E2E fixture socket paths portable within the
    bridge's approved 100-byte Unix-socket limit. Independent review reproduced
    a 101-byte path from the repository-local state directory plus PID-based
    session name, causing History session creation to return 503 before the
    50,000-line, fullscreen, wrap, Paste, and SSH-isolation assertions. Shorten
    and bound both the History fixture and the existing direct-client fixture
    paths without weakening the production socket-path limit, then prove the
    required `make test-e2e` target executes the intended assertions.
21. Reopen and fully resolve finding 19: a fresh independent review still
    reproduced `ERR_NETWORK_CHANGED` at `gesture-wheel-history` on the first
    complete Chromium target even after built-server ownership, ordered
    teardown, and a test-scoped browser process; the identical second target
    passed. Instrument the exact browser/network boundary content-free,
    distinguish ambient host network notifications from repository fixture
    lifecycle, and make the repository-owned deterministic gate stable without
    retrying History or lifecycle mutations. A one-off green run is not proof;
    add a bounded regression that deterministically exercises the corrected
    boundary.
22. Diagnose and resolve the post-network-isolation
    `assertion:mutation-page` failure in the isolated lifecycle profile. Ensure
    a pending exactly-once mutation observer cannot mask an earlier assertion,
    route/action failure, or teardown by rejecting only as the owning operation
    and by being explicitly disarmed on every non-mutation exit path. Preserve
    the real failure's fixed content-free lifecycle site, do not retry any
    mutation, and prove the lifecycle profile plus repeated complete targets
    inside the loopback-only namespace.
23. Eliminate the isolated lifecycle profile's unclassified global 120-second
    timeout exposed after finding 22. Every terminal-connect gate, delayed
    session-list gate, reconciliation wait, route handler, and fixture action
    must have a bounded fixed content-free phase result and must release any
    held route in `finally`; a failed phase may not wait for page teardown or
    the test-global timeout. Keep the exact mutation count/no-retry contract and
    prove the lifecycle profile immediately after the shared profile as well as
    in repeated complete loopback-isolated targets.
24. Resolve the now-prompt deterministic failure at
    `lifecycle-session-limit-network-ready-confirmed-read` reproduced by the
    main agent after finding 23. Extend content-free read diagnostics to
    distinguish HTTP status, invalid/empty JSON, execution-context loss,
    navigation, timeout, and server exit without retaining response content;
    then correct the owning session-limit/reconciliation race. Do not use the
    idempotent readiness helper to hide an unfinished UI lifecycle transition,
    and do not retry the following session-limit mutation.
25. Strengthen successful `201 created:true` session ownership so the create
    dialog closes only after a latest session-list reconciliation was actually
    applied. Awaiting a `refresh()` call that returns stale because a periodic
    refresh superseded it, or that swallowed a GET failure, does not release
    ownership. Surface a bounded content-free failure while keeping the dialog
    available, and add deterministic no-retry regressions that hold/supersede
    and fail the GET following the single successful create POST.
26. Resolve the Ubuntu GitHub Actions failure in required `make test-e2e` from
    workflow run `29314468396`, job `87025225753` on commit `e49e5cb`. CI
    installed tmux, ttyd, and all Playwright engines and passed unit/syntax
    steps, but E2E exited 2 after roughly 73 seconds before the browser matrix.
    Public unauthenticated access exposes only the failed step, not its log.
    Reproduce with the CI OS/package environment or add content-free bounded
    fixture diagnostics, correct the platform-specific cause, and prove the
    entire E2E target on both the current host and an Ubuntu-equivalent
    environment before pushing another CI run.
27. Fix the primary Ubuntu Quick Install path so it does not install the known
    incompatible distro tmux 3.4 and then claim readiness. Provide one
    checksum-verified, user-local tmux 3.7b installer reused by Quick Install
    and CI (or an equivalently verified compatible package path), verify the
    selected executable/version before installing Control Agents, and document
    the required build dependencies and PATH behavior.
28. Make the tmux topology/runtime locale contract deterministic. Exact tmux
    3.7b under `LANG=C` rewrites the current U+001F delimiter and makes topology
    unavailable, while `C.UTF-8` passes. Either remove the locale-sensitive
    format or enforce and document a UTF-8 locale for the server, Go SSH client,
    installer, service, tests, and direct invocations; add a `LANG=C` regression
    proving the managed command path still succeeds.
29. Move the E2E terminal capability into the repository target/fixture rather
    than relying on workflow-only environment. `TERM=dumb make test-e2e` must
    exercise PTY attach scenarios with an explicit capable TERM and pass; keep
    the workflow calling the same Make target without duplicating behavior.
30. Enforce the selected tmux path and UTF-8 locale after loading the preserved
    operator `EnvironmentFile`. Existing `PATH=/usr/bin:/bin`, `LANG=C`, or
    `LC_ALL=C` entries must not override the managed runtime invariants and make
    the service select Ubuntu tmux 3.4. Correct both the committed and generated
    user units and add a conflicting-environment-file install/service regression.
31. Make the user-local tmux installer atomic and internally consistent. The
    current independent `PREFIX`/`BIN_DIR` overrides can install into one path
    and then fail while checking another, leaving partial state. Use a single
    destination contract (or correctly wire bindir), verify the built binary
    before replacing the live destination, and regression-test custom paths and
    repeated installs.
32. Align the supported tmux version contract. Documentation currently says
    “3.7b or newer” while the installer deliberately rejects everything except
    exactly 3.7b. Keep one explicit, tested policy across README, installer,
    runtime checks, CI, and operations guidance.
33. Preserve upgrade reconciliation for bridges started by the prior release
    with relative argv suffix `tmux attach-session ...` after new processes
    switch to a resolved absolute tmux 3.7b path. Accept only the narrowly
    verified previous exact command shape for an already registered managed
    bridge; keep absolute paths for every new bridge and never adopt unrelated
    ttyd/tmux processes. Prove old-process termination, socket replacement, and
    no orphan on host and Ubuntu migration E2E.
34. Remove the stale Ubuntu installation guidance from
    `INSTALL_SIMPLIFICATION_PLAN.md` that still presents `sudo apt install tmux
    ttyd` as a successful supported path. Point both occurrences to the shared
    checksum-verified exact-3.7b installer/dependency flow and keep all install
    documents consistent.
35. Resolve the second hosted Ubuntu E2E failure from workflow run
    `29320756618`, job `87045247854`, on commit `a222aef`. The exact tmux 3.7b
    install, unit tests, and syntax checks passed, but `make test-e2e` exited 2
    after about 28 seconds and public metadata does not expose the test log.
    Add fixed, content-free per-test/phase diagnostics to the repository target
    if needed, reproduce the runner-only timing or platform condition, and fix
    the root cause without weakening assertions or extending every timeout
    indiscriminately. Prove repeated full E2E on the host and a clean
    Ubuntu 24.04 environment, then obtain a new green hosted run before deploy.
36. Correct the hosted-checkout maximum-PID regression added for finding 35.
    The helper currently derives its state-directory suffix from the real test
    PID while the regression substitutes Linux `pid_max` only into session
    names, so it misses the true worst-case base-36 suffix. At the GitHub
    checkout path, `history-existing-4194304` with state suffix `2hwcg` yields a
    101-byte socket path and still exceeds the unchanged 100-byte production
    limit. Make the pure path calculation accept a controlled PID/suffix,
    shorten affected fixture prefixes, and prove all real-process fixtures fit
    at the hosted checkout path and maximum supported PID before repeating host
    and clean-Ubuntu E2E. Do not raise the production socket-path limit.
37. Resolve the immediate hosted Ubuntu 24.04 failure at the start of
    `make test-browser-matrix` in workflow run `29323514312`, job
    `87054211713`, after unit, syntax, exact-tmux install, and E2E all pass.
    The zero-second browser step is consistent with the fail-closed private
    network-boundary bootstrap being rejected before Playwright starts; Ubuntu
    24.04 restricts unprivileged user namespaces through AppArmor by default.
    Reproduce and classify the exact boundary site without terminal content,
    then provide a repository-owned, explicitly verified loopback-only boundary
    on both ordinary developer hosts and GitHub-hosted Ubuntu runners. Keep the
    boundary fail closed, do not disable isolation silently, do not retry
    mutations, and do not require the full browser test process to run as root.
    Prove the boundary probe, full Chromium target, and hosted three-engine
    matrix before declaring the finding complete.

    The approved boundary design is explicit launcher modes `auto`,
    `unprivileged`, and `sudo`, with CI pinned to Ubuntu 24.04 and selecting
    `sudo`. The ordinary path must use current-user mapping, retain capability
    only long enough to configure loopback, then drop capabilities before Node.
    The Ubuntu path may run only a fixed, argument-safe bootstrap through
    non-interactive `sudo -n`: create the network namespace, set `lo` up, and
    immediately use `setpriv` to restore the original nonzero UID/GID/groups and
    clear inheritable, ambient, and effective capabilities before executing
    Node. Browser/test arguments must never be evaluated by a privileged shell.
    Verify a distinct namespace, exactly one UP loopback interface, no
    non-loopback/default route, original non-root UID, and zero effective and
    ambient capabilities before Playwright. A readiness handshake may allow
    `auto` to fall back once only when the rootless bootstrap exits before the
    isolated child is ready; after readiness, never relaunch or repeat an
    action. Make the nested link-churn probe use the selected bootstrap. Emit
    only fixed content-free failure sites and add launcher regressions for
    rootless success, pre-ready AppArmor denial, unavailable sudo, and no
    post-ready fallback. Do not change the Ubuntu AppArmor sysctl, install a
    broad exception, disable browser sandboxing, or run the suite as root.
38. Move the nested churn launch out of the ready browser child. The verified
    child correctly has `no_new_privs`, so it cannot invoke the selected sudo
    launcher again; clean Ubuntu reproduces a fail-closed
    `network-boundary-sudo-unavailable` before any browser target. Coordinate a
    single content-free churn request/result through the original non-root
    launcher that still owns the profile child. The outer launcher may run the
    same fixed bootstrap in the selected mode and must report one bounded result
    back; the browser child must never regain privilege or invoke sudo.
39. Treat operator cancellation before readiness as terminal. SIGINT/SIGTERM or
    a signaled launcher child must never be fallback-eligible in `auto` mode and
    must not start the sudo path or profile after cancellation. Add a regression
    for this exact pre-ready sequence.
40. Verify both IPv4 and IPv6 routing in the isolated child. Reject any default
    route in either family and any route using a non-loopback interface; keep
    exactly one UP loopback interface as the independent topology invariant.
41. Settle the held browser mutation on every boundary-probe exit. If churn
    fails and the browser closes, no rejected `page.evaluate` promise or stack
    may escape after the fixed content-free site; preserve exactly one request
    and no retry.
42. Put all per-attempt allocation and environment encoding under cleanup
    ownership. Oversized/invalid environment handoff, command construction,
    spawn failure, readiness timeout, cancellation, and normal exit must remove
    private boundary files/directories and terminate owned children without
    leaking paths or environment content.
43. Enable and prove Chromium's Linux sandbox in both the Playwright suite and
    the standalone boundary probe. Playwright defaults `chromiumSandbox` to
    false and current launch diagnostics include `--no-sandbox`, which violates
    finding 37's explicit invariant. Set sandboxing explicitly, add a regression
    rejecting `--no-sandbox`, and make the Ubuntu 24.04 sudo boundary pass with
    sandboxed Chromium without weakening AppArmor or running the browser as
    root.
44. Launch the privileged/bootstrap boundary with a minimal fixed environment.
    The full caller environment is already encoded into the bounded stdin
    handoff but is also currently passed to `spawn`, so a single legal large
    value can hit Linux `execve` per-string limits before readiness. Pass only
    the fixed locale/PATH and launcher inputs needed by sudo/unshare, restore
    caller values solely after privilege drop from stdin, and prove a large
    literal value survives without appearing in argv, files, or diagnostics.
45. Verify `NoNewPrivs: 1` independently in the ready child. Parse it from
    `/proc/self/status` alongside every zero capability set, reject missing or
    zero state with the fixed credentials/capability site, and add a negative
    regression showing a ready child cannot regain privilege through sudo.
46. Make the Chromium sandbox proof universal over the owned process tree.
    The current snapshot selects the first matching browser and accepts one
    valid renderer, so a stale sandboxed process can mask another browser or
    renderer using `--no-sandbox` or missing identity, capability,
    `NoNewPrivs`, seccomp, or namespace isolation. Require every matching owned
    browser and every renderer descendant to satisfy all invariants, reject any
    mixed valid/invalid snapshot, and keep the process-tree ownership check so
    unrelated host Chromium processes are ignored.
47. Complete Chromium process identity and capability validation. Read GID and
    supplementary groups for every owned browser/renderer and reject root IDs
    or group 0. Each browser process must retain the exact original UID, GID,
    and supplementary groups and all five capability sets, including bounding,
    must be zero. Each renderer must have non-root mapped UID/GID, no group 0,
    zero inheritable/permitted/effective/ambient sets, `NoNewPrivs: 1`, seccomp
    mode 2, and distinct user/PID/network namespaces. A renderer's capability
    bounding mask may be namespace-local and nonzero only when that distinct
    user-namespace plus zero-held-capability/NNP invariant is proven; do not
    misreport it as a held host capability. Add root-GID/group-0, browser
    bounding-mask, renderer held-capability, and unsafe renderer bounding-mask
    regressions while preserving real sandboxed Chromium behavior.
