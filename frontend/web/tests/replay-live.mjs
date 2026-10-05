import { chromium, expect } from "@playwright/test";
import { mkdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

const base = process.env.LIVE_URL || "http://192.168.0.124";
const out = resolve(process.env.LIVE_REPORT_DIR || "../../docs/replay/rework");
mkdirSync(out, { recursive: true });
const browser = await chromium.launch({ headless: true, executablePath: process.env.LIVE_BROWSER_EXECUTABLE || "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe" });
const context = await browser.newContext({ viewport: { width: 1586, height: 992 } });
await context.addInitScript(() => {
  localStorage.setItem("cookie_consent", "essential");
  localStorage.setItem("mobile_notice_shown_v1", "1");
});
const page = await context.newPage();
const checks = [], errors = [];
page.on("pageerror", (error) => errors.push(error.message));
const button = (name) => page.getByRole("button", { name, exact: true });
const chart = page.getByTestId("replay-chart");
const count = async () => Number(await chart.getAttribute("data-visible-count"));
try {
  const login = await context.request.post(base + "/api/auth/login", { data: { email: process.env.LIVE_ADMIN_EMAIL || "admin@local.dev", password: process.env.LIVE_ADMIN_PASSWORD } });
  expect(login.ok()).toBe(true);
  const catalog = await context.request.get(base + "/api/replay/symbols?exchange=bybit&market=linear");
  expect(catalog.ok()).toBe(true);
  const btc = (await catalog.json()).symbols.find((symbol) => symbol.symbol === "BTCUSDT");
  expect(btc).toBeTruthy();
  await page.goto(base + "/replay");
  await expect(chart).toBeVisible({ timeout: 30000 });
  await expect(button("Play")).toBeEnabled();
  await expect(button("Открыть Long")).toBeEnabled();
  await expect(page.getByText("Виртуальный счёт", { exact: true })).toHaveCount(0);
  await expect(page.getByTestId("paper-balance")).toBeVisible();
  expect(await page.getByTestId("paper-balance").evaluate((element) => !!element.closest(".rp-trade-panel"))).toBe(true);
  await expect(button("Открыть архив")).toHaveCount(0);
  await expect(page.getByLabel("Комиссия, %")).toHaveCount(0);
  await expect(page.locator(".rp-chart-toolbar .rp-controls")).toBeVisible();
  expect(await page.locator(".rp-return").count()).toBe(1);
  const chartBox = await page.locator(".rp-chart-wrap").boundingBox();
  const toolsBox = await page.locator(".rp-drawing-tools").boundingBox();
  const tradeBox = await page.locator(".rp-trade-panel").boundingBox();
  expect(tradeBox.x).toBeGreaterThan(chartBox.x + chartBox.width);
  expect(Math.abs(toolsBox.x + toolsBox.width - chartBox.x)).toBeLessThan(1.1);
  expect(await page.locator(".rp-drawing-tools").evaluate((element) => getComputedStyle(element).overflowY)).toBe("hidden");
  expect(await page.locator(".rp-drawing-tools").evaluate((element) => element.scrollHeight <= element.clientHeight + 1)).toBe(true);
  expect(chartBox.height).toBeGreaterThan(500);
  expect(await count()).toBeGreaterThan(100);
  checks.push("Счёт и PnL в карточке сделки; единая полоса управления, высокий график, без комиссии и кнопки архива");
  await page.screenshot({ path: out + "/desktop.png", fullPage: true });

  await page.getByRole("button", { name: /^Индикаторы/ }).click();
  const menu = page.getByRole("dialog", { name: "Каталог индикаторов" });
  await expect(menu).toBeVisible();
  await expect(menu.getByRole("switch")).toHaveCount(23);
  await page.screenshot({ path: out + "/indicators.png" });
  await menu.getByRole("switch", { name: /Объём/ }).click();
  await expect(menu.getByRole("switch", { name: /Объём/ })).toHaveAttribute("aria-checked", "false");
  await menu.getByRole("switch", { name: /Supertrend/ }).click();
  await menu.getByRole("switch", { name: /BOS/ }).click();
  await page.keyboard.press("Escape");
  await expect(page.locator(".rp-indicator-legend")).toContainText("ST 10×3");
  checks.push("Каталог страницы монеты: 22 индикатора и объём, включая Supertrend и Smart Money");

  const before = await count();
  const spanBefore = Number(await chart.getAttribute("data-visible-span"));
  await button("Следующая свеча").click();
  expect(await count()).toBe(before + 1);
  await button("Предыдущая свеча").click();
  expect(await count()).toBe(before);
  await page.getByLabel("Скорость воспроизведения").selectOption("20");
  await button("Play").click();
  await expect.poll(count).toBeGreaterThan(before + 8);
  await button("Pause").click();
  expect(Number(await chart.getAttribute("data-visible-span"))).toBeCloseTo(spanBefore, 1);
  await button("Play").click();
  const panBox = await chart.boundingBox();
  const panX = panBox.x + panBox.width * 0.5;
  const panY = panBox.y + panBox.height * 0.45;
  const rightBeforePan = Number(await chart.getAttribute("data-visible-right"));
  await page.mouse.move(panX, panY);
  await page.mouse.down();
  await page.mouse.move(panX + 210, panY, { steps: 12 });
  await page.mouse.up();
  const rightAfterPan = Number(await chart.getAttribute("data-visible-right"));
  expect(rightBeforePan - rightAfterPan).toBeGreaterThan(8);
  const countAfterPan = await count();
  await expect.poll(count).toBeGreaterThan(countAfterPan + 5);
  expect(Math.abs(Number(await chart.getAttribute("data-visible-right")) - rightAfterPan)).toBeLessThan(1.5);
  await button("К текущей свече").click();
  await expect.poll(async () => Number(await chart.getAttribute("data-visible-right"))).toBeGreaterThan(rightAfterPan + 8);
  await button("Pause").click();
  await button("250").click();
  await expect(page.locator("#rp-position-size")).toHaveValue("250");
  await button("Открыть Long").click();
  await expect(page.locator(".rp-open-position")).toContainText("LONG");
  await expect(button("Открыть Short")).toHaveCount(0);
  await button("Следующая свеча").click();
  await button("Закрыть позицию").click();
  await expect(page.getByRole("tab", { name: "Журнал сделок (1)" })).toBeVisible();
  checks.push("При 20x масштаб стабилен; ручное перетаскивание не откатывается во время Play; возврат включает слежение; сделки без комиссии");

  const target = new Date(btc.last - 2 * 86400000);
  const targetDay = Date.UTC(target.getUTCFullYear(), target.getUTCMonth(), target.getUTCDate());
  const dateRequests = [];
  page.on("request", (request) => { if (request.url().includes("/api/replay/candles?")) dateRequests.push(new URL(request.url())); });
  await page.getByLabel("Выбрать дату начала Replay").click();
  await expect(page.locator(".rp-calendar-popover")).toBeVisible();
  await page.screenshot({ path: out + "/calendar.png" });
  const month = ["Январь", "Февраль", "Март", "Апрель", "Май", "Июнь", "Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь"][target.getUTCMonth()];
  const dayName = `${target.getUTCDate()} ${month} ${target.getUTCFullYear()} UTC`;
  for (let attempt = 0; attempt < 2 && await button(dayName).count() === 0; attempt++) await button("Следующий месяц").click();
  await expect(button(dayName)).toBeEnabled();
  await button(dayName).click();
  await expect(chart).toBeVisible({ timeout: 30000 });
  await expect(page.getByRole("tab", { name: "Журнал сделок (0)" })).toBeVisible();
  await expect(page.locator(".rp-loading-overlay")).toHaveCount(0);
  expect(Number(await chart.getAttribute("data-last-time"))).toBeGreaterThanOrEqual(targetDay / 1000);
  const step = 300000;
  const first = Math.ceil(btc.first / step) * step;
  const last = Math.floor(Math.min(btc.last + step, Date.now()) / step) * step;
  const anchor = Math.max(first, Math.min(last - step, targetDay));
  expect(Math.min(...dateRequests.map((request) => Number(request.searchParams.get("from"))))).toBe(Math.max(first, anchor - 2000 * step));
  expect(Math.max(...dateRequests.map((request) => Number(request.searchParams.get("to"))))).toBe(Math.min(last, anchor + 5001 * step));
  checks.push("Клик по дню UTC загружает историю вокруг начала Replay и сбрасывает учебные сделки");

  await button("Трендовая линия").click();
  const drawBox = await page.getByTestId("drawing-canvas").boundingBox();
  await page.mouse.move(drawBox.x + 180, drawBox.y + 160);
  await page.mouse.down();
  await page.mouse.move(drawBox.x + 320, drawBox.y + 220, { steps: 8 });
  await page.mouse.up();
  await expect(chart).toHaveAttribute("data-drawing-count", "1");
  await button("Отменить последний объект").click();
  await expect(chart).toHaveAttribute("data-drawing-count", "0");
  checks.push("Рисование и отмена работают на архивном графике");

  for (const width of [1280, 1024, 768, 390]) {
    await page.setViewportSize({ width, height: 844 });
    await page.waitForTimeout(150);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
    expect(await page.locator(".rp-drawing-tools").evaluate((element) => element.scrollHeight <= element.clientHeight + 1)).toBe(true);
    if (width <= 1100) {
      const rail = await page.locator(".rp-drawing-tools").boundingBox();
      const plot = await page.locator(".rp-chart-wrap").boundingBox();
      expect(Math.abs(rail.y + rail.height - plot.y)).toBeLessThan(1.1);
    }
    await expect(button("Play")).toBeVisible();
    await page.screenshot({ path: `${out}/viewport-${width}.png`, fullPage: true });
  }
  checks.push("Без горизонтального переполнения на 1280/1024/768/390 px");
  expect(errors).toEqual([]);
  writeFileSync(out + "/checks.json", JSON.stringify({ checks, errors }, null, 2));
  console.log(JSON.stringify({ checks, errors }, null, 2));
} catch (error) {
  await page.screenshot({ path: out + "/failure.png", fullPage: true });
  writeFileSync(out + "/failure.json", JSON.stringify({ checks, errors, error: String(error) }, null, 2));
  throw error;
} finally {
  await browser.close();
}
