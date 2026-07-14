"use strict";

class ContentFreeReporter {
  constructor() {
    this.counts = new Map();
  }

  printsToStdio() {
    return true;
  }

  onBegin(_config, suite) {
    console.log(`Running ${suite.allTests().length} tests with content-free diagnostics.`);
  }

  onTestEnd(test, result) {
    let suite = test.parent;
    while (suite && !suite.project()) {
      suite = suite.parent;
    }
    const projectName = suite && suite.project() ? suite.project().name : "unknown";
    const reason = result.status === "passed" || result.status === "skipped"
      ? ""
      : ` (${failureReason(result)})`;
    console.log(`${result.status.toUpperCase()} [${projectName}] ${test.title}${reason}`);
    this.counts.set(result.status, (this.counts.get(result.status) || 0) + 1);
  }

  onError() {
    console.error("Playwright runner error; details suppressed by the content-free reporter.");
  }

  onEnd(result) {
    const passed = this.counts.get("passed") || 0;
    const skipped = this.counts.get("skipped") || 0;
    const failed = ["failed", "timedOut", "interrupted"]
      .reduce((total, status) => total + (this.counts.get(status) || 0), 0);
    console.log(`${passed} passed, ${failed} failed, ${skipped} skipped; run ${result.status}.`);
  }
}

function failureReason(result) {
  const message = result.error && typeof result.error.message === "string"
    ? result.error.message
    : "";
  const siteMatch = message.match(/\[site:([a-z0-9-]+)\]/);
  const stepSite = fixedFailureStepSite(result.steps || []);
  const site = siteMatch ? `:${siteMatch[1]}` : (stepSite ? `:${stepSite}` : "");
  if (message.includes("ERR_NETWORK_CHANGED") || message.includes("network-changed")) return `network-changed${site}`;
  if (message.includes("browserType.launch")) return `browser-launch${site}`;
  if (result.status === "timedOut" || /timeout/i.test(message)) return `timeout${site}`;
  if (result.status === "interrupted") return `interrupted${site}`;
  return `assertion${site}`;
}

function fixedFailureStepSite(steps) {
  const fixedSteps = new Map([
    ["iPhone portrait", "mobile-header-portrait"],
    ["iPhone landscape", "mobile-header-landscape"],
    ["current iPhone viewport", "mobile-profile-current-iphone"],
    ["oldest-supported iPhone viewport", "mobile-profile-oldest-iphone"],
    ["iPad portrait", "mobile-profile-ipad-portrait"],
    ["iPad landscape", "mobile-profile-ipad-landscape"],
    ["iPad Split View", "mobile-profile-ipad-split-view"],
    ["iPad Stage Manager", "mobile-profile-ipad-stage-manager"]
  ]);
  for (const step of steps) {
    if (step.error && fixedSteps.has(step.title)) return fixedSteps.get(step.title);
    const nested = fixedFailureStepSite(step.steps || []);
    if (nested) return nested;
  }
  return "";
}

module.exports = ContentFreeReporter;
