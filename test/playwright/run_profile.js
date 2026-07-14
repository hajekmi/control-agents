"use strict";

const fs = require("node:fs");
const http = require("node:http");
const path = require("node:path");
const { spawn, spawnSync } = require("node:child_process");

const repoRoot = path.resolve(__dirname, "../..");
const networkBoundaryMarker = "CONTROL_AGENTS_PLAYWRIGHT_PRIVATE_NETWORK";
const hostNetworkNamespaceVariable = "CONTROL_AGENTS_PLAYWRIGHT_HOST_NETWORK_NAMESPACE";

if (process.platform === "linux" && process.env[networkBoundaryMarker] !== "1") {
  enterPrivateNetworkNamespace();
} else if (process.argv.slice(2).includes("--network-boundary-probe")) {
  validateNetworkBoundary().catch(() => {
    console.error("[site:network-boundary-probe] Playwright network boundary probe failed");
    process.exitCode = 1;
  });
} else {
  try {
    runPlaywrightProfile();
  } catch (_error) {
    console.error("[site:network-boundary-verify] Playwright network boundary validation failed");
    process.exitCode = 1;
  }
}

function enterPrivateNetworkNamespace() {
  const hostNetworkNamespace = readNetworkNamespace();
  const isolated = spawn("unshare", [
    "--kill-child=SIGTERM",
    "--user",
    "--map-root-user",
    "--net",
    "sh",
    "-c",
    'ip link set lo up && exec "$@"',
    "control-agents-playwright-network",
    process.execPath,
    __filename,
    ...process.argv.slice(2)
  ], {
    cwd: repoRoot,
    env: {
      ...process.env,
      [networkBoundaryMarker]: "1",
      [hostNetworkNamespaceVariable]: hostNetworkNamespace
    },
    stdio: "inherit"
  });

  let forwardedSignal = "";
  for (const signal of ["SIGINT", "SIGTERM"]) {
    process.on(signal, () => {
      forwardedSignal = signal;
      signalProcess(isolated.pid, signal);
    });
  }
  isolated.once("error", () => {
    console.error("[site:network-boundary-enter] private Playwright network boundary could not start");
    process.exitCode = 1;
  });
  isolated.once("exit", (code, signal) => {
    process.exitCode = Number.isInteger(code) ? code : (signal || forwardedSignal ? 1 : 0);
  });
}

function runPlaywrightProfile() {
  assertPrivateNetworkBoundary();
  const fixtureID = `pw${process.pid}-${Date.now().toString(36)}`;
  const stateDir = path.join(repoRoot, ".cache", `pwr${process.pid}`);
  const managedSessions = [
    fixtureID,
    `${fixtureID}-created`,
    `${fixtureID}-next`,
    `${fixtureID}-external`,
    `${fixtureID}-conflict`
  ];
  const playwrightCLI = require.resolve("@playwright/test/cli");
  const profile = spawn(process.execPath, [playwrightCLI, "test", ...process.argv.slice(2)], {
    cwd: repoRoot,
    detached: true,
    env: {
      ...process.env,
      CONTROL_AGENTS_PLAYWRIGHT_FIXTURE_ID: fixtureID,
      CONTROL_AGENTS_PLAYWRIGHT_STATE_DIR: stateDir
    },
    stdio: "inherit"
  });

  let forwardedSignal = "";
  for (const signal of ["SIGINT", "SIGTERM"]) {
    process.on(signal, () => {
      forwardedSignal = signal;
      signalProcessGroup(profile.pid, signal);
    });
  }

  let finishing = false;
  const finishProfile = async (exitCode) => {
    if (finishing) return;
    finishing = true;

    let boundaryClean = await waitForProcessGroupExit(profile.pid, 2_000);
    if (!boundaryClean) {
      signalProcessGroup(profile.pid, "SIGTERM");
      boundaryClean = await waitForProcessGroupExit(profile.pid, 3_000);
    }
    if (!boundaryClean) {
      signalProcessGroup(profile.pid, "SIGKILL");
      boundaryClean = await waitForProcessGroupExit(profile.pid, 2_000);
    }

    const fixtureClean = cleanupFixture(stateDir, managedSessions);
    if ((!boundaryClean || !fixtureClean) && exitCode === 0) {
      console.error("[site:profile-process-boundary] browser fixture did not stop cleanly");
      process.exitCode = 1;
      return;
    }
    process.exitCode = exitCode;
  };

  profile.on("error", async () => {
    await finishProfile(1);
  });
  profile.on("exit", async (code, signal) => {
    const exitCode = Number.isInteger(code) ? code : (signal || forwardedSignal ? 1 : 0);
    await finishProfile(exitCode);
  });
}

function cleanupFixture(stateDir, managedSessions) {
  let clean = true;
  if (fs.existsSync(stateDir)) {
    clean = false;
    stopRegisteredProcesses(stateDir);
  }
  for (const session of managedSessions) {
    const result = spawnSync("tmux", ["has-session", "-t", session], { stdio: "ignore" });
    if (result.status === 0) {
      clean = false;
      spawnSync("tmux", ["kill-session", "-t", session], { stdio: "ignore" });
    }
  }
  fs.rmSync(stateDir, { force: true, recursive: true });
  return clean;
}

