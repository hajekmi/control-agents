"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");
const { spawn } = require("node:child_process");
const {
  ChromiumSandboxError,
  validateChromiumSandboxSnapshot
} = require("./chromium_sandbox.js");
const {
  BoundaryError,
  assertLoopbackOnlyRoutes,
  assertNoNewPrivilegesStatus,
  assertNoPrivilegeRegain,
  attemptBoundary,
  buildBoundaryCommand,
  capturePromiseOutcome,
  encodeEnvironmentHandoff,
  fixedBootstrapEnvironment,
  parseLauncherMode,
  runLauncherSequence
} = require("./network_boundary.js");

test("rootless launcher success does not invoke sudo", async () => {
  const attempted = [];
  const result = await runLauncherSequence("auto", async (mode) => {
    attempted.push(mode);
    return { exitCode: 0, ready: true };
  });

  assert.deepEqual(attempted, ["unprivileged"]);
  assert.deepEqual(result.attempts, ["unprivileged"]);
  assert.equal(result.exitCode, 0);
  assert.equal(result.ready, true);
});

test("auto falls back once after an AppArmor-style pre-ready rootless denial", async () => {
  const attempted = [];
  const result = await runLauncherSequence("auto", async (mode) => {
    attempted.push(mode);
    if (mode === "unprivileged") return { exitCode: 1, fallbackEligible: true, ready: false };
    return { exitCode: 0, ready: true };
  });

  assert.deepEqual(attempted, ["unprivileged", "sudo"]);
  assert.deepEqual(result.attempts, ["unprivileged", "sudo"]);
  assert.equal(result.exitCode, 0);
  assert.equal(result.ready, true);
});

test("unavailable sudo remains a fail-closed pre-ready result", async () => {
  const result = await runLauncherSequence("sudo", async () => ({
    exitCode: 1,
    fallbackEligible: false,
    failureSite: "",
    ready: false
  }));

  assert.deepEqual(result.attempts, ["sudo"]);
  assert.equal(result.exitCode, 1);
  assert.equal(result.ready, false);
});

test("auto does not fall back after a pre-ready timeout", async () => {
  const attempted = [];
  const result = await runLauncherSequence("auto", async (mode) => {
    attempted.push(mode);
    return { exitCode: 1, fallbackEligible: false, ready: false };
  });

  assert.deepEqual(attempted, ["unprivileged"]);
  assert.deepEqual(result.attempts, ["unprivileged"]);
  assert.equal(result.ready, false);
});

test("auto never falls back after readiness", async () => {
  const attempted = [];
  const result = await runLauncherSequence("auto", async (mode) => {
    attempted.push(mode);
    return { exitCode: 1, ready: true };
  });

  assert.deepEqual(attempted, ["unprivileged"]);
  assert.deepEqual(result.attempts, ["unprivileged"]);
  assert.equal(result.exitCode, 1);
  assert.equal(result.ready, true);
});

test("auto treats pre-ready cancellation and a signaled child as terminal", async () => {
  for (const result of [
    { cancelled: true, exitCode: 1, fallbackEligible: true, ready: false },
    { exitCode: 1, fallbackEligible: true, ready: false, signal: "SIGTERM" }
  ]) {
    const attempted = [];
    const sequence = await runLauncherSequence("auto", async (mode) => {
      attempted.push(mode);
      return result;
    });
    assert.deepEqual(attempted, ["unprivileged"]);
    assert.deepEqual(sequence.attempts, ["unprivileged"]);
    assert.equal(sequence.ready, false);
  }
});

test("explicit modes reject unknown values without launching", () => {
  assert.equal(parseLauncherMode(undefined), "auto");
  assert.equal(parseLauncherMode("unprivileged"), "unprivileged");
  assert.equal(parseLauncherMode("sudo"), "sudo");
  assert.throws(() => parseLauncherMode("disabled"), (error) => {
    assert.ok(error instanceof BoundaryError);
    assert.equal(error.site, "[site:network-boundary-mode]");
    return true;
  });
});

