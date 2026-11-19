// Takes the README's screenshots (docs/screenshots/) of a running demo:
//
//   test/demo.sh                       # in another terminal
//   cd test/browser && node screenshots.js [http://localhost:18080] [../../docs/screenshots]
//
// Every screenshot shows only the demo files of test/demo-data.sh.
"use strict";
const path = require("path");
const fs = require("fs");
const { chromium } = require("@playwright/test");

const base = process.argv[2] || "http://localhost:18080";
const out = path.resolve(process.argv[3] || path.join(__dirname, "../../docs/screenshots"));

async function shoot(browser, name, { width = 1200, height = 720, mobile = false } = {}, steps) {
  const context = await browser.newContext({
    viewport: { width, height }, deviceScaleFactor: 2, isMobile: mobile, hasTouch: mobile,
    colorScheme: "light", locale: "en-GB",
  });
  const page = await context.newPage();
  const r = await page.request.post(base + "/api/login", { data: { username: "demo", password: "demo" } });
  if (!r.ok()) throw new Error("login failed: is test/demo.sh running at " + base + "?");
  await steps(page);
  await page.waitForTimeout(400); // let fonts, images and the PDF settle
  await page.screenshot({ path: path.join(out, name + ".png") });
  await context.close();
  console.log("wrote", name + ".png");
}

const search = async (page, q) => {
  await page.goto(base + "/#/files");
  await page.locator("#q").fill(q);
  await page.locator("#q").press("Enter");
  await page.locator("a.hit").first().waitFor();
  await page.locator("#q").evaluate(q => { q.blur(); q.scrollLeft = 0; });
};

(async () => {
  fs.mkdirSync(out, { recursive: true });
  const browser = await chromium.launch({ args: ["--disable-spell-checking", "--disable-features=SpellCheck"] });
  try {
    await shoot(browser, "search", { height: 520 }, page => search(page, "Kundennummer"));
    await shoot(browser, "folder", { height: 480 }, async page => {
      await page.goto(base + "/#/files/demo/scans");
      await page.locator("td.name").first().waitFor();
    });
    await shoot(browser, "viewer-photo", {}, async page => {
      await page.goto(base + "/#/files/demo/photos?view=apollo-11-plaque.jpg");
      await page.locator("#v-body img").evaluate(i => i.complete || new Promise(r => i.onload = r));
    });
    await shoot(browser, "viewer-pdf", {}, async page => {
      await page.goto(base + "/#/files/demo/pdf?view=grundgesetz.pdf");
      await page.locator(".v-pdf .page canvas").first().waitFor();
    });
    await shoot(browser, "viewer-office", {}, async page => {
      await page.goto(base + "/#/files/demo/office?view=gettysburg-address.docx");
      await page.locator("#v-body iframe:not(.opening)").waitFor({ timeout: 30_000 });
      await page.waitForTimeout(1500);
    });
    await shoot(browser, "phone-search", { width: 390, height: 600, mobile: true }, page => search(page, "peace mankind"));
  } finally {
    await browser.close();
  }
})().catch(e => { console.error(e); process.exit(1); });
