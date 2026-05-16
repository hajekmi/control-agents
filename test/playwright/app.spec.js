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
      MIRROR_PASSWORD: "secret",
      MIRROR_BIND_ADDR: "127.0.0.1",
      MIRROR_PORT: String(port),
      MIRROR_STATE_DIR: stateDir
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
      MIRROR_STATE_DIR: stateDir,
      MIRROR_NO_ATTACH: "1",
      MIRROR_WEB_SCROLLBACK_LINES: "2345"
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
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);
});

test("terminal wheel and scrollbar controls route through tmux scroll API", async ({ page }) => {
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
  const scrollRequest = page.waitForRequest((request) => {
    return request.method() === "POST" && request.url().includes(`/api/sessions/${encodeURIComponent(sessionName)}/scroll`);
  });

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

  await expect.poll(async () => (await scrollState(page)).scrollTop).toBeLessThan(before.scrollTop);
  await expect.poll(async () => Number(await page.locator("#history-track").getAttribute("aria-valuenow"))).toBeLessThan(before.scrollTop);
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
