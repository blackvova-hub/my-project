import { chromium, expect } from "@playwright/test";
import { readFileSync, mkdirSync, writeFileSync } from "node:fs";
import { registerHooks } from "node:module";
registerHooks({
  resolve(specifier, context, next) {
    try {
      return next(specifier, context);
    } catch (e) {
      if (
        e.code === "ERR_MODULE_NOT_FOUND" &&
        specifier.startsWith(".") &&
        !/\.[a-z]+$/i.test(specifier)
      )
        return next(specifier + ".ts", context);
      throw e;
    }
  },
});
const { normalizeWorkspace } = await import(
  "../src/pages/public/charts/workspace.ts"
);
const base = process.env.CHARTS_E2E_BASE ?? "http://192.168.0.130",
  output = process.env.CHARTS_E2E_OUTPUT ?? "/output";
mkdirSync(output, { recursive: true });
const access = JSON.parse(
  readFileSync(process.env.CHARTS_E2E_ACCESS_FILE, "utf8"),
);
const browser = await chromium.launch({ headless: true }),
  context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    reducedMotion: "reduce",
  });
await context.addInitScript(() =>
  localStorage.setItem("cookie_consent", "essential"),
);
const page = await context.newPage(),
  errors = [],
  starts = [],
  completed = [],
  checks = [];
