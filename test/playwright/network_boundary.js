"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { spawn, spawnSync } = require("node:child_process");

const markerVariable = "CONTROL_AGENTS_PLAYWRIGHT_PRIVATE_NETWORK";
const hostNamespaceVariable = "CONTROL_AGENTS_PLAYWRIGHT_HOST_NETWORK_NAMESPACE";
const expectedUIDVariable = "CONTROL_AGENTS_PLAYWRIGHT_EXPECTED_UID";
const expectedGIDVariable = "CONTROL_AGENTS_PLAYWRIGHT_EXPECTED_GID";
const expectedGroupsVariable = "CONTROL_AGENTS_PLAYWRIGHT_EXPECTED_GROUPS";
const selectedModeVariable = "CONTROL_AGENTS_PLAYWRIGHT_SELECTED_NETWORK_MODE";
const readyPathVariable = "CONTROL_AGENTS_PLAYWRIGHT_BOUNDARY_READY";
const startPathVariable = "CONTROL_AGENTS_PLAYWRIGHT_BOUNDARY_START";
const failurePathVariable = "CONTROL_AGENTS_PLAYWRIGHT_BOUNDARY_FAILURE";
const churnRequestPathVariable = "CONTROL_AGENTS_PLAYWRIGHT_BOUNDARY_CHURN_REQUEST";
const churnResultPathVariable = "CONTROL_AGENTS_PLAYWRIGHT_BOUNDARY_CHURN_RESULT";
const expectedAppArmorProfileVariable = "CONTROL_AGENTS_PLAYWRIGHT_EXPECTED_APPARMOR_PROFILE";
const networkChurnArgument = "--network-boundary-churn-child";

const churnRequestContent = "churn\n";
const churnSuccessContent = "ok\n";
const churnFailureContent = "failed\n";

const fixedFailureSites = new Set([
  "[site:network-boundary-bootstrap-mode]",
  "[site:network-boundary-bootstrap-operation]",
  "[site:network-boundary-bootstrap-identity]",
  "[site:network-boundary-bootstrap-groups]",
  "[site:network-boundary-bootstrap-privilege]",
  "[site:network-boundary-bootstrap-loopback]",
  "[site:network-boundary-bootstrap-churn]",
  "[site:network-boundary-bootstrap-apparmor]",
  "[site:network-boundary-identity]",
  "[site:network-boundary-inspection]",
  "[site:network-boundary-interfaces]",
  "[site:network-boundary-routes]",
  "[site:network-boundary-user]",
  "[site:network-boundary-groups]",
  "[site:network-boundary-capabilities]",
  "[site:network-boundary-apparmor]",
  "[site:network-boundary-environment]",
  "[site:network-boundary-readiness]",
  "[site:network-boundary-churn]"
]);

class BoundaryError extends Error {
  constructor(site) {
    super(site);
    this.name = "BoundaryError";
    this.site = site;
  }
}

function parseLauncherMode(value) {
  const mode = value || "auto";
  if (!["auto", "unprivileged", "sudo"].includes(mode)) {
    throw new BoundaryError("[site:network-boundary-mode]");
  }
  return mode;
}

async function runLauncherSequence(requestedMode, attempt) {
  const mode = parseLauncherMode(requestedMode);
  const modes = mode === "auto" ? ["unprivileged", "sudo"] : [mode];
  const attempts = [];
  for (const selectedMode of modes) {
    const result = await attempt(selectedMode);
    attempts.push(selectedMode);
    const fallbackEligible = result.fallbackEligible && !result.cancelled && !result.signal;
    if (result.ready || !fallbackEligible || selectedMode === modes[modes.length - 1]) {
      return { ...result, attempts, selectedMode };
    }
  }
  throw new BoundaryError("[site:network-boundary-readiness]");
}

async function launchPrivateNetworkBoundary(options) {
  if (process.platform !== "linux") {
    throw new BoundaryError("[site:network-boundary-platform]");
  }
  const identity = originalIdentity();
  const requestedMode = parseLauncherMode(options.requestedMode);
  const result = await runLauncherSequence(requestedMode, (selectedMode) => attemptBoundary({
    args: options.args,
    coordinateChurn: (options.operation || "profile") === "profile",
    identity,
    operation: options.operation || "profile",
    repoRoot: options.repoRoot,
    runProfilePath: options.runProfilePath,
    selectedMode
  }));

  if (!result.ready) {
    const site = result.failureSite || (result.selectedMode === "sudo"
      ? "[site:network-boundary-sudo-unavailable]"
      : "[site:network-boundary-unprivileged-denied]");
    console.error(site);
    return 1;
  }
  return result.exitCode;
}

