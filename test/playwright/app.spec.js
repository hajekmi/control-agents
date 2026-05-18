const { test, expect } = require("@playwright/test");
const fs = require("fs");
const https = require("https");
const net = require("net");
const path = require("path");
const { spawn, spawnSync } = require("child_process");

const repoRoot = path.resolve(__dirname, "../..");
const sessionName = `pw${process.pid}`;
const stateDir = path.join(repoRoot, ".cache", `pw${process.pid}`);

let app;
let appLogs = "";
let baseURL = "";

test.skip(!commandExists("go"), "go is required for browser e2e tests");
test.skip(!commandExists("tmux"), "tmux is required for browser e2e tests");
test.skip(!commandExists("ttyd"), "ttyd is required for browser e2e tests");

test.beforeAll(async () => {
  fs.rmSync(stateDir, { force: true, recursive: true });
  fs.mkdirSync(stateDir, { recursive: true });

  const port = await freePort();
  baseURL = `https://127.0.0.1:${port}`;
  app = spawn("go", ["run", "./cmd/server"], {
    cwd: repoRoot,
    detached: true,
    env: {
      ...process.env,
      CONTROL_AGENTS_PASSWORD: "secret",
      CONTROL_AGENTS_BIND_ADDR: "127.0.0.1",
      CONTROL_AGENTS_PORT: String(port),
      CONTROL_AGENTS_STATE_DIR: stateDir
    },
    stdio: ["ignore", "pipe", "pipe"]
  });
  app.stdout.on("data", (chunk) => {
    appLogs += chunk.toString();
  });
  app.stderr.on("data", (chunk) => {
    appLogs += chunk.toString();
  });

  await waitForHTTPS(`${baseURL}/login`, 30_000);

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
    throw new Error(`control-agents failed\nstdout:\n${wrapper.stdout}\nstderr:\n${wrapper.stderr}`);
  }

  const historyCommand = "for i in $(seq 1 180); do echo playwright-line-$i; done";
  run("tmux", ["send-keys", "-t", sessionName, historyCommand, "C-m"]);
  await waitForTmuxHistory(sessionName, 60, 10_000);
});

test.afterAll(() => {
  killRegisteredTtyd();
  spawnSync("tmux", ["kill-session", "-t", sessionName], { stdio: "ignore" });
  if (app && app.pid) {
    try {
      process.kill(-app.pid, "SIGTERM");
    } catch (error) {
      try {
        app.kill("SIGTERM");
      } catch (_error) {
        // Best effort cleanup.
      }
    }
  }
  fs.rmSync(stateDir, { force: true, recursive: true });
});

