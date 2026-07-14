const { test: baseTest, expect } = require("@playwright/test");
const fs = require("fs");
const https = require("https");
const net = require("net");
const path = require("path");
const { spawn, spawnSync } = require("child_process");
const { historyViewportProfiles } = require("./history_profiles");

const test = baseTest.extend({
  page: async ({ playwright, browserName }, use, testInfo) => {
    const projectUse = testInfo.project.use || {};
    const launchOptions = { ...(projectUse.launchOptions || {}) };
    if (typeof projectUse.headless === "boolean") launchOptions.headless = projectUse.headless;
    if (typeof projectUse.channel === "string") launchOptions.channel = projectUse.channel;
    if (projectUse.proxy) launchOptions.proxy = projectUse.proxy;
    if (!Number.isFinite(launchOptions.timeout)) launchOptions.timeout = 10_000;

    let browser;
    let context;
    let page;
    let cleanupFailed = false;
    try {
      browser = await runBoundedBrowserPhase(
        "browser-fixture-launch",
        () => playwright[browserName].launch(launchOptions),
        12_000
      );
      context = await runBoundedBrowserPhase(
        "browser-fixture-context",
        () => browser.newContext(browserContextOptions(projectUse)),
        5_000
      );
      page = await runBoundedBrowserPhase("browser-fixture-page", () => context.newPage(), 5_000);
      await use(page);
    } finally {
      if (context) {
        try {
          await runBoundedBrowserPhase("browser-fixture-context-close", () => context.close(), 5_000);
        } catch (_error) {
          cleanupFailed = true;
        }
      }
      if (browser) {
        try {
          await runBoundedBrowserPhase("browser-fixture-browser-close", () => browser.close(), 5_000);
        } catch (_error) {
          cleanupFailed = true;
        }
        if (browser.isConnected()) cleanupFailed = true;
      }
      if (cleanupFailed) {
        throw new Error("[site:browser-process-boundary] test browser fixture did not stop cleanly");
      }
    }
  }
});

const repoRoot = path.resolve(__dirname, "../..");
const sessionName = process.env.CONTROL_AGENTS_PLAYWRIGHT_FIXTURE_ID || `pw${process.pid}`;
const createdSessionName = `${sessionName}-created`;
const nextSessionName = `${sessionName}-next`;
const conflictSessionName = `${sessionName}-conflict`;
const limitSessionName = `${sessionName}-limit`;
const externalSessionName = `${sessionName}-external`;
const managedTestSessions = [sessionName, createdSessionName, nextSessionName, externalSessionName];
const stateDir = process.env.CONTROL_AGENTS_PLAYWRIGHT_STATE_DIR || path.join(repoRoot, ".cache", `pw${process.pid}`);
const tmuxCommandLog = path.join(stateDir, "tmux-command.log");
const benchmarkLog = path.join(stateDir, "history-benchmark.log");

let app;
let appLogs = "";
let baseURL = "";
let appExitPromise = Promise.resolve();
const appLifecycle = {
  exited: false,
  ready: false,
  spawnFailed: false
};

test.skip(!commandExists("go"), "go is required for browser e2e tests");
test.skip(!commandExists("tmux"), "tmux is required for browser e2e tests");
test.skip(!commandExists("ttyd"), "ttyd is required for browser e2e tests");

test.beforeAll(async () => {
  fs.rmSync(stateDir, { force: true, recursive: true });
  fs.mkdirSync(stateDir, { recursive: true });
  const tmuxShimDir = installTmuxCommandShim();

  const port = await freePort();
  baseURL = `https://127.0.0.1:${port}`;
  app = spawn(path.join(repoRoot, "bin", "control-agents-server"), [], {
    cwd: repoRoot,
    env: {
      ...process.env,
      CONTROL_AGENTS_PASSWORD: "secret",
      CONTROL_AGENTS_BIND_ADDR: "127.0.0.1",
      CONTROL_AGENTS_PORT: String(port),
      CONTROL_AGENTS_STATE_DIR: stateDir,
      CONTROL_AGENTS_MAX_SESSIONS: "3",
      CONTROL_AGENTS_TEST_REAL_TMUX: resolveCommand("tmux"),
      CONTROL_AGENTS_TEST_TMUX_COMMAND_LOG: tmuxCommandLog,
      CONTROL_AGENTS_TEST_BENCHMARK_LOG: benchmarkLog,
      PATH: `${tmuxShimDir}${path.delimiter}${process.env.PATH || ""}`
    },
    stdio: ["ignore", "pipe", "pipe"]
  });
  appExitPromise = new Promise((resolve) => {
    let settled = false;
    const settle = () => {
      if (settled) return;
      settled = true;
      resolve();
    };
    app.once("error", () => {
      appLifecycle.spawnFailed = true;
      settle();
    });
    app.once("exit", () => {
      appLifecycle.exited = true;
      settle();
    });
  });
  app.stdout.on("data", (chunk) => {
    appLogs += chunk.toString();
  });
  app.stderr.on("data", (chunk) => {
    appLogs += chunk.toString();
  });

  await waitForHTTPS(`${baseURL}/login`, 30_000);
  appLifecycle.ready = true;

  const wrapper = spawnSync(path.join(repoRoot, "bin", "control-agents"), [sessionName], {
    cwd: repoRoot,
    env: {
      ...process.env,
      CONTROL_AGENTS_STATE_DIR: stateDir,
      CONTROL_AGENTS_NO_ATTACH: "1",
      CONTROL_AGENTS_WEB_SCROLLBACK_LINES: "2345"
    },
    encoding: "utf8"
  });
  if (wrapper.status !== 0) {
    throw new Error(`[site:session-fixture-create] control-agents fixture creation failed with status ${wrapper.status}`);
  }

  const historyReady = `control-agents-history-${process.pid}`;
  const historyCommand = `seq -f 'playwright-line-%05.0f' 1 60000; tmux wait-for -S ${historyReady}`;
  run("tmux", ["send-keys", "-t", sessionName, historyCommand, "C-m"]);
  run("tmux", ["wait-for", historyReady]);
  await waitForTmuxHistory(sessionName, 49_900, 20_000);
  const bidiReady = `control-agents-bidi-${process.pid}`;
  run("tmux", ["send-keys", "-t", sessionName, `printf 'bidi\\342\\200\\256warning\\n'; tmux wait-for -S ${bidiReady}`, "C-m"]);
  run("tmux", ["wait-for", bidiReady]);
  await waitForRegisteredTtyd(sessionName, 10_000);
  await assertServerFixtureHealthy("server-fixture-ready");
});

test.afterAll(async () => {
  const ttydPIDs = [];
  for (const managedSession of managedTestSessions) {
    const ttydPID = killRegisteredTtyd(managedSession);
    if (ttydPID) ttydPIDs.push(ttydPID);
    spawnSync("tmux", ["kill-session", "-t", managedSession], { stdio: "ignore" });
  }
  spawnSync("tmux", ["kill-session", "-t", conflictSessionName], { stdio: "ignore" });
  await Promise.all(ttydPIDs.map((pid) => waitForPIDExit(pid, 3_000)));
  await stopServerFixture();
  await waitForHTTPSExit(`${baseURL}/login`, 5_000);
  fs.rmSync(stateDir, { force: true, recursive: true });
});

test("authenticates, renders registered terminal, sends special keys, and logs out", async ({ page }) => {
  await navigateIdempotentWithRetry(page, `${baseURL}/`);
  await expect(page).toHaveURL(/\/login$/);

  await page.locator("#password").fill("wrong");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/login\?error=1$/);
  await expect(page.getByRole("alert")).toBeVisible();

  await login(page);
  await expect(page.locator("#version-badge")).toBeVisible();
  await expect(page.locator("#heartbeat")).toHaveAttribute("data-state", "online");
  await expect(page.getByRole("button", { name: sessionName })).toBeVisible();
  const managedSession = await publicSession(page, sessionName);
  expect(managedSession.id).not.toBe(sessionName);
  expect(managedSession.activePaneRef).toMatch(/^p_[A-Za-z0-9_-]+$/);

  const terminalFrame = page.locator(`iframe[title="${sessionName}"]`);
  await expect(terminalFrame).toBeVisible();
  await expect.poll(() => terminalFrame.getAttribute("src")).toContain(`/terminal/${encodeURIComponent(managedSession.id)}/`);
  await expect(terminalFrame).not.toHaveAttribute("src", new RegExp(escapeRegex(sessionName)));
  await waitForTerminalFrame(page);
  const terminalPolicy = await page.evaluate(async (src) => {
    const response = await fetch(src, { credentials: "same-origin", cache: "no-store" });
    return {
      status: response.status,
      csp: response.headers.get("content-security-policy"),
      html: await response.text()
    };
  }, await terminalFrame.getAttribute("src"));
  expect(terminalPolicy.status).toBe(200);
  expect(terminalPolicy.csp).toBe("frame-ancestors 'self'");
  expect(terminalPolicy.html).toContain('<script src="/terminal-observer.js"></script>');
  expect(terminalPolicy.html).not.toContain("ObservedWebSocket");

  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: "Resize" }).click();
  const resizePanel = page.getByRole("dialog", { name: /resize/i });
  await expect(resizePanel).toBeVisible();
  await expect(resizePanel.getByLabel("Fixed")).toBeVisible();
  await expect(resizePanel.getByLabel("Fit once")).toBeVisible();
  await expect(resizePanel.getByLabel("Follow this device (later)")).toBeDisabled();

  const currentViewer = await waitForActiveResizeViewer(page);
  const fitResizeRequest = await applyResizeMode(page, "Fit once");
  expect(fitResizeRequest.postDataJSON()).toEqual({
    mode: "fit-once",
    paneRef: managedSession.activePaneRef,
    viewerId: currentViewer.id
  });
  await expect.poll(() => tmuxWindowSizeOption(sessionName)).toBe("manual");
  await expect.poll(() => tmuxWindowSize(sessionName)).toEqual({ width: currentViewer.width, height: currentViewer.height });

  const fixedResizeRequest = await applyResizeMode(page, "Fixed");
  expect(fixedResizeRequest.postDataJSON()).toEqual({ mode: "fixed", paneRef: managedSession.activePaneRef });
  const fixedSize = tmuxWindowSize(sessionName);

  const controlClient = await attachControlClient(sessionName, "82,21");
  try {
    await expect.poll(() => tmuxWindowSizeOption(sessionName)).toBe("manual");
    await expect.poll(() => tmuxWindowSize(sessionName)).toEqual(fixedSize);
  } finally {
    detachControlClient(controlClient);
  }
  await page.getByLabel("Close resize settings").click();

  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: "Keys" }).click();
  await expect(page.getByRole("button", { name: "Ctrl+C" })).toBeVisible();

  const sentKey = await runObservedMutation(page, (request) => {
    return request.method() === "POST" && request.url().includes(`/api/sessions/${encodeURIComponent(managedSession.id)}/keys`);
  }, () => page.getByRole("button", { name: "Esc" }).click());
  expect(sentKey.request.postDataJSON()).toEqual({ key: "escape", paneRef: managedSession.activePaneRef });
  expect(sentKey.response.status()).toBe(200);

  await page.getByLabel("Close special keys").click();

  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: "T-Control" }).click();
  await expect(page.getByLabel("Tmux controls")).toBeVisible();
  const originalControlState = await tmuxControlState(page, managedSession.id);
  const originalWindow = originalControlState.windows.find((tmuxWindow) => tmuxWindow.active);
  expect(originalWindow.ref).toMatch(/^w_[A-Za-z0-9_-]+$/);
  const sessionWindowBadge = sessionTab(page, sessionName).locator(".tab-window-badge");
  await expect(sessionWindowBadge).toHaveCount(0);
  await expect(page.locator(`#tcontrol-windows button[data-window-ref="${originalWindow.ref}"]`)).toHaveText(originalWindow.name);

  const newWindowMutation = await runObservedMutation(page, (request) => {
    return request.method() === "POST" && request.url().includes(`/api/sessions/${encodeURIComponent(managedSession.id)}/tmux-control`);
  }, () => page.getByRole("button", { name: "New win" }).click());
  expect(newWindowMutation.request.postDataJSON()).toEqual({ action: "new-window", paneRef: managedSession.activePaneRef });
  expect(newWindowMutation.response.status()).toBe(200);
  await expect.poll(() => tmuxWindowCount(sessionName)).toBeGreaterThan(1);
  await expect(sessionWindowBadge).toHaveText(String(tmuxWindowCount(sessionName)));
  const createdWindow = activeTmuxWindow(sessionName);

  const currentSession = await publicSession(page, sessionName);
  const originalWindowButton = page.locator(`#tcontrol-windows button[data-window-ref="${originalWindow.ref}"]`);
  await expect(originalWindowButton).toBeEnabled();
  const controlRefreshRoutePattern = new RegExp(`/api/sessions/${escapeRegex(managedSession.id)}/tmux-control$`);
  const selectedWindow = await runObservedMutation(page, (request) => {
    return request.method() === "POST" && request.url().includes(`/api/sessions/${encodeURIComponent(managedSession.id)}/tmux-control`);
  }, async () => {
    await originalWindowButton.evaluate((button) => {
      button.dataset.interactionMarker = "held-window-button";
    });

    let releaseControlRefresh;
    let markControlRefreshReady;
    const controlRefreshGate = new Promise((resolve) => {
      releaseControlRefresh = resolve;
    });
    const controlRefreshReady = new Promise((resolve) => {
      markControlRefreshReady = resolve;
    });
    let delayedControlRefresh = false;
    await page.route(controlRefreshRoutePattern, async (route) => {
      if (route.request().method() !== "GET" || delayedControlRefresh) {
        await route.continue();
        return;
      }
      delayedControlRefresh = true;
      const response = await route.fetch();
      const payload = await response.json();
      const refreshedWindow = payload.windows.find((tmuxWindow) => tmuxWindow.ref === originalWindow.ref);
      refreshedWindow.panes = Number(refreshedWindow.panes) + 10;
      const refreshedTitle = `${refreshedWindow.panes} panes`;
      markControlRefreshReady(refreshedTitle);
      await controlRefreshGate;
      await route.fulfill({ response, body: JSON.stringify(payload) });
    });

    const buttonBox = await originalWindowButton.boundingBox();
    expect(buttonBox).not.toBeNull();
    await page.mouse.move(buttonBox.x + buttonBox.width / 2, buttonBox.y + buttonBox.height / 2);
    await page.mouse.down();
    const refreshedTitle = await controlRefreshReady;
    releaseControlRefresh();
    await expect(originalWindowButton).toHaveAttribute("title", refreshedTitle);
    await expect(originalWindowButton).toHaveAttribute("data-interaction-marker", "held-window-button");
    await expect(originalWindowButton).toBeEnabled();
    await page.unroute(controlRefreshRoutePattern);
    await page.mouse.up();
  });
  expect(selectedWindow.request.postDataJSON()).toEqual({
    action: "select-window",
    paneRef: currentSession.activePaneRef,
    windowRef: originalWindow.ref
  });
  expect(selectedWindow.response.status()).toBe(200);
  if (createdWindow !== originalWindow) {
    run("tmux", ["kill-window", "-t", `${sessionName}:${createdWindow}`]);
  }

  await page.getByLabel("Close T-Control").click();
  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);
});

