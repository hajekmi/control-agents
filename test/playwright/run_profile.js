"use strict";

const fs = require("node:fs");
const http = require("node:http");
const path = require("node:path");
const { spawn, spawnSync } = require("node:child_process");
const { ChromiumSandboxError, assertChromiumSandboxedProcess } = require("./chromium_sandbox.js");
const {
  BoundaryError,
  assertNoPrivilegeRegain,
  assertPrivateNetworkBoundary,
  capturePromiseOutcome,
  launchPrivateNetworkBoundary,
  markerVariable: networkBoundaryMarker,
  networkChurnArgument,
  recordBoundaryFailure,
  requestBoundaryChurn,
  restoreBoundaryEnvironment,
  selectedLauncherMode,
  signalBoundaryReady
} = require("./network_boundary.js");

const repoRoot = path.resolve(__dirname, "../..");
const networkModeVariable = "CONTROL_AGENTS_PLAYWRIGHT_NETWORK_MODE";
const networkProbeArgument = "--network-boundary-probe";

main().catch((error) => {
  const site = error instanceof BoundaryError ? error.site : "[site:network-boundary-launcher]";
  console.error(site);
  process.exitCode = 1;
});

async function main() {
  const args = process.argv.slice(2);
  if (process.platform === "linux" && process.env[networkBoundaryMarker] !== "1") {
    process.exitCode = await launchPrivateNetworkBoundary({
      args,
      operation: "profile",
      repoRoot,
      requestedMode: process.env[networkModeVariable],
      runProfilePath: __filename
    });
    return;
  }

  if (process.platform === "linux") {
    try {
      restoreBoundaryEnvironment();
      assertPrivateNetworkBoundary();
      await signalBoundaryReady();
    } catch (error) {
      recordBoundaryFailure(error);
      process.exitCode = 1;
      return;
    }
  }

  if (args.includes(networkChurnArgument)) return;
  if (args.includes(networkProbeArgument)) {
    try {
      await validateNetworkBoundary();
    } catch (error) {
      console.error(error instanceof ChromiumSandboxError
        ? error.site
        : "[site:network-boundary-probe] Playwright network boundary probe failed");
      process.exitCode = 1;
    }
    return;
  }

  process.exitCode = await runPlaywrightProfile();
}

function runPlaywrightProfile() {
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

  return new Promise((resolve) => {
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
        resolve(1);
        return;
      }
      resolve(exitCode);
    };

    profile.on("error", async () => {
      await finishProfile(1);
    });
    profile.on("exit", async (code, signal) => {
      const exitCode = Number.isInteger(code) ? code : (signal || forwardedSignal ? 1 : 0);
      await finishProfile(exitCode);
    });
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
  assertNoPrivilegeRegain(selectedLauncherMode());
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
  let mutation = null;
  let mutationSettlement = null;
  try {
    const port = await listenOnLoopback(server);
    const { chromium } = require("@playwright/test");
    browser = await chromium.launch({ chromiumSandbox: true });
    const page = await browser.newPage();
    await page.goto(`http://127.0.0.1:${port}/`, { waitUntil: "domcontentloaded" });
    await assertChromiumSandboxedProcess();
    mutationSettlement = new AbortController();
    mutation = capturePromiseOutcome(page.evaluate(async () => {
      const response = await fetch("/mutation", { method: "POST", body: "" });
      return response.status;
    }), mutationSettlement.signal);
    await boundedPromise(mutationReceived, 5_000);

    await requestBoundaryChurn();

    pendingMutationResponse.writeHead(204, { "Cache-Control": "no-store" });
    pendingMutationResponse.end();
    const mutationOutcome = await boundedPromise(mutation, 5_000);
    if (!mutationOutcome.fulfilled || mutationOutcome.value !== 204 || mutationCount !== 1) {
      throw new Error("loopback mutation was not completed exactly once");
    }
    await browser.close();
    browser = null;
    await closeServer(server);
    console.log("Playwright private network boundary validated with one exactly-once mutation.");
  } finally {
    if (pendingMutationResponse && !pendingMutationResponse.writableEnded) pendingMutationResponse.destroy();
    if (mutationSettlement) mutationSettlement.abort();
    if (mutation) await mutation;
    if (browser) await boundedPromise(browser.close(), 5_000).catch(() => {});
    if (server.listening) await boundedPromise(closeServer(server), 5_000).catch(() => {});
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
