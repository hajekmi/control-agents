const fs = require("fs");
const path = require("path");

const repoRoot = path.resolve(__dirname, "../..");
const reportDir = path.join(repoRoot, ".cache", "benchmarks");
const metricNames = [
  "capturePaneDuration",
  "ansiParseDuration",
  "snapshotRAM",
  "responseBytes",
  "firstHistoryPaint",
  "pagePrependDuration",
  "scrollFPS",
  "longTasks",
  "domNodeCount",
  "jsHeap",
  "anchorDrift",
  "liveInputToPaint",
  "reconnectToRedraw",
  "slowConsumer"
];
const forbidden = [
  /CONTROL_AGENTS_PASSWORD/,
  /control_agents_session/,
  /playwright-line-/,
  /SSH_AUTH_SOCK/,
  /<script/i,
  /\b(?:pt|hs|viewer)_[A-Za-z0-9_-]+\b/,
  /\b(?:pt|hs|viewer)-[A-Za-z0-9_-]+\b/
];

const server = readReport("server-report.json", 128 * 1024);
assert(server.schemaVersion === 1 && typeof server.runtime === "string", "invalid server report header");
assert(Array.isArray(server.datasets) && server.datasets.length === 6, "invalid server dataset matrix");
for (const dataset of server.datasets) {
  assert(/^dataset-\d{2}$/.test(dataset.id), "server report contains a non-opaque dataset ID");
  assertMeasurements(dataset.measurements);
}

const browser = readReport("browser-report.json", 32 * 1024);
assert(browser.schemaVersion === 1 && browser.runtime === "chromium-engine", "invalid browser report header");
assert(browser.dataset === "real-tmux-50000-lines", "invalid browser dataset identity");
assertMeasurements(browser.measurements, [...metricNames, "maxLongTask"]);
assert(browser.measurements.maxLongTask.supported === browser.measurements.longTasks.supported, "Long Tasks count and maximum support must match");
assert(browser.measurements.liveInputToPaint.supported === false, "ttyd input-to-paint must remain explicitly unsupported");
assert(browser.measurements.reconnectToRedraw.supported === false, "ttyd reconnect-to-redraw must remain explicitly unsupported");
assert(browser.measurements.slowConsumer.supported === false, "ttyd slow-consumer measurement must remain explicitly unsupported");

function readReport(name, maxBytes) {
  const reportPath = path.join(reportDir, name);
  const stat = fs.statSync(reportPath);
  assert(stat.size > 0 && stat.size <= maxBytes, `${name} is empty or unbounded`);
  if (process.platform !== "win32") {
    assert((stat.mode & 0o077) === 0, `${name} permissions are not private`);
  }
  const encoded = fs.readFileSync(reportPath, "utf8");
  for (const expression of forbidden) {
    assert(!expression.test(encoded), `${name} contains forbidden terminal or credential-shaped content`);
  }
  return JSON.parse(encoded);
}

function assertMeasurements(measurements, names = metricNames) {
  assert(measurements && typeof measurements === "object" && !Array.isArray(measurements), "missing measurements");
  for (const name of names) {
    const metric = measurements[name];
    assert(metric && typeof metric.supported === "boolean", `missing support status for ${name}`);
    if (metric.supported) {
      assert(Number.isFinite(metric.value) && metric.value >= 0 && typeof metric.unit === "string" && metric.unit.length > 0, `invalid value for ${name}`);
    } else {
      assert(typeof metric.reason === "string" && metric.reason.length > 0, `missing unsupported reason for ${name}`);
    }
  }
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}
