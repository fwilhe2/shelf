// The file viewer: pictures, video, music, PDFs and text (web/index.html, VIEW).
const { test, expect, open, viewer } = require("./helpers");

test("viewable files link to the viewer, others to the file itself", async ({ page }) => {
  await expect(page.locator("td.name a", { hasText: "testpage.pdf" })).toHaveAttribute("href", "#/files?view=testpage.pdf");
  await expect(page.locator("td.name a", { hasText: "blob.bin" })).toHaveAttribute("href", "/dav/files/blob.bin");
});

test("pictures: arrow keys flip through the folder, Esc closes", async ({ page }) => {
  const v = viewer(page);
  await open(page, "a-photo.png");
  await expect(v.root).toBeVisible();
  await expect(v.title).toHaveText("a-photo.png");
  await expect(page).toHaveURL(/#\/files\?view=a-photo\.png$/);
  const img = v.body.locator("img");
  await expect.poll(() => img.evaluate(i => i.complete && i.naturalWidth)).toBe(640);

  await page.keyboard.press("ArrowRight");
  await expect(v.title).toHaveText("b-photo.png");
  await expect(v.count).toHaveText(/^2 \/ \d+$/);
  await expect.poll(() => v.body.locator("img").evaluate(i => i.complete && i.naturalWidth)).toBe(300);
  await expect(page.locator("#v-prev")).toBeEnabled();

  await page.keyboard.press("ArrowLeft");
  await expect(v.title).toHaveText("a-photo.png");
  await expect(page.locator("#v-prev")).toBeDisabled();

  await page.keyboard.press("Escape");
  await expect(v.root).toBeHidden();
  await expect(page).toHaveURL(/#\/files$/);
});

test("a picture bigger than the viewer is scaled down to fit it", async ({ page }) => {
  const v = viewer(page);
  await open(page, "large-photo.png");
  const img = v.body.locator("img");
  await expect.poll(() => img.evaluate(i => i.complete && i.naturalWidth)).toBe(1600);
  const [box, body] = [await img.boundingBox(), await v.body.boundingBox()];
  expect(box.height).toBeLessThanOrEqual(body.height + 1);
  expect(box.width).toBeLessThanOrEqual(body.width + 1);
  expect(box.y).toBeGreaterThanOrEqual(body.y - 1);
});

test("the browser's Back button closes the viewer", async ({ page }) => {
  await open(page, "notes.md");
  await expect(viewer(page).root).toBeVisible();
  await page.goBack();
  await expect(viewer(page).root).toBeHidden();
  await expect(page).toHaveURL(/#\/files$/);
});

test("video plays", async ({ page }) => {
  await open(page, "clip.webm");
  const video = viewer(page).body.locator("video");
  await expect.poll(() => video.evaluate(m => m.error?.code ?? (m.currentTime > 0.2 && m.videoWidth))).toBe(160);
});

test("music plays", async ({ page }) => {
  await open(page, "ding.wav");
  const audio = viewer(page).body.locator("audio");
  await expect.poll(() => audio.evaluate(m => m.error?.code ?? m.currentTime > 0.2)).toBe(true);
});

test("PDF: rendered by PDF.js, with selectable text, page counter and zoom", async ({ page }) => {
  const v = viewer(page);
  await open(page, "testpage.pdf");
  await expect(v.body.locator(".page canvas").first()).toBeVisible({ timeout: 20_000 });
  await expect(v.body.locator(".textLayer").first()).toContainText("Hello from shelf");
  await expect(v.tools.locator(".v-count")).toHaveText("p. 1 of 2");
  const width = () => v.body.locator(".page").first().evaluate(p => p.getBoundingClientRect().width);
  const before = await width();
  await v.tools.getByTitle("Zoom in").click();
  await expect.poll(width).toBeGreaterThan(before * 1.05);
});

test("text files are shown as text", async ({ page }) => {
  await open(page, "notes.md");
  await expect(viewer(page).body.locator("pre.v-text")).toContainText("Hello from shelf.");
});

test("a link to a viewed file opens it on its own, e.g. in a new tab", async ({ page }) => {
  await page.goto("/#/files?view=notes.md");
  await expect(viewer(page).body.locator("pre.v-text")).toContainText("Hello from shelf.");
  await page.keyboard.press("Escape");
  await expect(page).toHaveURL(/#\/files$/);
  await expect(page.locator("td.name a", { hasText: "notes.md" })).toBeVisible();
});

test("a link to a file that isn't there says so", async ({ page }) => {
  await page.goto("/#/files?view=missing.txt");
  await expect(page.locator(".toast")).toHaveText("“missing.txt” is not in this folder");
  await expect(viewer(page).root).toBeHidden();
});

test.describe("on a phone", () => {
  test.use({ viewport: { width: 390, height: 800 } });

  test("the file name keeps its room in the bar", async ({ page }) => {
    await open(page, "testpage.pdf");
    await expect(viewer(page).body.locator(".page canvas").first()).toBeVisible({ timeout: 20_000 });
    const title = viewer(page).title;
    await expect(title).toHaveText("testpage.pdf");
    expect(await title.evaluate(t => t.clientWidth)).toBeGreaterThan(90);
  });
});

test("About lists the third-party licences, and every one of them is served", async ({ page }) => {
  await page.locator("#about-btn").click();
  const links = page.locator("#dlg .about-list a");
  await expect(links.first()).toBeVisible();
  const hrefs = await links.evaluateAll(as => as.map(a => a.getAttribute("href")).filter(h => h.startsWith("/")));
  expect(hrefs.length).toBeGreaterThan(8);
  for (const href of hrefs) {
    const r = await page.request.get(href);
    expect(r.status(), href).toBe(200);
  }
});