test("uses immutable local History without tmux scroll commands", async ({ page }) => {
  await login(page);
  const managedSession = await publicSession(page, sessionName);
  const terminalFrame = page.locator(`iframe[title="${sessionName}"]`);
  const frame = await waitForTerminalFrame(page);
  const sshLikeClient = await attachControlClient(sessionName, "92,28");
  const originalSSHClientState = tmuxClientState(sshLikeClient.name);
  const originalSrc = await terminalFrame.getAttribute("src");
  const originalWindowSize = tmuxWindowSize(sessionName);
  const originalPaneMode = run("tmux", ["display-message", "-p", "-t", sessionName, "#{pane_in_mode}"]).trim();
  expect(Number.parseInt(run("tmux", ["display-message", "-p", "-t", sessionName, "#{history_size}"]).trim(), 10)).toBeGreaterThanOrEqual(49_900);
  expect(run("tmux", ["display-message", "-p", "-t", sessionName, "#{history_limit}"]).trim()).toBe("50000");
  const forbiddenRequests = [];
  const historyCreates = [];
  const historyPages = [];
  let historyKeyRequests = 0;
  let historyPasteRequests = 0;
  const observeRequest = (request) => {
    const pathname = new URL(request.url()).pathname;
    if (pathname.endsWith("/scroll") || pathname.endsWith("/capture")) forbiddenRequests.push(request);
    if (request.method() === "POST" && pathname.startsWith("/api/v1/panes/") && pathname.endsWith("/history-snapshots")) {
      historyCreates.push(request);
    }
    if (request.method() === "GET" && pathname.endsWith("/pages")) historyPages.push(request);
    if (request.method() === "POST" && pathname.endsWith("/keys")) historyKeyRequests += 1;
    if (request.method() === "POST" && pathname.endsWith("/paste")) historyPasteRequests += 1;
  };
  page.on("request", observeRequest);

  const { request: createRequest, response } = await runObservedMutation(page, (request) => {
    const pathname = new URL(request.url()).pathname;
    return request.method() === "POST" && pathname.endsWith(`/${managedSession.activePaneRef}/history-snapshots`);
  }, async () => {
    await page.getByRole("button", { name: "Menu" }).click();
    await page.getByRole("button", { name: "History" }).click();
  });
  expect(response.status()).toBe(201);
  expect(createRequest.postDataJSON()).toEqual({ mode: "reflow" });
  expect(createRequest.headers()["x-control-agents-viewer-id"]).toMatch(/^viewer-/);
  expect(createRequest.headers()["x-control-agents-csrf-token"]).toMatch(/^[A-Za-z0-9_-]+$/);
  expect(response.headers()["cache-control"]).toBe("no-store");

  const overlay = page.getByRole("region", { name: "Terminal history" });
  await expect(overlay).toBeVisible();
  await expect(page.locator("#terminal-pane")).toHaveAttribute("data-ui-state", "HISTORY");
  await expect(page.locator("#terminal-pane")).toHaveClass(/history-open/);
  await expect(terminalFrame).toBeVisible();
  await expect(terminalFrame).toHaveAttribute("src", originalSrc);
  await expect(terminalFrame).toHaveAttribute("inert", "");
  await expect(page.locator("#history-content")).toContainText("playwright-line");
  await expect(page.locator("#history-content")).toContainText("bidi[BIDI U+202E]warning");
  await expect(page.locator("#history-content .bidi-warning")).toHaveCount(1);
  await expect(page.locator("#history-notice")).toContainText("Bidirectional text controls were replaced");
  await expect(page.locator("#history-new-output")).toBeHidden();
  const terminalOutgoingInputFrames = await terminalOutgoingInputFrameCount(frame);

  const initialLineCount = await page.locator("#history-content .history-line").count();
  expect(initialLineCount).toBe(3000);
  const pagingCommandCheckpoint = tmuxCommandCheckpoint();
  const olderPage = page.waitForResponse((candidate) => {
    return candidate.request().method() === "GET" && new URL(candidate.url()).pathname.endsWith("/pages");
  });
  const anchorBefore = await overlay.evaluate((historyOverlay) => {
    historyOverlay.scrollTop = 650;
    const overlayTop = historyOverlay.getBoundingClientRect().top;
    const anchor = Array.from(historyOverlay.querySelectorAll(".history-line"))
      .find((line) => line.getBoundingClientRect().bottom >= overlayTop);
    if (!anchor) throw new Error("history anchor unavailable");
    anchor.dataset.playwrightAnchor = "true";
    return anchor.getBoundingClientRect().top;
  });
  expect((await olderPage).status()).toBe(200);
  await expect.poll(() => page.locator("#history-content .history-line").count()).toBeGreaterThan(initialLineCount);
  const anchorAfter = await page.locator(".history-line[data-playwright-anchor=true]").evaluate((anchor) => anchor.getBoundingClientRect().top);
  expect(Math.abs(anchorAfter - anchorBefore)).toBeLessThanOrEqual(1);
  expectNoHistoryTmuxMutations(pagingCommandCheckpoint, "progressive History paging");
  for (let pageIndex = 0; pageIndex < 2; pageIndex += 1) {
    const countBefore = await page.locator("#history-content .history-line").count();
    const nextPage = page.waitForResponse((candidate) => {
      return candidate.request().method() === "GET" && new URL(candidate.url()).pathname.endsWith("/pages");
    });
    await overlay.evaluate((historyOverlay) => {
      historyOverlay.scrollTop = 0;
    });
    expect((await nextPage).status()).toBe(200);
    await expect.poll(() => page.locator("#history-content .history-line").count()).toBeGreaterThan(countBefore);
  }
  expect(await page.locator("#history-content .history-line").count()).toBeGreaterThanOrEqual(12000);
  expect(await terminalOutgoingInputFrameCount(frame)).toBe(terminalOutgoingInputFrames);

  const snapshotBefore = await page.locator("#history-content").evaluate((content) => ({
    children: content.childElementCount,
    text: content.textContent
  }));
  const wheelCommandCheckpoint = tmuxCommandCheckpoint();
  await page.locator("#history-overlay").dispatchEvent("wheel", {
    deltaY: -720,
    deltaX: 0,
    bubbles: true,
    cancelable: true
  });
  await page.waitForTimeout(150);
  expectNoHistoryTmuxMutations(wheelCommandCheckpoint, "History wheel scrolling");
  expect(await terminalOutgoingInputFrameCount(frame)).toBe(terminalOutgoingInputFrames);

  const touchCommandCheckpoint = tmuxCommandCheckpoint();
  await page.locator("#history-overlay").dispatchEvent("touchstart", {
    bubbles: true,
    cancelable: true
  });
  await page.locator("#history-overlay").dispatchEvent("touchmove", {
    bubbles: true,
    cancelable: true
  });
  await page.locator("#history-overlay").dispatchEvent("touchend", {
    bubbles: true,
    cancelable: true
  });
  await page.waitForTimeout(150);
  expectNoHistoryTmuxMutations(touchCommandCheckpoint, "History touch scrolling");
  expect(await terminalOutgoingInputFrameCount(frame)).toBe(terminalOutgoingInputFrames);
  expect(forbiddenRequests).toHaveLength(0);
  expect(run("tmux", ["display-message", "-p", "-t", sessionName, "#{pane_in_mode}"]).trim()).toBe(originalPaneMode);
  expect(tmuxWindowSize(sessionName)).toEqual(originalWindowSize);
  expect(tmuxClientState(sshLikeClient.name)).toEqual(originalSSHClientState);

  const selectionCommandCheckpoint = tmuxCommandCheckpoint();
  await page.locator("#history-content").evaluate((content) => {
    const first = content.querySelector(".history-line");
    const last = content.querySelector(".history-line:last-child");
    if (!first || !last) throw new Error("history lines unavailable");
    const range = document.createRange();
    range.setStart(first, 0);
    range.setEnd(last, last.childNodes.length);
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
  });
  await expect(page.locator("#terminal-pane")).toHaveAttribute("data-ui-state", "COPY");
  await overlay.evaluate((historyOverlay) => {
    historyOverlay.scrollTop = 0;
  });
  await page.waitForTimeout(150);
  expect(historyPages).toHaveLength(3);
  expect(await page.locator("#history-content").evaluate((content) => content.childElementCount)).toBe(snapshotBefore.children);
  expectNoHistoryTmuxMutations(selectionCommandCheckpoint, "History native selection");
  expect(await terminalOutgoingInputFrameCount(frame)).toBe(terminalOutgoingInputFrames);

  await page.keyboard.press("Control+c");
  await page.waitForTimeout(100);
  expect(historyKeyRequests).toBe(0);
  expect(historyPasteRequests).toBe(0);
  expect(await terminalOutgoingInputFrameCount(frame)).toBe(terminalOutgoingInputFrames);

  const liveMarker = `history-live-${Date.now()}`;
  run("tmux", ["send-keys", "-t", sessionName, `echo ${liveMarker}`, "C-m"]);
  await expect.poll(() => terminalBufferText(frame)).toContain(liveMarker);
  await expect(page.getByRole("button", { name: "New output" })).toBeVisible();
  const snapshotAfter = await page.locator("#history-content").evaluate((content) => ({
    children: content.childElementCount,
    text: content.textContent
  }));
  expect(snapshotAfter).toEqual(snapshotBefore);

  await page.getByRole("button", { name: "New output" }).click();
  await expect(overlay).toBeHidden();
  await expect(terminalFrame).not.toHaveAttribute("inert", "");
  await expect(page.locator("#terminal-pane")).toHaveAttribute("data-ui-state", "LIVE");
  await expect.poll(() => frame.evaluate(() => document.activeElement && document.activeElement.className)).toContain("xterm-helper-textarea");

  await runObservedMutation(page, (request) => {
    return request.method() === "POST" && new URL(request.url()).pathname.endsWith("/history-snapshots");
  }, async () => {
    await page.getByRole("button", { name: "Menu" }).click();
    await page.getByRole("button", { name: "History" }).click();
  });
  await page.keyboard.press("Escape");
  await expect(overlay).toBeHidden();
  expect(historyKeyRequests).toBe(0);

  await runObservedMutation(page, (request) => {
    return request.method() === "POST" && new URL(request.url()).pathname.endsWith("/history-snapshots");
  }, async () => {
    await page.getByRole("button", { name: "Menu" }).click();
    await page.getByRole("button", { name: "History" }).click();
  });
  const printed = await runObservedMutation(page, (request) => {
    return request.method() === "POST" && new URL(request.url()).pathname.endsWith("/keys") && request.postDataJSON().text === "x";
  }, () => page.keyboard.press("x"));
  expect(printed.request.postDataJSON()).toEqual({ text: "x", paneRef: managedSession.activePaneRef });
  expect(printed.response.status()).toBe(200);
  await expect(overlay).toBeHidden();
  run("tmux", ["send-keys", "-t", sessionName, "C-c"]);

  const fixedCreate = await runObservedMutation(page, (request) => {
    return request.method() === "POST" &&
      new URL(request.url()).pathname.endsWith("/history-snapshots") &&
      request.postDataJSON().mode === "fixed";
  }, async () => {
    await page.getByRole("button", { name: "Menu" }).click();
    await page.getByRole("button", { name: "History" }).click();
    await expect(overlay).toBeVisible();
    await page.getByRole("button", { name: "Fixed grid" }).click();
  });
  expect(fixedCreate.response.status()).toBe(201);
  await expect(page.locator("#history-content")).toHaveClass(/fixed/);
  const createCountBeforeOrientation = historyCreates.length;
  const orientationCommandCheckpoint = tmuxCommandCheckpoint();
  await page.setViewportSize({ width: 844, height: 390 });
  await page.waitForTimeout(250);
  expect(historyCreates).toHaveLength(createCountBeforeOrientation);
  expect(tmuxWindowSize(sessionName)).toEqual(originalWindowSize);
  expect(forbiddenRequests).toHaveLength(0);
  expectNoHistoryTmuxMutations(orientationCommandCheckpoint, "History orientation change");
  expect(tmuxClientState(sshLikeClient.name)).toEqual(originalSSHClientState);

  await page.getByLabel("Return to Live").click();
  detachControlClient(sshLikeClient);
  page.off("request", observeRequest);
});

test("defers an in-flight older History page while a cross-boundary native selection is active", async ({ page }) => {
  await login(page);
  await waitForTerminalFrame(page);

  const pageRoutePattern = /\/api\/v1\/history-snapshots\/[^/]+\/pages\?before=/;
  let releasePage;
  let markPageRequested;
  let markPageDelivered;
  const pageGate = new Promise((resolve) => {
    releasePage = resolve;
  });
  const pageRequested = new Promise((resolve) => {
    markPageRequested = resolve;
  });
  const pageDelivered = new Promise((resolve) => {
    markPageDelivered = resolve;
  });
  let delayed = false;
  await page.route(pageRoutePattern, async (route) => {
    if (delayed) {
      await route.continue();
      return;
    }
    delayed = true;
    const response = await route.fetch();
    markPageRequested();
    await pageGate;
    await route.fulfill({ response });
    markPageDelivered();
  });

  const createResponse = await runObservedMutation(page, (request) => {
    return request.method() === "POST" && new URL(request.url()).pathname.endsWith("/history-snapshots");
  }, async () => {
    await page.getByRole("button", { name: "Menu" }).click();
    await page.getByRole("button", { name: "History" }).click();
  });
  expect(createResponse.response.status()).toBe(201);

  const overlay = page.getByRole("region", { name: "Terminal history" });
  const content = page.locator("#history-content");
  await expect(overlay).toBeVisible();
  const initialLines = await content.locator(".history-line").count();
  expect(initialLines).toBe(3000);

  const commandCheckpoint = tmuxCommandCheckpoint();
  await overlay.evaluate((historyOverlay) => {
    historyOverlay.scrollTop = 0;
  });
  await pageRequested;

  const selectionState = await content.evaluate((historyContent) => {
    const headerText = document.querySelector("#history-paste").firstChild;
    const last = historyContent.lastElementChild;
    if (!headerText || !last) throw new Error("History selection endpoints unavailable");
    const range = document.createRange();
    range.setStart(headerText, 0);
    range.setEnd(last, last.childNodes.length);
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
    return {
      commonAncestorId: range.commonAncestorContainer.id,
      text: selection.toString()
    };
  });
  expect(selectionState.commonAncestorId).toBe("history-overlay");
  await expect(page.locator("#terminal-pane")).toHaveAttribute("data-ui-state", "COPY");
  const selectedSnapshot = await content.evaluate((historyContent) => ({
    children: historyContent.childElementCount,
    text: historyContent.textContent
  }));

  releasePage();
  await pageDelivered;
  await page.waitForTimeout(200);
  expect(await content.evaluate((historyContent) => ({
    children: historyContent.childElementCount,
    text: historyContent.textContent
  }))).toEqual(selectedSnapshot);
  expect(await page.evaluate(() => window.getSelection().toString())).toBe(selectionState.text);
  expectNoHistoryTmuxMutations(commandCheckpoint, "in-flight History paging with native selection");

  await page.unroute(pageRoutePattern);
  const retryResponse = page.waitForResponse((response) => {
    return response.request().method() === "GET" && new URL(response.url()).pathname.endsWith("/pages");
  });
  await page.evaluate(() => window.getSelection().removeAllRanges());
  expect((await retryResponse).status()).toBe(200);
  await expect.poll(() => content.locator(".history-line").count()).toBeGreaterThan(initialLines);
  await page.getByLabel("Return to Live").click();
});

test("shows History loading within one frame before capture completes", async ({ page }) => {
  await login(page);
  await waitForTerminalFrame(page);
  let releaseCapture;
  let markCaptureRequested;
  const captureGate = new Promise((resolve) => {
    releaseCapture = resolve;
  });
  const captureRequested = new Promise((resolve) => {
    markCaptureRequested = resolve;
  });
  const createPattern = /\/api\/v1\/panes\/[^/]+\/history-snapshots$/;
  await page.route(createPattern, async (route) => {
    markCaptureRequested();
    await captureGate;
    await route.continue();
  });
  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: "History" }).click();
  await captureRequested;
  const loadingFrame = await page.evaluate(() => new Promise((resolve) => {
    requestAnimationFrame(() => {
      const overlay = document.querySelector("#history-overlay");
      const pane = document.querySelector("#terminal-pane");
      resolve({
        visible: overlay && !overlay.hidden,
        state: pane && pane.dataset.uiState,
        message: document.querySelector("#history-content")?.textContent || ""
      });
    });
  }));
  expect(loadingFrame).toEqual({
    visible: true,
    state: "HISTORY_LOADING",
    message: "Loading terminal history..."
  });
  const completed = await runObservedMutation(
    page,
    (request) => createPattern.test(new URL(request.url()).pathname),
    async () => releaseCapture()
  );
  expect(completed.response.status()).toBe(201);
  await expect(page.locator("#terminal-pane")).toHaveAttribute("data-ui-state", "HISTORY");
  await page.unroute(createPattern);
  await page.getByLabel("Return to Live").click();
});

test("@isolated-mobile covers mobile and tablet viewport profiles, focus, reload, and two isolated viewers", async ({ page }) => {
  test.setTimeout(120_000);
  await assertServerFixtureHealthy("isolated-mobile-start");
  await login(page);
  let frame = await waitForTerminalFrame(page);
  const originalSize = tmuxWindowSize(sessionName);
  const commandCheckpoint = tmuxCommandCheckpoint();
  for (const profile of historyViewportProfiles) {
    await test.step(profile.name, async () => {
      await page.setViewportSize({ width: profile.width, height: profile.height });
      const state = await mobileLayoutState(page);
      expectParentDocumentToFitViewport(state);
      expect(state.topbar.right).toBeLessThanOrEqual(state.viewport.right + 1);
      await assertServerFixtureHealthy("isolated-mobile-profile-history");
      await waitForBrowserNetworkReady(page);
      const create = await runObservedMutation(page, (request) => {
        return request.method() === "POST" && new URL(request.url()).pathname.endsWith("/history-snapshots");
      }, async () => {
        await page.getByRole("button", { name: "Menu" }).click();
        await page.getByRole("button", { name: "History" }).click();
      }, "mobile-profile-history");
      expect(create.response.status()).toBe(201);
      await expect(page.getByRole("region", { name: "Terminal history" })).toBeVisible();
      const released = await runObservedMutation(
        page,
        (request) => request.method() === "DELETE" && new URL(request.url()).pathname.includes("/history-snapshots/"),
        () => page.getByLabel("Return to Live").click()
      );
      expect(released.response.status()).toBe(204);
      await expect.poll(() => frame.evaluate(() => document.activeElement && document.activeElement.className)).toContain("xterm-helper-textarea");
    });
  }
  expect(tmuxWindowSize(sessionName)).toEqual(originalSize);
  expectNoHistoryTmuxMutations(commandCheckpoint, "automated device viewport matrix");

  await page.evaluate(() => {
    sessionStorage.setItem("control-agents.resizeViewerId", `viewer-${crypto.randomUUID()}`);
  });
  await assertServerFixtureHealthy("isolated-mobile-primary-reload");
  await reloadIdempotentWithRetry(page);
  frame = await waitForTerminalFrame(page);

  const secondPage = await page.context().newPage();
  await assertServerFixtureHealthy("isolated-mobile-secondary-navigation");
  await navigateIdempotentWithRetry(secondPage, `${baseURL}/`, 3, "secondary-navigation");
  await waitForSecondaryPageReady(secondPage);
  await waitForTerminalFrame(secondPage);
  const viewerOne = await page.evaluate(() => sessionStorage.getItem("control-agents.resizeViewerId"));
  const viewerTwo = await secondPage.evaluate(() => sessionStorage.getItem("control-agents.resizeViewerId"));
  expect(viewerOne).toMatch(/^viewer-/);
  expect(viewerTwo).toMatch(/^viewer-/);
  expect(viewerTwo).not.toBe(viewerOne);

  for (const candidate of [page, secondPage]) {
    await assertServerFixtureHealthy("isolated-mobile-two-viewer-history");
    await waitForBrowserNetworkReady(candidate);
    const create = await runObservedMutation(candidate, (request) => {
      return request.method() === "POST" && new URL(request.url()).pathname.endsWith("/history-snapshots");
    }, async () => {
      await candidate.getByRole("button", { name: "Menu" }).click();
      await candidate.getByRole("button", { name: "History" }).click();
    }, "mobile-two-viewer-history");
    expect(create.response.status()).toBe(201);
  }
  const firstSnapshot = await page.locator("#history-content").evaluate((content) => ({ children: content.childElementCount, text: content.textContent }));
  const secondSnapshot = await secondPage.locator("#history-content").evaluate((content) => ({ children: content.childElementCount, text: content.textContent }));
  await page.evaluate(() => {
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "hidden" });
    document.dispatchEvent(new Event("visibilitychange"));
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  expect(await page.locator("#history-content").evaluate((content) => ({ children: content.childElementCount, text: content.textContent }))).toEqual(firstSnapshot);
  expect(await secondPage.locator("#history-content").evaluate((content) => ({ children: content.childElementCount, text: content.textContent }))).toEqual(secondSnapshot);
  await secondPage.getByLabel("Return to Live").click();
  await secondPage.close();

  await assertServerFixtureHealthy("isolated-mobile-final-reload");
  await reloadIdempotentWithRetry(page);
  frame = await waitForTerminalFrame(page);
  await expect(page.getByRole("region", { name: "Terminal history" })).toBeHidden();
  await expect.poll(() => frame.evaluate(() => document.activeElement && document.activeElement.className)).toContain("xterm-helper-textarea");
  expect(tmuxWindowSize(sessionName)).toEqual(originalSize);
});

