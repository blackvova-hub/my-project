import { test, expect } from "@playwright/test";

// Requires an isolated API/worker stack with migrated DB and an authenticated
// test user. Never injects mock candles, results or an authentication bypass.
test.skip(
  !process.env.BACKTEST_E2E_SESSION,
  "Provide a test-user session for the isolated stack",
);
test.beforeEach(async ({ context, baseURL }) => {
  await context.addCookies([
    {
      name: "sid",
      value: process.env.BACKTEST_E2E_SESSION!,
      domain: new URL(baseURL!).hostname,
      path: "/",
    },
  ]);
  await context.addInitScript(() => {
    localStorage.setItem("cookie_consent", "essential");
    localStorage.setItem("mobile_notice_shown_v1", "1");
  });
});

test("market controls, real job, reload, trades export and mobile navigation", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (err) => errors.push(err.message));
  await page.goto("/backtest");
  await expect(
    page.getByRole("heading", { name: "Тест стратегии", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Тест стратегии", exact: true }).first(),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Запустить тест", exact: true }),
  ).toBeEnabled();
  await page.getByRole("button", { name: "Spot", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Short (продажа)" }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "Futures", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Short (продажа)" }),
  ).toBeEnabled();
  await page.getByRole("button", { name: "Добавить", exact: true }).click();
  await page
    .getByRole("textbox", { name: "Поиск торговой пары" })
    .fill("ETHUSDT");
  await page.getByRole("button", { name: "ETHUSDT", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Удалить ETHUSDT" }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Добавить группу", exact: true })
    .click();
  await expect(page.locator(".bt-nested-group")).toBeVisible();
  await page
    .getByRole("button", { name: "Удалить вход в позицию 2", exact: true })
    .click();
  await page.getByRole("checkbox", { name: "Выход по условию" }).check();
  await expect(
    page.getByRole("button", { name: "Условие выхода: логика", exact: true }),
  ).toBeVisible();
  await page.getByRole("checkbox", { name: "Выход по условию" }).uncheck();
  const end = new Date();
  end.setUTCHours(0, 0, 0, 0);
  const start = new Date(end);
  start.setUTCDate(start.getUTCDate() - 30);
  const request = {
    exchange: "bybit",
    market: "linear",
    symbols: ["BTCUSDT", "ETHUSDT"],
    timeframe: "1h",
    from: start.toISOString(),
    to: end.toISOString(),
    strategy: {
      version: 2,
      direction: "long",
      initialCapital: 10_000,
      positionSizePct: 20,
      feePct: 0.1,
      slippagePct: 0.05,
      takeProfitPct: 1,
      stopLossPct: 1,
      entry: {
        kind: "and",
        children: [
          {
            kind: "indicator", indicator: "price", measure: "change_pct",
            operator: "gte", threshold: 0, direction: "both", windowHours: 24,
          },
        ],
      },
    },
  };
  await page
    .getByLabel("Импорт стратегии", { exact: true })
    .setInputFiles({
      name: "strategy.json",
      mimeType: "application/json",
      buffer: Buffer.from(JSON.stringify(request)),
    });
  await expect(page.getByLabel("Начало периода", { exact: true })).toHaveValue(
    start.toISOString().slice(0, 10),
  );
  const created = page.waitForResponse(
    (r) => /\/backtests$/.test(r.url()) && r.request().method() === "POST",
  );
  await page
    .getByRole("button", { name: "Запустить тест", exact: true })
    .click();
  const response = await created;
  expect(response.status()).toBe(202);
  const { jobId } = await response.json();
  await expect(page).toHaveURL(new RegExp(`job=${jobId}`));
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Результаты теста", exact: true }),
  ).toBeVisible({ timeout: 150_000 });
  await expect(page.locator(".bt-trades tbody tr").first()).toContainText(
    /BTCUSDT|ETHUSDT/,
  );
  await expect(page.locator(".bt-metrics")).not.toContainText("NaN");
  const next = page.getByRole("button", { name: "Следующая страница сделок" });
  if (await next.isEnabled()) {
    await next.click();
    await expect(page.locator(".bt-pagination")).toContainText("21–");
    await page
      .getByRole("button", { name: "Предыдущая страница сделок" })
      .click();
  }
  const downloaded = page.waitForEvent("download");
  await page
    .getByRole("button", { name: "Скачать сделки", exact: true })
    .click();
  const file = await downloaded;
  expect(file.suggestedFilename()).toBe(`backtest-${jobId}.csv`);
  await expect(page.locator(".bt-equity-chart canvas").first()).toBeVisible();
  await page
    .locator(".bt-results")
    .evaluate((el) =>
      window.scrollTo(0, el.getBoundingClientRect().top + window.scrollY - 20),
    );
  await page.waitForTimeout(400);
  await page.screenshot({ path: "../../tmp/backtest-qa/results-desktop.png" });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.waitForTimeout(400);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(
    390,
  );
  await page.getByLabel("Открыть навигацию").click();
  await expect(
    page
      .getByRole("navigation", { name: "Мобильная навигация" })
      .getByRole("link", { name: "Тест стратегии" }),
  ).toBeVisible();
  await page
    .getByRole("navigation", { name: "Мобильная навигация" })
    .getByRole("link", { name: "Тест стратегии" })
    .click();
  await page
    .getByRole("button", { name: "История тестов", exact: true })
    .click();
  await expect(page.locator(".bt-history-list")).toContainText(
    "BTCUSDT, ETHUSDT",
  );
  expect(errors).toEqual([]);
});