test("sudo bootstrap keeps browser arguments as literal argv", () => {
  const runProfilePath = path.join(__dirname, "run_profile.js");
  const browserArguments = [
    "--project=chromium",
    "--grep",
    "$(touch should-not-exist); opaque-argument"
  ];
  const command = buildBoundaryCommand({
    args: browserArguments,
    failurePath: "/private/failure",
    hostNamespace: "net:[1]",
    identity: { uid: 1000, gid: 1000, groups: [10, 1000] },
    operation: "profile",
    readyPath: "/private/ready",
    runProfilePath,
    selectedMode: "sudo",
    startPath: "/private/start"
  });

  assert.equal(command.file, "/usr/bin/sudo");
  assert.deepEqual(command.args.slice(-browserArguments.length), browserArguments);
  assert.ok(!command.args.includes("-c"));
  assert.deepEqual(command.args.slice(0, 5), [
    "-n",
    "--",
    "/bin/sh",
    path.join(__dirname, "network_boundary_bootstrap.sh"),
    "sudo-launch"
  ]);
  assert.ok(command.args.includes("CONTROL_AGENTS_PLAYWRIGHT_EXPECTED_APPARMOR_PROFILE=chrome"));
  assert.deepEqual(fixedBootstrapEnvironment(), {
    LANG: "C.UTF-8",
    LC_ALL: "C.UTF-8",
    PATH: "/usr/sbin:/usr/bin:/sbin:/bin"
  });
});

test("ready credentials require no_new_privs and sudo cannot regain privilege", () => {
  assert.doesNotThrow(() => assertNoNewPrivilegesStatus({ noNewPrivs: 1 }));
  for (const status of [{ noNewPrivs: 0 }, { noNewPrivs: null }, {}]) {
    assert.throws(() => assertNoNewPrivilegesStatus(status), (error) => {
      assert.ok(error instanceof BoundaryError);
      assert.equal(error.site, "[site:network-boundary-capabilities]");
      return true;
    });
  }

  assert.doesNotThrow(() => assertNoPrivilegeRegain("sudo", () => ({ status: 1 })));
  assert.throws(() => assertNoPrivilegeRegain("sudo", () => ({ status: 0 })), (error) => {
    assert.ok(error instanceof BoundaryError);
    assert.equal(error.site, "[site:network-boundary-capabilities]");
    return true;
  });
});

test("Chromium sandbox proof rejects no-sandbox and missing renderer isolation", () => {
  const snapshot = chromiumSandboxSnapshot();
  assert.doesNotThrow(() => validateChromiumSandboxSnapshot(snapshot, 100, sandboxIdentity()));

  const unsandboxed = structuredClone(snapshot);
  unsandboxed.find((entry) => entry.pid === 101).commandLine += " --no-sandbox";
  assert.throws(() => validateChromiumSandboxSnapshot(unsandboxed, 100, sandboxIdentity()), (error) => {
    assert.ok(error instanceof ChromiumSandboxError);
    assert.equal(error.site, "[site:chromium-sandbox-arguments]");
    return true;
  });

  const sharedNamespaces = structuredClone(snapshot);
  sharedNamespaces.find((entry) => entry.pid === 102).namespaces =
    structuredClone(sharedNamespaces.find((entry) => entry.pid === 101).namespaces);
  assert.throws(() => validateChromiumSandboxSnapshot(sharedNamespaces, 100, sandboxIdentity()), (error) => {
    assert.ok(error instanceof ChromiumSandboxError);
    assert.equal(error.site, "[site:chromium-sandbox-isolation]");
    return true;
  });
});

test("Chromium sandbox proof rejects every invalid owned browser in a mixed snapshot", () => {
  const invalidMutations = [
    (browser) => { browser.commandLine += " --no-sandbox"; },
    (browser) => { browser.uid[3] = 0; },
    (browser) => { browser.uid[3] = 1001; },
    (browser) => { browser.gid[3] = 1001; },
    (browser) => { browser.groups = [1000]; },
    ...capabilityMutations(true),
    (browser) => { browser.noNewPrivs = 0; }
  ];

  for (const mutate of invalidMutations) {
    const snapshot = chromiumSandboxSnapshot();
    appendOwnedBrowser(snapshot, 103, 104);
    mutate(snapshot.find((entry) => entry.pid === 103));
    assert.throws(() => validateChromiumSandboxSnapshot(
      snapshot,
      100,
      sandboxIdentity()
    ), ChromiumSandboxError);
  }
});