test("@isolated-network-failure rejects one failed History create without retry", async ({ page }) => {
  await assertServerFixtureHealthy("isolated-network-failure-start");
  await login(page);
  await waitForTerminalFrame(page);
  await waitForBrowserNetworkReady(page);

  const createPattern = /\/api\/v1\/panes\/[^/]+\/history-snapshots$/;
  let failedCreates = 0;
  await page.route(createPattern, async (route) => {
    failedCreates += 1;
    await route.abort("connectionreset");
  });
  const failedCreate = runObservedMutation(
    page,
    (request) => createPattern.test(new URL(request.url()).pathname),
    async () => {
      await page.getByRole("button", { name: "Menu" }).click();
      await page.getByRole("button", { name: "History" }).click();
    },
    "isolated-history-create-failure"
  );
  const failedCreateAssertion = expect(failedCreate).rejects.toThrow(
    /\[site:isolated-history-create-failure\] mutation failed before a response: connection-reset/
  );
  await failedCreateAssertion;
  await expect(page.locator("#history-content")).toHaveText("Failed to load terminal history.");
  await page.waitForTimeout(250);
  expect(failedCreates).toBe(1);
  await page.unroute(createPattern);
  await page.getByLabel("Return to Live").click();
});

test("@benchmark emits bounded content-free History browser measurements", async ({ page, browserName }) => {
  test.skip(browserName !== "chromium", "timing baselines are emitted once by the Chromium engine job");
  fs.mkdirSync(path.join(repoRoot, ".cache", "benchmarks"), { recursive: true });
  fs.writeFileSync(benchmarkLog, "", { mode: 0o600 });
  await login(page);
  let frame = await waitForTerminalFrame(page);
  const managedSession = await publicSession(page, sessionName);
  const createResult = await runObservedMutation(page, (request) => {
    return request.method() === "POST" && new URL(request.url()).pathname.endsWith("/history-snapshots");
  }, async () => {
    await page.getByRole("button", { name: "Menu" }).click();
    await page.evaluate(() => {
      window.__historyBenchmarkPaint = new Promise((resolve) => {
        const started = performance.now();
        let frames = 0;
        const check = () => {
          frames += 1;
          const overlay = document.querySelector("#history-overlay");
          if (overlay && !overlay.hidden) {
            resolve({ duration: performance.now() - started, frames });
            return;
          }
          requestAnimationFrame(check);
        };
        requestAnimationFrame(check);
        document.querySelector("#history-toggle").click();
      });
    });
  });
  const { response: createResponse } = createResult;
  expect(createResponse.status()).toBe(201);
  const firstPaint = await page.evaluate(() => window.__historyBenchmarkPaint);
  expect(firstPaint.frames).toBeLessThanOrEqual(1);
  const responseBytes = (await createResponse.body()).byteLength;
  const captureDuration = latestContentFreeDuration(benchmarkLog, "capture_pane_duration_ns");
  await expect.poll(() => historyParseDurations(appLogs).length).toBeGreaterThan(0);
  const parseDurations = historyParseDurations(appLogs);

  const overlay = page.getByRole("region", { name: "Terminal history" });
  const initialLines = await page.locator("#history-content .history-line").count();
  const prependSetup = await page.locator("#history-content").evaluate(async (content) => {
    const overlayElement = document.querySelector("#history-overlay");
    overlayElement.scrollTop = 700;
    await new Promise((resolve) => requestAnimationFrame(resolve));
    const overlayTop = overlayElement.getBoundingClientRect().top;
    const anchor = Array.from(content.children).find((line) => line.getBoundingClientRect().bottom >= overlayTop);
    if (!anchor) throw new Error("benchmark anchor unavailable");
    anchor.dataset.benchmarkAnchor = "true";
    const started = performance.now();
    window.__historyBenchmarkPrepend = new Promise((resolve) => {
      const observer = new MutationObserver(() => {
        observer.disconnect();
        resolve(performance.now() - started);
      });
      observer.observe(content, { childList: true });
    });
    return anchor.getBoundingClientRect().top;
  });
  const olderPage = page.waitForResponse((response) => response.request().method() === "GET" && new URL(response.url()).pathname.endsWith("/pages"));
  await overlay.evaluate((element) => {
    element.scrollTop = 699;
  });
  expect((await olderPage).status()).toBe(200);
  await expect.poll(() => page.locator("#history-content .history-line").count()).toBeGreaterThan(initialLines);
  const prependDuration = await page.evaluate(() => window.__historyBenchmarkPrepend);
  const anchorAfter = await page.locator("[data-benchmark-anchor=true]").evaluate((anchor) => anchor.getBoundingClientRect().top);
  const anchorDrift = Math.abs(anchorAfter - prependSetup);
  expect(anchorDrift).toBeLessThanOrEqual(1);

  const scrollMetrics = await overlay.evaluate(async (element) => {
    const longTasks = [];
    let observer = null;
    if (typeof PerformanceObserver === "function" && PerformanceObserver.supportedEntryTypes?.includes("longtask")) {
      observer = new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) {
          longTasks.push({ startTime: entry.startTime, duration: entry.duration });
        }
      });
      observer.observe({ type: "longtask", buffered: false });
    }
    const scrollStarted = performance.now();
    const frames = [];
    for (let index = 0; index < 60; index += 1) {
      await new Promise((resolve) => requestAnimationFrame((timestamp) => {
        frames.push(timestamp);
        element.scrollTop = Math.max(0, element.scrollHeight - element.clientHeight - index * 20);
        resolve();
      }));
    }
    const scrollEnded = performance.now();
    if (observer) {
      await new Promise((resolve) => setTimeout(resolve, 0));
      for (const entry of observer.takeRecords()) {
        longTasks.push({ startTime: entry.startTime, duration: entry.duration });
      }
      observer.disconnect();
    }
    const scopedLongTasks = longTasks
      .filter((entry) => entry.startTime >= scrollStarted && entry.startTime <= scrollEnded)
      .map((entry) => entry.duration);
    const elapsed = Math.max(1, frames[frames.length - 1] - frames[0]);
    return {
      fps: Math.round(((frames.length - 1) * 1000 * 100) / elapsed) / 100,
      longTaskSupported: Boolean(observer),
      longTaskCount: scopedLongTasks.length,
      maxLongTask: scopedLongTasks.length ? Math.max(...scopedLongTasks) : 0
    };
  });
  const domNodeCount = await page.locator("#history-content").evaluate((content) => content.querySelectorAll("*").length);
  const jsHeap = await page.evaluate(() => {
    const value = performance.memory && performance.memory.usedJSHeapSize;
    return Number.isFinite(value) ? value : null;
  });

  await page.getByLabel("Return to Live").click();
  await expect(overlay).toBeHidden();
  const reconnectCreate = await runObservedMutation(page, (request) => {
    return request.method() === "POST" && new URL(request.url()).pathname.endsWith("/history-snapshots");
  }, async () => {
    await page.getByRole("button", { name: "Menu" }).click();
    await page.getByRole("button", { name: "History" }).click();
  });
  expect(reconnectCreate.response.status()).toBe(201);
  const immutableBeforeReconnect = await page.locator("#history-content").evaluate((content) => ({ children: content.childElementCount, text: content.textContent }));
  let releaseReconnect;
  let markReconnectRequested;
  const reconnectGate = new Promise((resolve) => {
    releaseReconnect = resolve;
  });
  const reconnectRequested = new Promise((resolve) => {
    markReconnectRequested = resolve;
  });
  const tokenRoute = /\/terminal\/[^/]+\/token(?:\?|$)/;
  await page.route(tokenRoute, async (route) => {
    markReconnectRequested();
    await reconnectGate;
    await route.continue();
  });
  await frame.evaluate(() => {
    const socket = window.__controlAgentsTerminalSocket;
    if (!socket || socket.readyState !== WebSocket.OPEN) throw new Error("terminal socket unavailable");
    socket.close(4001, "benchmark transport loss");
  });
  await reconnectRequested;
  await expect(page.locator(`iframe[title="${sessionName}"]`)).toHaveAttribute("data-transport-state", "CONNECTING");
  releaseReconnect();
  await expect.poll(() => page.locator(`iframe[title="${sessionName}"]`).getAttribute("data-transport-state")).toBe("CONNECTED");
  frame = await waitForTerminalFrame(page);
  await page.unroute(tokenRoute);
  expect(await page.locator("#history-content").evaluate((content) => ({ children: content.childElementCount, text: content.textContent }))).toEqual(immutableBeforeReconnect);

  const metric = (value, unit) => ({ supported: true, value, unit });
  const unsupported = (reason) => ({ supported: false, reason });
  const report = {
    schemaVersion: 1,
    runtime: "chromium-engine",
    dataset: "real-tmux-50000-lines",
    measurements: {
      capturePaneDuration: metric(captureDuration, "ns"),
      ansiParseDuration: metric(parseDurations[parseDurations.length - 1], "ms"),
      snapshotRAM: unsupported("measured by the server benchmark"),
      responseBytes: metric(responseBytes, "bytes"),
      firstHistoryPaint: metric(Math.round(firstPaint.duration * 1000), "us"),
      pagePrependDuration: metric(Math.round(prependDuration * 1000), "us"),
      scrollFPS: metric(Math.round(scrollMetrics.fps * 100), "fps_x100"),
      longTasks: scrollMetrics.longTaskSupported ? metric(scrollMetrics.longTaskCount, "count") : unsupported("Long Tasks API unavailable"),
      maxLongTask: scrollMetrics.longTaskSupported ? metric(Math.round(scrollMetrics.maxLongTask * 1000), "us") : unsupported("Long Tasks API unavailable"),
      domNodeCount: metric(domNodeCount, "nodes"),
      jsHeap: jsHeap === null ? unsupported("precise JS heap unavailable") : metric(jsHeap, "bytes"),
      anchorDrift: metric(Math.round(anchorDrift * 1000), "px_x1000"),
      liveInputToPaint: unsupported("ttyd/xterm does not expose a deterministic browser paint completion signal"),
      reconnectToRedraw: unsupported("ttyd exposes reconnect state but no deterministic terminal redraw completion signal"),
      slowConsumer: unsupported("ttyd does not expose a bounded queue or slow-consumer disconnect signal")
    }
  };
  const encoded = `${JSON.stringify(report, null, 2)}\n`;
  expect(encoded.length).toBeLessThan(32 * 1024);
  for (const forbidden of [sessionName, managedSession.id, managedSession.activePaneRef, "playwright-line-", "control_agents_session", "CONTROL_AGENTS_PASSWORD", "secret", "pt_", "hs_", "viewer-"]) {
    expect(encoded).not.toContain(forbidden);
  }
  fs.writeFileSync(path.join(repoRoot, ".cache", "benchmarks", "browser-report.json"), encoded, { mode: 0o600 });
  await page.getByLabel("Return to Live").click();
});

test("opens History from desktop gestures and rejects first input during a real WebSocket reconnect", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await login(page);
  const managedSession = await publicSession(page, sessionName);
  const terminalFrame = page.locator(`iframe[title="${sessionName}"]`);
  let frame = await waitForTerminalFrame(page);
  const overlay = page.getByRole("region", { name: "Terminal history" });
  const historyCreates = [];
  const keyRequests = [];
  const observeRequests = (request) => {
    const pathname = new URL(request.url()).pathname;
    if (request.method() === "POST" && pathname.endsWith("/history-snapshots")) historyCreates.push(request);
    if (request.method() === "POST" && pathname.endsWith("/keys")) keyRequests.push(request);
  };
  page.on("request", observeRequests);

  const wheelCommandCheckpoint = tmuxCommandCheckpoint();
  const wheelCreate = await runObservedMutation(page, (request) => {
    return request.method() === "POST" && new URL(request.url()).pathname.endsWith("/history-snapshots");
  }, () => frame.evaluate(() => {
    window.dispatchEvent(new WheelEvent("wheel", { deltaX: 0, deltaY: -480, bubbles: true, cancelable: true }));
  }), "gesture-wheel-history");
  expect(wheelCreate.response.status()).toBe(201);
  await expect(overlay).toBeVisible();
  expect(await page.locator("#heartbeat").evaluate((element) => getComputedStyle(element).animationName)).toBe("none");
  expect(await overlay.evaluate((element) => {
    const style = getComputedStyle(element);
    const touchMove = new Event("touchmove", { bubbles: true, cancelable: true });
    element.dispatchEvent(touchMove);
    return {
      overflowY: style.overflowY,
      touchAction: style.touchAction,
      userSelect: style.userSelect,
      touchMovePrevented: touchMove.defaultPrevented
    };
  })).toEqual({
    overflowY: "auto",
    touchAction: "pan-y pinch-zoom",
    userSelect: "text",
    touchMovePrevented: false
  });
  await expect(page.locator("#history-new-output")).toHaveAttribute("aria-live", "polite");
  await page.keyboard.press("Tab");
  await expect(page.locator("#history-paste")).toBeFocused();
  await expect.poll(() => overlay.evaluate((element) => element.scrollHeight - element.clientHeight - element.scrollTop)).toBeGreaterThan(300);
  expectNoHistoryTmuxMutations(wheelCommandCheckpoint, "Live upward wheel and native History scrolling");
  await page.getByLabel("Return to Live").click();

  for (const shiftKey of [false, true]) {
    const pageUpCommandCheckpoint = tmuxCommandCheckpoint();
    const pageUpCreate = await runObservedMutation(page, (request) => {
      return request.method() === "POST" && new URL(request.url()).pathname.endsWith("/history-snapshots");
    }, () => frame.evaluate((shift) => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "PageUp", shiftKey: shift, bubbles: true, cancelable: true }));
    }, shiftKey), shiftKey ? "gesture-shift-pageup-history" : "gesture-pageup-history");
    expect(pageUpCreate.response.status()).toBe(201);
    await expect(overlay).toBeVisible();
    expectNoHistoryTmuxMutations(pageUpCommandCheckpoint, shiftKey ? "Shift+PageUp History entry" : "PageUp History entry");
    if (!shiftKey) {
      const enterMutation = await runObservedMutation(page, (request) => {
        return request.method() === "POST" && new URL(request.url()).pathname.endsWith("/keys") && request.postDataJSON().key === "enter";
      }, () => page.keyboard.press("Enter"));
      expect(enterMutation.request.postDataJSON()).toEqual({ key: "enter", paneRef: managedSession.activePaneRef });
      expect(enterMutation.response.status()).toBe(200);
      await page.waitForTimeout(150);
      expect(keyRequests.filter((request) => request.postDataJSON().key === "enter")).toHaveLength(1);
      await expect.poll(() => frame.evaluate(() => document.activeElement && document.activeElement.className)).toContain("xterm-helper-textarea");
    } else {
      await page.getByLabel("Return to Live").click();
    }
  }

  const reconnectHistory = await runObservedMutation(page, (request) => {
    return request.method() === "POST" && new URL(request.url()).pathname.endsWith("/history-snapshots");
  }, async () => {
    await page.getByRole("button", { name: "Menu" }).click();
    await page.getByRole("button", { name: "History" }).click();
  }, "gesture-reconnect-history");
  expect(reconnectHistory.response.status()).toBe(201);
  let releaseReconnectToken;
  let markReconnectTokenRequested;
  const reconnectTokenRequested = new Promise((resolve) => {
    markReconnectTokenRequested = resolve;
  });
  const reconnectTokenGate = new Promise((resolve) => {
    releaseReconnectToken = resolve;
  });
  let delayedReconnectToken = false;
  const tokenRoutePattern = /\/terminal\/[^/]+\/token(?:\?|$)/;
  await page.route(tokenRoutePattern, async (route) => {
    if (delayedReconnectToken) {
      await route.continue();
      return;
    }
    delayedReconnectToken = true;
    markReconnectTokenRequested();
    await reconnectTokenGate;
    await route.continue();
  });
  const terminalURLBeforeReconnect = frame.url();
  await terminalFrame.evaluate((element) => {
    window.__terminalFrameLoadCount = 0;
    element.addEventListener("load", () => {
      window.__terminalFrameLoadCount += 1;
    });
  });
  await frame.evaluate(() => {
    const socket = window.__controlAgentsTerminalSocket;
    if (!socket || socket.readyState !== WebSocket.OPEN) throw new Error("observed ttyd WebSocket is not open");
    socket.close(4001, "Playwright reconnect test");
  });
  await reconnectTokenRequested;
  await expect(terminalFrame).toHaveAttribute("data-transport-state", "CONNECTING");
  const keysBeforeReconnectInput = keyRequests.length;
  const reconnectInputCommandCheckpoint = tmuxCommandCheckpoint();
  await page.keyboard.press("Enter");
  await expect(overlay).toBeHidden();
  await expect(page.locator("#terminal-pane")).toHaveAttribute("data-ui-state", "LIVE_RECONNECTING");
  await page.waitForTimeout(150);
  expect(keyRequests).toHaveLength(keysBeforeReconnectInput);
  expectNoHistoryTmuxMutations(reconnectInputCommandCheckpoint, "rejected reconnect input");
  expect(frame.url()).toBe(terminalURLBeforeReconnect);
  expect(await page.evaluate(() => window.__terminalFrameLoadCount)).toBe(0);
  releaseReconnectToken();
  await expect.poll(() => terminalFrame.getAttribute("data-transport-state")).toBe("CONNECTED");
  await page.unroute(tokenRoutePattern);
  await expect.poll(() => frame.evaluate(() => window.__controlAgentsTerminalSocket.readyState)).toBe(1);

  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByLabel("Scroll gesture").selectOption("application");
  await expect.poll(() => page.evaluate(() => sessionStorage.getItem("control-agents.scrollGestureMode"))).toBe("application");
  await page.getByRole("button", { name: "Menu" }).click();
  const applicationCreateCount = historyCreates.length;
  await frame.evaluate(() => {
    window.dispatchEvent(new WheelEvent("wheel", { deltaX: 0, deltaY: -360, bubbles: true, cancelable: true }));
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "PageUp", shiftKey: true, bubbles: true, cancelable: true }));
  });
  await page.waitForTimeout(250);
  expect(historyCreates).toHaveLength(applicationCreateCount);
  await expect(overlay).toBeHidden();

  page.off("request", observeRequests);
});

