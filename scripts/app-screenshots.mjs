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

// Bottom NavigationBar: six destinations for an admin, evenly spaced.
const NAV_Y = 815;
const navX = (i) => Math.round((390 / 6) * (i + 0.5));
// Chat is the home tab now; Pipelines moved off the bar into Fleet's app bar.
const TABS = [
  { i: 0, name: "chat" },
  { i: 1, name: "fleet" },
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

  const ok = await signIn(page, { server: API, email: EMAIL, password: PASSWORD });
  if (!ok) {
    // Better no screenshot than a login screen filed as the app.
    await ctx.close();
    throw new Error(`sign-in did not complete (${scheme})`);
  }
  // The chat's first load fetches the channel, the fleet and the setup plan.
  // With semantics on, the first message's author is a real node: wait for
  // it (up to 45 s on a loaded host) instead of filing the spinner as the
  // product, then a beat for the paint.
  await page.getByText("Oaf", { exact: false }).first().waitFor({ timeout: 45000 }).catch(() => {});
  await page.waitForTimeout(2500);

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
