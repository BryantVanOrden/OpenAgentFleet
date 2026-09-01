// The launch demo, recorded uncut from the real console.
//
// No mocking and no cuts: the recording starts before the goal is typed, shows
// it being typed into the Assign task panel and started, then watches the
// agent's own desktop stream while a real vision model drives Firefox, and
// stops when the task settles. The output is one continuous webm; the GIF and
// MP4 for the README are derived from it without removing frames from the
// middle — anyone accusing the demo of being staged gets the raw file.
//
// Prerequisites (the Makefile target stages them): a running stack, an
// instance whose id is in INSTANCE_ID, and an enabled vision provider.
import { chromium } from "playwright";

const BASE = process.env.BASE_URL ?? "http://admin:80";
const EMAIL = process.env.AF_EMAIL ?? "demo@agentfleet.local";
const PASSWORD = process.env.AF_PASSWORD ?? "agentfleet-demo-1234";
const INSTANCE = process.env.INSTANCE_ID;
const GOAL = process.env.DEMO_GOAL ??
  "Open Firefox, go to news.ycombinator.com, and read the title of the #1 story. " +
  "Then finish, reporting that exact title.";
const OUT = process.env.OUT_DIR ?? "/out";
// The hard cap. A stuck run should produce a diagnosis, not an hour of video.
const MAX_MINUTES = Number(process.env.DEMO_MAX_MINUTES ?? 18);

if (!INSTANCE) {
  console.error("INSTANCE_ID is required");
  process.exit(1);
}

process.on("uncaughtException", async (err) => {
  // A diagnosis beats a stack trace: whatever the page looked like when it
  // failed is saved next to where the video would have gone.
  console.error(String(err).slice(0, 300));
  try { await page.screenshot({ path: `${OUT}/demo-failed.png` }); } catch {}
  process.exit(1);
});

const browser = await chromium.launch();
const ctx = await browser.newContext({
  viewport: { width: 1600, height: 950 },
  recordVideo: { dir: OUT, size: { width: 1600, height: 950 } },
  colorScheme: "dark",
});
const page = await ctx.newPage();
await page.addInitScript(() => {
  localStorage.setItem("agentfleet.mode", "dark");
  localStorage.setItem("agentfleet.accent", "amber");
});

// Sign in (on camera, which is fine: it shows the product's own front door).
await page.goto(`${BASE}/`, { waitUntil: "networkidle" });
await page.fill('input[type="email"]', EMAIL);
await page.fill('input[type="password"]', PASSWORD);
await page.click('button[type="submit"]');
// Signed-in is when the shell's navigation exists, not a fixed sleep — a slow
// first login inside a cold container otherwise races the next goto back to
// the login screen, where there is no goal box to find.
await page.getByText("Fleet", { exact: true }).first().waitFor({ timeout: 30000 });

// The instance page: live desktop on the left, Assign task on the right.
await page.goto(`${BASE}/instances/${INSTANCE}`, { waitUntil: "networkidle" });
await page.getByText("Assign task").waitFor({ timeout: 30000 });
// Let the noVNC stream connect and paint before anything happens, so the video
// opens on a live desktop rather than a black rectangle.
await page.waitForTimeout(9000);

// Type the goal like a person would — visibly, not instantly. The box is
// disabled while the console still thinks the instance is not running, so this
// waits for editability rather than existence.
const goalBox = page.locator("textarea").first();
await goalBox.click({ timeout: 90000 });
await goalBox.pressSequentially(GOAL, { delay: 18 });
await page.waitForTimeout(600);
await page.getByRole("button", { name: /start agent/i }).click();
console.log("goal submitted; watching the run");

// Watch until the task settles. The console updates itself over the event
// socket; this loop only decides when to stop filming.
const deadline = Date.now() + MAX_MINUTES * 60_000;
let finalState = "timeout";
while (Date.now() < deadline) {
  await page.waitForTimeout(5000);
  const state = await page.evaluate(async () => {
    const token = localStorage.getItem("agentfleet.token");
    const res = await fetch("/api/tasks?limit=5", {
      headers: { Authorization: `Bearer ${token}` },
    });
    const tasks = await res.json();
    const mine = (tasks ?? []).find(
      (t) => t.instance_id === window.location.pathname.split("/").pop(),
    );
    return mine ? { state: mine.state, result: mine.result, error: mine.error } : null;
  });
  if (state) {
    console.log(`  task: ${state.state}`);
    if (["succeeded", "failed", "cancelled"].includes(state.state)) {
      finalState = state.state;
      console.log(`  result: ${(state.result || state.error || "").slice(0, 200)}`);
      break;
    }
  }
}

// Hold on the finished screen for a beat so the video does not cut on the
// exact frame the state flips.
await page.waitForTimeout(6000);

const video = page.video();
await ctx.close();
const path = await video.path();
console.log(`recorded: ${path} (${finalState})`);
await browser.close();