async function attemptBoundary(options) {
  let boundaryDir = "";
  let readyPath = "";
  let startPath = "";
  let failurePath = "";
  let churnRequestPath = "";
  let churnResultPath = "";
  let child;
  let ready = false;
  let forwardedSignal = "";
  let exitResult = null;
  let spawnFailed = false;
  let cancelled = Boolean(options.signal && options.signal.aborted);
  let timedOut = false;
  let preReadyDiagnostics = "";
  let result = null;
  let churnController = null;
  let churnPromise = null;
  const signalHandlers = new Map();
  let abortHandler = null;

  try {
    boundaryDir = fs.mkdtempSync(path.join(options.repoRoot, ".cache", "pwn-"));
    fs.chmodSync(boundaryDir, 0o700);
    readyPath = path.join(boundaryDir, "ready");
    startPath = path.join(boundaryDir, "start");
    failurePath = path.join(boundaryDir, "failure");
    churnRequestPath = path.join(boundaryDir, "churn-request");
    churnResultPath = path.join(boundaryDir, "churn-result");
    const failureDescriptor = fs.openSync(failurePath, "wx", 0o600);
    fs.closeSync(failureDescriptor);

    const requestCancellation = (signal) => {
      cancelled = true;
      if (signal) forwardedSignal = signal;
      if (child) signalProcess(child.pid, signal || "SIGTERM");
      if (churnController) churnController.abort();
    };
    for (const signal of ["SIGINT", "SIGTERM"]) {
      const handler = () => requestCancellation(signal);
      signalHandlers.set(signal, handler);
      process.on(signal, handler);
    }
    if (options.signal) {
      abortHandler = () => requestCancellation("SIGTERM");
      options.signal.addEventListener("abort", abortHandler, { once: true });
    }

    if (cancelled) {
      result = cancelledAttemptResult();
      return result;
    }

    const environment = options.environment || process.env;
    const environmentHandoff = encodeEnvironmentHandoff(environment);
    const command = buildBoundaryCommand({
      ...options,
      churnRequestPath: options.coordinateChurn ? churnRequestPath : "",
      churnResultPath: options.coordinateChurn ? churnResultPath : "",
      failurePath,
      hostNamespace: readNetworkNamespace(),
      readyPath,
      startPath
    });
    const spawnChild = options.spawnImpl || spawn;
    child = spawnChild(command.file, command.args, {
      cwd: options.repoRoot,
      env: fixedBootstrapEnvironment(),
      stdio: ["pipe", "pipe", "pipe"]
    });
    child.stdin.on("error", () => {
      // A pre-ready launcher exit is classified through the fixed boundary site.
    });
    child.stdin.end(environmentHandoff);
    child.stdout.on("data", (chunk) => {
      if (ready) process.stdout.write(chunk);
    });
    child.stderr.on("data", (chunk) => {
      if (ready) {
        process.stderr.write(chunk);
      } else if (preReadyDiagnostics.length < 1_024) {
        preReadyDiagnostics += chunk.toString("utf8", 0, 1_024 - preReadyDiagnostics.length);
      }
    });
    child.once("error", () => {
      spawnFailed = true;
    });
    child.once("exit", (code, signal) => {
      exitResult = {
        exitCode: Number.isInteger(code) ? code : (signal || forwardedSignal ? 1 : 0),
        signal
      };
    });

    const readiness = await waitForReadiness({
      child,
      expectedUID: options.identity.uid,
      isCancelled: () => cancelled,
      isExited: () => exitResult !== null || spawnFailed,
      readyPath,
      timeoutMs: options.readinessTimeoutMs || 10_000
    });
    ready = readiness.ready;

    if (ready && !cancelled) {
      fs.writeFileSync(startPath, "start\n", { encoding: "utf8", flag: "wx", mode: 0o600 });
    } else if (!exitResult && !spawnFailed) {
      await stopChild(child);
    }

    const runtimeDeadline = ready && options.runtimeTimeoutMs
      ? Date.now() + options.runtimeTimeoutMs
      : 0;
    while (!exitResult && !spawnFailed) {
      if (cancelled) {
        await stopChild(child);
      } else if (runtimeDeadline && Date.now() >= runtimeDeadline) {
        timedOut = true;
        await stopChild(child);
      } else if (ready && options.coordinateChurn && !churnPromise) {
        const requestState = fixedFileState(churnRequestPath, churnRequestContent, options.identity.uid);
        if (requestState !== "missing") {
          churnController = new AbortController();
          churnPromise = settleChurnRequest({
            ...options,
            requestValid: requestState === "valid",
            resultPath: churnResultPath,
            signal: churnController.signal
          });
        }
      }
      if (!exitResult && !spawnFailed) await delay(10);
    }

    const signalled = Boolean(exitResult && exitResult.signal);
    result = {
      exitCode: exitResult ? exitResult.exitCode : 1,
      cancelled: cancelled || signalled,
      fallbackEligible: readiness.exited && Boolean(exitResult) && exitResult.exitCode !== 0 && !signalled && !cancelled,
      failureSite: readFixedFailureSite(failurePath) || readFixedFailureDiagnostic(preReadyDiagnostics) ||
        (spawnFailed ? "[site:network-boundary-readiness]" : ""),
      ready,
      signal: exitResult ? exitResult.signal : null,
      timedOut
    };
  } catch (error) {
    result = {
      exitCode: 1,
      cancelled,
      fallbackEligible: false,
      failureSite: error instanceof BoundaryError ? error.site : "[site:network-boundary-readiness]",
      ready,
      signal: exitResult ? exitResult.signal : null,
      timedOut
    };
  } finally {
    if (churnController) churnController.abort();
    if (churnPromise) await churnPromise.catch(() => {});
    if (child && child.exitCode === null && child.signalCode === null) await stopChild(child);
    if (options.signal && abortHandler) options.signal.removeEventListener("abort", abortHandler);
    for (const [signal, handler] of signalHandlers) process.off(signal, handler);
    if (boundaryDir) fs.rmSync(boundaryDir, { force: true, recursive: true });
  }
  return result || cancelledAttemptResult();
}

