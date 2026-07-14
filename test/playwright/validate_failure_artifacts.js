"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

const repositoryRoot = path.resolve(__dirname, "../..");
const probeRoot = fs.mkdtempSync(
  path.join(repositoryRoot, ".cache", "playwright-artifact-probe-")
);

const canaries = {
  terminal: "terminal-output-canary-6e7a634fb8",
  paste: "paste-content-canary-a8b1c20f5d\nsecond-line",
  cookieName: "control_agents_session",
  cookieValue: "auth-cookie-canary-93ca50e7d1",
  credentialName: "CONTROL_AGENTS_PASSWORD",
  credentialValue: "credential-canary-c03ef46b91"
};

function writeProbeFiles() {
  const rootConfigPath = path.join(repositoryRoot, "playwright.config.js");
  const configSource = `
const path = require("node:path");
const base = require(${JSON.stringify(rootConfigPath)});

module.exports = {
  ...base,
  testDir: __dirname,
  testMatch: "probe.spec.js",
  outputDir: path.join(__dirname, "output"),
  preserveOutput: "always",
  projects: base.projects.filter((project) => project.name === "chromium"),
  use: {
    ...base.use,
    trace: "off",
    screenshot: "off",
    video: "off"
  }
};
`;

  const specSource = `
const { test } = require("@playwright/test");

test("intentional content-free failure probe", async ({ page, context }) => {
  const terminal = process.env.PROBE_TERMINAL;
  const paste = process.env.PROBE_PASTE;
  const cookieName = process.env.PROBE_COOKIE_NAME;
  const cookieValue = process.env.PROBE_COOKIE_VALUE;
  const credentialName = process.env.PROBE_CREDENTIAL_NAME;
  const credentialValue = process.env.PROBE_CREDENTIAL_VALUE;

  await page.route("https://probe.invalid/**", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "text/html",
      body: "<!doctype html><main>artifact policy probe</main>"
    });
  });
  await context.addCookies([{
    name: cookieName,
    value: cookieValue,
    url: "https://probe.invalid/",
    httpOnly: true,
    secure: true,
    sameSite: "Lax"
  }]);
  await page.goto("https://probe.invalid/");
  await page.evaluate(async (probe) => {
    const terminalNode = document.createElement("pre");
    terminalNode.textContent = probe.terminal;
    document.body.replaceChildren(terminalNode);
    await fetch("/mutation", {
      method: "POST",
      headers: {
        "Content-Type": "text/plain",
        [probe.credentialName]: probe.credentialValue
      },
      body: probe.paste
    });
  }, { terminal, paste, credentialName, credentialValue });

  throw new Error("[site:history-create] mutation failed before a response: network-changed");
});
`;

  fs.writeFileSync(path.join(probeRoot, "probe.config.js"), configSource);
  fs.writeFileSync(path.join(probeRoot, "probe.spec.js"), specSource);
}

function listFiles(directory) {
  const files = [];
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const entryPath = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      files.push(...listFiles(entryPath));
    } else {
      files.push(entryPath);
    }
  }
  return files;
}

function assertContentFree(buffer, source) {
  for (const canary of Object.values(canaries)) {
    assert.equal(
      buffer.includes(Buffer.from(canary)),
      false,
      `${source} contained forbidden probe content`
    );
  }
}

try {
  writeProbeFiles();
  const profileRunner = path.join(repositoryRoot, "test", "playwright", "run_profile.js");
  const result = spawnSync(
    process.execPath,
    [profileRunner, "--config", path.join(probeRoot, "probe.config.js"), "--project=chromium"],
    {
      cwd: repositoryRoot,
      encoding: "buffer",
      env: {
        ...process.env,
        CI: "1",
        DEBUG: "",
        PWDEBUG: "",
        PROBE_TERMINAL: canaries.terminal,
        PROBE_PASTE: canaries.paste,
        PROBE_COOKIE_NAME: canaries.cookieName,
        PROBE_COOKIE_VALUE: canaries.cookieValue,
        PROBE_CREDENTIAL_NAME: canaries.credentialName,
        PROBE_CREDENTIAL_VALUE: canaries.credentialValue
      }
    }
  );

  assert.equal(result.error, undefined, "Playwright failure probe could not start");
  assert.equal(result.status, 1, "Playwright failure probe did not fail exactly once");

  const diagnostics = Buffer.concat([result.stdout || Buffer.alloc(0), result.stderr || Buffer.alloc(0)]);
  const diagnosticsText = diagnostics.toString("utf8");
  assert.match(diagnosticsText, /intentional content-free failure probe/);
  assert.match(diagnosticsText, /network-changed:history-create/);
  assert.match(diagnosticsText, /1 failed/);
  assertContentFree(diagnostics, "Playwright failure diagnostics");

  const retainedFiles = listFiles(probeRoot);
  assert.equal(
    retainedFiles.some((file) => path.basename(file) === "error-context.md"),
    true,
    "Playwright failure probe did not retain sanitized error context"
  );
  for (const file of retainedFiles) {
    const relativePath = path.relative(probeRoot, file);
    assert.doesNotMatch(
      relativePath,
      /(?:trace|network|playwright-report|\.zip$|\.png$|\.webm$|\.html$)/i,
      "Playwright retained a disabled failure artifact"
    );
    assertContentFree(fs.readFileSync(file), "Playwright retained files");
  }

  console.log("Playwright failure artifact policy validated.");
} finally {
  fs.rmSync(probeRoot, { recursive: true, force: true });
}
