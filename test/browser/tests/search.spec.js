// Search (docs/search.md), with server.sh's stand-in for clerk.
const { test, expect, viewer } = require("./helpers");

test("search finds files, marks the words and opens them in the viewer", async ({ page }) => {
  await page.locator("#q").fill("hello");
  await page.locator("#q").press("Enter");
  await expect(page).toHaveURL(/#\/\?q=hello$/);
  const hits = page.locator("a.hit");
  await expect(hits).toHaveCount(3);
  await expect(page.locator(".hits-note")).toHaveText(/^3 files found · (not everything is indexed yet|index updated .*)$/);

  const pdf = hits.filter({ hasText: "testpage.pdf" });
  await expect(pdf.locator(".hit-dir")).toHaveText("files");
  await expect(pdf.locator("mark")).toHaveText("Hello");
  await expect(hits.filter({ hasText: "blob.bin" })).toHaveAttribute("href", "/dav/files/blob.bin");
  await expect(hits.filter({ hasText: "notes.md" })).toHaveAttribute("href", "#/files?view=notes.md");

  await pdf.click();
  const v = viewer(page);
  await expect(v.root).toBeVisible();
  await expect(v.title).toHaveText("testpage.pdf");
  await page.keyboard.press("Escape");
  await expect(v.root).toBeHidden();
  await expect(page).toHaveURL(/#\/\?q=hello$/);
  await expect(hits).toHaveCount(3);
  await expect(page.locator("#q")).toHaveValue("hello");
});

test("a search that finds nothing says so", async ({ page }) => {
  await page.locator("#q").fill("nothing here");
  await page.locator("#q").press("Enter");
  await expect(page.locator(".empty")).toContainText("Nothing found for “nothing here”.");
  await page.locator(".crumbs a", { hasText: "Shares" }).click();
  await expect(page.locator(".share")).toHaveCount(1);
});