test("invalid JSON, validation, cancellation and job ownership", async ({
  page,
  request: http,
}) => {
  await page.goto("/backtest");
  await expect(
    page.getByRole("button", { name: "Запустить тест", exact: true }),
  ).toBeEnabled();
  await page
    .getByLabel("Импорт стратегии", { exact: true })
    .setInputFiles({
      name: "invalid.json",
      mimeType: "application/json",
      buffer: Buffer.from('{"strategy":{"version":999}}'),
    });
  await expect(page.getByRole("alert")).toContainText("Некорректный формат");
  await page.getByLabel("Комиссия, %", { exact: true }).fill("7");
  await page
    .getByRole("button", { name: "Запустить тест", exact: true })
    .click();
  expect(
    await page
      .getByLabel("Комиссия, %", { exact: true })
      .evaluate((el: HTMLInputElement) => el.validity.rangeOverflow),
  ).toBe(true);
  await page.getByLabel("Комиссия, %", { exact: true }).fill("0.1");
  // Use an uncached trade-history job so cancellation is observable even when
  // the OHLCV archive is warm and a simple price test completes immediately.
  const indicator = page.getByLabel("Вход в позицию 1: индикатор", { exact: true });
  await indicator.scrollIntoViewIfNeeded();
  await page.waitForTimeout(450);
  await indicator.click();
  await page.getByRole("option").filter({ has: page.getByText("Кумулятивная дельта объёма (CVD)", { exact: true }) }).click();
  const created = page.waitForResponse(
    (r) => /\/backtests$/.test(r.url()) && r.request().method() === "POST",
  );
  await page
    .getByRole("button", { name: "Запустить тест", exact: true })
    .click();
  const response = await created;
  expect(response.status()).toBe(202);
  const { jobId } = await response.json();
  await page
    .getByRole("button", { name: "Отменить тест", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Тест отменён", exact: true }),
  ).toBeVisible();
  const api = process.env.BACKTEST_E2E_API ?? "http://127.0.0.1:28080";
  const unauthorized = await http.get(`${api}/api/backtests/${jobId}`);
  expect(unauthorized.status()).toBe(401);
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Тест отменён", exact: true }),
  ).toBeVisible();
});