test("Chromium sandbox proof rejects every invalid renderer in a mixed snapshot", () => {
  const invalidMutations = [
    (renderer) => { renderer.commandLine += " --no-sandbox"; },
    (renderer) => { renderer.uid[0] = 0; },
    ...capabilityMutations(),
    (renderer) => { renderer.capBnd = "invalid"; },
    (renderer) => { renderer.noNewPrivs = 0; },
    (renderer) => { renderer.seccomp = 0; },
    (renderer) => { delete renderer.namespaces.user; },
    (renderer, browser) => { renderer.namespaces = structuredClone(browser.namespaces); }
  ];

  for (const mutate of invalidMutations) {
    const snapshot = chromiumSandboxSnapshot();
    const browser = snapshot.find((entry) => entry.pid === 101);
    const renderer = appendRenderer(snapshot, 103, browser.pid, 3);
    mutate(renderer, browser);
    assert.throws(() => validateChromiumSandboxSnapshot(
      snapshot,
      100,
      sandboxIdentity()
    ), ChromiumSandboxError);
  }
});

test("Chromium sandbox proof rejects root GIDs and group 0 in browsers and renderers", () => {
  for (const mutate of [
    (snapshot) => { snapshot.find((entry) => entry.pid === 101).gid[1] = 0; },
    (snapshot) => { snapshot.find((entry) => entry.pid === 101).groups.push(0); },
    (snapshot) => { snapshot.find((entry) => entry.pid === 102).gid[1] = 0; },
    (snapshot) => { snapshot.find((entry) => entry.pid === 102).groups.push(0); }
  ]) {
    const snapshot = chromiumSandboxSnapshot();
    mutate(snapshot);
    assert.throws(() => validateChromiumSandboxSnapshot(
      snapshot,
      100,
      sandboxIdentity()
    ), ChromiumSandboxError);
  }
});

test("Chromium sandbox proof rejects browser bounding and renderer held capabilities", () => {
  const browserBounding = chromiumSandboxSnapshot();
  browserBounding.find((entry) => entry.pid === 101).capBnd = "0000000000000400";
  assert.throws(() => validateChromiumSandboxSnapshot(
    browserBounding,
    100,
    sandboxIdentity()
  ), ChromiumSandboxError);

  for (const field of ["capInh", "capPrm", "capEff", "capAmb"]) {
    const rendererHeld = chromiumSandboxSnapshot();
    rendererHeld.find((entry) => entry.pid === 102)[field] = "0000000000000400";
    assert.throws(() => validateChromiumSandboxSnapshot(
      rendererHeld,
      100,
      sandboxIdentity()
    ), ChromiumSandboxError);
  }
});

test("Chromium sandbox proof accepts only namespace-local renderer bounding masks", () => {
  const safeSnapshot = chromiumSandboxSnapshot();
  assert.notEqual(safeSnapshot.find((entry) => entry.pid === 102).capBnd, "0000000000000000");
  assert.doesNotThrow(() => validateChromiumSandboxSnapshot(
    safeSnapshot,
    100,
    sandboxIdentity()
  ));

  for (const mutate of [
    (renderer, browser) => { renderer.namespaces.user = browser.namespaces.user; },
    (renderer) => { renderer.capEff = "0000000000000400"; },
    (renderer) => { renderer.noNewPrivs = 0; }
  ]) {
    const unsafeSnapshot = chromiumSandboxSnapshot();
    const browser = unsafeSnapshot.find((entry) => entry.pid === 101);
    const renderer = unsafeSnapshot.find((entry) => entry.pid === 102);
    mutate(renderer, browser);
    assert.throws(() => validateChromiumSandboxSnapshot(
      unsafeSnapshot,
      100,
      sandboxIdentity()
    ), ChromiumSandboxError);
  }
});

test("Chromium sandbox proof ignores invalid Chromium processes outside the owned tree", () => {
  const snapshot = chromiumSandboxSnapshot();
  const browser = appendBrowser(snapshot, 200, 1);
  browser.commandLine += " --no-sandbox";
  const renderer = appendRenderer(snapshot, 201, browser.pid, 4);
  renderer.uid.fill(0);
  renderer.capEff = "0000000000000400";
  renderer.noNewPrivs = 0;
  renderer.seccomp = 0;
  renderer.namespaces = structuredClone(browser.namespaces);

  assert.doesNotThrow(() => validateChromiumSandboxSnapshot(snapshot, 100, sandboxIdentity()));
});

test("Chromium sandbox proof rejects an invalid renderer under a later owned browser", () => {
  const snapshot = chromiumSandboxSnapshot();
  appendOwnedBrowser(snapshot, 103, 104);
  snapshot.find((entry) => entry.pid === 104).seccomp = 0;

  assert.throws(() => validateChromiumSandboxSnapshot(
    snapshot,
    100,
    sandboxIdentity()
  ), ChromiumSandboxError);
});

