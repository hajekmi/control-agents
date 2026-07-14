"use strict";

const fs = require("node:fs");

const sandboxProcessSite = "[site:chromium-sandbox-process]";
const sandboxArgumentsSite = "[site:chromium-sandbox-arguments]";
const sandboxIsolationSite = "[site:chromium-sandbox-isolation]";
const heldCapabilityFields = ["capInh", "capPrm", "capEff", "capAmb"];
const allCapabilityFields = [...heldCapabilityFields, "capBnd"];

class ChromiumSandboxError extends Error {
  constructor(site, retryable = false) {
    super(site);
    this.name = "ChromiumSandboxError";
    this.site = site;
    this.retryable = retryable;
  }
}

async function assertChromiumSandboxedProcess(rootPID = process.pid, timeoutMs = 5_000) {
  if (process.platform !== "linux") return;
  const expectedIdentity = currentIdentity();
  const deadline = Date.now() + timeoutMs;
  let lastError = new ChromiumSandboxError(sandboxProcessSite, true);
  while (Date.now() < deadline) {
    try {
      validateChromiumSandboxSnapshot(readProcessSnapshot(), rootPID, expectedIdentity);
      return;
    } catch (error) {
      if (!(error instanceof ChromiumSandboxError) || !error.retryable) throw error;
      lastError = error;
    }
    await delay(25);
  }
  throw lastError;
}

function validateChromiumSandboxSnapshot(snapshot, rootPID, expectedIdentity) {
  const ownedProcesses = descendantProcesses(snapshot, rootPID);
  const browsers = ownedProcesses.filter((entry) =>
    entry.commandLine.includes("--remote-debugging-pipe") && !entry.commandLine.includes("--type="));
  if (browsers.length === 0) throw new ChromiumSandboxError(sandboxProcessSite, true);

  for (const browser of browsers) {
    const browserProcesses = descendantProcesses(ownedProcesses, browser.pid);
    if (browserProcesses.some((entry) => hasExactArgument(entry.commandLine, "--no-sandbox"))) {
      throw new ChromiumSandboxError(sandboxArgumentsSite);
    }

    const renderers = browserProcesses.filter((entry) => hasExactArgument(entry.commandLine, "--type=renderer"));
    if (renderers.length === 0) throw new ChromiumSandboxError(sandboxProcessSite, true);
    if (!validBrowserProcess(browser, expectedIdentity) || browser.noNewPrivs !== 1) {
      throw new ChromiumSandboxError(sandboxIsolationSite);
    }

    for (const renderer of renderers) {
      if (!validRendererProcess(renderer, browser) ||
          renderer.noNewPrivs !== 1 ||
          renderer.seccomp !== 2 ||
          !differentNamespaces(browser.namespaces, renderer.namespaces)) {
        throw new ChromiumSandboxError(sandboxIsolationSite);
      }
    }
  }
}

function readProcessSnapshot() {
  const snapshot = [];
  for (const name of fs.readdirSync("/proc")) {
    if (!/^[1-9][0-9]*$/.test(name)) continue;
    const pid = Number(name);
    try {
      const stat = fs.readFileSync(`/proc/${pid}/stat`, "utf8");
      const commandEnd = stat.lastIndexOf(")");
      if (commandEnd < 0) continue;
      const statFields = stat.slice(commandEnd + 2).split(/\s+/);
      const status = fs.readFileSync(`/proc/${pid}/status`, "utf8");
      snapshot.push({
        pid,
        ppid: Number(statFields[1]),
        commandLine: fs.readFileSync(`/proc/${pid}/cmdline`).toString("utf8").replace(/\0/g, " ").trim(),
        uid: statusField(status, "Uid").split(/\s+/).map(Number),
        gid: statusField(status, "Gid").split(/\s+/).map(Number),
        groups: statusGroups(status),
        capInh: statusField(status, "CapInh"),
        capPrm: statusField(status, "CapPrm"),
        capEff: statusField(status, "CapEff"),
        capBnd: statusField(status, "CapBnd"),
        capAmb: statusField(status, "CapAmb"),
        noNewPrivs: Number(statusField(status, "NoNewPrivs")),
        seccomp: Number(statusField(status, "Seccomp")),
        namespaces: {
          user: fs.readlinkSync(`/proc/${pid}/ns/user`),
          pid: fs.readlinkSync(`/proc/${pid}/ns/pid`),
          net: fs.readlinkSync(`/proc/${pid}/ns/net`)
        }
      });
    } catch (_error) {
      // Processes may exit between the bounded /proc reads.
    }
  }
  return snapshot;
}