test("stages clipboard and fallback Paste text before exactly one confirmed send", async ({ page }) => {
  await login(page);
  const managedSession = await publicSession(page, sessionName);
  await waitForTerminalFrame(page);
  await page.evaluate(() => {
    const state = { mode: "deny", text: "", activations: [] };
    window.__pasteClipboardState = state;
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        readText() {
          state.activations.push(Boolean(navigator.userActivation && navigator.userActivation.isActive));
          if (state.mode === "deny") return Promise.reject(new DOMException("denied", "NotAllowedError"));
          return Promise.resolve(state.text);
        }
      }
    });
  });
  const pasteRequests = [];
  const observePaste = (request) => {
    if (request.method() === "POST" && new URL(request.url()).pathname.endsWith("/paste")) pasteRequests.push(request);
  };
  page.on("request", observePaste);

  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: "Paste", exact: true }).click();
  const overlay = page.getByRole("region", { name: "Terminal history" });
  const fallback = page.getByLabel("Paste text from the system menu");
  await expect(overlay).toBeVisible();
  await expect(page.locator("#history-paste-status")).toContainText("denied or unavailable");
  await expect(fallback).toBeFocused();
  expect(await page.evaluate(() => window.__pasteClipboardState.activations)).toEqual([true]);
  expect(pasteRequests).toHaveLength(0);

  await fallback.evaluate((element) => {
    const data = new DataTransfer();
    data.setData("text/plain", "fallback-one");
    element.dispatchEvent(new ClipboardEvent("paste", { bubbles: true, cancelable: true, clipboardData: data }));
  });
  const confirmDialog = page.getByRole("dialog", { name: "Review paste" });
  await expect(confirmDialog).toBeVisible();
  await expect(page.locator("#paste-confirm-summary")).toHaveText("12 UTF-8 bytes, 1 logical line.");
  expect(pasteRequests).toHaveLength(0);
  await confirmDialog.getByRole("button", { name: "Cancel" }).click();
  await expect(confirmDialog).toBeHidden();
  await expect(overlay).toBeVisible();
  await expect(fallback).toHaveValue("");
  expect(pasteRequests).toHaveLength(0);

  await page.evaluate(() => {
    window.__pasteClipboardState.mode = "allow";
    window.__pasteClipboardState.text = "écho";
  });
  await page.getByRole("button", { name: "Paste", exact: true }).click();
  await expect(confirmDialog).toBeVisible();
  await expect(page.locator("#paste-confirm-summary")).toHaveText("5 UTF-8 bytes, 1 logical line.");
  await expect(page.locator("#paste-confirm-warning")).toBeHidden();
  const singlePasteMutation = await runObservedMutation(page, (request) => {
    return request.method() === "POST" && new URL(request.url()).pathname.endsWith("/paste");
  }, () => confirmDialog.getByRole("button", { name: "Paste to terminal" }).click());
  expect(singlePasteMutation.response.status()).toBe(200);
  expect(singlePasteMutation.request.postDataJSON()).toMatchObject({ text: "écho", paneRef: managedSession.activePaneRef });
  expect(singlePasteMutation.request.postDataJSON().token).toMatch(/^pt_[A-Za-z0-9_-]+$/);
  await expect(overlay).toBeHidden();
  run("tmux", ["send-keys", "-t", sessionName, "C-c"]);

  const multilineText = "first line\nsecond\tline";
  await page.evaluate((text) => {
    window.__pasteClipboardState.text = text;
  }, multilineText);
  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: "History" }).click();
  await expect(overlay).toBeVisible();
  await page.getByRole("button", { name: "Paste", exact: true }).click();
  await expect(confirmDialog).toBeVisible();
  await expect(page.locator("#paste-confirm-summary")).toContainText("2 logical lines");
  await expect(page.locator("#paste-confirm-warning")).toBeVisible();
  const multilinePasteMutation = await runObservedMutation(page, (request) => {
    return request.method() === "POST" && new URL(request.url()).pathname.endsWith("/paste");
  }, () => confirmDialog.getByRole("button", { name: "Confirm paste" }).click());
  expect(multilinePasteMutation.response.status()).toBe(200);
  expect(multilinePasteMutation.request.postDataJSON().text).toBe(multilineText);
  await expect(overlay).toBeHidden();
  run("tmux", ["send-keys", "-t", sessionName, "C-c"]);

  const controlText = "tabbed\tvalue";
  await page.evaluate((text) => {
    window.__pasteClipboardState.text = text;
  }, controlText);
  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: "History" }).click();
  await expect(overlay).toBeVisible();
  await page.getByRole("button", { name: "Paste", exact: true }).click();
  await expect(confirmDialog).toBeVisible();
  await expect(page.locator("#paste-confirm-summary")).toContainText("1 logical line");
  await expect(page.locator("#paste-confirm-warning")).toBeVisible();
  const controlPasteMutation = await runObservedMutation(page, (request) => {
    return request.method() === "POST" && new URL(request.url()).pathname.endsWith("/paste");
  }, () => confirmDialog.getByRole("button", { name: "Confirm paste" }).click());
  expect(controlPasteMutation.response.status()).toBe(200);
  expect(controlPasteMutation.request.postDataJSON().text).toBe(controlText);
  await expect(overlay).toBeHidden();
  run("tmux", ["send-keys", "-t", sessionName, "C-c"]);

  const trailingNewlineText = "review-only\n";
  await page.evaluate((text) => {
    window.__pasteClipboardState.text = text;
  }, trailingNewlineText);
  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: "History" }).click();
  await expect(overlay).toBeVisible();
  await page.getByRole("button", { name: "Paste", exact: true }).click();
  await expect(confirmDialog).toBeVisible();
  await expect(page.locator("#paste-confirm-warning")).toContainText("trailing newline that may execute a command");
  const trailingPasteMutation = await runObservedMutation(page, (request) => {
    return request.method() === "POST" && new URL(request.url()).pathname.endsWith("/paste");
  }, () => confirmDialog.getByRole("button", { name: "Confirm paste" }).click());
  expect(trailingPasteMutation.response.status()).toBe(200);
  expect(trailingPasteMutation.request.postDataJSON().text).toBe(trailingNewlineText);
  await expect(overlay).toBeHidden();
  run("tmux", ["send-keys", "-t", sessionName, "C-c"]);

  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: "History" }).click();
  await expect(overlay).toBeVisible();
  await page.evaluate(() => {
    window.__pasteClipboardState.text = "network-failure";
  });
  let failedPasteCount = 0;
  await page.route(/\/api\/sessions\/[^/]+\/paste$/, async (route) => {
    failedPasteCount += 1;
    await route.fulfill({ status: 502, body: "failed" });
  });
  await page.getByRole("button", { name: "Paste", exact: true }).click();
  await expect(confirmDialog).toBeVisible();
  await confirmDialog.getByRole("button", { name: "Paste to terminal" }).click();
  await expect(overlay).toBeVisible();
  await expect(page.locator("#history-paste-status")).toContainText("Nothing was retried");
  await page.waitForTimeout(350);
  expect(failedPasteCount).toBe(1);
  await page.unroute(/\/api\/sessions\/[^/]+\/paste$/);
  await expect(fallback).toHaveValue("");

  await page.evaluate(() => {
    window.__pasteClipboardState.text = "disconnect-before-execution";
  });
  const beforeDisconnectCheckpoint = tmuxCommandCheckpoint();
  let beforeDisconnectRequests = 0;
  await page.route(/\/api\/sessions\/[^/]+\/paste$/, async (route) => {
    beforeDisconnectRequests += 1;
    await route.abort("failed");
  });
  await page.getByRole("button", { name: "Paste", exact: true }).click();
  await expect(confirmDialog).toBeVisible();
  await confirmDialog.getByRole("button", { name: "Paste to terminal" }).click();
  await expect(page.locator("#history-paste-status")).toContainText("Nothing was retried");
  await page.waitForTimeout(350);
  expect(beforeDisconnectRequests).toBe(1);
  expect(tmuxCommandEntries().slice(beforeDisconnectCheckpoint).filter((entry) => /(?:^| )paste-buffer(?: |$)/.test(entry))).toHaveLength(0);
  await page.unroute(/\/api\/sessions\/[^/]+\/paste$/);

  await page.evaluate(() => {
    window.__pasteClipboardState.text = "disconnect-after-execution";
  });
  const afterDisconnectCheckpoint = tmuxCommandCheckpoint();
  let afterDisconnectRequests = 0;
  await page.route(/\/api\/sessions\/[^/]+\/paste$/, async (route) => {
    afterDisconnectRequests += 1;
    await route.fetch();
    await route.abort("failed");
  });
  await page.getByRole("button", { name: "Paste", exact: true }).click();
  await expect(confirmDialog).toBeVisible();
  await confirmDialog.getByRole("button", { name: "Paste to terminal" }).click();
  await expect(page.locator("#history-paste-status")).toContainText("Nothing was retried");
  await page.waitForTimeout(350);
  expect(afterDisconnectRequests).toBe(1);
  expect(tmuxCommandEntries().slice(afterDisconnectCheckpoint).filter((entry) => /(?:^| )paste-buffer(?: |$)/.test(entry))).toHaveLength(1);
  await page.unroute(/\/api\/sessions\/[^/]+\/paste$/);
  run("tmux", ["send-keys", "-t", sessionName, "C-c"]);

  page.off("request", observePaste);
  await page.getByLabel("Return to Live").click();
});

test("tracks keyboard viewport changes without tmux resize or pane SIGWINCH", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await installMockVisualViewport(page, { height: 844, width: 390, offsetLeft: 0, offsetTop: 0 });
  await login(page);
  await waitForTerminalFrame(page);

  const beforeMode = tmuxWindowSizeOption(sessionName);
  const beforeSize = tmuxWindowSize(sessionName);
  const winchMarker = path.join(stateDir, "keyboard-winch-marker");
  fs.rmSync(winchMarker, { force: true });
  run("tmux", ["send-keys", "-t", sessionName, `trap 'touch ${winchMarker}' WINCH`, "C-m"]);
  await page.waitForTimeout(150);
  const commandCheckpoint = tmuxCommandCheckpoint();
  const heartbeatMutation = await runObservedMutation(page, (request) => {
    if (request.method() !== "POST" || !new URL(request.url()).pathname.endsWith("/resize/viewer")) return false;
    return request.postDataJSON().transient === true;
  }, () => setMockVisualViewport(page, { height: 520, offsetTop: 120 }, "resize"));
  await expect.poll(() => cssAppViewportHeight(page)).toBe(520);
  await expect.poll(() => cssAppViewportOffsetTop(page)).toBe(120);
  expect(heartbeatMutation.request.postDataJSON().transient).toBe(true);
  expect(heartbeatMutation.response.status()).toBe(200);
  await page.waitForTimeout(700);
  const keyboardLayout = await mobileLayoutState(page);
  expect(keyboardLayout.topbar.top).toBeGreaterThanOrEqual(keyboardLayout.viewport.top - 1);
  expect(keyboardLayout.terminalPane.top).toBeGreaterThanOrEqual(keyboardLayout.viewport.top - 1);
  expect(keyboardLayout.terminalPane.bottom).toBeLessThanOrEqual(keyboardLayout.viewport.bottom + 1);
  expect(keyboardLayout.terminalFrame.bottom).toBeLessThanOrEqual(keyboardLayout.viewport.bottom + 1);
  expect(tmuxWindowSizeOption(sessionName)).toBe(beforeMode);
  expect(tmuxWindowSize(sessionName)).toEqual(beforeSize);
  expect(fs.existsSync(winchMarker)).toBe(false);
  expectNoHistoryTmuxMutations(commandCheckpoint, "software keyboard viewport change");

  await setMockVisualViewport(page, { height: null, offsetTop: 0 }, "resize");
  await page.setViewportSize({ width: 390, height: 560 });
  const resizedHeight = await appViewportHeight(page);
  await expect.poll(() => cssAppViewportHeight(page)).toBe(resizedHeight);
  expect(tmuxWindowSizeOption(sessionName)).toBe(beforeMode);
  expect(tmuxWindowSize(sessionName)).toEqual(beforeSize);
  expect(fs.existsSync(winchMarker)).toBe(false);
  run("tmux", ["send-keys", "-t", sessionName, "trap - WINCH", "C-m"]);
  fs.rmSync(winchMarker, { force: true });
});

test("keeps the mobile header inside the viewport while tabs and terminal overflow", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await installMockVisualViewport(page, { width: 320, offsetLeft: 42 });
  await login(page);
  const frame = await waitForTerminalFrame(page);
  const beforeMode = tmuxWindowSizeOption(sessionName);

  await expect.poll(() => cssAppViewportWidth(page)).toBe(320);
  await expect.poll(() => cssAppViewportOffsetLeft(page)).toBe(42);
  const initialVisualViewport = await mobileLayoutState(page);
  expectParentDocumentToFitViewport(initialVisualViewport);
  expect(initialVisualViewport.topbar.left).toBeCloseTo(42, 0);
  expect(initialVisualViewport.topbar.width).toBeCloseTo(320, 0);
  expect(initialVisualViewport.menu.right).toBeLessThanOrEqual(initialVisualViewport.viewport.right + 1);

  await setMockVisualViewport(page, { width: null, offsetLeft: 0 }, "resize");

  await page.route("**/api/sessions", async (route) => {
    if (route.request().method() === "GET") {
      await route.abort();
      return;
    }
    await route.continue();
  });
  await page.locator("#tabs").evaluate((tabs) => {
    for (let index = 1; index <= 10; index += 1) {
      const button = document.createElement("button");
      button.type = "button";
      button.dataset.sessionId = `overflow-${index}`;
      const label = document.createElement("span");
      label.className = "tab-label";
      label.textContent = `overflow-session-${index}`;
      button.appendChild(label);
      tabs.appendChild(button);
    }
  });

  for (const viewport of [
    { name: "iPhone portrait", width: 390, height: 844 },
    { name: "iPhone landscape", width: 844, height: 390 }
  ]) {
    await test.step(viewport.name, async () => {
      await page.setViewportSize({ width: viewport.width, height: viewport.height });
      await page.locator("#tabs").evaluate((tabs) => {
        tabs.scrollLeft = 0;
      });
      await page.locator("#terminal-strip").evaluate((strip) => {
        strip.scrollLeft = 0;
      });

      expect(await frame.evaluate(() => {
        const input = document.querySelector(".xterm-helper-textarea");
        if (!input) return false;
        input.focus();
        return document.activeElement === input;
      })).toBe(true);
      await expect.poll(() => page.evaluate(() => document.activeElement && document.activeElement.className)).toContain("terminal-frame");

      const initial = await mobileLayoutState(page);
      expectParentDocumentToFitViewport(initial);
      expect(initial.topbar.scrollWidth).toBeLessThanOrEqual(initial.topbar.clientWidth);
      expect(initial.menu.left).toBeGreaterThanOrEqual(initial.viewport.left - 1);
      expect(initial.menu.right).toBeLessThanOrEqual(initial.viewport.right + 1);
      expect(initial.topbar.right - initial.menu.right).toBeGreaterThanOrEqual(0);
      expect(initial.topbar.right - initial.menu.right).toBeLessThanOrEqual(40);
      expect(initial.menu.left).toBeGreaterThan(initial.topbar.left + initial.topbar.width / 2);
      expect(initial.tabs.scrollWidth).toBeGreaterThan(initial.tabs.clientWidth);
      expect(initial.terminalStrip.scrollWidth).toBeGreaterThan(initial.terminalStrip.clientWidth);

      await page.locator("#tabs").evaluate((tabs) => {
        tabs.scrollLeft = tabs.scrollWidth;
      });
      const tabsPanned = await mobileLayoutState(page);
      expect(tabsPanned.tabs.scrollLeft).toBeGreaterThan(0);
      expect(tabsPanned.menu.left).toBeCloseTo(initial.menu.left, 0);
      expect(tabsPanned.menu.right).toBeCloseTo(initial.menu.right, 0);
      expectParentDocumentToFitViewport(tabsPanned);

      await page.locator("#terminal-strip").evaluate((strip) => {
        strip.scrollLeft = strip.scrollWidth;
      });
      const terminalPanned = await mobileLayoutState(page);
      expect(terminalPanned.terminalStrip.scrollLeft).toBeGreaterThan(0);
      expect(terminalPanned.menu.left).toBeCloseTo(initial.menu.left, 0);
      expect(terminalPanned.menu.right).toBeCloseTo(initial.menu.right, 0);
      expectParentDocumentToFitViewport(terminalPanned);
    });
  }

  await page.setViewportSize({ width: 390, height: 844 });
  await setMockVisualViewport(page, { width: 320, offsetLeft: 34 }, "resize");
  await expect.poll(() => cssAppViewportWidth(page)).toBe(320);
  await expect.poll(() => cssAppViewportOffsetLeft(page)).toBe(34);
  const resizedVisualViewport = await mobileLayoutState(page);
  expectParentDocumentToFitViewport(resizedVisualViewport);
  expect(resizedVisualViewport.topbar.left).toBeCloseTo(34, 0);
  expect(resizedVisualViewport.topbar.width).toBeCloseTo(320, 0);
  expect(resizedVisualViewport.menu.right).toBeLessThanOrEqual(resizedVisualViewport.viewport.right + 1);

  await setMockVisualViewport(page, { offsetLeft: 56 }, "scroll");
  await expect.poll(() => cssAppViewportOffsetLeft(page)).toBe(56);
  const scrolledVisualViewport = await mobileLayoutState(page);
  expectParentDocumentToFitViewport(scrolledVisualViewport);
  expect(scrolledVisualViewport.topbar.left).toBeCloseTo(56, 0);
  expect(scrolledVisualViewport.topbar.width).toBeCloseTo(320, 0);
  expect(scrolledVisualViewport.menu.right).toBeLessThanOrEqual(scrolledVisualViewport.viewport.right + 1);
  expect(scrolledVisualViewport.menu.left - resizedVisualViewport.menu.left).toBeCloseTo(22, 0);
  expect(tmuxWindowSizeOption(sessionName)).toBe(beforeMode);
});