test("unprivileged launcher uses current-user mapping and retained setup capabilities", () => {
  const command = buildBoundaryCommand({
    args: ["--network-boundary-probe"],
    failurePath: "/private/failure",
    hostNamespace: "net:[1]",
    identity: { uid: 1000, gid: 1000, groups: [10, 1000] },
    operation: "profile",
    readyPath: "/private/ready",
    runProfilePath: path.join(__dirname, "run_profile.js"),
    selectedMode: "unprivileged",
    startPath: "/private/start"
  });

  assert.equal(command.file, "/usr/bin/unshare");
  assert.ok(command.args.includes("--map-current-user"));
  assert.ok(command.args.includes("--keep-caps"));
  assert.ok(!command.args.includes("--map-root-user"));
  assert.ok(!command.args.includes("-c"));
});

test("route verification checks IPv4 and IPv6 and accepts only loopback routes", () => {
  const families = [];
  assert.doesNotThrow(() => assertLoopbackOnlyRoutes((_file, args) => {
    families.push(args[1]);
    return {
      status: 0,
      stdout: JSON.stringify([{ dst: args[1] === "-4" ? "127.0.0.0/8" : "::1", dev: "lo" }])
    };
  }));
  assert.deepEqual(families, ["-4", "-6"]);

  assert.throws(() => assertLoopbackOnlyRoutes((_file, args) => ({
    status: 0,
    stdout: JSON.stringify(args[1] === "-6"
      ? [{ dst: "default", dev: "lo" }]
      : [{ dst: "127.0.0.0/8", dev: "lo" }])
  })), (error) => error instanceof BoundaryError && error.site === "[site:network-boundary-routes]");

  assert.throws(() => assertLoopbackOnlyRoutes((_file, args) => ({
    status: 0,
    stdout: JSON.stringify(args[1] === "-4"
      ? [{ dst: "192.0.2.0/24", dev: "eth0" }]
      : [{ dst: "::1", dev: "lo" }])
  })), (error) => error instanceof BoundaryError && error.site === "[site:network-boundary-routes]");
});

test("profile child asks the original launcher for one selected-mode churn", async () => {
  await withAttemptRepository(async (repoRoot) => {
    const identity = currentIdentity();
    const selectedModes = [];
    const result = await attemptBoundary({
      args: ["--network-boundary-probe"],
      coordinateChurn: true,
      identity,
      launchChurn: async (options) => {
        selectedModes.push(options.selectedMode);
        return { cancelled: false, exitCode: 0, ready: true };
      },
      operation: "profile",
      repoRoot,
      runProfilePath: path.join(__dirname, "run_profile.js"),
      selectedMode: "sudo",
      spawnImpl: spawnCoordinatedProfileChild
    });

    assert.equal(result.ready, true);
    assert.equal(result.exitCode, 0);
    assert.deepEqual(selectedModes, ["sudo"]);
    assertNoAttemptDirectories(repoRoot);
  });
});

test("failed selected-mode churn returns one bounded failure to the profile child", async () => {
  await withAttemptRepository(async (repoRoot) => {
    const selectedModes = [];
    const result = await attemptBoundary({
      args: ["--network-boundary-probe"],
      coordinateChurn: true,
      identity: currentIdentity(),
      launchChurn: async (options) => {
        selectedModes.push(options.selectedMode);
        return { cancelled: false, exitCode: 1, ready: false };
      },
      operation: "profile",
      repoRoot,
      runProfilePath: path.join(__dirname, "run_profile.js"),
      selectedMode: "sudo",
      spawnImpl: spawnCoordinatedProfileChild
    });

    assert.equal(result.ready, true);
    assert.equal(result.exitCode, 3);
    assert.deepEqual(selectedModes, ["sudo"]);
    assertNoAttemptDirectories(repoRoot);
  });
});

