// Shared steps for the browser tests.
const { test: base, expect } = require("@playwright/test");

// `test` with a page that is logged in as alice and shows the share "files".
// Errors thrown by the page fail the test.
const test = base.extend({
  page: async ({ page }, use) => {
    const errors = [];
    page.on("pageerror", e => errors.push(e.message));
    const r = await page.request.post("/api/login", { data: { username: "alice", password: "pw" } });
    expect(r.ok()).toBeTruthy();
    await page.goto("/#/files");
    await expect(page.locator("td.name").first()).toBeVisible();
    await use(page);
    expect(errors, "errors thrown by the page").toEqual([]);
  },
});

// open clicks a file's name in the list.
const open = (page, name) => page.locator("td.name a", { hasText: name }).click();

// The viewer's parts.
const viewer = page => ({
  root: page.locator("#viewer"),
  title: page.locator("#v-title"),
  count: page.locator(".v-bar > .v-count"),
  tools: page.locator("#v-tools"),
  body: page.locator("#v-body"),
});

module.exports = { test, expect, open, viewer };