function stopRegisteredProcesses(stateDir) {
  const sessionsDir = path.join(stateDir, "sessions");
  let entries = [];
  try {
    entries = fs.readdirSync(sessionsDir, { withFileTypes: true });
  } catch (_error) {
    return;
  }
  for (const entry of entries) {
    if (!entry.isFile() || !entry.name.endsWith(".json")) continue;
    try {
      const record = JSON.parse(fs.readFileSync(path.join(sessionsDir, entry.name), "utf8"));
      if (Number.isInteger(record.pid) && record.pid > 0) process.kill(record.pid, "SIGTERM");
    } catch (error) {
      if (!error || error.code !== "ESRCH") {
        // Fixture cleanup remains best-effort; the fixed boundary error is reported above.
      }
    }
  }
}

async function validateNetworkBoundary() {
  if (process.platform !== "linux") {
    console.log("Playwright private network boundary is not required on this platform.");
    return;
  }
  assertPrivateNetworkBoundary();
  let mutationCount = 0;
  let pendingMutationResponse = null;
  let markMutationReceived;
  const mutationReceived = new Promise((resolve) => {
    markMutationReceived = resolve;
  });
  const server = http.createServer((request, response) => {
    if (request.method === "POST" && request.url === "/mutation") {
      mutationCount += 1;
      pendingMutationResponse = response;
      request.resume();
      request.once("end", markMutationReceived);
      return;
    }
    response.writeHead(200, { "Content-Type": "text/html", "Cache-Control": "no-store" });
    response.end("<!doctype html><title>network boundary probe</title>");
  });

  let browser;
  try {
    const port = await listenOnLoopback(server);
    const { chromium } = require("@playwright/test");
    browser = await chromium.launch();
    const page = await browser.newPage();
    await page.goto(`http://127.0.0.1:${port}/`, { waitUntil: "domcontentloaded" });
    const mutation = page.evaluate(async () => {
      const response = await fetch("/mutation", { method: "POST", body: "" });
      return response.status;
    });
    await boundedPromise(mutationReceived, 5_000);

    const churn = spawnSync("unshare", [
      "--user",
      "--map-root-user",
      "--net",
      "sh",
      "-c",
      "ip link set lo up && ip link add boundary0 type dummy && ip link set boundary0 up && ip link del boundary0"
    ], { stdio: "ignore", timeout: 5_000 });
    if (churn.status !== 0 || churn.error) {
      throw new Error("nested network notification probe failed");
    }

    pendingMutationResponse.writeHead(204, { "Cache-Control": "no-store" });
    pendingMutationResponse.end();
    if (await boundedPromise(mutation, 5_000) !== 204 || mutationCount !== 1) {
      throw new Error("loopback mutation was not completed exactly once");
    }
    await browser.close();
    browser = null;
    await closeServer(server);
    console.log("Playwright private network boundary validated with one exactly-once mutation.");
  } finally {
    if (pendingMutationResponse && !pendingMutationResponse.writableEnded) pendingMutationResponse.destroy();
    if (browser) await browser.close().catch(() => {});
    if (server.listening) await closeServer(server).catch(() => {});
  }
}

function assertPrivateNetworkBoundary() {
  if (process.platform !== "linux") return;
  const hostNetworkNamespace = process.env[hostNetworkNamespaceVariable] || "";
  const currentNetworkNamespace = readNetworkNamespace();
  if (!hostNetworkNamespace || hostNetworkNamespace === currentNetworkNamespace) {
    throw new Error("[site:network-boundary-identity] Playwright did not enter a private network namespace");
  }
  const linkState = spawnSync("ip", ["-j", "link", "show"], { encoding: "utf8", timeout: 5_000 });
  if (linkState.status !== 0 || linkState.error) {
    throw new Error("[site:network-boundary-inspection] Playwright network boundary could not be inspected");
  }
  const interfaces = JSON.parse(linkState.stdout);
  if (interfaces.length !== 1 || interfaces[0].ifname !== "lo" || !interfaces[0].flags.includes("UP")) {
    throw new Error("[site:network-boundary-interfaces] Playwright network boundary is not loopback-only");
  }
}

function readNetworkNamespace() {
  try {
    return fs.readlinkSync("/proc/self/ns/net");
  } catch (_error) {
    return "";
  }
}

function listenOnLoopback(server) {
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      server.off("error", reject);
      resolve(server.address().port);
    });
  });
}

function closeServer(server) {
  return new Promise((resolve, reject) => {
    server.close((error) => error ? reject(error) : resolve());
  });
}

function boundedPromise(promise, timeoutMs) {
  return Promise.race([
    promise,
    delay(timeoutMs).then(() => {
      throw new Error("bounded network boundary operation timed out");
    })
  ]);
}

function signalProcess(pid, signal) {
  if (!Number.isInteger(pid) || pid <= 0) return;
  try {
    process.kill(pid, signal);
  } catch (error) {
    if (!error || error.code !== "ESRCH") {
      // The child exit event determines the final result.
    }
  }
}

function signalProcessGroup(pid, signal) {
  if (!Number.isInteger(pid) || pid <= 0) return;
  try {
    process.kill(-pid, signal);
  } catch (error) {
    if (!error || error.code !== "ESRCH") {
      // The bounded process-group exit check determines the final result.
    }
  }
}

async function waitForProcessGroupExit(pid, timeoutMs) {
  if (!Number.isInteger(pid) || pid <= 0) return true;
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      process.kill(-pid, 0);
    } catch (error) {
      if (error && error.code === "ESRCH") return true;
    }
    await delay(50);
  }
  return false;
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
