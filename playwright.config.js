const { defineConfig, devices } = require("@playwright/test");

// Playwright's automatic failure context otherwise captures live page text.
process.env.PLAYWRIGHT_NO_COPY_PROMPT = "1";

module.exports = defineConfig({
  testDir: "./test/playwright",
  timeout: 45_000,
  expect: {
    timeout: 10_000
  },
  fullyParallel: false,
  workers: 1,
  preserveOutput: "never",
  reporter: [[require.resolve("./test/playwright/content_free_reporter.js")]],
  use: {
    ignoreHTTPSErrors: true,
    trace: "off",
    screenshot: "off",
    video: "off",
    viewport: { width: 1280, height: 820 }
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] }
    },
    {
      name: "firefox",
      use: { browserName: "firefox", viewport: { width: 1280, height: 820 } }
    },
    {
      name: "webkit",
      use: { browserName: "webkit", viewport: { width: 1280, height: 820 } }
    }
  ]
});
