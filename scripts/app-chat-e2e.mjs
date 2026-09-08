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
const values = { server: API, email: EMAIL, password: PASSWORD };
for (const [name, xy] of Object.entries(FIELD)) {
  if (name === "signIn") continue;
  await page.mouse.click(...xy);
  await page.keyboard.press("Control+A");
  await page.keyboard.type(values[name], { delay: 20 });
}
await page.mouse.click(...FIELD.signIn);
await page.waitForTimeout(9000);

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
