const { defineConfig, devices } = require("@playwright/test");

module.exports = defineConfig({
  testDir: "./test/playwright",
  timeout: 45_000,
  expect: {
    timeout: 10_000
  },
  fullyParallel: false,
  workers: 1,
  reporter: [["list"], ["html", { open: "never" }]],
  use: {
    ignoreHTTPSErrors: true,
    trace: "retain-on-failure",
    viewport: { width: 1280, height: 820 }
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] }
    }
  ]
});
