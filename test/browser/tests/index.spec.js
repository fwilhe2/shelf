// The search index dialog (docs/search.md), with server.sh's stand-in for clerk.
const { test, expect } = require("./helpers");

test("the index dialog tells how current the index is, and updates it", async ({ page }) => {
  await page.locator("#index-btn").click();
  const dlg = page.locator("#dlg");
  await expect(dlg.locator("h2")).toHaveText("Search index");
  const row = dlg.locator("tbody tr").first();
  await expect(row.locator("td").first()).toHaveText("files");
  await expect(row.locator("td").nth(1)).toHaveText("12 files");
  await expect(dlg).toContainText("every hour");
  await expect(dlg).toContainText("10 documents, 9 with text.");

  await dlg.locator("#ix-update").click();
  await expect(dlg.locator(".ix-state")).toHaveText(/Updating now|Starting/);
  await expect(dlg.locator("#ix-update")).toBeDisabled();
  await expect(dlg.locator(".ix-state")).toHaveText(/^Updated (just now|1 minute ago)$/, { timeout: 15_000 });
  await expect(row.locator("td").nth(2)).toHaveText(/just now|1 minute ago/);
  await expect(dlg.locator("tr.rep")).toHaveText("Last run: 12 files, 1 read; 1 added, 0 moved, 0 copied, 0 changed, 0 removed; 9 with text");
  await expect(dlg.locator("#ix-update")).toBeEnabled();

  await dlg.getByRole("button", { name: "Close" }).click();
  await expect(dlg).toBeHidden();

  // A search says how old the index is, and links to the dialog.
  await page.locator("#q").fill("hello");
  await page.locator("#q").press("Enter");
  const age = page.locator(".hits-note a");
  await expect(age).toHaveText(/index updated (just now|1 minute ago)/);
  await age.click();
  await expect(dlg.locator("h2")).toHaveText("Search index");
});