test("authenticates, renders registered terminal, sends special keys, and logs out", async ({ page }) => {
  await page.goto(`${baseURL}/`);
  await expect(page).toHaveURL(/\/login$/);

  await page.locator("#password").fill("wrong");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/login\?error=1$/);
  await expect(page.getByRole("alert")).toBeVisible();

  await login(page);
  await expect(page.locator("#version-badge")).toBeVisible();
  await expect(page.locator("#heartbeat")).toHaveAttribute("data-state", "online");
  await expect(page.getByRole("button", { name: sessionName })).toBeVisible();

  const terminalFrame = page.locator(`iframe[title="${sessionName}"]`);
  await expect(terminalFrame).toBeVisible();
  await expect.poll(() => terminalFrame.getAttribute("src")).toContain(`/terminal/${encodeURIComponent(sessionName)}/`);
  await waitForTerminalFrame(page);

  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: "Resize" }).click();
  const resizePanel = page.getByRole("dialog", { name: /resize/i });
  await expect(resizePanel).toBeVisible();
  await expect(resizePanel.getByLabel("Off")).toBeVisible();
  await expect(resizePanel.getByLabel("Automatic smallest")).toBeVisible();
  await expect(resizePanel.getByLabel("Follow web window")).toBeVisible();
  await expect(resizePanel.getByLabel("Follow primary SSH/tmux")).toBeVisible();

  const currentViewer = await waitForActiveResizeViewer(page);
  const webResizeRequest = await applyResizeMode(page, "Follow web window");
  expect(webResizeRequest.postDataJSON()).toEqual({ mode: "web", viewerId: currentViewer.id });
  await expect.poll(() => tmuxWindowSizeOption(sessionName)).toBe("manual");

  const smallestResizeRequest = await applyResizeMode(page, "Automatic smallest");
  expect(smallestResizeRequest.postDataJSON()).toEqual({ mode: "smallest" });
  await expect.poll(() => tmuxWindowSizeOption(sessionName)).toBe("smallest");

  const offResizeRequest = await applyResizeMode(page, "Off");
  expect(offResizeRequest.postDataJSON()).toEqual({ mode: "off" });
  expect(tmuxWindowSizeOption(sessionName)).not.toBe("latest");

  const controlClient = await attachControlClient(sessionName, "82,21");
  try {
    const primaryResizeRequest = await applyResizeMode(page, "Follow primary SSH/tmux");
    expect(primaryResizeRequest.postDataJSON()).toEqual({ mode: "primary" });
    await expect.poll(() => tmuxWindowSizeOption(sessionName)).toBe("manual");
    await expect.poll(() => tmuxWindowSize(sessionName)).toEqual({ width: 82, height: 21 });
  } finally {
    detachControlClient(controlClient);
  }
  await page.getByLabel("Close resize settings").click();
  run("tmux", ["set-option", "-w", "-t", `${sessionName}:`, "window-size", "smallest"]);

  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: "Keys" }).click();
  await expect(page.getByRole("button", { name: "Ctrl+C" })).toBeVisible();

  const keyRequest = page.waitForRequest((request) => {
    return request.method() === "POST" && request.url().includes(`/api/sessions/${encodeURIComponent(sessionName)}/keys`);
  });
  const keyResponse = page.waitForResponse((response) => {
    return response.request().method() === "POST" && response.url().includes(`/api/sessions/${encodeURIComponent(sessionName)}/keys`);
  });
  await page.getByRole("button", { name: "Esc" }).click();
  expect((await keyRequest).postDataJSON()).toEqual({ key: "escape" });
  expect((await keyResponse).status()).toBe(200);

  await page.getByLabel("Close special keys").click();

  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: "T-Control" }).click();
  await expect(page.getByLabel("Tmux controls")).toBeVisible();
  const originalWindow = activeTmuxWindow(sessionName);
  const sessionWindowBadge = page.locator(`#tabs button[data-session-id="${sessionName}"] .tab-window-badge`);
  await expect(sessionWindowBadge).toHaveCount(0);
  await expect(page.locator("#tcontrol-windows button").filter({ hasText: new RegExp(`^${originalWindow}:`) })).toBeVisible();

  const controlRequest = page.waitForRequest((request) => {
    return request.method() === "POST" && request.url().includes(`/api/sessions/${encodeURIComponent(sessionName)}/tmux-control`);
  });
  const controlResponse = page.waitForResponse((response) => {
    return response.request().method() === "POST" && response.url().includes(`/api/sessions/${encodeURIComponent(sessionName)}/tmux-control`);
  });
  await page.getByRole("button", { name: "New win" }).click();
  expect((await controlRequest).postDataJSON()).toEqual({ action: "new-window" });
  expect((await controlResponse).status()).toBe(200);
  await expect.poll(() => tmuxWindowCount(sessionName)).toBeGreaterThan(1);
  await expect(sessionWindowBadge).toHaveText(String(tmuxWindowCount(sessionName)));
  const createdWindow = activeTmuxWindow(sessionName);

  const selectRequest = page.waitForRequest((request) => {
    return request.method() === "POST" && request.url().includes(`/api/sessions/${encodeURIComponent(sessionName)}/tmux-control`);
  });
  const selectResponse = page.waitForResponse((response) => {
    return response.request().method() === "POST" && response.url().includes(`/api/sessions/${encodeURIComponent(sessionName)}/tmux-control`);
  });
  await page.locator("#tcontrol-windows button").filter({ hasText: new RegExp(`^${originalWindow}:`) }).click();
  expect((await selectRequest).postDataJSON()).toEqual({ action: "select-window", windowIndex: originalWindow });
  expect((await selectResponse).status()).toBe(200);
  if (createdWindow !== originalWindow) {
    run("tmux", ["kill-window", "-t", `${sessionName}:${createdWindow}`]);
  }

  await page.getByLabel("Close T-Control").click();
  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);
});