function cancelledAttemptResult() {
  return {
    exitCode: 1,
    cancelled: true,
    fallbackEligible: false,
    failureSite: "[site:network-boundary-readiness]",
    ready: false,
    signal: "SIGTERM",
    timedOut: false
  };
}

function buildBoundaryCommand(options) {
  const bootstrapPath = path.join(path.dirname(options.runProfilePath), "network_boundary_bootstrap.sh");
  const commonUnshareArguments = ["--kill-child=SIGTERM", "--net", "--fork"];
  let file;
  let args;
  if (options.selectedMode === "unprivileged") {
    file = "/usr/bin/unshare";
    args = [
      "--user",
      "--map-current-user",
      "--keep-caps",
      ...commonUnshareArguments,
      "/bin/sh",
      bootstrapPath,
      "unprivileged"
    ];
  } else if (options.selectedMode === "sudo") {
    file = "/usr/bin/sudo";
    args = ["-n", "--", "/bin/sh", bootstrapPath, "sudo-launch"];
  } else {
    throw new BoundaryError("[site:network-boundary-mode]");
  }

  const environmentArguments = [
    "-i",
    `${markerVariable}=1`,
    `${hostNamespaceVariable}=${options.hostNamespace}`,
    `${expectedUIDVariable}=${options.identity.uid}`,
    `${expectedGIDVariable}=${options.identity.gid}`,
    `${expectedGroupsVariable}=${options.identity.groups.join(",")}`,
    `${selectedModeVariable}=${options.selectedMode}`,
    `${readyPathVariable}=${options.readyPath}`,
    `${startPathVariable}=${options.startPath}`,
    `${failurePathVariable}=${options.failurePath}`
  ];
  if (options.churnRequestPath && options.churnResultPath) {
    environmentArguments.push(
      `${churnRequestPathVariable}=${options.churnRequestPath}`,
      `${churnResultPathVariable}=${options.churnResultPath}`
    );
  }
  if (options.selectedMode === "sudo") {
    environmentArguments.push(`${expectedAppArmorProfileVariable}=chrome`);
  }

  args.push(
    options.operation,
    String(options.identity.uid),
    String(options.identity.gid),
    options.identity.groups.join(","),
    "/usr/bin/env",
    ...environmentArguments,
    process.execPath,
    options.runProfilePath,
    ...options.args
  );
  return { args, file };
}

