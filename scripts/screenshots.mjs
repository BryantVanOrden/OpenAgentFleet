/**
 * Capture documentation screenshots of the admin console.
 *
 * Runs headless in a Playwright container joined to the compose network, so it
 * talks to the console the same way a browser on the host does — through nginx,
 * with the real API behind it. Nothing here is mocked; if a page is broken the
 * screenshot shows it broken, which is the point.
 *
 *   docker run --rm --network agentfleet_control \
 *     -v "$PWD/scripts:/scripts" -v "$PWD/docs/images:/out" \
 *     -e BASE_URL=http://admin:80 -e AF_EMAIL=... -e AF_PASSWORD=... \
 *     mcr.microsoft.com/playwright:v1.49.1-noble \
 *     node /scripts/screenshots.mjs
 */

import { chromium } from "playwright";
import { mkdir } from "node:fs/promises";

const BASE = process.env.BASE_URL ?? "http://admin:80";
const EMAIL = process.env.AF_EMAIL ?? "demo@agentfleet.local";
const PASSWORD = process.env.AF_PASSWORD ?? "agentfleet-demo-1234";
const OUT = process.env.OUT_DIR ?? "/out";

const VIEWPORT = { width: 1440, height: 900 };

/** Themes to capture for the gallery. */
const THEMES = [
  ["dark", "amber"],
  ["dark", "blue"],
  ["dark", "purple"],
  ["dark", "green"],
  ["dark", "red"],
  ["light", "amber"],
  ["light", "blue"],
  ["light", "purple"],
  ["light", "green"],
  ["light", "red"],
];

const shots = [];

async function shot(page, name) {
  const path = `${OUT}/${name}.png`;
  await page.screenshot({ path });
  shots.push(name);
  console.log(`  captured ${name}.png`);
}

/** Set theme + session before any script runs, so there is no flash of default. */
async function seed(context, { mode, accent, token }) {
  await context.addInitScript(
    ([m, a, t]) => {
      localStorage.setItem("agentfleet.mode", m);
      localStorage.setItem("agentfleet.accent", a);
      if (t) localStorage.setItem("agentfleet.token", t);
    },
    [mode, accent, token ?? ""],
  );
}

async function login(page) {
  await page.goto(`${BASE}/`, { waitUntil: "networkidle" });
  await page.fill('input[type="email"]', EMAIL);
  await page.fill('input[type="password"]', PASSWORD);
  await page.click('button[type="submit"]');
  await page.waitForSelector("text=Fleet", { timeout: 20000 });
  await page.waitForTimeout(1200);
  return page.evaluate(() => localStorage.getItem("agentfleet.token"));
}

async function main() {
  await mkdir(OUT, { recursive: true });
  const browser = await chromium.launch();

  // ---- pass 1: sign in once and keep the token for every later context ----
  let ctx = await browser.newContext({ viewport: VIEWPORT, deviceScaleFactor: 2 });
  await seed(ctx, { mode: "dark", accent: "amber" });
  let page = await ctx.newPage();

  console.log("signing in…");
  const token = await login(page);
  if (!token) throw new Error("no session token after login — is the API reachable?");
  await ctx.close();

  // ---- the login screen, captured clean (dark and light) ----
  for (const mode of ["dark", "light"]) {
    ctx = await browser.newContext({ viewport: VIEWPORT, deviceScaleFactor: 2 });
    await seed(ctx, { mode, accent: "amber" });
    page = await ctx.newPage();
    await page.goto(`${BASE}/`, { waitUntil: "networkidle" });
    await page.waitForTimeout(600);
    await shot(page, `login-${mode}`);
    await ctx.close();
  }

  // ---- the signed-in console ----
  const pages = [
    ["fleet", "/fleet", "text=Fleet"],
    ["engines", "/models", "text=AI engines"],
    ["skills", "/skills", "text=Skills"],
    ["alerts", "/alerts", "text=Alerts"],
    ["settings", "/settings", "text=Settings"],
  ];

  for (const [mode, accent] of [
    ["dark", "amber"],
    ["light", "blue"],
  ]) {
    ctx = await browser.newContext({ viewport: VIEWPORT, deviceScaleFactor: 2 });
    await seed(ctx, { mode, accent, token });
    page = await ctx.newPage();

    for (const [name, path, ready] of pages) {
      await page.goto(`${BASE}${path}`, { waitUntil: "networkidle" });
      await page.waitForSelector(ready, { timeout: 15000 }).catch(() => {});
      await page.waitForTimeout(900);
      await shot(page, `${name}-${mode}-${accent}`);
    }

    // Launch dialog — the primary flow, and the most interesting screen.
    await page.goto(`${BASE}/fleet`, { waitUntil: "networkidle" });
    await page.waitForTimeout(700);
    const launch = page.locator("button", { hasText: "Launch an agent" }).last();
    if (await launch.count()) {
      await launch.click();
      await page.waitForTimeout(800);
      await shot(page, `launch-${mode}-${accent}`);
      await page.keyboard.press("Escape");
      await page.waitForTimeout(400);
    }

    // Bot catalog, if this build has it.
    const catalog = page.locator("button", { hasText: "Bot Catalog" }).first();
    if (await catalog.count()) {
      await catalog.click();
      await page.waitForTimeout(900);
      await shot(page, `catalog-${mode}-${accent}`);
      await page.keyboard.press("Escape");
      await page.waitForTimeout(400);
    }

    await ctx.close();
  }

  // ---- theme gallery: the same screen in every theme ----
  for (const [mode, accent] of THEMES) {
    ctx = await browser.newContext({
      viewport: { width: 1280, height: 800 },
      deviceScaleFactor: 2,
    });
    await seed(ctx, { mode, accent, token });
    page = await ctx.newPage();
    await page.goto(`${BASE}/fleet`, { waitUntil: "networkidle" });
    await page.waitForTimeout(900);
    await shot(page, `theme-${mode}-${accent}`);
    await ctx.close();
  }

  // ---- the theme picker itself, open ----
  ctx = await browser.newContext({ viewport: VIEWPORT, deviceScaleFactor: 2 });
  await seed(ctx, { mode: "dark", accent: "purple", token });
  page = await ctx.newPage();
  await page.goto(`${BASE}/fleet`, { waitUntil: "networkidle" });
  await page.waitForTimeout(800);
  const themeBtn = page.locator('button[aria-label="Theme"]').first();
  if (await themeBtn.count()) {
    await themeBtn.click();
    await page.waitForTimeout(500);
    await shot(page, "theme-picker");
  }
  await ctx.close();

  await browser.close();
  console.log(`\n${shots.length} screenshots written to ${OUT}`);
}

main().catch((err) => {
  console.error("screenshot run failed:", err.message);
  process.exit(1);
});
