import { chromium, expect } from "@playwright/test";
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import path from "node:path";
import { randomUUID } from "node:crypto";
import { indicators, makeRule } from "../src/pages/public/backtest/rules.ts";
import { defaultRequest } from "../src/pages/public/backtest/model.ts";
import { exerciseUI } from "./backtest-indicators-ui.mjs";
const root = path.resolve("../.."),
  out = path.join(root, "docs/backtest/indicators");
mkdirSync(out, { recursive: true });
const lines = readFileSync(path.join(root, "README.md"), "utf8").split(/\r?\n/),
  login = lines.findIndex((l) => l.trim() === "admin@local.dev");
if (login < 0) throw Error("QA account missing");
const browser = await chromium.launch({ channel: "msedge", headless: true });
const checks = [],
  jobs = [],
  errors = [];
const check = (name) => {
  checks.push(name);
  console.log("PASS", name);
};
let activeId, context;
try {
  context = await browser.newContext({
    viewport: { width: 1536, height: 1050 },
  });
  await context.addInitScript(() => {
    localStorage.setItem("cookie_consent", "essential");
    localStorage.setItem("mobile_notice_shown_v1", "1");
  });
  const page = await context.newPage();
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto("http://localhost:3000/login");
  await page.locator("input[type=email]").fill(lines[login].trim());
  await page.locator("input[type=password]").fill(lines[login + 1].trim());
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page
    .getByRole("link", { name: "Тест стратегии", exact: true })
    .waitFor();
  const base = defaultRequest();
  base.symbols = ["ADAUSDT"];
  const end = new Date();
  end.setUTCHours(0, 0, 0, 0);
  end.setUTCDate(end.getUTCDate() - 1);
  const start = new Date(end);
  start.setUTCDate(start.getUTCDate() - 3);
  base.from = start.toISOString();
  base.to = end.toISOString();
  base.strategy.entry.children = [
    { ...makeRule(), direction: "both", threshold: 0 },
  ];
  base.strategy.takeProfitPct = 1;
  base.strategy.stopLossPct = 1;
  if (!process.env.BACKTEST_API_ONLY)
    await exerciseUI(page, context, base, out, check, jobs, (id) => {
      activeId = id;
    });
  const apiJob = async (
    label,
    rules,
    market = "linear",
    tf = "1h",
    patch = {},
  ) => {
    const payload = structuredClone(base);
    payload.market = market;
    payload.timeframe = tf;
    Object.assign(payload.strategy, patch);
    payload.strategy.entry = { kind: "and", children: rules };
    const started = Date.now();
    const created = await context.request.post(
      "http://localhost:3000/api/backtests",
      { data: { ...payload, requestKey: randomUUID() } },
    );
    expect(created.status(), await created.text()).toBe(202);
    activeId = (await created.json()).jobId;
    let result;
    for (;;) {
      result = await (
        await context.request.get(
          `http://localhost:3000/api/backtests/${activeId}`,
        )
      ).json();
      if (!["queued", "running"].includes(result.status)) break;
      if (Date.now() - started > 240000)
        throw Error(`Timed out ${label}: ${result.phase}`);
      await new Promise((resolve) => setTimeout(resolve, 1500));
    }
    expect(result.status, `${label}: ${result.error}`).toBe("completed");
    expect(result.result.engineVersion).toBe("2.0.0");
    expect(result.result.signals[0].evaluatedBars).toBeGreaterThan(0);
    jobs.push({
      id: activeId,
      label,
      seconds: (Date.now() - started) / 1000,
      ...result.result.metrics,
      signals: result.result.signals,
    });
    activeId = undefined;
    console.log("JOB", label, result.result.metrics.tradeCount);
    return result;
  };
  for (const [name, def] of Object.entries(indicators))
    for (const measure of def.measures)
      await apiJob(`${name}/${measure.value}`, [makeRule(name, measure.value)]);
  await apiJob("Spot real CVD", [makeRule("cvd", "imbalance_pct")], "spot");
  await apiJob(
    "4h OI + CVD",
    [
      { ...makeRule("openInterest"), direction: "both", threshold: 0 },
      { ...makeRule("cvd", "imbalance_pct"), threshold: -100 },
    ],
    "linear",
    "4h",
  );
  await apiJob(
    "Short + indicator exit without TP",
    [{ ...makeRule(), threshold: 0, direction: "both" }],
    "linear",
    "1h",
    {
      direction: "short",
      takeProfitPct: 0,
      exit: {
        kind: "or",
        children: [{ ...makeRule("rsi"), operator: "gte", threshold: 0 }],
      },
    },
  );
  const cancelPayload = structuredClone(base);
  cancelPayload.symbols = ["SOLUSDT"];
  cancelPayload.strategy.entry.children = [makeRule("cvd")];
  const cancelCreated = await context.request.post(
    "http://localhost:3000/api/backtests",
    { data: { ...cancelPayload, requestKey: randomUUID() } },
  );
  expect(cancelCreated.status()).toBe(202);
  activeId = (await cancelCreated.json()).jobId;
  await page.goto(`http://localhost:3000/backtest?job=${activeId}`);
  await page
    .getByRole("button", { name: "Отменить тест", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Тест отменён", exact: true }),
  ).toBeVisible();
  activeId = undefined;
  const bad = structuredClone(base);
  bad.market = "spot";
  bad.strategy.entry.children = [makeRule("openInterest")];
  expect(
    (
      await context.request.post("http://localhost:3000/api/backtests", {
        data: { ...bad, requestKey: randomUUID() },
      })
    ).status(),
  ).toBe(422);
  const anonymous = await browser.newContext();
  expect(
    (
      await anonymous.request.get(
        `http://localhost:3000/api/backtests/${jobs[0].id}`,
      )
    ).status(),
  ).toBe(401);
  await anonymous.close();
  expect(errors).toEqual([]);
  check(
    "all real-history engine measures, Spot CVD, 4h OI+CVD, Short exit, UI cancellation, API restrictions, no browser exceptions",
  );
  writeFileSync(
    path.join(out, process.env.BACKTEST_API_ONLY ? "api-checks.json" : "checks.json"),
    JSON.stringify(
      { checkedAt: new Date().toISOString(), checks, jobs, errors },
      null,
      2,
    ),
  );
} finally {
  if (activeId && context)
    await context.request
      .post(`http://localhost:3000/api/backtests/${activeId}/cancel`, {
        data: {},
      })
      .catch(() => {});
  writeFileSync(
    path.join(out, "partial-checks.json"),
    JSON.stringify({ checks, jobs, errors }, null, 2),
  );
  await browser.close();
}