test("keeps fixed tmux dimensions when a smaller SSH client attaches", async ({ page }) => {
  await login(page);
  await waitForTerminalFrame(page);

  expect(run("tmux", ["show-options", "-w", "-v", "-t", `${sessionName}:`, "window-size"]).trim()).toBe("manual");
  const fixedSize = tmuxWindowSize(sessionName);

  const controlClient = await attachControlClient(sessionName, "80,20");
  try {
    await expect.poll(() => tmuxWindowSize(sessionName)).toEqual(fixedSize);
  } finally {
    detachControlClient(controlClient);
  }
});

test("@isolated-lifecycle classifies idempotent read failures without response details", async ({ page }) => {
  await login(page);
  await expectContentFreeReadDiagnostic(page, "status", {
    status: 503,
    contentType: "text/plain",
    body: "private response details"
  }, "status-503");
  await expectContentFreeReadDiagnostic(page, "empty", {
    status: 200,
    contentType: "application/json",
    body: ""
  }, "empty-json");
  await expectContentFreeReadDiagnostic(page, "invalid", {
    status: 200,
    contentType: "application/json",
    body: "private invalid response details"
  }, "invalid-json");

  expect(contentFreeReadFailureReason(new Error("Execution context was destroyed"), false)).toBe("execution-context-lost");
  expect(contentFreeReadFailureReason(new Error("Navigation interrupted the operation"), false)).toBe("navigation");
  expect(contentFreeReadFailureReason(new Error("request timed out"), false)).toBe("timeout");
  expect(contentFreeReadFailureReason(new Error("ignored private failure details"), true)).toBe("server-exit");
});

test("@isolated-lifecycle creates, selects, validates, terminates, and reconciles managed sessions", async ({ page }) => {
  test.setTimeout(120_000);
  let lifecycleSite = "login";
  let countCreateRequests;
  let countDeleteRequests;
  try {
  await runBoundedBrowserPhase("lifecycle-login", () => login(page), 25_000);
  const terminateMenuItem = page.locator("#terminate-session-toggle");
  await expect(terminateMenuItem).toBeEnabled({ timeout: 5_000 });

  lifecycleSite = "mutation-observer-ownership";
  const listenerCounts = mutationListenerCounts(page);
  const actionFailure = new Error("owned mutation action failed");
  await expect(runLifecycleCreate(page, async () => {
    throw actionFailure;
  }, "lifecycle-mutation-observer-ownership")).rejects.toBe(actionFailure);
  expect(mutationListenerCounts(page)).toEqual(listenerCounts);

  const closingPage = await runBoundedBrowserPhase(
    "lifecycle-mutation-observer-page-create",
    () => page.context().newPage(),
    5_000
  );
  await expect(runObservedMutation(
    closingPage,
    (request) => request.method() === "POST" && new URL(request.url()).pathname === "/api/sessions",
    () => closingPage.close(),
    "lifecycle-mutation-observer-page"
  )).rejects.toThrow("[site:lifecycle-mutation-observer-page] page closed while waiting for a mutation response");

  let lifecycleCreateRequests = 0;
  countCreateRequests = (request) => {
    if (request.method() === "POST" && new URL(request.url()).pathname === "/api/sessions") {
      lifecycleCreateRequests += 1;
    }
  };
  page.on("request", countCreateRequests);

  lifecycleSite = "invalid-create-dialog";
  const createDialog = page.getByRole("dialog", { name: "New session" });
  const sessionNameInput = createDialog.getByLabel("Session name");
  await runBoundedBrowserPhase("lifecycle-invalid-create-dialog", async () => {
    await openCreateSessionDialog(page);
    await expect(sessionNameInput).toBeFocused({ timeout: 5_000 });
    await expect(createDialog).toContainText("maximum 64 characters", { timeout: 5_000 });
    await sessionNameInput.fill("invalid name", { timeout: 5_000 });
    await expect(page.locator("#create-session-status")).toContainText(
      "Start with a letter or digit",
      { timeout: 5_000 }
    );
    await createDialog.getByRole("button", { name: "Create" }).click({ timeout: 5_000 });
    await page.waitForTimeout(100);
    expect(lifecycleCreateRequests).toBe(0);
    await page.keyboard.press("Escape");
    await expect(createDialog).toBeHidden({ timeout: 5_000 });
    await expectVisibleFocus(page, "#actions-toggle");
  }, 12_000);

  lifecycleSite = "terminal-connect-focus";
  await runBoundedBrowserPhase("lifecycle-terminal-connect-dialog", async () => {
    await openCreateSessionDialog(page);
    await sessionNameInput.fill(createdSessionName, { timeout: 5_000 });
  }, 8_000);
  const terminalConnectGate = createHeldRouteGate();
  let delayedTerminalConnect = false;
  let terminalTokenRouteInstalled = false;
  let terminalConnectError;
  const terminalTokenRoute = /\/terminal\/[^/]+\/token(?:\?|$)/;
  const terminalTokenHandler = async (route) => {
    if (delayedTerminalConnect) {
      await runBoundedBrowserPhase(
        "lifecycle-terminal-connect-pass-route",
        () => route.continue(),
        5_000
      );
      return;
    }
    delayedTerminalConnect = true;
    terminalConnectGate.start();
    let handlerError = null;
    try {
      await runBoundedBrowserPhase(
        "lifecycle-terminal-connect-held-route",
        () => terminalConnectGate.released,
        5_000
      );
    } catch (error) {
      handlerError = error;
    } finally {
      try {
        await runBoundedBrowserPhase(
          "lifecycle-terminal-connect-route-continue",
          () => route.continue(),
          5_000
        );
      } catch (error) {
        if (!handlerError) handlerError = error;
      }
      terminalConnectGate.finish(handlerError);
    }
  };
  try {
    await runBoundedBrowserPhase(
      "lifecycle-terminal-connect-route-install",
      () => page.route(terminalTokenRoute, terminalTokenHandler),
      5_000
    );
    terminalTokenRouteInstalled = true;
    const activeTerminalFrame = sessionFrame(page, sessionName);
    await runBoundedBrowserPhase("lifecycle-terminal-connect-frame-action", () => {
      return activeTerminalFrame.evaluate((frame) => {
        const source = new URL(frame.src);
        source.searchParams.set("focus-regression", String(Date.now()));
        frame.src = source.href;
      });
    }, 5_000);
    await runBoundedBrowserPhase(
      "lifecycle-terminal-connect-request",
      () => terminalConnectGate.started,
      5_000
    );
    await runBoundedBrowserPhase("lifecycle-terminal-connect-focus-held", async () => {
      await expect(activeTerminalFrame).toHaveAttribute("data-transport-state", "CONNECTING", { timeout: 5_000 });
      await expect(sessionNameInput).toBeFocused({ timeout: 5_000 });
    }, 6_000);
    terminalConnectGate.release();
    await waitForHeldRouteGate(terminalConnectGate, "lifecycle-terminal-connect-route-result", 6_000);
    await runBoundedBrowserPhase("lifecycle-terminal-connect-reconciliation", async () => {
      await expect.poll(
        () => activeTerminalFrame.getAttribute("data-transport-state"),
        { timeout: 15_000 }
      ).toBe("CONNECTED");
      await expect(sessionNameInput).toBeFocused({ timeout: 5_000 });
    }, 16_000);
  } catch (error) {
    terminalConnectError = error;
    throw error;
  } finally {
    terminalConnectGate.release();
    let routeCleanupError = null;
    if (terminalConnectGate.hasStarted()) {
      try {
        await waitForHeldRouteGate(terminalConnectGate, "lifecycle-terminal-connect-route-cleanup", 6_000);
      } catch (error) {
        routeCleanupError = error;
      }
    }
    if (terminalTokenRouteInstalled && !page.isClosed()) {
      try {
        await runBoundedBrowserPhase(
          "lifecycle-terminal-connect-route-remove",
          () => page.unroute(terminalTokenRoute, terminalTokenHandler),
          5_000
        );
      } catch (error) {
        if (!routeCleanupError) routeCleanupError = error;
      }
    }
    if (!terminalConnectError && routeCleanupError) throw routeCleanupError;
  }

  // Absorb any idempotent browser network-change recovery before the exactly-once create.
  await waitForBrowserNetworkReady(page, "lifecycle-session-create-network-ready");

  lifecycleSite = "session-create-request";
  const supersededCreateFirstRead = createHeldRouteGate();
  const supersededCreateLatestRead = createHeldRouteGate();
  let supersededCreateRouteInstalled = false;
  let supersededCreateRouteError;
  let supersededCreateArmed = false;
  let supersededCreateMutationCount = 0;
  let supersededCreateReadCount = 0;
  const supersededCreateRoute = "**/api/sessions";
  const supersededCreateHandler = async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
    if (request.method() === "POST" && pathname === "/api/sessions") {
      supersededCreateMutationCount += 1;
      await fetchRouteOnceAndFulfill(
        route,
        "lifecycle-created-refresh-create-route",
        (response) => {
          supersededCreateArmed = response.status() === 201;
        }
      );
      return;
    }
    if (!supersededCreateArmed || request.method() !== "GET" || pathname !== "/api/sessions") {
      await continueRouteBounded(route, "lifecycle-created-refresh-pass-route");
      return;
    }
    supersededCreateReadCount += 1;
    const readNumber = supersededCreateReadCount;
    const gate = readNumber === 1
      ? supersededCreateFirstRead
      : (readNumber === 2 ? supersededCreateLatestRead : null);
    if (!gate) {
      await continueRouteBounded(route, "lifecycle-created-refresh-extra-route");
      return;
    }
    gate.start();
    let handlerError = null;
    try {
      await runBoundedBrowserPhase(
        `lifecycle-created-refresh-${readNumber}-held-route`,
        () => gate.released,
        8_000
      );
    } catch (error) {
      handlerError = error;
    } finally {
      try {
        await runBoundedBrowserPhase(
          `lifecycle-created-refresh-${readNumber}-route-continue`,
          () => route.continue(),
          5_000
        );
      } catch (error) {
        if (!handlerError) handlerError = error;
      }
      gate.finish(handlerError);
    }
  };
  try {
    await runBoundedBrowserPhase(
      "lifecycle-created-refresh-route-install",
      () => page.route(supersededCreateRoute, supersededCreateHandler),
      5_000
    );
    supersededCreateRouteInstalled = true;
    const created = await runLifecycleCreate(
      page,
      () => sessionNameInput.press("Enter"),
      "lifecycle-session-create-request"
    );
    expect(created.request.postDataJSON()).toEqual({ name: createdSessionName });
    expect(created.response.status()).toBe(201);
    await runBoundedBrowserPhase(
      "lifecycle-created-refresh-first-request",
      () => supersededCreateFirstRead.started,
      5_000
    );
    await runBoundedBrowserPhase(
      "lifecycle-created-refresh-superseding-request",
      () => supersededCreateLatestRead.started,
      5_000
    );
    supersededCreateFirstRead.release();
    await waitForHeldRouteGate(
      supersededCreateFirstRead,
      "lifecycle-created-refresh-first-route-result",
      6_000
    );
    await runBoundedBrowserPhase("lifecycle-created-refresh-stale-ownership", async () => {
      await expect(createDialog).toBeVisible({ timeout: 5_000 });
      await expect(sessionNameInput).toBeDisabled({ timeout: 5_000 });
      await expect(createDialog.getByRole("button", { name: "Creating..." })).toBeDisabled({ timeout: 5_000 });
      expect(supersededCreateMutationCount).toBe(1);
      expect(lifecycleCreateRequests).toBe(1);
    }, 6_000);
    supersededCreateLatestRead.release();
    await waitForHeldRouteGate(
      supersededCreateLatestRead,
      "lifecycle-created-refresh-latest-route-result",
      6_000
    );
    lifecycleSite = "session-create-reconciliation";
    await runBoundedBrowserPhase("lifecycle-session-create-reconciliation", async () => {
      await expect(createDialog).toBeHidden({ timeout: 10_000 });
      await expect(sessionTab(page, createdSessionName)).toHaveClass(/active/, { timeout: 10_000 });
      await expect(sessionFrame(page, createdSessionName)).toBeVisible({ timeout: 10_000 });
      await waitForSessionTerminalFrame(page, createdSessionName);
      expect(run("tmux", ["display-message", "-p", "-t", createdSessionName, "#{pane_current_path}"]).trim()).toBe(process.env.HOME);
      expect(supersededCreateMutationCount).toBe(1);
      expect(lifecycleCreateRequests).toBe(1);
    }, 20_000);
  } catch (error) {
    supersededCreateRouteError = error;
    throw error;
  } finally {
    supersededCreateFirstRead.release();
    supersededCreateLatestRead.release();
    let routeCleanupError = null;
    for (const [gate, site] of [
      [supersededCreateFirstRead, "lifecycle-created-refresh-first-route-cleanup"],
      [supersededCreateLatestRead, "lifecycle-created-refresh-latest-route-cleanup"]
    ]) {
      if (!gate.hasStarted()) continue;
      try {
        await waitForHeldRouteGate(gate, site, 6_000);
      } catch (error) {
        if (!routeCleanupError) routeCleanupError = error;
      }
    }
    if (supersededCreateRouteInstalled && !page.isClosed()) {
      try {
        await runBoundedBrowserPhase(
          "lifecycle-created-refresh-route-remove",
          () => page.unroute(supersededCreateRoute, supersededCreateHandler),
          5_000
        );
      } catch (error) {
        if (!routeCleanupError) routeCleanupError = error;
      }
    }
    if (!supersededCreateRouteError && routeCleanupError) throw routeCleanupError;
  }

  const delayedSessionListGate = createHeldRouteGate();
  let delayNextSessionList = false;
  let delayedSessionListRouteInstalled = false;
  let delayedSessionListError;
  const delayedSessionListRoute = "**/api/sessions";
  const delayedSessionListHandler = async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
    if (request.method() === "POST" && pathname === "/api/sessions") {
      delayNextSessionList = true;
    } else if (delayNextSessionList && request.method() === "GET" && pathname === "/api/sessions") {
      delayNextSessionList = false;
      delayedSessionListGate.start();
      let handlerError = null;
      try {
        await runBoundedBrowserPhase(
          "lifecycle-delayed-session-list-held-route",
          () => delayedSessionListGate.released,
          5_000
        );
      } catch (error) {
        handlerError = error;
      } finally {
        try {
          await runBoundedBrowserPhase(
            "lifecycle-delayed-session-list-route-continue",
            () => route.continue(),
            5_000
          );
        } catch (error) {
          if (!handlerError) handlerError = error;
        }
        delayedSessionListGate.finish(handlerError);
      }
      return;
    }
    await runBoundedBrowserPhase(
      "lifecycle-delayed-session-list-pass-route",
      () => route.continue(),
      5_000
    );
  };

  lifecycleSite = "duplicate-create-reconciliation";
  try {
    await runBoundedBrowserPhase(
      "lifecycle-delayed-session-list-route-install",
      () => page.route(delayedSessionListRoute, delayedSessionListHandler),
      5_000
    );
    delayedSessionListRouteInstalled = true;
    await waitForBrowserNetworkReady(page, "lifecycle-duplicate-create-network-ready");
    const duplicateResponse = await runLifecycleCreate(page, async () => {
      await openCreateSessionDialog(page);
      await page.locator("#create-session-name").fill(createdSessionName);
      await page.locator("#create-session-name").press("Enter");
    }, "lifecycle-duplicate-create-reconciliation");
    expect(duplicateResponse.response.status()).toBe(200);
    await runBoundedBrowserPhase(
      "lifecycle-delayed-session-list-request",
      () => delayedSessionListGate.started,
      5_000
    );
    await runBoundedBrowserPhase("lifecycle-duplicate-create-held-reconciliation", async () => {
      await expect(sessionTab(page, createdSessionName)).toHaveCount(1, { timeout: 5_000 });
      await expect(sessionFrame(page, createdSessionName)).toHaveCount(1, { timeout: 5_000 });
      await expect(sessionTab(page, createdSessionName)).toHaveClass(/active/, { timeout: 5_000 });
      await sessionTab(page, sessionName).click({ timeout: 5_000 });
      await expect(sessionTab(page, sessionName)).toHaveClass(/active/, { timeout: 5_000 });
    }, 10_000);
    const delayedSessionListResponse = runBoundedBrowserPhase(
      "lifecycle-delayed-session-list-response",
      () => page.waitForResponse((response) => {
        return response.request().method() === "GET" && new URL(response.url()).pathname === "/api/sessions";
      }, { timeout: 5_000 }),
      6_000
    );
    delayedSessionListResponse.catch(() => {});
    delayedSessionListGate.release();
    await waitForHeldRouteGate(
      delayedSessionListGate,
      "lifecycle-delayed-session-list-route-result",
      6_000
    );
    expect((await delayedSessionListResponse).status()).toBe(200);
    await runBoundedBrowserPhase("lifecycle-delayed-session-list-reconciliation", async () => {
      await expect(sessionTab(page, sessionName)).toHaveClass(/active/, { timeout: 5_000 });
    }, 6_000);
  } catch (error) {
    delayedSessionListError = error;
    throw error;
  } finally {
    delayedSessionListGate.release();
    let routeCleanupError = null;
    if (delayedSessionListGate.hasStarted()) {
      try {
        await waitForHeldRouteGate(
          delayedSessionListGate,
          "lifecycle-delayed-session-list-route-cleanup",
          6_000
        );
      } catch (error) {
        routeCleanupError = error;
      }
    }
    if (delayedSessionListRouteInstalled && !page.isClosed()) {
      try {
        await runBoundedBrowserPhase(
          "lifecycle-delayed-session-list-route-remove",
          () => page.unroute(delayedSessionListRoute, delayedSessionListHandler),
          5_000
        );
      } catch (error) {
        if (!routeCleanupError) routeCleanupError = error;
      }
    }
    if (!delayedSessionListError && routeCleanupError) throw routeCleanupError;
  }

  lifecycleSite = "unmanaged-conflict";
  run("tmux", ["new-session", "-d", "-s", conflictSessionName]);
  try {
    await waitForBrowserNetworkReady(page, "lifecycle-unmanaged-conflict-network-ready");
    const conflictResponse = await runLifecycleCreate(page, async () => {
      await openCreateSessionDialog(page);
      await page.locator("#create-session-name").fill(conflictSessionName);
      await page.locator("#create-session-name").press("Enter");
    }, "lifecycle-unmanaged-conflict");
    expect(conflictResponse.response.status()).toBe(409);
    await runBoundedBrowserPhase("lifecycle-unmanaged-conflict-reconciliation", async () => {
      await expect(createDialog).toBeVisible({ timeout: 5_000 });
      await expect(page.locator("#create-session-status")).toContainText(
        "conflicts with an unmanaged tmux session",
        { timeout: 5_000 }
      );
      await page.locator("#create-session-cancel").click({ timeout: 5_000 });
    }, 10_000);
  } finally {
    spawnSync("tmux", ["kill-session", "-t", conflictSessionName], { stdio: "ignore" });
  }

  lifecycleSite = "session-limit";
  await waitForBrowserNetworkReady(page, "lifecycle-session-limit-create-network-ready");
  let failedCreateRefreshArmed = false;
  let failedCreateMutationCount = 0;
  let failedCreateRefreshCount = 0;
  const failedCreateRefreshRoute = "**/api/sessions";
  const failedCreateRefreshHandler = async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
    if (request.method() === "POST" && pathname === "/api/sessions") {
      failedCreateMutationCount += 1;
      await fetchRouteOnceAndFulfill(
        route,
        "lifecycle-failed-created-refresh-create-route",
        (response) => {
          failedCreateRefreshArmed = response.status() === 201;
        }
      );
      return;
    }
    if (failedCreateRefreshArmed && failedCreateRefreshCount === 0 && request.method() === "GET" && pathname === "/api/sessions") {
      failedCreateRefreshCount += 1;
      await fulfillRouteBounded(route, {
        status: 503,
        contentType: "text/plain",
        body: "private response details"
      }, "lifecycle-failed-created-refresh-read-route");
      return;
    }
    await continueRouteBounded(route, "lifecycle-failed-created-refresh-pass-route");
  };
  await runBoundedBrowserPhase(
    "lifecycle-failed-created-refresh-route-install",
    () => page.route(failedCreateRefreshRoute, failedCreateRefreshHandler),
    5_000
  );
  try {
    const failedRefreshCreate = await createSessionThroughUI(
      page,
      nextSessionName,
      "lifecycle-session-limit-create"
    );
    expect(failedRefreshCreate.response.status()).toBe(201);
    await runBoundedBrowserPhase("lifecycle-failed-created-refresh-reconciliation", async () => {
      await expect(createDialog).toBeVisible({ timeout: 5_000 });
      await expect(page.locator("#create-session-status")).toHaveText(
        "The session was created, but the latest session list could not be confirmed. Check the connection, then cancel or submit again.",
        { timeout: 5_000 }
      );
      await expect(sessionNameInput).toBeEnabled({ timeout: 5_000 });
      await expect(createDialog.getByRole("button", { name: "Cancel" })).toBeEnabled({ timeout: 5_000 });
      await expect(createDialog.getByRole("button", { name: "Create", exact: true })).toBeEnabled({ timeout: 5_000 });
      await expect(sessionTab(page, nextSessionName)).toHaveClass(/active/, { timeout: 5_000 });
      await expect(sessionFrame(page, nextSessionName)).toBeVisible({ timeout: 5_000 });
      expect(failedCreateMutationCount).toBe(1);
      expect(failedCreateRefreshCount).toBe(1);
      expect(lifecycleCreateRequests).toBe(4);
      await page.waitForTimeout(250);
      expect(failedCreateMutationCount).toBe(1);
      expect(lifecycleCreateRequests).toBe(4);
      await createDialog.getByRole("button", { name: "Cancel" }).click({ timeout: 5_000 });
      await expect(createDialog).toBeHidden({ timeout: 5_000 });
      await waitForSessionTerminalFrame(page, nextSessionName);
    }, 16_000);
  } finally {
    await runBoundedBrowserPhase(
      "lifecycle-failed-created-refresh-route-remove",
      () => page.unroute(failedCreateRefreshRoute, failedCreateRefreshHandler),
      5_000
    );
  }

  const limitResponse = await runLifecycleCreate(page, async () => {
    await openCreateSessionDialog(page);
    await page.locator("#create-session-name").fill(limitSessionName);
    await page.locator("#create-session-name").press("Enter");
  }, "lifecycle-session-limit");
  expect(limitResponse.response.status()).toBe(409);
  await runBoundedBrowserPhase("lifecycle-session-limit-reconciliation", async () => {
    await expect(createDialog).toBeVisible({ timeout: 5_000 });
    await expect(page.locator("#create-session-status")).toContainText(
      "session limit has been reached",
      { timeout: 5_000 }
    );
    await page.locator("#create-session-cancel").click({ timeout: 5_000 });
    await expect(sessionTab(page, limitSessionName)).toHaveCount(0, { timeout: 5_000 });
  }, 10_000);

  lifecycleSite = "session-termination";
  await runBoundedBrowserPhase("lifecycle-session-termination-selection", async () => {
    await sessionTab(page, createdSessionName).click({ timeout: 5_000 });
    await expect(sessionTab(page, createdSessionName)).toHaveClass(/active/, { timeout: 5_000 });
  }, 6_000);
  let deleteRequests = 0;
  countDeleteRequests = (request) => {
    if (request.method() === "DELETE" && new URL(request.url()).pathname.startsWith("/api/sessions/")) {
      deleteRequests += 1;
    }
  };
  page.on("request", countDeleteRequests);

  const terminateDialog = page.getByRole("alertdialog", { name: "Terminate session" });
  await runBoundedBrowserPhase("lifecycle-session-termination-cancel", async () => {
    await openTerminateSessionDialog(page);
    await expect(terminateDialog.locator("#terminate-session-name")).toHaveText(createdSessionName, { timeout: 5_000 });
    await expect(terminateDialog).toContainText("all SSH and web clients", { timeout: 5_000 });
    await terminateDialog.getByRole("button", { name: "Cancel" }).click({ timeout: 5_000 });
    await expect(terminateDialog).toBeHidden({ timeout: 5_000 });
    await expectVisibleFocus(page, "#actions-toggle");
  }, 10_000);
  expect(deleteRequests).toBe(0);
  expect(spawnSync("tmux", ["has-session", "-t", createdSessionName]).status).toBe(0);

  await runBoundedBrowserPhase("lifecycle-session-termination-escape", async () => {
    await openTerminateSessionDialog(page);
    await page.keyboard.press("Escape");
    await expect(terminateDialog).toBeHidden({ timeout: 5_000 });
    await expectVisibleFocus(page, "#actions-toggle");
  }, 10_000);
  expect(deleteRequests).toBe(0);

  const createdSession = await publicSession(page, createdSessionName);
  await waitForBrowserNetworkReady(page, "lifecycle-session-termination-created-network-ready");
  const terminatedCreated = await runLifecycleDelete(page, async () => {
    await openTerminateSessionDialog(page);
    await terminateDialog.getByRole("button", { name: "Terminate", exact: true }).click();
  }, "lifecycle-session-termination-created");
  expect(terminatedCreated.request.postDataJSON()).toEqual({
    confirmName: createdSessionName,
    paneRef: createdSession.activePaneRef
  });
  expect(terminatedCreated.response.status()).toBe(204);
  await runBoundedBrowserPhase("lifecycle-session-termination-created-reconciliation", async () => {
    await expectVisibleFocus(page, "#actions-toggle");
    await expect(sessionTab(page, createdSessionName)).toHaveCount(0, { timeout: 10_000 });
    await expect(sessionFrame(page, createdSessionName)).toHaveCount(0, { timeout: 10_000 });
    await expect(sessionTab(page, nextSessionName)).toHaveClass(/active/, { timeout: 10_000 });
    expect(spawnSync("tmux", ["has-session", "-t", createdSessionName]).status).not.toBe(0);
  }, 15_000);

  await waitForBrowserNetworkReady(page, "lifecycle-session-termination-next-network-ready");
  const terminateNextResponse = await runLifecycleDelete(page, async () => {
    await openTerminateSessionDialog(page);
    await terminateDialog.getByRole("button", { name: "Terminate", exact: true }).click();
  }, "lifecycle-session-termination-next");
  expect(terminateNextResponse.response.status()).toBe(204);
  await runBoundedBrowserPhase("lifecycle-session-termination-next-reconciliation", async () => {
    await expect(sessionTab(page, nextSessionName)).toHaveCount(0, { timeout: 10_000 });
    await expect(sessionTab(page, sessionName)).toHaveClass(/active/, { timeout: 10_000 });
  }, 12_000);

  expect(lifecycleCreateRequests).toBe(5);
  expect(deleteRequests).toBe(2);
  } catch (error) {
    if (/^\[site:[a-z0-9-]+\]/.test(String(error && error.message))) throw error;
    const reason = contentFreeNetworkReason(error && error.message);
    const category = reason === "network-failure" ? "assertion" : reason;
    throw new Error(`[site:lifecycle-${safeSite(lifecycleSite)}] isolated lifecycle phase failed: ${category}`);
  } finally {
    if (countCreateRequests) page.off("request", countCreateRequests);
    if (countDeleteRequests) page.off("request", countDeleteRequests);
  }
});

