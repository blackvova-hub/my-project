// Real exchange feeds; no mocked candles or quote data. Run against a development
// preview with its existing DEV-only route guard, or an authenticated test account.
import { chromium, expect } from "@playwright/test";
import { mkdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
const target = process.env.CHARTS_E2E_URL ?? "http://127.0.0.1:5173/charts";
const base = new URL(target).origin;
const output = resolve(process.env.CHARTS_E2E_OUTPUT ?? "../../docs/charts");
mkdirSync(output, { recursive: true });
const browser = await chromium.launch({ channel: "msedge", headless: true });
const context = await browser.newContext({
  viewport: { width: 1536, height: 1024 },
  reducedMotion: "reduce",
  acceptDownloads: true,
  ...(process.env.CHARTS_E2E_STORAGE
    ? { storageState: process.env.CHARTS_E2E_STORAGE }
    : {}),
});
await context.addInitScript(() => {
  localStorage.setItem("cookie_consent", "essential");
  localStorage.setItem("mobile_notice_shown_v1", "1");
});
const page = await context.newPage(),
  errors = [],
  sources = new Set(),
  checks = [];
page.on("pageerror", (e) => errors.push(e.message));
page.on("response", (r) => {
  if (/api\.(bybit|binance)\.com|fapi.binance.com/.test(r.url()) && r.ok())
    sources.add(r.url());
});
const check = (name) => {
  checks.push(name);
  console.log("PASS", name);
};
let originalCloud;
try {
  if (process.env.CHARTS_E2E_EMAIL && process.env.CHARTS_E2E_PASSWORD) {
    await page.goto(`${base}/login`);
    await page.getByLabel("Email", { exact: true }).fill(process.env.CHARTS_E2E_EMAIL);
    await page.getByLabel("Пароль", { exact: true }).fill(process.env.CHARTS_E2E_PASSWORD);
    await page.getByRole("button", { name: "Войти", exact: true }).click();
    await page.waitForURL((url) => !url.pathname.includes("login"), { timeout: 30000 });
    expect((await context.request.get(`${base}/api/auth/me`)).status()).toBe(200);
    check("production login and authenticated session");
  }
  if (process.env.CHARTS_E2E_EMAIL || process.env.CHARTS_E2E_STORAGE) {
    const config = await context.request.get(`${base}/api/chart/config`);
    expect(config.status()).toBe(200);
    originalCloud = (await config.json()).config;
  }
  await page.goto(target);
  if (
    await page
      .getByRole("button", { name: "Не разрешать", exact: true })
      .isVisible()
  )
    await page
      .getByRole("button", { name: "Не разрешать", exact: true })
      .click();
  await expect(page.locator("[data-chart-surface]")).toHaveAttribute(
    "data-visible-count",
    /^\d+$/,
  );
  await expect
    .poll(
      async () =>
        Number(
          await page
            .locator("[data-chart-surface]")
            .getAttribute("data-visible-count"),
        ),
      { timeout: 45000 },
    )
    .toBeGreaterThan(100);
  check("real Bybit candles and ticker catalog");
  await page.getByRole("button", { name: /^Индикаторы/ }).click();
  await page.getByRole("switch", { name: /^MACD/ }).click();
  await expect(page.getByRole("switch", { name: /^MACD/ })).toHaveAttribute(
    "aria-checked",
    "true",
  );
  await page.getByRole("switch", { name: /^MACD/ }).click();
  await page.keyboard.press("Escape");
  check("indicator catalog and MACD panel toggle");
  await page
    .getByRole("button", { name: "Настройки графика", exact: true })
    .click();
  await page.getByLabel("EMA", { exact: true }).fill("150");
  await expect(page.getByLabel("EMA", { exact: true })).toHaveValue("150");
  await page.getByLabel("EMA", { exact: true }).fill("200");
  await page
    .getByLabel("Дополнительный индикатор", { exact: true })
    .selectOption("sma");
  await expect(
    page.getByRole("button", { name: /^Удалить sma-/ }),
  ).toBeVisible();
  await page.getByRole("button", { name: /^Удалить sma-/ }).click();
  await page.keyboard.press("Escape");
  check("multiple indicator instances and editable periods");
  await page.getByRole("button", { name: /^Индикаторы/ }).click();
  await page
    .getByRole("button", { name: "Выключить все", exact: true })
    .click();
  await expect(page.getByRole("switch", { name: /^EMA/ })).toHaveAttribute(
    "aria-checked",
    "false",
  );
  for (const name of [/^EMA/, /^RSI Индекс/, /^Объём/])
    await page.getByRole("switch", { name }).click();
  await page.keyboard.press("Escape");
  await page
    .getByRole("button", { name: "Настройки графика", exact: true })
    .click();
  await expect(page.getByRole("button", { name: /^Удалить ema/ })).toHaveCount(
    0,
  );
  await page
    .getByLabel("Дополнительный индикатор", { exact: true })
    .selectOption("ema");
  await page.getByLabel("EMA", { exact: true }).fill("200");
  await page.keyboard.press("Escape");
  check("clear all includes additional indicators and volume");

  const derivativeResponses = [];
  page.on("response", (r) => {
    if (
      /open-interest|funding\/history|mark-price-kline|index-price-kline|account-ratio/.test(
        r.url(),
      ) &&
      r.ok()
    )
      derivativeResponses.push(r.url());
  });
  await page.getByRole("button", { name: "Деривативы", exact: true }).click();
  for (const label of [
    "Открытый интерес · базовый актив",
    "Funding · фактическая ставка, %",
    "Базис · mark/index, %",
    "Long/Short · отношение аккаунтов",
  ])
    await page.getByLabel(label, { exact: true }).check();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Закрыть", exact: true })
    .click();
  await expect
    .poll(() => derivativeResponses.length, { timeout: 45000 })
    .toBeGreaterThanOrEqual(5);
  await expect(page.locator(".ct-external-errors")).toHaveCount(0);
  check("four derivative panes from real Bybit endpoints");
  await page.getByRole("button", { name: "Деривативы", exact: true }).click();
  for (const label of [
    "Открытый интерес · базовый актив",
    "Funding · фактическая ставка, %",
    "Базис · mark/index, %",
    "Long/Short · отношение аккаунтов",
  ])
    await page.getByLabel(label, { exact: true }).uncheck();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Закрыть", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Фирменная зелёная тема", exact: true })
    .click();
  const surface = page.locator("[data-chart-surface]").first();
  await page
    .getByRole("button", { name: "Линии и каналы", exact: true })
    .click();
  await page.getByRole("menuitem", { name: /^Линия тренда/ }).click();
  const bounds = await page
    .locator(".ct-drawing-overlay")
    .first()
    .boundingBox();
  const p = (x, y) => ({
    x: bounds.x + bounds.width * x,
    y: bounds.y + bounds.height * y,
  });
  const before = Number(await surface.getAttribute("data-drawing-count"));
  await page.mouse.click(p(0.35, 0.45).x, p(0.35, 0.45).y);
  await page.mouse.click(p(0.7, 0.3).x, p(0.7, 0.3).y);
  await expect(surface).toHaveAttribute(
    "data-drawing-count",
    String(before + 1),
  );
  check("trend drawing");
  const handle = page.locator("[data-drawing-type=trend] circle").last(),
    h = await handle.boundingBox();
  await page.mouse.move(h.x + 5, h.y + 5);
  await page.mouse.down();
  await page.mouse.move(h.x + 70, h.y - 25, { steps: 10 });
  await page.mouse.up();
  check("anchor dragging");
  await surface.focus();
  await page.keyboard.press("Control+z");
  await page.keyboard.press("Control+z");
  await expect(surface).toHaveAttribute("data-drawing-count", String(before));
  await page.keyboard.press("Control+Shift+z");
  await expect(surface).toHaveAttribute(
    "data-drawing-count",
    String(before + 1),
  );
  check("undo and redo include edits");
  await page.reload();
  await expect
    .poll(
      async () => Number(await surface.getAttribute("data-drawing-count")),
      { timeout: 45000 },
    )
    .toBe(before + 1);
  await expect
    .poll(
      async () => Number(await surface.getAttribute("data-visible-count")),
      { timeout: 45000 },
    )
    .toBeGreaterThan(100);
  await page.waitForTimeout(350);
  check("drawing persistence after reload");
  for (const theme of [
    "Светлая тема",
    "Тёмная тема",
    "Фирменная зелёная тема",
  ]) {
    await page.getByRole("button", { name: theme, exact: true }).click();
    await page.screenshot({
      path: resolve(
        output,
        theme === "Светлая тема"
          ? "desktop-light.png"
          : theme === "Тёмная тема"
            ? "desktop-dark.png"
            : "desktop-green.png",
      ),
    });
  }
  check("Light, Dark and Green");
  await page
    .getByRole("button", { name: "Фибоначчи и Ганн", exact: true })
    .click();
  await page.getByRole("menuitem", { name: /^Расширение Фибоначчи/ }).click();
  for (const [x, y] of [
    [0.25, 0.6],
    [0.45, 0.35],
    [0.65, 0.5],
  ]) {
    const point = p(x, y);
    await page.mouse.click(point.x, point.y);
  }
  await expect(page.locator("[data-drawing-type=fib-extension]")).toHaveCount(
    1,
  );
  check("three-anchor Fibonacci without partial-draft crashes");
  await page
    .getByRole("button", { name: "Позиции и измерения", exact: true })
    .click();
  await page.getByRole("menuitem", { name: /^Длинная позиция/ }).click();
  for (const [x, y] of [
    [0.3, 0.5],
    [0.55, 0.3],
    [0.55, 0.65],
  ]) {
    const point = p(x, y);
    await page.mouse.click(point.x, point.y);
  }
  await expect(page.locator("[data-drawing-type=long]")).toHaveCount(1);
  check("long position reward and risk");
  if (originalCloud) {
    await page.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(page.getByText("Сохранено в аккаунте", { exact: true }).first()).toBeVisible();
    const response = await context.request.get(`${base}/api/chart/config`);
    expect(response.status()).toBe(200);
    const saved = await response.json();
    expect(saved.saved).toBe(true);
    const drawings = Object.values(saved.config.drawing_sets).flat();
    expect(drawings.some((drawing) => drawing.type === "long")).toBe(true);
    expect(drawings.some((drawing) => drawing.type === "fib-extension")).toBe(true);
    check("cloud save and PostgreSQL round trip of enriched drawings");
  }
  await page
    .getByRole("button", { name: "Настройки объекта", exact: true })
    .click();
  await expect(
    page.getByRole("dialog", { name: "Настройки объекта" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Закрыть настройки" }).click();
  check("object settings");
  await page
    .getByRole("button", { name: "Список объектов", exact: true })
    .click();
  await expect(page.locator(".ct-object-list")).toBeVisible();
  const downloadPromise = page.waitForEvent("download");
  await page
    .locator(".ct-object-actions")
    .getByRole("button", { name: "JSON", exact: true })
    .click();
  const download = await downloadPromise;
  await download.saveAs(resolve("../../tmp/charts-drawings-export.json"));
  await page.getByRole("button", { name: "Закрыть список" }).click();
  check("object list and JSON export");
  const imagePromise = page.waitForEvent("download");
  await page
    .getByRole("button", { name: "Снимок графика", exact: true })
    .click();
  await (await imagePromise).saveAs(resolve("../../tmp/charts-export.png"));
  check("PNG chart export with drawings");
  await page.getByRole("button", { name: "Таймфрейм 1д", exact: true }).click();
  await expect
    .poll(
      async () => Number(await surface.getAttribute("data-visible-count")),
      { timeout: 45000 },
    )
    .toBeGreaterThan(50);
  check("daily timeframe");
  for (const value of ["line", "area", "bars", "candles"]) {
    await page.getByLabel("Тип графика", { exact: true }).selectOption(value);
    await expect(surface).toBeVisible();
  }
  check("all chart types and lifecycle");
  await page.getByRole("button", { name: "Количество графиков" }).click();
  await expect(page.locator("[data-chart-surface]")).toHaveCount(2);
  await page.getByRole("button", { name: "Количество графиков" }).click();
  await expect(page.locator("[data-chart-surface]")).toHaveCount(4);
  check("two and four chart layouts");
  await page.getByRole("button", { name: "Количество графиков" }).click();
  await page.getByLabel("Биржа", { exact: true }).selectOption("binance");
  await expect
    .poll(
      async () => Number(await surface.getAttribute("data-visible-count")),
      { timeout: 45000 },
    )
    .toBeGreaterThan(50);
  check("real Binance futures candles");
  await page.getByLabel("Рынок", { exact: true }).selectOption("spot");
  await expect
    .poll(
      async () => Number(await surface.getAttribute("data-visible-count")),
      { timeout: 45000 },
    )
    .toBeGreaterThan(50);
  check("real Binance spot candles");
  await page.getByRole("button", { name: "Сравнить", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByLabel("Инструмент", { exact: true })
    .fill("ETHUSDT");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: /^ETHUSDT / })
    .click();
  await expect(page.locator(".ct-comparison .ct-line-chart")).toBeVisible({
    timeout: 45000,
  });
  check("normalized comparison from real history");
  await page.getByRole("button", { name: "Закрыть сравнение" }).click();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({
    path: resolve(output, "mobile-green.png"),
    fullPage: true,
  });
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > window.innerWidth,
  );
  expect(overflow).toBe(false);
  check("390px responsive layout without horizontal page overflow");
  expect(errors).toEqual([]);
  check("no browser runtime errors");
} finally {
  if (originalCloud) {
    const restored = await context.request.put(`${base}/api/chart/config`, {
      data: originalCloud,
      headers: { Origin: base },
    });
    expect(restored.status()).toBe(200);
  }
  writeFileSync(
    resolve(output, "browser-verification.json"),
    JSON.stringify({ url: target, checks, sources: [...sources], errors }, null, 2),
  );
  await browser.close();
}