test("terminal wheel, touch, and scrollbar controls route through tmux scroll API", async ({ page }) => {
  await login(page);
  const terminalFrame = page.locator(`iframe[title="${sessionName}"]`);
  await expect(terminalFrame).toBeVisible();
  const frame = await waitForTerminalFrame(page);

  await expect.poll(async () => (await scrollState(page)).scrollMax).toBeGreaterThan(0);

  await page.getByLabel("History top").click();
  await expect.poll(async () => (await scrollState(page)).scrollTop).toBe(0);

  await page.getByLabel("Live bottom").click();
  await expect.poll(async () => {
    const state = await scrollState(page);
    return state.scrollTop === state.scrollMax;
  }).toBe(true);

  const before = await scrollState(page);
  const isScrollRequest = (request) => {
    return request.method() === "POST" && request.url().includes(`/api/sessions/${encodeURIComponent(sessionName)}/scroll`);
  };
  const isPasteRequest = (request) => {
    return request.method() === "POST" && request.url().includes(`/api/sessions/${encodeURIComponent(sessionName)}/paste`);
  };
  const scrollRequest = page.waitForRequest(isScrollRequest);
  const scrollResponse = page.waitForResponse((response) => isScrollRequest(response.request()));

  await frame.evaluate(() => {
    window.dispatchEvent(new WheelEvent("wheel", {
      deltaY: -720,
      deltaX: 0,
      bubbles: true,
      cancelable: true
    }));
  });

  const request = await scrollRequest;
  expect(request.postDataJSON()).toMatchObject({ action: "line-up" });
  expect((await scrollResponse).status()).toBe(200);

  await expect.poll(async () => (await scrollState(page)).scrollTop).toBeLessThan(before.scrollTop);
  await expect.poll(async () => Number(await page.locator("#history-track").getAttribute("aria-valuenow"))).toBeLessThan(before.scrollTop);

  await page.getByLabel("Live bottom").click();
  await expect.poll(async () => {
    const state = await scrollState(page);
    return state.scrollTop === state.scrollMax;
  }).toBe(true);

  const beforeTouch = await scrollState(page);
  const touchRequest = page.waitForRequest(isScrollRequest);
  const touchResponse = page.waitForResponse((response) => isScrollRequest(response.request()));
  await dispatchTerminalTouch(frame, [
    ["touchstart", 120, 220],
    ["touchmove", 122, 260],
    ["touchmove", 122, 315],
    ["touchend", 122, 315]
  ]);

  const touchPost = await touchRequest;
  expect(touchPost.postDataJSON()).toMatchObject({ action: "line-up" });
  expect((await touchResponse).status()).toBe(200);

  await expect.poll(async () => (await scrollState(page)).scrollTop).toBeLessThan(beforeTouch.scrollTop);

  await page.getByRole("button", { name: "Menu" }).click();
  const captureResponse = page.waitForResponse((response) => {
    return response.request().method() === "GET" && response.url().includes(`/api/sessions/${encodeURIComponent(sessionName)}/capture`);
  });
  await page.locator("#copy-mode-toggle").click();
  expect((await captureResponse).status()).toBe(200);
  await expect(page.locator("#copy-mode-toggle")).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator("#terminal-pane")).toHaveClass(/copy-mode/);
  await expect(page.locator("#copy-panel")).toBeVisible();
  await expect(page.locator("#copy-text")).toContainText("playwright-line");

  let copyModeScrollPosts = 0;
  const countCopyModeScrollPost = (request) => {
    if (isScrollRequest(request)) {
      copyModeScrollPosts += 1;
    }
  };
  page.on("request", countCopyModeScrollPost);
  await dispatchTerminalTouch(frame, [
    ["touchstart", 120, 220],
    ["touchmove", 122, 270],
    ["touchmove", 122, 330],
    ["touchend", 122, 330]
  ]);
  await page.waitForTimeout(250);
  page.off("request", countCopyModeScrollPost);
  expect(copyModeScrollPosts).toBe(0);

  await page.getByRole("button", { name: "Menu" }).click();
  await page.locator("#copy-mode-toggle").click();
  await expect(page.locator("#copy-mode-toggle")).toHaveAttribute("aria-pressed", "false");
  await expect(page.locator("#terminal-pane")).not.toHaveClass(/copy-mode/);
  await expect(page.locator("#copy-panel")).toBeHidden();

  await page.context().grantPermissions(["clipboard-read", "clipboard-write"], { origin: baseURL });
  await page.evaluate(() => navigator.clipboard.writeText("playwright-paste-text"));
  const pasteRequest = page.waitForRequest(isPasteRequest);
  const pasteResponse = page.waitForResponse((response) => isPasteRequest(response.request()));
  await page.getByRole("button", { name: "Menu" }).click();
  await page.locator("#paste-toggle").click();
  expect((await pasteRequest).postDataJSON()).toEqual({ text: "playwright-paste-text" });
  expect((await pasteResponse).status()).toBe(200);
});

test("tracks local visual viewport changes without applying tmux resize", async ({ page }) => {
  await login(page);
  await waitForTerminalFrame(page);

  const beforeMode = tmuxWindowSizeOption(sessionName);
  const initialHeight = await appViewportHeight(page);
  await expect.poll(() => cssAppViewportHeight(page)).toBe(initialHeight);

  await page.setViewportSize({ width: 390, height: 560 });
  const resizedHeight = await appViewportHeight(page);
  await expect.poll(() => cssAppViewportHeight(page)).toBe(resizedHeight);
  expect(tmuxWindowSizeOption(sessionName)).toBe(beforeMode);
});

