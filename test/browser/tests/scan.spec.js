// Scanning pages into a PDF (docs/scan.md). The tests choose pictures with the file input
// that, on a phone, opens the camera; a desktop browser shows a file chooser instead.
const { test, expect } = require("./helpers");

async function scanFolder(page, browserName) {
  const dir = `scans-${browserName}-${Date.now()}`;
  expect((await page.request.fetch(`/dav/files/${dir}`, { method: "MKCOL" })).status()).toBe(201);
  await page.goto(`/#/files/${dir}`);
  await expect(page.locator(".empty")).toBeVisible();
  return dir;
}

const picture = async (page, name) => ({
  name, mimeType: "image/png", buffer: await (await page.request.get(`/dav/files/${name}`)).body(),
});

test("pictures become the pages of one PDF, turned and in order", async ({ page, browserName }) => {
  const dir = await scanFolder(page, browserName);
  await page.locator("#scan-btn").click();
  const scan = page.locator("#scan");
  await expect(scan).toBeVisible();
  await expect(page.locator("#scan-save")).toBeDisabled();

  await page.locator("#scan-pick").setInputFiles([await picture(page, "a-photo.png"), await picture(page, "b-photo.png")]);
  const pages = scan.locator(".pg:not(.add)");
  await expect(pages).toHaveCount(2);
  await expect(page.locator("#scan-save")).toHaveText("Save PDF (2 pages)");

  // b-photo (300×500, portrait) first, turned to landscape.
  await pages.nth(1).getByTitle("Earlier").click();
  await pages.nth(0).getByTitle("Turn right").click();
  const size = i => pages.nth(i).locator("canvas").evaluate(c => [c.width, c.height]);
  expect(await size(0)).toEqual([480, 288]);
  expect(await size(1)).toEqual([480, 300]);

  await page.locator("#scan-name").fill("Letter");
  await page.locator("#scan-save").click();
  await expect(scan).toBeHidden();
  await expect(page.locator(".toast")).toContainText("Saved Letter.pdf");
  await expect(page.locator("td.name a", { hasText: "Letter.pdf" })).toBeVisible();

  const pdf = (await (await page.request.get(`/dav/files/${dir}/Letter.pdf`)).body()).toString("latin1");
  expect(pdf.startsWith("%PDF-1.4")).toBeTruthy();
  expect(pdf.match(/\/Type \/Page /g)).toHaveLength(2);
  const sizes = [...pdf.matchAll(/\/Width (\d+) \/Height (\d+)/g)].map(m => [+m[1], +m[2]]);
  expect(sizes).toEqual([[500, 300], [640, 400]]);

  // PDF.js opens it, with both pages.
  await page.locator("td.name a", { hasText: "Letter.pdf" }).click();
  await expect(page.locator("#v-body .page canvas").first()).toBeVisible({ timeout: 20_000 });
  await expect(page.locator("#v-tools .v-count")).toHaveText("p. 1 of 2");
});

test("leaving a scan with pages asks first", async ({ page, browserName }) => {
  await scanFolder(page, browserName);
  await page.locator("#scan-btn").click();
  await page.locator("#scan-pick").setInputFiles([await picture(page, "a-photo.png")]);
  await expect(page.locator("#scan .pg:not(.add)")).toHaveCount(1);

  await page.goBack();
  const dlg = page.locator("#dlg");
  await expect(dlg.locator("h2")).toHaveText("Discard this scan?");
  await dlg.getByRole("button", { name: "Cancel" }).click();
  await expect(page.locator("#scan")).toBeVisible();
  await expect(page.locator("#scan .pg:not(.add)")).toHaveCount(1);

  await page.locator("#scan-close").click();
  await dlg.getByRole("button", { name: "Discard" }).click();
  await expect(page.locator("#scan")).toBeHidden();
  await expect(page.locator(".empty")).toBeVisible();
});