function descendantProcesses(snapshot, rootPID) {
  const descendants = new Set([rootPID]);
  let changed = true;
  while (changed) {
    changed = false;
    for (const entry of snapshot) {
      if (!descendants.has(entry.pid) && descendants.has(entry.ppid)) {
        descendants.add(entry.pid);
        changed = true;
      }
    }
  }
  return snapshot.filter((entry) => descendants.has(entry.pid));
}

function currentIdentity() {
  const uid = process.getuid();
  const gid = process.getgid();
  const status = fs.readFileSync("/proc/self/status", "utf8");
  return {
    gid,
    groups: uniqueSortedNumbers(statusGroups(status)),
    uid
  };
}

function validBrowserProcess(entry, expectedIdentity) {
  return validExpectedIdentity(expectedIdentity) &&
    validExactIDs(entry.uid, expectedIdentity.uid) &&
    validExactIDs(entry.gid, expectedIdentity.gid) &&
    validGroups(entry.groups) &&
    equalNumberLists(uniqueSortedNumbers(entry.groups), expectedIdentity.groups) &&
    allCapabilityFields.every((field) => zeroCapabilityMask(entry[field]));
}

function validRendererProcess(entry, browser) {
  const heldCapabilitiesAreZero = heldCapabilityFields.every((field) => zeroCapabilityMask(entry[field]));
  const distinctUserNamespace = differentNamespace(browser.namespaces, entry.namespaces, "user");
  return validMappedIDs(entry.uid) &&
    validMappedIDs(entry.gid) &&
    validGroups(entry.groups) &&
    heldCapabilitiesAreZero &&
    validCapabilityMask(entry.capBnd) &&
    (zeroCapabilityMask(entry.capBnd) ||
      (distinctUserNamespace && heldCapabilitiesAreZero && entry.noNewPrivs === 1));
}

function validExpectedIdentity(identity) {
  return identity &&
    Number.isInteger(identity.uid) && identity.uid > 0 &&
    Number.isInteger(identity.gid) && identity.gid > 0 &&
    validGroups(identity.groups);
}

function validExactIDs(values, expected) {
  return validMappedIDs(values) && values.every((value) => value === expected);
}

function validMappedIDs(values) {
  return Array.isArray(values) &&
    values.length === 4 &&
    values.every((value) => Number.isInteger(value) && value > 0);
}

function validGroups(groups) {
  return Array.isArray(groups) &&
    groups.every((value) => Number.isInteger(value) && value > 0);
}

function validCapabilityMask(value) {
  return typeof value === "string" && /^[0-9a-fA-F]+$/.test(value);
}

function zeroCapabilityMask(value) {
  return validCapabilityMask(value) && /^0+$/.test(value);
}

function differentNamespaces(parent, child) {
  if (!parent || !child) return false;
  return ["user", "pid", "net"].every((name) =>
    differentNamespace(parent, child, name));
}

function differentNamespace(parent, child, name) {
  return parent && child &&
    typeof parent[name] === "string" && parent[name].length > 0 &&
    typeof child[name] === "string" && child[name].length > 0 &&
    parent[name] !== child[name];
}

function hasExactArgument(commandLine, argument) {
  return new RegExp(`(?:^| )${escapeRegex(argument)}(?: |$)`).test(commandLine);
}

function statusField(status, name) {
  const match = status.match(new RegExp(`^${name}:\\s+([^\\n]+)$`, "m"));
  if (!match) throw new ChromiumSandboxError(sandboxProcessSite, true);
  return match[1].trim();
}

function statusGroups(status) {
  const match = status.match(/^Groups:\s*([^\n]*)$/m);
  if (!match) throw new ChromiumSandboxError(sandboxProcessSite, true);
  const value = match[1].trim();
  if (!value) return [];
  const groups = value.split(/\s+/).map(Number);
  if (groups.some((group) => !Number.isInteger(group))) {
    throw new ChromiumSandboxError(sandboxProcessSite, true);
  }
  return groups;
}

function uniqueSortedNumbers(values) {
  return [...new Set(values)].sort((left, right) => left - right);
}

function equalNumberLists(left, right) {
  return left.length === right.length && left.every((value, index) => value === right[index]);
}

function escapeRegex(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

module.exports = {
  ChromiumSandboxError,
  assertChromiumSandboxedProcess,
  validateChromiumSandboxSnapshot
};
