// Companion-app screenshots, captured from the Flutter web build.
//
// Flutter renders to a canvas, so there is no DOM for selectors to find —
// everything here is coordinate-driven at a fixed 390x844 viewport (an
// ordinary phone), which is why the viewport must never change without
// re-deriving the coordinates. deviceScaleFactor 2 makes the PNGs crisp at
// README size without moving a single coordinate.
//
// Run via docker on the stack's network (see `make app-screenshots`):
//   the web build is served from inside the container, and the app is signed
//   in against the real API, so the screens show a real fleet rather than
//   staged mock data. Seed a couple of instances first or the Fleet tab
//   photographs an empty state.
import { chromium } from "playwright";

const BASE = process.env.APP_URL ?? "http://localhost:8099";
const API = process.env.API_URL ?? "http://api:8080";
const EMAIL = process.env.AF_EMAIL ?? "demo@agentfleet.local";
const PASSWORD = process.env.AF_PASSWORD ?? "agentfleet-demo-1234";
const OUT = process.env.OUT_DIR ?? "/out";

// Login form geometry at 390x844 (Flutter lays this screen out from the
// centre, so these survive content changes above and below the card).
const FIELD = { server: [195, 393], email: [195, 453], password: [195, 513], signIn: [195, 576] };

// Bottom NavigationBar: six destinations for an admin, evenly spaced.
const NAV_Y = 815;
const navX = (i) => Math.round((390 / 6) * (i + 0.5));
const TABS = [
  { i: 0, name: "fleet" },
  { i: 1, name: "pipelines" },
  { i: 2, name: "vault" },
  { i: 3, name: "alerts" },
  { i: 4, name: "settings" },
];

async function captureTheme(browser, scheme) {
  const ctx = await browser.newContext({
    viewport: { width: 390, height: 844 },
    deviceScaleFactor: 2,
    colorScheme: scheme,
  });
  const page = await ctx.newPage();
  // A Flutter app that throws on startup renders nothing and says why only
  // here. Surfacing it beats staring at a blank PNG.
  page.on("console", (m) => {
    if (m.type() === "error") console.log(`  [page] ${m.text().slice(0, 200)}`);
  });
  page.on("pageerror", (e) => console.log(`  [page uncaught] ${String(e).slice(0, 200)}`));
  await page.goto(BASE, { waitUntil: "networkidle" });
  // CanvasKit compiles and the first frame paints well after networkidle.
  await page.waitForTimeout(7000);

  if (scheme === "dark") {
    await page.screenshot({ path: `${OUT}/app-login-dark.png` });
  }

  // Sign in. click + keyboard, because there is no DOM to fill().
  const clickType = async ([x, y], text) => {
    await page.mouse.click(x, y);
    await page.waitForTimeout(400);
    await page.keyboard.press("Control+a");
    await page.keyboard.type(text, { delay: 15 });
  };
  await clickType(FIELD.server, API);
  await clickType(FIELD.email, EMAIL);
  await clickType(FIELD.password, PASSWORD);
  await page.mouse.click(...FIELD.signIn);
  await page.waitForTimeout(6000);

  for (const tab of TABS) {
    await page.mouse.click(navX(tab.i), NAV_Y);
    await page.waitForTimeout(2500);
    await page.screenshot({ path: `${OUT}/app-${tab.name}-${scheme}.png` });
    console.log(`  app-${tab.name}-${scheme}.png`);
  }
  await ctx.close();
}

// Headed under xvfb, not headless: Flutter's CanvasKit renderer paints a
// blank canvas in headless Chromium (no usable GL surface), and a screenshot
// of a blank canvas looks exactly like success with an empty screen. The
// Playwright image ships xvfb; `make app-screenshots` wraps this in xvfb-run.
const browser = await chromium.launch({ headless: false });
for (const scheme of ["dark", "light"]) {
  console.log(`${scheme}:`);
  await captureTheme(browser, scheme);
}
await browser.close();
console.log("done");