function originalIdentity() {
  if (typeof process.getuid !== "function" || typeof process.getgid !== "function" || process.getuid() <= 0 || process.getgid() <= 0) {
    throw new BoundaryError("[site:network-boundary-caller]");
  }
  const uid = process.getuid();
  const gid = process.getgid();
  const groups = uniqueSortedNumbers([...process.getgroups(), gid]);
  return { gid, groups, uid };
}

function restoreBoundaryEnvironment() {
  const internalEnvironment = {};
  for (const name of [
    markerVariable,
    hostNamespaceVariable,
    expectedUIDVariable,
    expectedGIDVariable,
    expectedGroupsVariable,
    selectedModeVariable,
    readyPathVariable,
    startPathVariable,
    failurePathVariable,
    churnRequestPathVariable,
    churnResultPathVariable,
    expectedAppArmorProfileVariable
  ]) {
    internalEnvironment[name] = process.env[name];
  }

  let restoredEnvironment;
  try {
    const header = readExact(0, 4);
    const length = header.readUInt32BE(0);
    if (length === 0 || length > 1_048_576) throw new Error("invalid environment handoff");
    restoredEnvironment = JSON.parse(readExact(0, length).toString("utf8"));
  } catch (_error) {
    throw new BoundaryError("[site:network-boundary-environment]");
  }
  if (!restoredEnvironment || Array.isArray(restoredEnvironment) || typeof restoredEnvironment !== "object" ||
      Object.entries(restoredEnvironment).some(([name, value]) => !validEnvironmentEntry(name, value))) {
    throw new BoundaryError("[site:network-boundary-environment]");
  }

  for (const name of Object.keys(process.env)) delete process.env[name];
  Object.assign(process.env, restoredEnvironment, internalEnvironment);
}

function encodeEnvironmentHandoff(environment = process.env) {
  const snapshot = {};
  for (const [name, value] of Object.entries(environment)) {
    if (name.startsWith("CONTROL_AGENTS_PLAYWRIGHT_BOUNDARY_")) continue;
    if (!validEnvironmentEntry(name, value)) {
      throw new BoundaryError("[site:network-boundary-environment]");
    }
    snapshot[name] = value;
  }
  const payload = Buffer.from(JSON.stringify(snapshot), "utf8");
  if (payload.length === 0 || payload.length > 1_048_576) {
    throw new BoundaryError("[site:network-boundary-environment]");
  }
  const header = Buffer.alloc(4);
  header.writeUInt32BE(payload.length, 0);
  return Buffer.concat([header, payload]);
}

function validEnvironmentEntry(name, value) {
  return typeof name === "string" && name.length > 0 && !name.includes("=") && !name.includes("\0") &&
    typeof value === "string" && !value.includes("\0");
}

function fixedBootstrapEnvironment() {
  return {
    LANG: "C.UTF-8",
    LC_ALL: "C.UTF-8",
    PATH: "/usr/sbin:/usr/bin:/sbin:/bin"
  };
}

function readExact(descriptor, length) {
  const result = Buffer.alloc(length);
  let offset = 0;
  while (offset < length) {
    const count = fs.readSync(descriptor, result, offset, length - offset, null);
    if (count === 0) throw new Error("unexpected environment handoff end");
    offset += count;
  }
  return result;
}

function assertPrivateNetworkBoundary() {
  if (process.platform !== "linux") return;
  const hostNamespace = process.env[hostNamespaceVariable] || "";
  const currentNamespace = readNetworkNamespace();
  if (!hostNamespace || !currentNamespace || hostNamespace === currentNamespace) {
    throw new BoundaryError("[site:network-boundary-identity]");
  }

  const linkState = spawnSync("/usr/sbin/ip", ["-j", "link", "show"], { encoding: "utf8", timeout: 5_000 });
  if (linkState.status !== 0 || linkState.error) {
    throw new BoundaryError("[site:network-boundary-inspection]");
  }
  let interfaces;
  try {
    interfaces = JSON.parse(linkState.stdout);
  } catch (_error) {
    throw new BoundaryError("[site:network-boundary-inspection]");
  }
  if (interfaces.length !== 1 || interfaces[0].ifname !== "lo" || !interfaces[0].flags.includes("UP")) {
    throw new BoundaryError("[site:network-boundary-interfaces]");
  }

  assertLoopbackOnlyRoutes();

  assertBoundaryIdentityAndCapabilities();
}