test("@isolated-secondary-lifecycle synchronizes lifecycle ownership across two viewers", async ({ page }) => {
  test.setTimeout(90_000);
  let lifecycleSite = "secondary-viewer-login";
  let otherPage;
  try {
    await login(page);
    await waitForTerminalFrame(page);

    lifecycleSite = "secondary-viewer-navigation";
    otherPage = await page.context().newPage();
    await navigateIdempotentWithRetry(otherPage, `${baseURL}/`);
    await waitForSecondaryPageReady(otherPage);
    await waitForTerminalFrame(otherPage);

    let externalCreateRequests = 0;
    let externalDeleteRequests = 0;
    const countExternalMutations = (request) => {
      const pathname = new URL(request.url()).pathname;
      if (request.method() === "POST" && pathname === "/api/sessions") externalCreateRequests += 1;
      if (request.method() === "DELETE" && pathname.startsWith("/api/sessions/")) externalDeleteRequests += 1;
    };
    otherPage.on("request", countExternalMutations);
    try {
      lifecycleSite = "secondary-viewer-create";
      await waitForBrowserNetworkReady(otherPage);
      const externalCreated = await runObservedMutation(otherPage, (request) => {
        return request.method() === "POST" && new URL(request.url()).pathname === "/api/sessions";
      }, async () => {
        await openCreateSessionDialog(otherPage);
        await otherPage.locator("#create-session-name").fill(externalSessionName);
        await otherPage.locator("#create-session-name").press("Enter");
      }, "lifecycle-secondary-viewer-create");
      expect(externalCreated.request.postDataJSON()).toEqual({ name: externalSessionName });
      expect(externalCreated.response.status()).toBe(201);
      expect(externalCreateRequests).toBe(1);
      expect(externalDeleteRequests).toBe(0);
      await expect(sessionTab(otherPage, externalSessionName)).toHaveClass(/active/);
      await waitForSessionTerminalFrame(otherPage, externalSessionName);

      lifecycleSite = "secondary-viewer-create-reconciliation";
      await expect(sessionTab(page, externalSessionName)).toBeVisible({ timeout: 10_000 });
      await expect(sessionTab(page, sessionName)).toHaveClass(/active/);

      lifecycleSite = "secondary-viewer-selection";
      await sessionTab(page, externalSessionName).click();
      await expect(sessionTab(page, externalSessionName)).toHaveClass(/active/);
      await waitForSessionTerminalFrame(page, externalSessionName);
      await expect(sessionTab(otherPage, externalSessionName)).toHaveClass(/active/);

      lifecycleSite = "secondary-viewer-delete";
      await waitForBrowserNetworkReady(otherPage);
      const externalSession = await publicSession(otherPage, externalSessionName);
      const externalDeleted = await runObservedMutation(otherPage, (request) => {
        return request.method() === "DELETE" && new URL(request.url()).pathname === `/api/sessions/${encodeURIComponent(externalSession.id)}`;
      }, async () => {
        await openTerminateSessionDialog(otherPage);
        const externalTerminateDialog = otherPage.getByRole("alertdialog", { name: "Terminate session" });
        await expect(externalTerminateDialog.locator("#terminate-session-name")).toHaveText(externalSessionName);
        await externalTerminateDialog.getByRole("button", { name: "Terminate", exact: true }).click();
      }, "lifecycle-secondary-viewer-delete");
      expect(externalDeleted.request.postDataJSON()).toEqual({
        confirmName: externalSessionName,
        paneRef: externalSession.activePaneRef
      });
      expect(externalDeleted.response.status()).toBe(204);
      expect(externalCreateRequests).toBe(1);
      expect(externalDeleteRequests).toBe(1);

      lifecycleSite = "secondary-viewer-delete-reconciliation";
      await expect(sessionTab(otherPage, externalSessionName)).toHaveCount(0);
      await expect(sessionFrame(otherPage, externalSessionName)).toHaveCount(0);
      await expect(sessionTab(otherPage, sessionName)).toHaveClass(/active/);
      await expect(sessionTab(page, externalSessionName)).toHaveCount(0, { timeout: 10_000 });
      await expect(sessionFrame(page, externalSessionName)).toHaveCount(0);
      await expect(sessionTab(page, sessionName)).toHaveClass(/active/);
    } finally {
      otherPage.off("request", countExternalMutations);
    }
  } catch (error) {
    if (/^\[site:[a-z0-9-]+\]/.test(String(error && error.message))) throw error;
    const reason = contentFreeNetworkReason(error && error.message);
    const category = reason === "network-failure" ? "assertion" : reason;
    throw new Error(`[site:lifecycle-${safeSite(lifecycleSite)}] isolated secondary lifecycle phase failed: ${category}`);
  } finally {
    if (otherPage && !otherPage.isClosed()) await otherPage.close();
  }
});

async function login(page) {
  await navigateIdempotentWithRetry(page, `${baseURL}/`);
  if (!/\/login/.test(page.url())) {
    await navigateIdempotentWithRetry(page, `${baseURL}/login`);
  }
  await page.locator("#password").fill("secret");
  const loginMutation = await runObservedMutation(page, (request) => {
    return request.method() === "POST" && new URL(request.url()).pathname === "/login";
  }, () => page.getByRole("button", { name: "Sign in" }).click(), "login");
  const loginResponse = loginMutation.response;
  expect(loginResponse.status()).toBeGreaterThanOrEqual(300);
  expect(loginResponse.status()).toBeLessThan(400);
  await expect(page).toHaveURL(`${baseURL}/`);
  await expect(page.getByRole("button", { name: sessionName })).toBeVisible({ timeout: 15_000 });
}

async function resizeState(page) {
  const id = await sessionRef(page, sessionName);
  return page.evaluate(async (sessionRef) => {
    const response = await fetch(`/api/sessions/${encodeURIComponent(sessionRef)}/resize`, {
      credentials: "same-origin"
    });
    if (!response.ok) {
      throw new Error(`resize state failed: ${response.status}`);
    }
    return response.json();
  }, id);
}

async function waitForActiveResizeViewer(page) {
  await expect.poll(async () => {
    const state = await resizeState(page);
    return (state.viewers || []).filter((viewer) => viewer.active && viewer.width > 0 && viewer.height > 0).length;
  }).toBeGreaterThan(0);
  const state = await resizeState(page);
  return state.viewers.find((viewer) => viewer.active && viewer.width > 0 && viewer.height > 0);
}

async function applyResizeMode(page, label) {
  await page.getByLabel(label).check();
  const apply = page.getByRole("button", { name: "Apply" });
  await expect(apply).toBeEnabled();
  const { request, response } = await runObservedMutation(page, (request) => {
    return request.method() === "POST" && new URL(request.url()).pathname.endsWith("/resize");
  }, () => apply.click());
  expect(response.status()).toBe(200);
  return request;
}

async function appViewportHeight(page) {
  return page.evaluate(() => {
    const viewport = window.visualViewport;
    return Math.round(viewport && viewport.height > 0 ? viewport.height : window.innerHeight);
  });
}