test("a genuinely signaled pre-ready launcher child cannot trigger auto fallback", async () => {
  await withAttemptRepository(async (repoRoot) => {
    const attempts = [];
    const result = await runLauncherSequence("auto", async (selectedMode) => {
      attempts.push(selectedMode);
      return attemptBoundary({
        args: [],
        identity: currentIdentity(),
        operation: "profile",
        readinessTimeoutMs: 500,
        repoRoot,
        runProfilePath: path.join(__dirname, "run_profile.js"),
        selectedMode,
        spawnImpl: () => spawn(process.execPath, ["-e", "process.kill(process.pid, 'SIGTERM')"], {
          stdio: ["pipe", "pipe", "pipe"]
        })
      });
    });

    assert.deepEqual(attempts, ["unprivileged"]);
    assert.equal(result.ready, false);
    assert.equal(result.signal, "SIGTERM");
    assert.equal(result.fallbackEligible, false);
    assertNoAttemptDirectories(repoRoot);
  });
});

test("pre-ready operator cancellation terminates its child and cannot trigger auto fallback", async () => {
  await withAttemptRepository(async (repoRoot) => {
    const controller = new AbortController();
    const attempts = [];
    let childPID = 0;
    const cancellation = setTimeout(() => controller.abort(), 25);
    try {
      const result = await runLauncherSequence("auto", async (selectedMode) => {
        attempts.push(selectedMode);
        return attemptBoundary({
          args: [],
          identity: currentIdentity(),
          operation: "profile",
          readinessTimeoutMs: 1_000,
          repoRoot,
          runProfilePath: path.join(__dirname, "run_profile.js"),
          selectedMode,
          signal: controller.signal,
          spawnImpl: () => {
            const child = spawn(process.execPath, ["-e", "setInterval(() => {}, 1000)"], {
              stdio: ["pipe", "pipe", "pipe"]
            });
            childPID = child.pid;
            return child;
          }
        });
      });

      assert.deepEqual(attempts, ["unprivileged"]);
      assert.equal(result.ready, false);
      assert.equal(result.cancelled, true);
      assert.equal(result.fallbackEligible, false);
      assertProcessExited(childPID);
      assertNoAttemptDirectories(repoRoot);
    } finally {
      clearTimeout(cancellation);
    }
  });
});

test("a real pre-ready SIGTERM is terminal and never starts the sudo attempt", async () => {
  await withAttemptRepository(async (repoRoot) => {
    const helper = `
      const { spawn } = require("node:child_process");
      const { attemptBoundary, runLauncherSequence } = require(process.argv[1]);
      const repoRoot = process.argv[2];
      const identity = {
        uid: process.getuid(),
        gid: process.getgid(),
        groups: [...new Set([...process.getgroups(), process.getgid()])].sort((left, right) => left - right)
      };
      const attempts = [];
      setTimeout(() => process.kill(process.pid, "SIGTERM"), 25);
      runLauncherSequence("auto", async (selectedMode) => {
        attempts.push(selectedMode);
        return attemptBoundary({
          args: [], identity, operation: "profile", readinessTimeoutMs: 1000, repoRoot,
          runProfilePath: process.argv[1], selectedMode,
          spawnImpl: () => spawn(process.execPath, ["-e", "setInterval(() => {}, 1000)"], {
            stdio: ["pipe", "pipe", "pipe"]
          })
        });
      }).then((result) => {
        if (attempts.length !== 1 || attempts[0] !== "unprivileged" || !result.cancelled || result.fallbackEligible) {
          process.exitCode = 1;
        }
      }, () => {
        process.exitCode = 1;
      });
    `;
    const result = await runChild(spawn(process.execPath, [
      "-e",
      helper,
      require.resolve("./network_boundary.js"),
      repoRoot
    ], { stdio: "ignore" }));

    assert.equal(result.signal, null);
    assert.equal(result.code, 0);
    assertNoAttemptDirectories(repoRoot);
  });
});