test("keeps fullscreen tmux apps visible when an SSH-sized client is smaller than the browser", async ({ page }) => {
  await login(page);
  await waitForTerminalFrame(page);

  expect(run("tmux", ["show-options", "-w", "-v", "-t", `${sessionName}:`, "window-size"]).trim()).toBe("smallest");

  const controlClient = await attachControlClient(sessionName, "80,20");
  try {
    await expect.poll(() => tmuxWindowSize(sessionName).height).toBe(20);
    await expect.poll(() => tmuxClientViews(sessionName).every((client) => client.offsetY === "" || client.offsetY === "0")).toBe(true);
  } finally {
    detachControlClient(controlClient);
  }
});

async function login(page) {
  await page.goto(`${baseURL}/`);
  if (!/\/login/.test(page.url())) {
    await page.goto(`${baseURL}/login`);
  }
  await page.locator("#password").fill("secret");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(`${baseURL}/`);
  await expect(page.getByRole("button", { name: sessionName })).toBeVisible({ timeout: 15_000 });
}

async function scrollState(page) {
  return page.evaluate(async (session) => {
    const response = await fetch(`/api/sessions/${encodeURIComponent(session)}/scroll`, {
      credentials: "same-origin"
    });
    if (!response.ok) {
      throw new Error(`scroll state failed: ${response.status}`);
    }
    return response.json();
  }, sessionName);
}

async function dispatchTerminalTouch(frame, events) {
  await frame.evaluate((touchEvents) => {
    const dispatchTouch = ([type, x, y]) => {
      const event = new Event(type, { bubbles: true, cancelable: true });
      const touch = {
        identifier: 1,
        target: document.body,
        clientX: x,
        clientY: y,
        pageX: x,
        pageY: y,
        screenX: x,
        screenY: y
      };
      Object.defineProperty(event, "touches", {
        configurable: true,
        value: type === "touchend" || type === "touchcancel" ? [] : [touch]
      });
      Object.defineProperty(event, "changedTouches", {
        configurable: true,
        value: [touch]
      });
      window.dispatchEvent(event);
    };
    for (const touchEvent of touchEvents) {
      dispatchTouch(touchEvent);
    }
  }, events);
}

async function resizeState(page) {
  return page.evaluate(async (session) => {
    const response = await fetch(`/api/sessions/${encodeURIComponent(session)}/resize`, {
      credentials: "same-origin"
    });
    if (!response.ok) {
      throw new Error(`resize state failed: ${response.status}`);
    }
    return response.json();
  }, sessionName);
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
  const resizeRequest = waitForResizeApply(page);
  await apply.click();
  const { request, response } = await resizeRequest;
  expect(response.status()).toBe(200);
  return request;
}

async function waitForResizeApply(page) {
  const isResizeApply = (candidate) => {
    return candidate.method() === "POST" &&
      candidate.url().endsWith(`/api/sessions/${encodeURIComponent(sessionName)}/resize`);
  };
  const requestPromise = page.waitForRequest(isResizeApply);
  const responsePromise = page.waitForResponse((candidate) => isResizeApply(candidate.request()));
  const [request, response] = await Promise.all([requestPromise, responsePromise]);
  return { request, response };
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

async function waitForTerminalFrame(page) {
  await expect.poll(() => {
    return page.frames().some((frame) => frame.url().includes(`/terminal/${encodeURIComponent(sessionName)}/`));
  }).toBe(true);
  return page.frames().find((frame) => frame.url().includes(`/terminal/${encodeURIComponent(sessionName)}/`));
}

function commandExists(name) {
  return spawnSync("sh", ["-c", `command -v ${name}`], { stdio: "ignore" }).status === 0;
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

function killRegisteredTtyd() {
  const sessionFile = path.join(stateDir, "sessions", `${sessionName}.json`);
  if (!fs.existsSync(sessionFile)) return;
  const session = JSON.parse(fs.readFileSync(sessionFile, "utf8"));
  if (!session.pid) return;
  try {
    process.kill(session.pid, "SIGTERM");
  } catch (_error) {
    // Best effort cleanup.
  }
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

async function waitForHTTPS(url, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      await httpsGet(url);
      return;
    } catch (_error) {
      await delay(100);
    }
  }
  throw new Error(`timed out waiting for ${url}\nserver logs:\n${appLogs}`);
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
