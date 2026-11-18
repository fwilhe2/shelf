// Browser tests of shelf's web UI, in Chromium and Firefox. See README.md here.
const { defineConfig, devices } = require("@playwright/test");

const port = Number(process.env.PORT || 18090);

module.exports = defineConfig({
  testDir: "tests",
  timeout: 60_000,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }], ["list"]] : "list",
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"] } },
    { name: "firefox", use: { ...devices["Desktop Firefox"] } },
  ],
  webServer: {
    command: "./server.sh",
    url: `http://127.0.0.1:${port}/`,
    env: { PORT: String(port) },
    timeout: 120_000, // includes go build
    reuseExistingServer: false,
    stdout: "ignore",
    stderr: "pipe",
  },
});
