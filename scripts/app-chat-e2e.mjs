// Drives the companion app's fleet chat end to end from the Flutter web build:
// sign in, tap a quick-action chip, type a slash command, and photograph the
// results. Coordinate-driven at 390x844 like app-screenshots.mjs (Flutter
// paints to a canvas; there is no DOM to query), so the viewport is fixed.
//
// Run the same way as app-screenshots.mjs (see the Makefile's app-screenshots
// target); writes app-chat-e2e-*.png into OUT_DIR.
import { chromium } from "playwright";

const BASE = process.env.APP_URL ?? "http://localhost:8099";
const API = process.env.API_URL ?? "http://api:8080";
const EMAIL = process.env.AF_EMAIL ?? "demo@agentfleet.local";
const PASSWORD = process.env.AF_PASSWORD ?? "agentfleet-demo-1234";
const OUT = process.env.OUT_DIR ?? "/out";

// Same login geometry as app-screenshots.mjs. The server field matters: the
// build defaults to the Android emulator's 10.0.2.2, which is nowhere from
// inside this container, and sign-in then spins forever.
const FIELD = { server: [195, 393], email: [195, 453], password: [195, 513], signIn: [195, 576] };

// Flutter paints to a canvas, so there is no DOM to fill() -- unless the
// semantics tree is switched on, which Flutter offers through a hidden
// placeholder button for assistive technology. Clicking it gives real,
// labelled inputs to type into, and a "Sign in" button whose disappearance
// proves the login actually happened. Coordinates stay as the fallback: on a
// loaded CI box the first frame can paint late and a click at a fixed point
// lands on nothing, which is how a run once shipped a folder of login screens
// as "screenshots of the app".
async function signIn(page, { server, email, password }) {
  const placeholder = page.locator("flt-semantics-placeholder");
  if (await placeholder.count()) {
    await placeholder.first().click({ force: true }).catch(() => {});
    await page.waitForTimeout(800);
  }
  const byLabel = async (label, value) => {
    const el = page.getByLabel(label, { exact: false }).first();
    if (!(await el.count())) return false;
    await el.click({ force: true });
    await page.keyboard.press("Control+a");
    await page.keyboard.type(value, { delay: 15 });
    return true;
  };
  let viaTree = await byLabel("Orchestrator URL", server);
  if (viaTree) {
    await byLabel("Email", email);
    await byLabel("Password", password);
    const btn = page.getByRole("button", { name: /sign in/i }).first();
    if (await btn.count()) await btn.click({ force: true });
    else viaTree = false;
  }
  if (!viaTree) {
    const clickType = async ([x, y], text) => {
      await page.mouse.click(x, y);
      await page.waitForTimeout(400);
      await page.keyboard.press("Control+a");
      await page.keyboard.type(text, { delay: 15 });
    };
    await clickType(FIELD.server, server);
    await clickType(FIELD.email, email);
    await clickType(FIELD.password, password);
    await page.mouse.click(...FIELD.signIn);
  }
  // Wait for the login screen to go, up to 30s; the semantics tree tells us.
  for (let i = 0; i < 30; i++) {
    await page.waitForTimeout(1000);
    const still = await page.getByRole("button", { name: /sign in/i }).count().catch(() => 0);
    if (!still) return true;
  }
  console.log("  [login] still on the sign-in screen after 30s");
  return false;
}
// Chat screen geometry (see app-chat-dark.png): quick chips sit just above the
// composer; the composer field is centred at y≈722.
const CHIP_BOTS = [43, 661];
const COMPOSER = [175, 722];
const SEND = [345, 732];

// Headed under xvfb, like app-screenshots.mjs: CanvasKit paints nothing in
// headless Chromium and every capture comes out white.
const browser = await chromium.launch({ headless: false });
const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, colorScheme: "dark" });
const page = await ctx.newPage();
page.on("pageerror", (e) => console.log(`  [page uncaught] ${String(e).slice(0, 200)}`));

await page.goto(BASE, { waitUntil: "networkidle" });
await page.waitForTimeout(7000);
if (!(await signIn(page, { server: API, email: EMAIL, password: PASSWORD }))) {
  await browser.close();
  throw new Error("sign-in did not complete");
}
await page.waitForTimeout(2500);

// 1. A quick-action chip runs /bots and renders the result card.
await page.mouse.click(...CHIP_BOTS);
await page.waitForTimeout(4000);
await page.screenshot({ path: `${OUT}/app-chat-e2e-bots.png` });
console.log("  app-chat-e2e-bots.png");

// 2. Typing a slash opens the command panel.
await page.mouse.click(...COMPOSER);
await page.waitForTimeout(800);
await page.keyboard.type("/he", { delay: 40 });
await page.waitForTimeout(1500);
await page.screenshot({ path: `${OUT}/app-chat-e2e-palette.png` });
console.log("  app-chat-e2e-palette.png");

// 3. Sending /help renders the command list as a card.
await page.keyboard.type("lp", { delay: 40 });
await page.mouse.click(...SEND);
await page.waitForTimeout(4000);
await page.screenshot({ path: `${OUT}/app-chat-e2e-help.png` });
console.log("  app-chat-e2e-help.png");

await browser.close();