test("attempt resources are cleaned on encoding, construction, spawn, timeout, and normal exit", async () => {
  await withAttemptRepository(async (repoRoot) => {
    const common = {
      args: [],
      identity: currentIdentity(),
      operation: "profile",
      repoRoot,
      runProfilePath: path.join(__dirname, "run_profile.js")
    };

    const oversized = await attemptBoundary({
      ...common,
      environment: { OVERSIZED: "x".repeat(1_048_577) },
      selectedMode: "unprivileged"
    });
    assert.equal(oversized.failureSite, "[site:network-boundary-environment]");
    assertNoAttemptDirectories(repoRoot);

    const invalidEnvironment = await attemptBoundary({
      ...common,
      environment: { INVALID: "value\0suffix" },
      selectedMode: "unprivileged"
    });
    assert.equal(invalidEnvironment.failureSite, "[site:network-boundary-environment]");
    assertNoAttemptDirectories(repoRoot);

    const invalidCommand = await attemptBoundary({ ...common, selectedMode: "invalid" });
    assert.equal(invalidCommand.failureSite, "[site:network-boundary-mode]");
    assertNoAttemptDirectories(repoRoot);

    const spawnFailure = await attemptBoundary({
      ...common,
      selectedMode: "unprivileged",
      spawnImpl: () => {
        throw new Error("fixed spawn failure");
      }
    });
    assert.equal(spawnFailure.failureSite, "[site:network-boundary-readiness]");
    assertNoAttemptDirectories(repoRoot);

    const asynchronousSpawnFailure = await attemptBoundary({
      ...common,
      selectedMode: "unprivileged",
      spawnImpl: () => spawn(path.join(repoRoot, "missing-launcher"), [], {
        stdio: ["pipe", "pipe", "pipe"]
      })
    });
    assert.equal(asynchronousSpawnFailure.failureSite, "[site:network-boundary-readiness]");
    assertNoAttemptDirectories(repoRoot);

    const timeout = await attemptBoundary({
      ...common,
      readinessTimeoutMs: 30,
      selectedMode: "unprivileged",
      spawnImpl: () => spawn(process.execPath, ["-e", "setInterval(() => {}, 1000)"], {
        stdio: ["pipe", "pipe", "pipe"]
      })
    });
    assert.equal(timeout.ready, false);
    assert.equal(timeout.fallbackEligible, false);
    assertNoAttemptDirectories(repoRoot);

    const normal = await attemptBoundary({
      ...common,
      selectedMode: "unprivileged",
      spawnImpl: spawnReadyChild
    });
    assert.equal(normal.ready, true);
    assert.equal(normal.exitCode, 0);
    assertNoAttemptDirectories(repoRoot);
  });
});

test("a legal execve-sized literal environment value is restored only from bounded stdin", async () => {
  await withAttemptRepository(async (repoRoot) => {
    const largeLiteral = `literal-start:${"x".repeat(160_000)}:literal-end`;
    let launch;
    let diagnostics = "";
    const result = await attemptBoundary({
      args: ["--network-boundary-probe"],
      environment: { CONTROL_AGENTS_LITERAL_ENVIRONMENT_PROBE: largeLiteral },
      identity: currentIdentity(),
      operation: "profile",
      repoRoot,
      runProfilePath: path.join(__dirname, "run_profile.js"),
      selectedMode: "unprivileged",
      spawnImpl: (file, args, options) => {
        launch = { args, env: options.env, file };
        const boundaryDirectory = path.dirname(boundaryPath(args, "CONTROL_AGENTS_PLAYWRIGHT_BOUNDARY_READY"));
        for (const entry of fs.readdirSync(boundaryDirectory)) {
          const entryPath = path.join(boundaryDirectory, entry);
          if (fs.lstatSync(entryPath).isFile()) {
            assert.equal(fs.readFileSync(entryPath).includes(Buffer.from("literal-start:")), false);
          }
        }
        const child = spawnRestoredEnvironmentChild(args, options);
        child.stdout.on("data", (chunk) => {
          diagnostics += chunk.toString("utf8");
        });
        child.stderr.on("data", (chunk) => {
          diagnostics += chunk.toString("utf8");
        });
        return child;
      }
    });

    assert.equal(result.ready, true);
    assert.equal(result.exitCode, 0);
    assert.deepEqual(launch.env, fixedBootstrapEnvironment());
    assert.equal(launch.args.some((argument) => argument.includes("literal-start:")), false);
    assert.equal(Object.values(launch.env).some((value) => value.includes("literal-start:")), false);
    assert.equal(diagnostics.includes("literal-start:"), false);
    assertNoAttemptDirectories(repoRoot);
  });
});