page.on("pageerror", (e) => errors.push(e.stack));
const liveURL = (url) => {
  const u = new URL(url);
  return (
    u.hostname === "fapi.binance.com" &&
    u.pathname === "/fapi/v1/klines" &&
    u.searchParams.get("limit") === "2" &&
    u.searchParams.get("symbol") === "BTCUSDT"
  );
};
page.on("request", (r) => {
  if (liveURL(r.url())) starts.push(Date.now());
});
page.on("response", (r) => {
  if (liveURL(r.url())) console.log("SNAPSHOT_HTTP", r.status());
  if (r.ok() && liveURL(r.url())) completed.push(Date.now());
});
page.on("requestfailed", (r) => {
  if (liveURL(r.url())) console.log("SNAPSHOT_FAILED", r.failure()?.errorText);
});
// Exercise a real exchange REST feed when WebSocket is unavailable.
await context.routeWebSocket(/wss:\/\//, (ws) => ws.close());
let owner;
const local = () =>
  page.evaluate(
    (owner) =>
      JSON.parse(localStorage.getItem(`shortlong:terminal:v3:${owner}`))
        .workspace,
    owner,
  );
const wait = () =>
  expect
    .poll(
      async () =>
        Number(
          await page
            .locator("[data-chart-surface]")
            .getAttribute("data-visible-count"),
        ),
      { timeout: 60000 },
    )
    .toBeGreaterThan(100);
try {
  await page.goto(`${base}/login`);
  await page
    .getByLabel("Email", { exact: true })
    .fill(access.email ?? "admin@local.dev");
  await page.getByLabel("Пароль", { exact: true }).fill(access.password);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page.waitForURL((url) => !url.pathname.includes("login"));
  const me = await (await context.request.get(`${base}/api/auth/me`)).json();
  owner = (me.user ?? me).id;
  const cloud = await (
    await context.request.get(`${base}/api/chart/terminal`)
  ).json();
  const workspace = normalizeWorkspace(null);
  workspace.windows[0].exchange = "binance";
  workspace.windows[0].panels.scales.volume = {
    auto: false,
    log: true,
    range: { from: 0, to: 1.4150746702e23 },
  };
  await page.goto(`${base}/charts`);
  await page.evaluate(
    ({ owner, workspace, revision }) =>
      localStorage.setItem(
        `shortlong:terminal:v3:${owner}`,
        JSON.stringify({ workspace, saved: "test preferences", revision }),
      ),
    { owner, workspace, revision: cloud.revision },
  );
  await page.reload();
  await wait();
  await expect
    .poll(async () => (await local()).windows[0].panels.scales.volume.auto)
    .toBe(true);
  checks.push("corrupt saved volume range resets to real-data autoscale");
  starts.length = 0;
  completed.length = 0;
  await expect
    .poll(() => completed.length, { timeout: 20000 })
    .toBeGreaterThanOrEqual(6);
  const intervals = starts
    .slice(-5)
    .map((v, n, a) => (n ? v - a[n - 1] : 0))
    .slice(1);
  console.log("CADENCE", JSON.stringify(intervals));
  // The timer is one second; a slow exchange response can occupy two ticks.
  // Require a sustained one-second run, while retaining latency gaps in evidence.
  let steady = 0;
  expect(
    intervals.some((v) => {
      steady = v >= 850 && v < 1500 ? steady + 1 : 0;
      return steady >= 3;
    }),
  ).toBe(true);
  await expect(page.locator(".ct-stream-status")).toHaveText(/Поток/);
  checks.push(
    "real BTC candles refresh about once per second with WebSocket blocked",
  );
  // Restore a reasonable manual logarithmic range in original volume units.
  await page.evaluate((owner) => {
    const key = `shortlong:terminal:v3:${owner}`;
    const v = JSON.parse(localStorage.getItem(key));
    v.workspace.windows[0].panels.scales.volume = {
      auto: false,
      log: true,
      range: { from: 0, to: 20000 },
    };
    v.saved = "test preferences";
    localStorage.setItem(key, JSON.stringify(v));
  }, owner);
  await page.reload();
  await wait();
  const host = await page.locator(".ct-chart-host").boundingBox(),
    legend = await page.locator('[data-pane-key="volume"]').boundingBox();
  await page.mouse.move(host.x + host.width - 28, legend.y + 26);
  await page.mouse.down();
  await page.mouse.move(host.x + host.width - 28, legend.y + 48, { steps: 8 });
  await page.mouse.up();
  await expect
    .poll(
      async () =>
        (await local()).windows[0].panels.scales.volume.range?.to ?? Infinity,
    )
    .toBeLessThan(1000000);
  expect((await local()).windows[0].panels.scales.volume.auto).toBe(false);
  await page.waitForTimeout(500);
  console.log(
    "SCALE_AFTER_DRAG",
    JSON.stringify((await local()).windows[0].panels.scales.volume),
  );
  const before = (await local()).windows[0].panels.scales.volume.range;
  expect(before).toBeDefined();
  await page.reload();
  await wait();
  // Pointer-up also reads the native restored range back into preferences.
  await expect(page.locator('[data-pane-key="volume"]')).toBeVisible();
  await page.waitForTimeout(500);
  console.log(
    "SCALE_AFTER_RELOAD",
    JSON.stringify((await local()).windows[0].panels.scales.volume),
  );
  await page.mouse.click(host.x + 150, legend.y + 35);
  await page.mouse.move(10, 10);
  await expect
    .poll(async () =>
      Math.abs(
        ((await local()).windows[0].panels.scales.volume.range?.to ??
          Infinity) - before.to,
      ),
    )
    .toBeLessThan(Math.max(1, before.to * 0.01));
  await page.screenshot({ path: `${output}/volume-log-restored.png` });
  checks.push(
    "manual logarithmic volume scale stays finite through drag and reload",
  );
  expect(errors).toEqual([]);
  writeFileSync(
    `${output}/verification.json`,
    JSON.stringify(
      {
        checks,
        errors,
        requestIntervalsMs: intervals,
        completedSnapshots: completed.length,
        websocket: "blocked intentionally",
        source: "real fapi.binance.com",
        cloudPreferences: "read only",
      },
      null,
      2,
    ),
  );
  console.log(
    JSON.stringify({ checks, requestIntervalsMs: intervals, errors }),
  );
} finally {
  await context.request.post(`${base}/api/auth/logout`).catch(() => {});
  await browser.close();
}