function assertLoopbackOnlyRoutes(runCommand = spawnSync) {
  for (const family of ["-4", "-6"]) {
    const routeState = runCommand(
      "/usr/sbin/ip",
      ["-j", family, "route", "show", "table", "all"],
      { encoding: "utf8", timeout: 5_000 }
    );
    if (!routeState || routeState.status !== 0 || routeState.error) {
      throw new BoundaryError("[site:network-boundary-inspection]");
    }
    let routes;
    try {
      routes = JSON.parse(routeState.stdout);
    } catch (_error) {
      throw new BoundaryError("[site:network-boundary-inspection]");
    }
    if (!Array.isArray(routes)) {
      throw new BoundaryError("[site:network-boundary-inspection]");
    }
    if (routes.some((route) => !route || route.dst === "default" || route.dev !== "lo")) {
      throw new BoundaryError("[site:network-boundary-routes]");
    }
  }
}

function assertBoundaryIdentityAndCapabilities() {
  const expectedUID = parsePositiveInteger(process.env[expectedUIDVariable]);
  const expectedGID = parsePositiveInteger(process.env[expectedGIDVariable]);
  const expectedGroups = parseNumberList(process.env[expectedGroupsVariable]);
  const selectedMode = process.env[selectedModeVariable];
  const status = readProcessStatus();
  if (!expectedUID || !expectedGID || process.getuid() !== expectedUID || process.geteuid() !== expectedUID ||
      process.getgid() !== expectedGID || process.getegid() !== expectedGID ||
      status.uid.some((value) => value !== expectedUID) || status.gid.some((value) => value !== expectedGID)) {
    throw new BoundaryError("[site:network-boundary-user]");
  }

  const actualGroups = uniqueSortedNumbers(process.getgroups());
  if (selectedMode === "sudo") {
    if (!equalNumberLists(actualGroups, expectedGroups)) {
      throw new BoundaryError("[site:network-boundary-groups]");
    }
  } else if (selectedMode === "unprivileged") {
    if (!actualGroups.includes(expectedGID) || actualGroups.includes(0)) {
      throw new BoundaryError("[site:network-boundary-groups]");
    }
  } else {
    throw new BoundaryError("[site:network-boundary-user]");
  }

  for (const capability of [status.capInh, status.capPrm, status.capEff, status.capBnd, status.capAmb]) {
    if (!/^0+$/.test(capability)) {
      throw new BoundaryError("[site:network-boundary-capabilities]");
    }
  }
  assertNoNewPrivilegesStatus(status);

  if (selectedMode === "sudo") {
    const expectedProfile = process.env[expectedAppArmorProfileVariable];
    if (expectedProfile !== "chrome" || !currentAppArmorProfile(expectedProfile)) {
      throw new BoundaryError("[site:network-boundary-apparmor]");
    }
  }
}

function assertNoNewPrivilegesStatus(status) {
  if (!status || status.noNewPrivs !== 1) {
    throw new BoundaryError("[site:network-boundary-capabilities]");
  }
}

function currentAppArmorProfile(expectedProfile) {
  try {
    const current = fs.readFileSync("/proc/self/attr/current", "utf8").trim();
    return current === `${expectedProfile} (unconfined)` || current === `${expectedProfile} (enforce)`;
  } catch (_error) {
    return false;
  }
}

function assertNoPrivilegeRegain(selectedMode, runCommand = spawnSync) {
  if (selectedMode !== "sudo") return;
  const result = runCommand(
    "/usr/bin/sudo",
    ["-n", "--", "/usr/bin/id", "-u"],
    { env: fixedBootstrapEnvironment(), stdio: "ignore", timeout: 5_000 }
  );
  if (!result || result.error || result.signal || result.status === 0) {
    throw new BoundaryError("[site:network-boundary-capabilities]");
  }
}

async function signalBoundaryReady() {
  const readyPath = process.env[readyPathVariable] || "";
  const startPath = process.env[startPathVariable] || "";
  if (!readyPath || !startPath) throw new BoundaryError("[site:network-boundary-readiness]");
  fs.writeFileSync(readyPath, "ready\n", { encoding: "utf8", flag: "wx", mode: 0o600 });
  const deadline = Date.now() + 5_000;
  while (Date.now() < deadline) {
    if (readFixedFile(startPath, "start\n")) return;
    await delay(10);
  }
  throw new BoundaryError("[site:network-boundary-readiness]");
}