test("environment encoding is bounded and rejected mutation promises settle without retaining errors", async () => {
  assert.throws(() => encodeEnvironmentHandoff({ LARGE: "x".repeat(1_048_577) }), (error) => {
    assert.ok(error instanceof BoundaryError);
    assert.equal(error.site, "[site:network-boundary-environment]");
    return true;
  });
  for (const environment of [
    { INVALID: 7 },
    { "INVALID=NAME": "value" },
    { INVALID: "value\0suffix" }
  ]) {
    assert.throws(() => encodeEnvironmentHandoff(environment), (error) => {
      assert.ok(error instanceof BoundaryError);
      assert.equal(error.site, "[site:network-boundary-environment]");
      return true;
    });
  }

  const unhandled = [];
  const recordUnhandled = (error) => unhandled.push(error);
  process.on("unhandledRejection", recordUnhandled);
  try {
    let mutationCount = 0;
    let rejectMutation;
    const heldMutation = new Promise((_resolve, reject) => {
      mutationCount += 1;
      rejectMutation = reject;
    });
    const settlement = new AbortController();
    const outcome = capturePromiseOutcome(heldMutation, settlement.signal);
    settlement.abort();
    assert.deepEqual(await outcome, { fulfilled: false });
    rejectMutation(new Error("sensitive rejected mutation detail"));
    await new Promise((resolve) => setImmediate(resolve));
    assert.equal(mutationCount, 1);
    assert.deepEqual(unhandled, []);
  } finally {
    process.off("unhandledRejection", recordUnhandled);
  }
});

function currentIdentity() {
  const uid = process.getuid();
  const gid = process.getgid();
  return { uid, gid, groups: [...new Set([...process.getgroups(), gid])].sort((left, right) => left - right) };
}

async function withAttemptRepository(action) {
  const repoRoot = fs.mkdtempSync(path.join(os.tmpdir(), "control-agents-boundary-test-"));
  fs.mkdirSync(path.join(repoRoot, ".cache"), { mode: 0o700 });
  try {
    await action(repoRoot);
  } finally {
    fs.rmSync(repoRoot, { force: true, recursive: true });
  }
}

function assertNoAttemptDirectories(repoRoot) {
  const entries = fs.readdirSync(path.join(repoRoot, ".cache"));
  assert.deepEqual(entries.filter((entry) => entry.startsWith("pwn-")), []);
}

function assertProcessExited(pid) {
  assert.ok(Number.isInteger(pid) && pid > 0);
  assert.throws(() => process.kill(pid, 0), (error) => error && error.code === "ESRCH");
}

function runChild(child) {
  return new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("exit", (code, signal) => resolve({ code, signal }));
  });
}

function boundaryPath(args, variable) {
  const assignment = args.find((argument) => argument.startsWith(`${variable}=`));
  assert.ok(assignment, `missing fixed boundary assignment for ${variable}`);
  return assignment.slice(variable.length + 1);
}

function spawnCoordinatedProfileChild(_file, args) {
  const readyPath = boundaryPath(args, "CONTROL_AGENTS_PLAYWRIGHT_BOUNDARY_READY");
  const startPath = boundaryPath(args, "CONTROL_AGENTS_PLAYWRIGHT_BOUNDARY_START");
  const requestPath = boundaryPath(args, "CONTROL_AGENTS_PLAYWRIGHT_BOUNDARY_CHURN_REQUEST");
  const resultPath = boundaryPath(args, "CONTROL_AGENTS_PLAYWRIGHT_BOUNDARY_CHURN_RESULT");
  const script = `
    const fs = require("node:fs");
    const [readyPath, startPath, requestPath, resultPath] = process.argv.slice(1);
    fs.writeFileSync(readyPath, "ready\\n", { flag: "wx", mode: 0o600 });
    const deadline = Date.now() + 2000;
    const timer = setInterval(() => {
      if (Date.now() >= deadline) process.exit(2);
      if (fs.existsSync(startPath) && !fs.existsSync(requestPath)) {
        fs.writeFileSync(requestPath, "churn\\n", { flag: "wx", mode: 0o600 });
      }
      if (fs.existsSync(resultPath)) {
        const result = fs.readFileSync(resultPath, "utf8");
        clearInterval(timer);
        process.exit(result === "ok\\n" ? 0 : 3);
      }
    }, 5);
  `;
  return spawn(process.execPath, ["-e", script, readyPath, startPath, requestPath, resultPath], {
    stdio: ["pipe", "pipe", "pipe"]
  });
}

function spawnReadyChild(_file, args) {
  const readyPath = boundaryPath(args, "CONTROL_AGENTS_PLAYWRIGHT_BOUNDARY_READY");
  const startPath = boundaryPath(args, "CONTROL_AGENTS_PLAYWRIGHT_BOUNDARY_START");
  const script = `
    const fs = require("node:fs");
    const [readyPath, startPath] = process.argv.slice(1);
    fs.writeFileSync(readyPath, "ready\\n", { flag: "wx", mode: 0o600 });
    const deadline = Date.now() + 1000;
    const timer = setInterval(() => {
      if (fs.existsSync(startPath)) {
        clearInterval(timer);
        process.exit(0);
      }
      if (Date.now() >= deadline) process.exit(2);
    }, 5);
  `;
  return spawn(process.execPath, ["-e", script, readyPath, startPath], {
    stdio: ["pipe", "pipe", "pipe"]
  });
}

