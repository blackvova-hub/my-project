// Real LAN HTTP regression: normal login, actual form submit, no API mocks.
import { chromium, expect } from "@playwright/test";
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { defaultRequest } from "../src/pages/public/backtest/model.ts";

const base = process.env.BACKTEST_HTTP_URL;
const out = process.env.BACKTEST_HTTP_OUTPUT || "../../tmp/backtest-http";
if (!base || !process.env.BACKTEST_HTTP_EMAIL || !process.env.BACKTEST_HTTP_PASSWORD) throw Error("Provide BACKTEST_HTTP_URL/EMAIL/PASSWORD");
mkdirSync(out, { recursive: true });
const browser = await chromium.launch({ channel: "msedge", headless: true });
const checks = [], errors = [];
let page, activeId, context;
const check = (s) => { checks.push(s); console.log("PASS", s); };
try {
  context = await browser.newContext({ viewport: { width: 1536, height: 1024 }, reducedMotion: "reduce" });
  await context.addInitScript(() => {
    localStorage.setItem("cookie_consent", "essential");
    localStorage.setItem("mobile_notice_shown_v1", "1");
  });
  page = await context.newPage();
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto(`${base}/login`);
  await page.locator("input[type=email]").fill(process.env.BACKTEST_HTTP_EMAIL);
  await page.locator("input[type=password]").fill(process.env.BACKTEST_HTTP_PASSWORD);
  const loggedIn = page.waitForResponse((r) => r.url().includes("/api/") && r.url().includes("login") && r.request().method() === "POST", { timeout: 30000 });
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  const loginResponse = await loggedIn;
  expect(loginResponse.status()).toBe(200);
  await expect(page.getByRole("link", { name: "Тест стратегии", exact: true }).first()).toBeVisible({ timeout: 30000 });
  await page.goto(`${base}/backtest`);
  const capability = await page.evaluate(() => ({ secure: isSecureContext, uuid: typeof crypto.randomUUID, random: typeof crypto.getRandomValues }));
  expect(capability).toEqual({ secure: false, uuid: "undefined", random: "function" });
  check("real HTTP origin lacks randomUUID and provides getRandomValues");
  const before = await (await context.request.get(`${base}/api/backtests`)).json();
  if (before.jobs.some((j) => ["queued", "running"].includes(j.status))) throw Error("Existing user job is active; leave it untouched");
  const payload = defaultRequest();
  payload.exchange = "bybit";
  payload.market = "spot";
  payload.symbols = ["BTCUSDT"];
  payload.timeframe = "1m";
  payload.from = "2026-01-01T00:00:00.000Z";
  payload.to = "2026-01-02T00:00:00.000Z";
  payload.strategy.direction = "long";
  payload.strategy.entry = { kind: "and", children: [{ kind: "indicator", indicator: "price", measure: "change_pct", operator: "gte", threshold: 0, direction: "both", windowHours: 1 }] };
  await page.getByLabel("Импорт стратегии", { exact: true }).setInputFiles({ name: "http-regression.json", mimeType: "application/json", buffer: Buffer.from(JSON.stringify(payload)) });
  await expect(page.getByLabel("Начало периода", { exact: true })).toHaveValue("2026-01-01");
  const created = page.waitForResponse((r) => r.url() === `${base}/api/backtests` && r.request().method() === "POST", { timeout: 30000 });
  await page.getByRole("button", { name: "Запустить тест", exact: true }).click();
  const response = await created;
  expect(response.status(), await response.text()).toBe(202);
  const request = response.request().postDataJSON();
  expect(request.requestKey).toMatch(/^[\da-f]{8}-[\da-f]{4}-4[\da-f]{3}-[89ab][\da-f]{3}-[\da-f]{12}$/);
  activeId = (await response.json()).jobId;
  check("HTTP UI submit creates a real job with valid UUIDv4");
  const duplicate = await context.request.post(`${base}/api/backtests`, { data: request });
  expect(duplicate.status(), await duplicate.text()).toBe(202);
  expect((await duplicate.json()).jobId).toBe(activeId);
  check("same request key reuses the job without duplicate execution");
  await expect(page).toHaveURL(new RegExp(`job=${activeId}`));
  await page.reload();
  await expect.poll(async () => {
    const job = await (await context.request.get(`${base}/api/backtests/${activeId}`)).json();
    if (job.status === "failed") throw Error(`Actual job failed: ${job.error}`);
    return job.status;
  }, { timeout: 150000, intervals: [1000, 2000, 3000] }).toBe("completed");
  await expect(page.getByRole("heading", { name: "Результаты теста", exact: true })).toBeVisible({ timeout: 15000 });
  const jobId = activeId;
  activeId = undefined;
  check("real historical backtest completes and survives reload");
  await expect(page.locator(".bt-equity-chart canvas").first()).toBeVisible({ timeout: 20000 });
  await page.screenshot({ path: path.join(out, "backtest-http-desktop.png"), fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 2)).toBe(true);
  await page.screenshot({ path: path.join(out, "backtest-http-mobile.png"), fullPage: true });
  check("mobile results render without horizontal overflow");
  expect(errors).toEqual([]);
  writeFileSync(path.join(out, "result.json"), JSON.stringify({ ok: true, checks, errors, capability, jobId }, null, 2));
} catch (e) {
  await page?.screenshot({ path: path.join(out, "failure.png"), fullPage: true });
  writeFileSync(path.join(out, "result.json"), JSON.stringify({ ok: false, checks, errors, error: String(e) }, null, 2));
  throw e;
} finally {
  if (activeId) await context.request.post(`${base}/api/backtests/${activeId}/cancel`, { data: {} }).catch(() => {});
  await browser.close();
}