async function cssAppViewportHeight(page) {
  return page.evaluate(() => {
    const raw = window.getComputedStyle(document.documentElement).getPropertyValue("--app-viewport-height");
    return Math.round(Number.parseFloat(raw));
  });
}

async function cssAppViewportWidth(page) {
  return page.evaluate(() => {
    const raw = window.getComputedStyle(document.documentElement).getPropertyValue("--app-viewport-width");
    return Math.round(Number.parseFloat(raw));
  });
}

async function cssAppViewportOffsetTop(page) {
  return page.evaluate(() => {
    const raw = window.getComputedStyle(document.documentElement).getPropertyValue("--app-viewport-offset-top");
    return Math.round(Number.parseFloat(raw));
  });
}

async function cssAppViewportOffsetLeft(page) {
  return page.evaluate(() => {
    const raw = window.getComputedStyle(document.documentElement).getPropertyValue("--app-viewport-offset-left");
    return Math.round(Number.parseFloat(raw));
  });
}

async function installMockVisualViewport(page, initial) {
  await page.addInitScript((initialState) => {
    if (window !== window.top) return;
    const state = {
      height: null,
      offsetLeft: 0,
      offsetTop: 0,
      width: null,
      ...initialState
    };
    class MockVisualViewport extends EventTarget {
      get height() {
        return state.height === null ? window.innerHeight : state.height;
      }

      get offsetLeft() {
        return state.offsetLeft;
      }

      get offsetTop() {
        return state.offsetTop;
      }

      get width() {
        return state.width === null ? window.innerWidth : state.width;
      }
    }
    const viewport = new MockVisualViewport();
    Object.defineProperty(window, "visualViewport", {
      configurable: true,
      value: viewport
    });
    window.__setMockVisualViewport = (next, eventType) => {
      Object.assign(state, next);
      viewport.dispatchEvent(new Event(eventType));
    };
  }, initial);
}

async function setMockVisualViewport(page, state, eventType) {
  await page.evaluate(({ next, type }) => {
    window.__setMockVisualViewport(next, type);
  }, { next: state, type: eventType });
}

async function mobileLayoutState(page) {
  return page.evaluate(() => {
    const rect = (element) => {
      const bounds = element.getBoundingClientRect();
      return {
        bottom: bounds.bottom,
        height: bounds.height,
        left: bounds.left,
        right: bounds.right,
        top: bounds.top,
        width: bounds.width
      };
    };
    const viewport = window.visualViewport;
    const viewportLeft = viewport ? viewport.offsetLeft : 0;
    const viewportTop = viewport ? viewport.offsetTop : 0;
    const viewportWidth = viewport ? viewport.width : window.innerWidth;
    const viewportHeight = viewport ? viewport.height : window.innerHeight;
    const documentElement = document.documentElement;
    return {
      viewport: {
        bottom: viewportTop + viewportHeight,
        height: viewportHeight,
        left: viewportLeft,
        right: viewportLeft + viewportWidth,
        top: viewportTop,
        width: viewportWidth
      },
      windowScrollX: window.scrollX,
      document: {
        clientWidth: documentElement.clientWidth,
        scrollWidth: documentElement.scrollWidth,
        scrollLeft: documentElement.scrollLeft
      },
      body: {
        clientWidth: document.body.clientWidth,
        scrollWidth: document.body.scrollWidth,
        scrollLeft: document.body.scrollLeft
      },
      topbar: {
        ...rect(document.querySelector(".topbar")),
        clientWidth: document.querySelector(".topbar").clientWidth,
        scrollWidth: document.querySelector(".topbar").scrollWidth
      },
      menu: rect(document.querySelector("#actions-toggle")),
      terminalFrame: rect(document.querySelector(".terminal-frame:not([hidden])")),
      terminalPane: rect(document.querySelector("#terminal-pane")),
      tabs: {
        clientWidth: document.querySelector("#tabs").clientWidth,
        scrollWidth: document.querySelector("#tabs").scrollWidth,
        scrollLeft: document.querySelector("#tabs").scrollLeft
      },
      terminalStrip: {
        clientWidth: document.querySelector("#terminal-strip").clientWidth,
        scrollWidth: document.querySelector("#terminal-strip").scrollWidth,
        scrollLeft: document.querySelector("#terminal-strip").scrollLeft
      }
    };
  });
}

function expectParentDocumentToFitViewport(state) {
  expect(state.windowScrollX).toBe(0);
  expect(state.document.scrollLeft).toBe(0);
  expect(state.body.scrollLeft).toBe(0);
  expect(state.document.scrollWidth).toBeLessThanOrEqual(state.document.clientWidth);
  expect(state.body.scrollWidth).toBeLessThanOrEqual(state.body.clientWidth);
  expect(state.topbar.left).toBeGreaterThanOrEqual(state.viewport.left - 1);
  expect(state.topbar.right).toBeLessThanOrEqual(state.viewport.right + 1);
}

async function waitForTerminalFrame(page, name = sessionName) {
  const terminalFrame = sessionFrame(page, name);
  await expect(terminalFrame).toBeVisible();
  await expect(terminalFrame).toHaveAttribute("data-transport-state", "CONNECTED", { timeout: 15_000 });
  const src = await terminalFrame.getAttribute("src");
  expect(src).toBeTruthy();
  await expect.poll(() => page.frames().some((frame) => frame.url().includes(src))).toBe(true);
  return page.frames().find((frame) => frame.url().includes(src));
}

async function terminalBufferText(frame) {
  return frame.evaluate(() => {
    const terminal = window.term || window.terminal || window.xterm;
    const buffer = terminal && terminal.buffer && terminal.buffer.active;
    if (!buffer || typeof buffer.getLine !== "function") return "";
    const lines = [];
    for (let index = 0; index < buffer.length; index += 1) {
      const line = buffer.getLine(index);
      if (line && typeof line.translateToString === "function") lines.push(line.translateToString(true));
    }
    return lines.join("\n");
  });
}

async function terminalOutgoingInputFrameCount(frame) {
  const count = await frame.evaluate(() => window.__controlAgentsTerminalOutgoingInputFrames);
  expect(Number.isInteger(count)).toBe(true);
  expect(count).toBeGreaterThanOrEqual(0);
  return count;
}

function sessionTab(page, name) {
  const label = page.locator(".tab-label", { hasText: new RegExp(`^${escapeRegex(name)}$`) });
  return page.locator("#tabs button").filter({ has: label });
}

function sessionFrame(page, name) {
  return page.locator(`#terminal-strip iframe[title="${name}"]`);
}

async function openCreateSessionDialog(page) {
  await page.getByRole("button", { name: "Menu" }).click();
  await page.locator("#new-session-toggle").click();
  await expect(page.getByRole("dialog", { name: "New session" })).toBeVisible();
}

async function createSessionThroughUI(page, name, site) {
  return runLifecycleCreate(page, async () => {
    await openCreateSessionDialog(page);
    await page.locator("#create-session-name").fill(name);
    await page.locator("#create-session-name").press("Enter");
  }, site);
}

async function openTerminateSessionDialog(page) {
  await page.getByRole("button", { name: "Menu" }).click();
  await page.locator("#terminate-session-toggle").click();
  await expect(page.getByRole("alertdialog", { name: "Terminate session" })).toBeVisible();
}

async function runLifecycleCreate(page, operation, site) {
  return runObservedMutation(page, (request) => {
    return request.method() === "POST" && new URL(request.url()).pathname === "/api/sessions";
  }, operation, site);
}

function mutationListenerCounts(page) {
  return {
    close: page.listenerCount("close"),
    crash: page.listenerCount("crash"),
    requestfailed: page.listenerCount("requestfailed"),
    response: page.listenerCount("response")
  };
}

async function runObservedMutation(page, predicate, operation, siteOverride = "") {
  const observer = observeMutation(page, predicate, siteOverride);
  try {
    const [, mutation] = await Promise.all([
      Promise.resolve().then(operation),
      observer.result
    ]);
    return mutation;
  } finally {
    observer.disarm();
  }
}

function observeMutation(page, predicate, siteOverride = "") {
  let settled = false;
  let resolveResult;
  let rejectResult;
  const result = new Promise((resolve, reject) => {
    resolveResult = resolve;
    rejectResult = reject;
  });
  const cleanup = () => {
    clearTimeout(timeout);
    page.off("response", handleResponse);
    page.off("requestfailed", handleFailure);
    page.off("crash", handleCrash);
    page.off("close", handleClose);
  };
  const resolveOwned = (value) => {
    if (settled) return;
    settled = true;
    cleanup();
    resolveResult(value);
  };
  const rejectOwned = (error) => {
    if (settled) return;
    settled = true;
    cleanup();
    rejectResult(error);
  };
  const matches = (request) => {
    try {
      return predicate(request);
    } catch (error) {
      rejectOwned(error);
      return false;
    }
  };
  const handleResponse = (response) => {
    const request = response.request();
    if (!matches(request)) return;
    resolveOwned({ request, response });
  };
  const handleFailure = (request) => {
    if (!matches(request)) return;
    const detail = request.failure();
    const site = siteOverride ? safeSite(siteOverride) : mutationSite(request);
    const reason = contentFreeNetworkReason(detail && detail.errorText);
    rejectOwned(new Error(`[site:${site}] mutation failed before a response: ${reason}`));
  };
  const handleCrash = () => {
    const site = siteOverride ? safeSite(siteOverride) : "mutation-page";
    rejectOwned(new Error(`[site:${site}] page crashed while waiting for a mutation response`));
  };
  const handleClose = () => {
    const site = siteOverride ? safeSite(siteOverride) : "mutation-page";
    rejectOwned(new Error(`[site:${site}] page closed while waiting for a mutation response`));
  };
  const timeout = setTimeout(() => {
    const site = siteOverride ? safeSite(siteOverride) : "mutation";
    rejectOwned(new Error(`[site:${site}] mutation response timed out`));
  }, 10_000);
  page.on("response", handleResponse);
  page.on("requestfailed", handleFailure);
  page.on("crash", handleCrash);
  page.on("close", handleClose);
  return {
    result,
    disarm: () => {
      if (settled) return;
      settled = true;
      cleanup();
    }
  };
}

async function runLifecycleDelete(page, operation, site) {
  return runObservedMutation(page, (request) => {
    return request.method() === "DELETE" && new URL(request.url()).pathname.startsWith("/api/sessions/");
  }, operation, site);
}

async function waitForSessionTerminalFrame(page, name) {
  return waitForTerminalFrame(page, name);
}

async function publicSession(page, name) {
  const payload = await idempotentJSONGet(page, "/api/sessions");
  const sessions = payload.sessions || [];
  const session = sessions.find((candidate) => candidate.name === name);
  if (!session) throw new Error(`managed session ${name} is not visible`);
  return session;
}

async function navigateIdempotentWithRetry(page, url, attempts = 3, site = "primary-navigation") {
  return runIdempotentNavigationWithRetry(
    page,
    () => page.goto(url, { waitUntil: "domcontentloaded", timeout: 5_000 }),
    site,
    attempts
  );
}

async function reloadIdempotentWithRetry(page, attempts = 3) {
  return runIdempotentNavigationWithRetry(
    page,
    () => page.reload({ waitUntil: "domcontentloaded", timeout: 5_000 }),
    "reload",
    attempts
  );
}

async function runIdempotentNavigationWithRetry(page, navigate, site, attempts) {
  let lastError = null;
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    try {
      const response = await navigate();
      if (!response || response.ok()) return response;
      if (!isRetryableReadStatus(response.status()) || attempt + 1 === attempts) {
        const error = new Error(`[site:${safeSite(site)}] idempotent navigation failed with status ${response.status()}`);
        error.nonRetryableRead = true;
        error.contentFreeRead = true;
        throw error;
      }
    } catch (error) {
      lastError = error;
      if (error.nonRetryableRead || page.isClosed() || attempt + 1 === attempts) {
        if (error.contentFreeRead) throw error;
        throw new Error(`[site:${safeSite(site)}] idempotent navigation failed: ${contentFreeNetworkReason(error && error.message)}`);
      }
    }
    await page.waitForTimeout(100 * (attempt + 1));
  }
  throw new Error(`[site:${safeSite(site)}] idempotent navigation failed: ${contentFreeNetworkReason(lastError && lastError.message)}`);
}

async function waitForSecondaryPageReady(page) {
  let lastError = null;
  for (let attempt = 0; attempt < 3; attempt += 1) {
    try {
      await expect(page.locator("#version-badge")).toBeVisible({ timeout: 5_000 });
      await expect(sessionTab(page, sessionName)).toBeVisible({ timeout: 5_000 });
      return;
    } catch (error) {
      lastError = error;
      if (page.isClosed() || attempt === 2) {
        throw new Error("[site:secondary-readiness] secondary page did not become ready");
      }
      await navigateIdempotentWithRetry(page, `${baseURL}/`, 3, "secondary-navigation");
    }
  }
  throw new Error(`[site:secondary-readiness] secondary page did not become ready: ${contentFreeNetworkReason(lastError && lastError.message)}`);
}

async function idempotentJSONGet(page, pathname, attempts = 3, siteOverride = "") {
  const site = siteOverride ? safeSite(siteOverride) : readSite(pathname);
  let lastError = null;
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    try {
      const result = await page.evaluate(async (path) => {
        const controller = new AbortController();
        const timeout = setTimeout(() => controller.abort(), 5_000);
        try {
          let response;
          try {
            response = await fetch(path, { credentials: "same-origin", cache: "no-store", signal: controller.signal });
          } catch (error) {
            if (controller.signal.aborted) return { outcome: "timeout" };
            throw error;
          }
          if (response.status < 200 || response.status >= 300) {
            return { outcome: "status", status: response.status };
          }
          const body = await response.text();
          if (!body) return { outcome: "empty-json", status: response.status };
          try {
            return { outcome: "json", status: response.status, payload: JSON.parse(body) };
          } catch (_error) {
            return { outcome: "invalid-json", status: response.status };
          }
        } finally {
          clearTimeout(timeout);
        }
      }, pathname);
      if (serverFixtureExited()) {
        throw contentFreeReadError(site, "server-exit", false);
      }
      if (result.outcome === "json") return result.payload;
      if (result.outcome === "status") {
        throw contentFreeReadError(site, `status-${result.status}`, isRetryableReadStatus(result.status));
      }
      throw contentFreeReadError(site, result.outcome, true);
    } catch (error) {
      lastError = error;
      const reason = contentFreeReadFailureReason(error, serverFixtureExited());
      const retryable = error && error.contentFreeRead
        ? error.retryableContentFreeRead
        : !["server-exit", "page-closed"].includes(reason);
      if (!retryable || page.isClosed() || attempt + 1 === attempts) {
        if (error.contentFreeRead && error.contentFreeReadReason === reason) throw error;
        throw contentFreeReadError(site, reason, false);
      }
    }
    await page.waitForTimeout(100 * (attempt + 1));
  }
  throw contentFreeReadError(site, contentFreeReadFailureReason(lastError), false);
}

function contentFreeReadError(site, reason, retryable) {
  const error = new Error(`[site:${safeSite(site)}] idempotent GET failed: ${reason}`);
  error.contentFreeRead = true;
  error.contentFreeReadReason = reason;
  error.retryableContentFreeRead = retryable;
  return error;
}

function contentFreeReadFailureReason(error, fixtureExited = false) {
  if (fixtureExited) return "server-exit";
  if (error && error.contentFreeReadReason) return error.contentFreeReadReason;
  const message = String(error && error.message || "");
  if (/execution context was destroyed|cannot find context with specified id|js handle.*context/i.test(message)) {
    return "execution-context-lost";
  }
  if (/navigation|navigating frame|frame was detached/i.test(message)) return "navigation";
  return contentFreeNetworkReason(message);
}

function serverFixtureExited() {
  return appLifecycle.spawnFailed || appLifecycle.exited || !app || app.exitCode !== null || Boolean(app.signalCode);
}

async function expectContentFreeReadDiagnostic(page, probe, response, reason) {
  const pathname = `/api/sessions?content-free-read=${probe}`;
  const routePattern = `**${pathname}`;
  const handler = (route) => route.fulfill(response);
  await page.route(routePattern, handler);
  let failure = null;
  try {
    await idempotentJSONGet(page, pathname, 1, `lifecycle-read-${probe}`);
  } catch (error) {
    failure = error;
  } finally {
    await page.unroute(routePattern, handler);
  }
  expect(failure).not.toBeNull();
  expect(failure.message).toBe(`[site:lifecycle-read-${probe}] idempotent GET failed: ${reason}`);
}

async function waitForBrowserNetworkReady(page, site = "browser-network-ready") {
  const fixedSite = safeSite(site);
  await runBoundedBrowserPhase(`${fixedSite}-initial-read`, () => {
    return idempotentJSONGet(page, "/api/sessions", 3, `${fixedSite}-initial-read`);
  }, 17_000);
  await runBoundedBrowserPhase(`${fixedSite}-frame`, () => {
    return page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve())));
  }, 5_000);
  await runBoundedBrowserPhase(`${fixedSite}-confirmed-read`, () => {
    return idempotentJSONGet(page, "/api/sessions", 3, `${fixedSite}-confirmed-read`);
  }, 17_000);
}

function createHeldRouteGate() {
  let markStarted;
  let release;
  let finish;
  let started = false;
  let released = false;
  let finished = false;
  const startedPromise = new Promise((resolve) => {
    markStarted = resolve;
  });
  const releasedPromise = new Promise((resolve) => {
    release = resolve;
  });
  const finishedPromise = new Promise((resolve) => {
    finish = resolve;
  });
  return {
    started: startedPromise,
    released: releasedPromise,
    start: () => {
      if (started) return;
      started = true;
      markStarted();
    },
    release: () => {
      if (released) return;
      released = true;
      release();
    },
    finish: (error) => {
      if (finished) return;
      finished = true;
      finish({ error: error || null });
    },
    hasStarted: () => started,
    finished: finishedPromise
  };
}

