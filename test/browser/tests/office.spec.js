// Office documents in grind's page, inside the viewer (web/index.html, VIEW.office;
// docs/office.md). Needs GRIND_WEB, see ../README.md.
const { test, expect, open, viewer } = require("./helpers");

test.beforeEach(async ({ page }) => {
  const me = await (await page.request.get("/api/me", { headers: { "X-Requested-With": "shelf" } })).json();
  test.skip(!me.office?.length, "office documents are off: set GRIND_WEB");
});

// openOffice opens a document and records, every animation frame, whether grind's own
// page was on screen and what it showed: it must never show its welcome pane.
async function openOffice(page, name) {
  await page.evaluate(() => {
    window.seen = [];
    const look = () => {
      const f = document.querySelector("#v-body iframe");
      if (f && !f.classList.contains("opening")) {
        const w = f.contentDocument?.getElementById("welcome");
        window.seen.push(w && !w.hidden ? "welcome" : "document");
      }
      if (window.seen.length < 5) requestAnimationFrame(look);
    };
    requestAnimationFrame(look);
  });
  await open(page, name);
}
const frame = page => page.frameLocator("#v-body iframe");

test("a spreadsheet opens in one piece, under its real name", async ({ page }) => {
  const v = viewer(page);
  await openOffice(page, "My budget.fods");
  await expect(v.body.locator("iframe:not(.opening)")).toBeVisible({ timeout: 30_000 });
  await expect(v.body.locator(".v-wait")).toHaveCount(0);
  await expect(frame(page).locator("#name")).toHaveText("My budget.fods");
  await expect(frame(page).locator("#body")).toContainText("Groceries");
  await expect.poll(() => page.evaluate(() => window.seen.length)).toBeGreaterThan(0);
  expect(await page.evaluate(() => window.seen)).not.toContain("welcome");
  await expect(page.locator("#v-raw")).toBeHidden(); // the raw XML is no use to anybody
});

test("a file grind can't open shows why, and offers the download", async ({ page }) => {
  const v = viewer(page);
  await open(page, "broken.ods");
  await expect(v.body.locator(".v-msg")).toContainText("not an OpenDocument file", { timeout: 30_000 });
  await expect(v.body.locator("iframe")).toHaveCount(0);
  await expect(v.body.locator(".v-msg a")).toHaveAttribute("href", "/dav/files/broken.ods");
});

test("closing with unsaved edits asks first, from the close button and from Back", async ({ page }) => {
  const v = viewer(page);
  await open(page, "My budget.fods");
  await expect(v.body.locator("iframe:not(.opening)")).toBeVisible({ timeout: 30_000 });

  // No edits yet: closes right away.
  await page.locator("#v-close").click();
  await expect(v.root).toBeHidden();

  await open(page, "My budget.fods");
  await expect(v.body.locator("iframe:not(.opening)")).toBeVisible({ timeout: 30_000 });
  await frame(page).locator("#formula").fill("edited");
  await frame(page).locator("#formula").press("Enter");
  await expect(frame(page).locator("#undo")).toBeEnabled();

  const dlg = page.locator("#dlg");
  await page.locator("#v-close").click();
  await expect(dlg).toContainText("Discard your changes?");
  await dlg.getByRole("button", { name: "Cancel" }).click();
  await expect(v.root).toBeVisible();
  await expect(page).toHaveURL(/\?view=My%20budget\.fods$/);
  await expect(frame(page).locator("#body")).toContainText("edited");

  await page.goBack();
  await expect(dlg).toContainText("Discard your changes?");
  await dlg.getByRole("button", { name: "Discard" }).click();
  await expect(v.root).toBeHidden();
  await expect(page).toHaveURL(/#\/files$/);
});

test("flipping away from an edited document asks too", async ({ page }) => {
  const v = viewer(page);
  await open(page, "My budget.fods");
  await expect(v.body.locator("iframe:not(.opening)")).toBeVisible({ timeout: 30_000 });
  await frame(page).locator("#formula").fill("edited");
  await frame(page).locator("#formula").press("Enter");
  await expect(frame(page).locator("#undo")).toBeEnabled();
  await page.locator("#v-next").click();
  await expect(page.locator("#dlg")).toContainText("Discard your changes?");
  await page.locator("#dlg").getByRole("button", { name: "Cancel" }).click();
  await expect(v.title).toHaveText("My budget.fods");
});