async function requestBoundaryChurn() {
  const requestPath = process.env[churnRequestPathVariable] || "";
  const resultPath = process.env[churnResultPathVariable] || "";
  const expectedUID = parsePositiveInteger(process.env[expectedUIDVariable]);
  if (!requestPath || !resultPath || !expectedUID) {
    throw new BoundaryError("[site:network-boundary-churn]");
  }
  try {
    fs.writeFileSync(requestPath, churnRequestContent, { encoding: "utf8", flag: "wx", mode: 0o600 });
  } catch (_error) {
    throw new BoundaryError("[site:network-boundary-churn]");
  }
  const deadline = Date.now() + 20_000;
  while (Date.now() < deadline) {
    if (readFixedFile(resultPath, churnSuccessContent, expectedUID)) return;
    if (readFixedFile(resultPath, churnFailureContent, expectedUID)) {
      throw new BoundaryError("[site:network-boundary-churn]");
    }
    await delay(10);
  }
  throw new BoundaryError("[site:network-boundary-churn]");
}

async function settleChurnRequest(options) {
  let success = false;
  if (options.requestValid && !options.signal.aborted) {
    try {
      const launchChurn = options.launchChurn || ((churnOptions) => attemptBoundary(churnOptions));
      const result = await launchChurn({
        args: [networkChurnArgument],
        coordinateChurn: false,
        identity: options.identity,
        operation: "churn",
        readinessTimeoutMs: 10_000,
        repoRoot: options.repoRoot,
        runProfilePath: options.runProfilePath,
        runtimeTimeoutMs: 5_000,
        selectedMode: options.selectedMode,
        signal: options.signal
      });
      success = result.ready && result.exitCode === 0 && !result.cancelled;
    } catch (_error) {
      success = false;
    }
  }
  if (options.signal.aborted) return;
  try {
    fs.writeFileSync(options.resultPath, success ? churnSuccessContent : churnFailureContent, {
      encoding: "utf8",
      flag: "wx",
      mode: 0o600
    });
  } catch (_error) {
    // The requesting child has its own fixed, bounded churn failure site.
  }
}

function recordBoundaryFailure(error) {
  const failurePath = process.env[failurePathVariable] || "";
  const site = error instanceof BoundaryError && fixedFailureSites.has(error.site)
    ? error.site
    : "[site:network-boundary-readiness]";
  if (!failurePath) return;
  try {
    fs.writeFileSync(failurePath, `${site}\n`, { encoding: "utf8", flag: "w", mode: 0o600 });
  } catch (_error) {
    // The parent still reports a fixed launcher failure if this channel is unavailable.
  }
}

function selectedLauncherMode() {
  const mode = process.env[selectedModeVariable];
  if (mode !== "unprivileged" && mode !== "sudo") {
    throw new BoundaryError("[site:network-boundary-user]");
  }
  return mode;
}

function readNetworkNamespace() {
  try {
    return fs.readlinkSync("/proc/self/ns/net");
  } catch (_error) {
    return "";
  }
}

function readProcessStatus() {
  let status;
  try {
    status = fs.readFileSync("/proc/self/status", "utf8");
  } catch (_error) {
    throw new BoundaryError("[site:network-boundary-inspection]");
  }
  const values = {};
  for (const name of ["Uid", "Gid", "CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb"]) {
    const match = status.match(new RegExp(`^${name}:\\s+([^\\n]+)$`, "m"));
    if (!match) throw new BoundaryError("[site:network-boundary-inspection]");
    values[name] = match[1].trim();
  }
  const noNewPrivs = status.match(/^NoNewPrivs:\s+([^\n]+)$/m);
  return {
    capAmb: values.CapAmb,
    capBnd: values.CapBnd,
    capEff: values.CapEff,
    capInh: values.CapInh,
    capPrm: values.CapPrm,
    gid: values.Gid.split(/\s+/).map(Number),
    noNewPrivs: noNewPrivs && /^\s*[01]\s*$/.test(noNewPrivs[1]) ? Number(noNewPrivs[1].trim()) : null,
    uid: values.Uid.split(/\s+/).map(Number)
  };
}