function spawnRestoredEnvironmentChild(args, options) {
  const internalEnvironment = { ...options.env };
  const environmentIndex = args.indexOf("/usr/bin/env");
  const nodeIndex = args.indexOf(process.execPath, environmentIndex + 1);
  assert.ok(environmentIndex >= 0 && nodeIndex > environmentIndex);
  for (const argument of args.slice(environmentIndex + 2, nodeIndex)) {
    const separator = argument.indexOf("=");
    assert.ok(separator > 0);
    internalEnvironment[argument.slice(0, separator)] = argument.slice(separator + 1);
  }
  const script = `
    const fs = require("node:fs");
    const { restoreBoundaryEnvironment } = require(process.argv[1]);
    restoreBoundaryEnvironment();
    const expected = "literal-start:" + "x".repeat(160000) + ":literal-end";
    if (process.env.CONTROL_AGENTS_LITERAL_ENVIRONMENT_PROBE !== expected) process.exit(2);
    const readyPath = process.env.CONTROL_AGENTS_PLAYWRIGHT_BOUNDARY_READY;
    const startPath = process.env.CONTROL_AGENTS_PLAYWRIGHT_BOUNDARY_START;
    fs.writeFileSync(readyPath, "ready\\n", { flag: "wx", mode: 0o600 });
    const deadline = Date.now() + 1000;
    const timer = setInterval(() => {
      if (fs.existsSync(startPath)) {
        clearInterval(timer);
        process.exit(0);
      }
      if (Date.now() >= deadline) process.exit(3);
    }, 5);
  `;
  return spawn(process.execPath, ["-e", script, require.resolve("./network_boundary.js")], {
    ...options,
    env: internalEnvironment
  });
}

function chromiumSandboxSnapshot() {
  const snapshot = [
    sandboxProcess(100, 1, "node worker", 1, 0),
    sandboxProcess(101, 100, "/browser/chrome --remote-debugging-pipe --headless", 1, 0)
  ];
  appendRenderer(snapshot, 102, 101, 2);
  return snapshot;
}

function appendOwnedBrowser(snapshot, browserPID, rendererPID) {
  const browser = appendBrowser(snapshot, browserPID, 100);
  appendRenderer(snapshot, rendererPID, browserPID, rendererPID);
  return browser;
}

function appendBrowser(snapshot, pid, ppid) {
  const browser = sandboxProcess(
    pid,
    ppid,
    "/browser/chrome --remote-debugging-pipe --headless",
    pid,
    0
  );
  snapshot.push(browser);
  return browser;
}

function appendRenderer(snapshot, pid, ppid, namespaceID) {
  const renderer = sandboxProcess(
    pid,
    ppid,
    "/browser/chrome --type=renderer --headless",
    namespaceID,
    2
  );
  renderer.capBnd = "000001ffffffffff";
  snapshot.push(renderer);
  return renderer;
}

function sandboxProcess(pid, ppid, commandLine, namespaceID, seccomp) {
  const zeroCapabilities = "0000000000000000";
  return {
    pid,
    ppid,
    commandLine,
    uid: [1000, 1000, 1000, 1000],
    gid: [1000, 1000, 1000, 1000],
    groups: [10, 1000],
    capInh: zeroCapabilities,
    capPrm: zeroCapabilities,
    capEff: zeroCapabilities,
    capBnd: zeroCapabilities,
    capAmb: zeroCapabilities,
    noNewPrivs: 1,
    seccomp,
    namespaces: {
      user: `user:[${namespaceID}]`,
      pid: `pid:[${namespaceID}]`,
      net: `net:[${namespaceID}]`
    }
  };
}

function sandboxIdentity() {
  return { uid: 1000, gid: 1000, groups: [10, 1000] };
}

function capabilityMutations(includeBounding = false) {
  const fields = ["capInh", "capPrm", "capEff", "capAmb"];
  if (includeBounding) fields.push("capBnd");
  return fields.map((field) =>
    (entry) => { entry[field] = "0000000000000400"; });
}
