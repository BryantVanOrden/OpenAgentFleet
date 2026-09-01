// The README's hero: one agent's live desktop inside the console.
//
// This is the shot that explains the product faster than any sentence — a real
// XFCE desktop with Firefox open, streaming into the operator's browser, with
// the chat, activity and teach-by-demonstration controls around it. The desktop
// is interactive for operators (that IS takeover: click the stream and drive),
// so the caption in the README can say "take over at any time" over a picture
// of exactly that.
//
// Prerequisites, staged by `make hero-shot`: a running instance with
// shell_access, with something worth looking at on screen (Firefox on a real
// page). An idle grey desktop is technically honest and sells nothing.
import { chromium } from "playwright";

const BASE = process.env.BASE_URL ?? "http://admin:80";
const EMAIL = process.env.AF_EMAIL ?? "demo@agentfleet.local";
const PASSWORD = process.env.AF_PASSWORD ?? "agentfleet-demo-1234";
const INSTANCE = process.env.INSTANCE_ID; // required
const OUT = process.env.OUT_DIR ?? "/out";

if (!INSTANCE) {
  console.error("INSTANCE_ID is required: the id of a running instance to photograph");
  process.exit(1);
}

const browser = await chromium.launch();
for (const mode of ["dark", "light"]) {
  const ctx = await browser.newContext({
    viewport: { width: 1600, height: 950 },
    deviceScaleFactor: 2,
    colorScheme: mode,
  });
  const page = await ctx.newPage();

  // Theme is chosen the same way scripts/screenshots.mjs chooses it: seed the
  // console's own localStorage keys before the app boots.
  await page.addInitScript(
    ([m]) => {
      localStorage.setItem("agentfleet.mode", m);
      localStorage.setItem("agentfleet.accent", "amber");
    },
    [mode],
  );

  await page.goto(`${BASE}/`, { waitUntil: "networkidle" });
  await page.fill('input[type="email"]', EMAIL);
  await page.fill('input[type="password"]', PASSWORD);
  await page.click('button[type="submit"]');
  await page.waitForTimeout(1500);

  await page.goto(`${BASE}/instances/${INSTANCE}`, { waitUntil: "networkidle" });
  // The noVNC stream connects, handshakes and paints its first full frame well
  // after the page settles; screenshotting early gets a black rectangle that
  // looks like a broken product.
  await page.waitForTimeout(12000);
  await page.screenshot({ path: `${OUT}/desktop-live-${mode}.png` });
  console.log(`desktop-live-${mode}.png`);
  await ctx.close();
}
await browser.close();