function parsePositiveInteger(value) {
  if (!/^[1-9][0-9]*$/.test(value || "")) return 0;
  return Number(value);
}

function parseNumberList(value) {
  if (!/^[0-9]+(?:,[0-9]+)*$/.test(value || "")) return [];
  return uniqueSortedNumbers(value.split(",").map(Number));
}

function uniqueSortedNumbers(values) {
  return [...new Set(values)].sort((left, right) => left - right);
}

function equalNumberLists(left, right) {
  return left.length === right.length && left.every((value, index) => value === right[index]);
}

async function waitForReadiness(options) {
  const deadline = Date.now() + options.timeoutMs;
  while (Date.now() < deadline) {
    if (options.isCancelled && options.isCancelled()) return { cancelled: true, exited: false, ready: false };
    if (readFixedFile(options.readyPath, "ready\n", options.expectedUID)) return { exited: false, ready: true };
    if (options.isExited()) return { exited: true, ready: false };
    await delay(10);
  }
  return { cancelled: Boolean(options.isCancelled && options.isCancelled()), exited: false, ready: false };
}

function readFixedFile(filePath, expectedContent, expectedUID) {
  try {
    const status = fs.lstatSync(filePath);
    if (!status.isFile() || (status.mode & 0o777) !== 0o600) return false;
    if (Number.isInteger(expectedUID) && status.uid !== expectedUID) return false;
    return fs.readFileSync(filePath, "utf8") === expectedContent;
  } catch (_error) {
    return false;
  }
}

function fixedFileState(filePath, expectedContent, expectedUID) {
  try {
    fs.lstatSync(filePath);
  } catch (_error) {
    return "missing";
  }
  return readFixedFile(filePath, expectedContent, expectedUID) ? "valid" : "invalid";
}

function readFixedFailureSite(filePath) {
  try {
    const site = fs.readFileSync(filePath, "utf8").trim();
    return fixedFailureSites.has(site) ? site : "";
  } catch (_error) {
    return "";
  }
}

function readFixedFailureDiagnostic(diagnostic) {
  for (const line of diagnostic.split(/\r?\n/)) {
    const site = line.trim();
    if (fixedFailureSites.has(site)) return site;
  }
  return "";
}

async function stopChild(child) {
  if (!child || child.exitCode !== null || child.signalCode !== null) return;
  signalProcess(child.pid, "SIGTERM");
  const deadline = Date.now() + 1_000;
  while (Date.now() < deadline && child.exitCode === null && child.signalCode === null) await delay(10);
  if (child.exitCode === null && child.signalCode === null) {
    signalProcess(child.pid, "SIGKILL");
    const killDeadline = Date.now() + 1_000;
    while (Date.now() < killDeadline && child.exitCode === null && child.signalCode === null) await delay(10);
  }
}

function signalProcess(pid, signal) {
  if (!Number.isInteger(pid) || pid <= 0) return;
  try {
    process.kill(pid, signal);
  } catch (error) {
    if (!error || error.code !== "ESRCH") {
      // The bounded child exit check owns the final result.
    }
  }
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

function capturePromiseOutcome(promise, signal) {
  const source = Promise.resolve(promise);
  return new Promise((resolve) => {
    let settled = false;
    const finish = (outcome) => {
      if (settled) return;
      settled = true;
      if (signal) signal.removeEventListener("abort", abort);
      resolve(outcome);
    };
    const abort = () => finish({ fulfilled: false });
    source.then(
      (value) => finish({ fulfilled: true, value }),
      () => finish({ fulfilled: false })
    );
    if (signal) {
      if (signal.aborted) abort();
      else signal.addEventListener("abort", abort, { once: true });
    }
  });
}

module.exports = {
  BoundaryError,
  assertLoopbackOnlyRoutes,
  assertNoNewPrivilegesStatus,
  assertNoPrivilegeRegain,
  assertPrivateNetworkBoundary,
  attemptBoundary,
  buildBoundaryCommand,
  capturePromiseOutcome,
  encodeEnvironmentHandoff,
  fixedBootstrapEnvironment,
  launchPrivateNetworkBoundary,
  markerVariable,
  networkChurnArgument,
  parseLauncherMode,
  readNetworkNamespace,
  recordBoundaryFailure,
  requestBoundaryChurn,
  restoreBoundaryEnvironment,
  runLauncherSequence,
  selectedLauncherMode,
  signalBoundaryReady
};