async function waitForHeldRouteGate(gate, site, timeoutMs) {
  const result = await runBoundedBrowserPhase(site, () => gate.finished, timeoutMs);
  if (result.error) throw result.error;
}

async function fetchRouteOnceAndFulfill(route, site, beforeFulfill) {
  let handled = false;
  try {
    const response = await runBoundedBrowserPhase(
      `${site}-fetch`,
      () => route.fetch(),
      5_000
    );
    if (beforeFulfill) beforeFulfill(response);
    await fulfillRouteBounded(route, { response }, `${site}-fulfill`);
    handled = true;
    return response;
  } finally {
    if (!handled) {
      try {
        await runBoundedBrowserPhase(`${site}-abort`, () => route.abort(), 2_000);
      } catch (_error) {
        // The route may already have completed while the bounded phase expired.
      }
    }
  }
}

function continueRouteBounded(route, site) {
  return runBoundedBrowserPhase(site, () => route.continue(), 5_000);
}

function fulfillRouteBounded(route, options, site) {
  return runBoundedBrowserPhase(site, () => route.fulfill(options), 5_000);
}

async function runBoundedBrowserPhase(site, operation, timeoutMs) {
  const fixedSite = safeSite(site);
  let timeout;
  const timeoutResult = new Promise((_, reject) => {
    timeout = setTimeout(() => {
      const error = new Error(`[site:${fixedSite}] browser phase timed out`);
      error.boundedBrowserPhaseTimeout = true;
      reject(error);
    }, timeoutMs);
  });
  try {
    return await Promise.race([
      Promise.resolve().then(operation),
      timeoutResult
    ]);
  } catch (error) {
    if (/^\[site:[a-z0-9-]+\]/.test(String(error && error.message))) throw error;
    const networkReason = contentFreeNetworkReason(error && error.message);
    const reason = error && error.boundedBrowserPhaseTimeout
      ? "timeout"
      : (networkReason === "network-failure" ? "assertion" : networkReason);
    throw new Error(`[site:${fixedSite}] browser phase failed: ${reason}`);
  } finally {
    clearTimeout(timeout);
  }
}

function isRetryableReadStatus(status) {
  return status === 408 || status === 425 || status === 429 || status >= 500;
}

function mutationSite(request) {
  let pathname = "";
  try {
    pathname = new URL(request.url()).pathname;
  } catch (_error) {
    return "mutation";
  }
  const method = request.method();
  if (method === "POST" && pathname === "/login") return "login";
  if (method === "POST" && pathname === "/api/sessions") return "session-create";
  if (method === "POST" && pathname.endsWith("/paste/token")) return "paste-token";
  if (method === "POST" && pathname.endsWith("/paste")) return "paste";
  if (method === "POST" && pathname.endsWith("/keys")) return "keys";
  if (method === "POST" && pathname.endsWith("/tmux-control")) return "tmux-control";
  if (method === "POST" && pathname.endsWith("/resize/viewer")) return "resize-viewer";
  if (method === "POST" && pathname.endsWith("/resize")) return "resize";
  if (method === "POST" && pathname.endsWith("/history-snapshots")) return "history-create";
  if (method === "DELETE" && pathname.includes("/history-snapshots/")) return "history-delete";
  if (method === "DELETE" && pathname.startsWith("/api/sessions/")) return "session-delete";
  return "mutation";
}

function readSite(pathname) {
  if (pathname === "/api/sessions") return "session-list";
  if (pathname.endsWith("/tmux-control")) return "tmux-control-read";
  if (pathname.endsWith("/resize")) return "resize-read";
  if (pathname.endsWith("/pages")) return "history-page";
  if (pathname.includes("/history-snapshots/")) return "history-state";
  return "api-read";
}

function safeSite(site) {
  return /^[a-z0-9-]+$/.test(site || "") ? site : "browser-read";
}

function contentFreeNetworkReason(value) {
  const message = String(value || "");
  if (message.includes("ERR_NETWORK_CHANGED") || message.includes("network-changed")) return "network-changed";
  if (message.includes("ERR_INTERNET_DISCONNECTED") || message.includes("internet-disconnected")) return "internet-disconnected";
  if (message.includes("ERR_CONNECTION_REFUSED") || message.includes("ECONNREFUSED")) return "connection-refused";
  if (message.includes("ERR_CONNECTION_RESET") || message.includes("ECONNRESET")) return "connection-reset";
  if (message.includes("ERR_ABORTED")) return "aborted";
  if (/timeout|timed out/i.test(message)) return "timeout";
  if (/page.*closed|browser.*closed|context.*closed/i.test(message)) return "page-closed";
  return "network-failure";
}

async function assertServerFixtureHealthy(site) {
  const fixedSite = safeSite(site);
  if (!appLifecycle.ready || appLifecycle.spawnFailed || appLifecycle.exited || !app || app.exitCode !== null || app.signalCode) {
    throw new Error(`[site:${fixedSite}] server fixture is not running`);
  }
  try {
    await httpsGet(`${baseURL}/login`);
  } catch (_error) {
    throw new Error(`[site:${fixedSite}] server fixture readiness probe failed`);
  }
  if (appLifecycle.exited || app.exitCode !== null || app.signalCode) {
    throw new Error(`[site:${fixedSite}] server fixture exited during readiness probe`);
  }
}

async function sessionRef(page, name) {
  return (await publicSession(page, name)).id;
}

async function tmuxControlState(page, ref) {
  return idempotentJSONGet(page, `/api/sessions/${encodeURIComponent(ref)}/tmux-control`);
}

function escapeRegex(value) {
  return String(value).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

async function expectVisibleFocus(page, selector) {
  const control = page.locator(selector);
  await expect(control).toBeVisible();
  await expect(control).toBeFocused();
  const id = await control.getAttribute("id");
  expect(await page.evaluate(() => document.activeElement && document.activeElement.id)).toBe(id);
}

function commandExists(name) {
  return spawnSync("sh", ["-c", `command -v ${name}`], { stdio: "ignore" }).status === 0;
}

function resolveCommand(name) {
  const result = spawnSync("sh", ["-c", `command -v ${name}`], { encoding: "utf8" });
  if (result.status !== 0 || !result.stdout.trim()) {
    throw new Error(`command not found: ${name}`);
  }
  return result.stdout.trim();
}

function installTmuxCommandShim() {
  const directory = path.join(stateDir, "test-bin");
  const shimPath = path.join(directory, "tmux");
  fs.mkdirSync(directory, { recursive: true });
  fs.writeFileSync(shimPath, [
    "#!/bin/sh",
    'marker=""',
    'capture=""',
    'for argument in "$@"; do',
    '  case "$argument" in',
    '    copy-mode|resize-window|send-keys|paste-buffer|refresh-client|-X|-U|-D|-L|-R) marker="$marker $argument" ;;',
    '    capture-pane) capture="1" ;;',
    "  esac",
    "done",
    'if [ -n "${CONTROL_AGENTS_TEST_TMUX_COMMAND_LOG:-}" ]; then',
    '  printf "%s\\n" "$marker" >> "$CONTROL_AGENTS_TEST_TMUX_COMMAND_LOG"',
    "fi",
    'if [ -n "$capture" ] && [ -n "${CONTROL_AGENTS_TEST_BENCHMARK_LOG:-}" ]; then start="$(date +%s%N)"; fi',
    '"$CONTROL_AGENTS_TEST_REAL_TMUX" "$@"',
    'status="$?"',
    'if [ -n "$capture" ] && [ -n "${CONTROL_AGENTS_TEST_BENCHMARK_LOG:-}" ]; then',
    '  end="$(date +%s%N)"',
    '  printf "capture_pane_duration_ns %s\\n" "$((end - start))" >> "$CONTROL_AGENTS_TEST_BENCHMARK_LOG"',
    "fi",
    'exit "$status"',
    ""
  ].join("\n"), { mode: 0o755 });
  return directory;
}

function tmuxCommandCheckpoint() {
  return tmuxCommandEntries().length;
}

function tmuxCommandEntries() {
  if (!fs.existsSync(tmuxCommandLog)) return [];
  return fs.readFileSync(tmuxCommandLog, "utf8").split("\n").filter(Boolean);
}

function latestContentFreeDuration(logPath, metricName) {
  if (!fs.existsSync(logPath)) throw new Error(`missing benchmark metric ${metricName}`);
  const values = fs.readFileSync(logPath, "utf8").split("\n").map((line) => {
    const [name, rawValue, extra] = line.trim().split(/\s+/);
    if (name !== metricName || extra !== undefined || !/^\d+$/.test(rawValue || "")) return null;
    return Number.parseInt(rawValue, 10);
  }).filter((value) => Number.isFinite(value));
  if (!values.length) throw new Error(`missing benchmark metric ${metricName}`);
  return values[values.length - 1];
}

function historyParseDurations(logs) {
  const values = [];
  for (const line of logs.split("\n")) {
    if (!line.includes("history parse")) continue;
    const match = line.match(/(?:"duration_ms":|duration_ms=)(\d+)/);
    if (match) values.push(Number.parseInt(match[1], 10));
  }
  return values;
}

function expectNoHistoryTmuxMutations(checkpoint, interaction) {
  const commands = tmuxCommandEntries().slice(checkpoint);
  const forbidden = commands.filter((command) => {
    return /(?:^| )(copy-mode|resize-window|send-keys)(?: |$)/.test(command) ||
      /(?:^| )refresh-client(?: -(?:U|D|L|R))+(?: |$)/.test(command);
  });
  expect(forbidden, `${interaction} emitted a forbidden tmux command`).toEqual([]);
}

function run(name, args) {
  const result = spawnSync(name, args, { encoding: "utf8" });
  if (result.status !== 0) {
    throw new Error(`${name} ${args.join(" ")} failed\nstdout:\n${result.stdout}\nstderr:\n${result.stderr}`);
  }
  return result.stdout;
}

async function attachControlClient(session, size) {
  const control = spawn("tmux", ["-C", "attach-session", "-t", session], {
    cwd: repoRoot,
    env: process.env,
    stdio: ["pipe", "pipe", "pipe"]
  });
  let logs = "";
  control.stdout.on("data", (chunk) => {
    logs += chunk.toString();
  });
  control.stderr.on("data", (chunk) => {
    logs += chunk.toString();
  });

  const name = await waitForControlClient(session, 5_000, () => logs);
  run("tmux", ["refresh-client", "-t", name, "-C", size]);
  return { process: control, name };
}

function detachControlClient(controlClient) {
  try {
    if (controlClient.name) {
      spawnSync("tmux", ["detach-client", "-t", controlClient.name], { stdio: "ignore" });
    }
  } finally {
    if (controlClient.process && !controlClient.process.killed) {
      controlClient.process.kill("SIGTERM");
    }
  }
}

async function waitForControlClient(session, timeoutMs, logs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const client = tmuxClientViews(session).find((view) => view.control);
    if (client) return client.name;
    await delay(100);
  }
  throw new Error(`control tmux client did not attach\n${logs()}`);
}

function tmuxClientViews(session) {
  return run("tmux", [
    "list-clients",
    "-t",
    session,
    "-F",
    "#{client_name}|#{client_control_mode}|#{client_width}|#{client_height}|#{window_width}|#{window_height}|#{window_offset_y}"
  ]).trim().split("\n").filter(Boolean).map((line) => {
    const [name, control, clientWidth, clientHeight, windowWidth, windowHeight, offsetY] = line.split("|");
    return {
      name,
      control: control === "1",
      clientWidth: Number.parseInt(clientWidth, 10),
      clientHeight: Number.parseInt(clientHeight, 10),
      windowWidth: Number.parseInt(windowWidth, 10),
      windowHeight: Number.parseInt(windowHeight, 10),
      offsetY
    };
  });
}

function tmuxClientState(clientName) {
  const values = run("tmux", [
    "display-message",
    "-p",
    "-c",
    clientName,
    "#{window_id}|#{pane_id}|#{client_width}|#{client_height}|#{window_width}|#{window_height}|#{window_offset_x}|#{window_offset_y}"
  ]).trim().split("|");
  return {
    windowID: values[0],
    paneID: values[1],
    clientWidth: values[2],
    clientHeight: values[3],
    windowWidth: values[4],
    windowHeight: values[5],
    offsetX: values[6],
    offsetY: values[7]
  };
}

function tmuxWindowSize(session) {
  const [width, height] = run("tmux", ["display-message", "-p", "-t", session, "#{window_width}|#{window_height}"]).trim().split("|");
  return {
    width: Number.parseInt(width, 10),
    height: Number.parseInt(height, 10)
  };
}

function tmuxWindowSizeOption(session) {
  return run("tmux", ["show-options", "-w", "-v", "-t", `${session}:`, "window-size"]).trim();
}

function tmuxWindowCount(session) {
  return run("tmux", ["list-windows", "-t", session]).trim().split("\n").filter(Boolean).length;
}

function activeTmuxWindow(session) {
  return Number.parseInt(run("tmux", ["display-message", "-p", "-t", session, "#{window_index}"]).trim(), 10);
}

function killRegisteredTtyd(managedSession) {
  const sessionFile = path.join(stateDir, "sessions", `${managedSession}.json`);
  if (!fs.existsSync(sessionFile)) return 0;
  const session = JSON.parse(fs.readFileSync(sessionFile, "utf8"));
  if (!Number.isInteger(session.pid) || session.pid <= 0) return 0;
  try {
    process.kill(session.pid, "SIGTERM");
    return session.pid;
  } catch (error) {
    if (error && error.code === "ESRCH") return 0;
    return session.pid;
  }
}

async function stopServerFixture() {
  if (!app || !app.pid || appLifecycle.exited || appLifecycle.spawnFailed) return;
  signalServerFixture("SIGTERM");
  if (await waitForPromise(appExitPromise, 5_000)) return;
  signalServerFixture("SIGKILL");
  if (!await waitForPromise(appExitPromise, 2_000)) {
    throw new Error("[site:server-teardown] server fixture did not exit");
  }
}

function signalServerFixture(signal) {
  try {
    app.kill(signal);
  } catch (error) {
    if (error && error.code === "ESRCH") return;
    // The exit promise decides whether teardown completed.
  }
}

async function waitForPromise(promise, timeoutMs) {
  return Promise.race([
    promise.then(() => true),
    delay(timeoutMs).then(() => false)
  ]);
}

async function waitForPIDExit(pid, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      process.kill(pid, 0);
    } catch (error) {
      if (error && error.code === "ESRCH") return;
    }
    await delay(50);
  }
  try {
    process.kill(pid, "SIGKILL");
  } catch (error) {
    if (error && error.code === "ESRCH") return;
  }
  const killDeadline = Date.now() + 1_000;
  while (Date.now() < killDeadline) {
    try {
      process.kill(pid, 0);
    } catch (error) {
      if (error && error.code === "ESRCH") return;
    }
    await delay(50);
  }
  throw new Error("[site:ttyd-teardown] ttyd fixture did not exit");
}

async function waitForTmuxHistory(session, minimum, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const result = spawnSync("tmux", ["display-message", "-p", "-t", session, "#{history_size}"], {
      encoding: "utf8"
    });
    const size = Number.parseInt(result.stdout.trim(), 10);
    if (Number.isFinite(size) && size >= minimum) return;
    await delay(100);
  }
  throw new Error(`tmux history did not reach ${minimum} lines`);
}

async function waitForRegisteredTtyd(session, timeoutMs) {
  const sessionFile = path.join(stateDir, "sessions", `${session}.json`);
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const record = JSON.parse(fs.readFileSync(sessionFile, "utf8"));
      const socketReady = typeof record.socket === "string" && fs.statSync(record.socket).isSocket();
      if (Number.isInteger(record.pid) && record.pid > 0 && socketReady) {
        process.kill(record.pid, 0);
        return;
      }
    } catch (error) {
      if (error && !["ENOENT", "ESRCH"].includes(error.code)) {
        throw new Error("[site:ttyd-readiness] ttyd fixture readiness probe failed");
      }
    }
    await delay(50);
  }
  throw new Error("[site:ttyd-readiness] ttyd fixture readiness timed out");
}

async function waitForHTTPS(url, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (appLifecycle.spawnFailed || appLifecycle.exited || (app && (app.exitCode !== null || app.signalCode))) {
      throw new Error("[site:server-readiness] server fixture exited before readiness");
    }
    try {
      await httpsGet(url);
      return;
    } catch (_error) {
      await delay(100);
    }
  }
  throw new Error("[site:server-readiness] server fixture readiness timed out");
}

async function waitForHTTPSExit(url, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      await httpsGet(url);
    } catch (_error) {
      return;
    }
    await delay(50);
  }
  throw new Error("[site:server-teardown] server fixture listener did not close");
}

function httpsGet(url) {
  return new Promise((resolve, reject) => {
    const request = https.get(url, { rejectUnauthorized: false }, (response) => {
      response.resume();
      response.on("end", resolve);
    });
    request.on("error", reject);
    request.setTimeout(2_000, () => {
      request.destroy(new Error("request timed out"));
    });
  });
}

function freePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.on("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const port = server.address().port;
      server.close(() => resolve(port));
    });
  });
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function browserContextOptions(projectUse) {
  const options = {};
  const supported = [
    "acceptDownloads",
    "bypassCSP",
    "clientCertificates",
    "colorScheme",
    "deviceScaleFactor",
    "extraHTTPHeaders",
    "geolocation",
    "hasTouch",
    "httpCredentials",
    "ignoreHTTPSErrors",
    "isMobile",
    "javaScriptEnabled",
    "locale",
    "offline",
    "permissions",
    "reducedMotion",
    "screen",
    "serviceWorkers",
    "storageState",
    "timezoneId",
    "userAgent",
    "viewport"
  ];
  for (const name of supported) {
    if (projectUse[name] !== undefined) options[name] = projectUse[name];
  }
  return options;
}
