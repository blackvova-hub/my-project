import { expect } from "@playwright/test";
import path from "node:path";
import {
  indicators,
  makeRule,
  hasWindow,
} from "../src/pages/public/backtest/rules.ts";

export async function exerciseUI(
  page,
  context,
  base,
  out,
  check,
  jobs,
  setActive,
) {
  await page.goto("http://localhost:3000/backtest");
  const field = (name) => page.getByLabel(name, { exact: true });
  const button = (name) => page.getByRole("button", { name, exact: true });
  const select = async (label, text) => {
    await field(label).scrollIntoViewIfNeeded();
    await page.waitForTimeout(450);
    await field(label).click();
    await page
      .getByRole("option")
      .filter({ has: page.getByText(text, { exact: true }) })
      .click();
    await page.waitForTimeout(350);
  };
  await expect(button("Запустить тест")).toBeEnabled();
  await expect(
    page.getByText(
      /Начать с примера|Выбрать пример условий|Smart Money|Снятие ликвидности/,
    ),
  ).toHaveCount(0);
  await expect(page.locator('[aria-label="Таймфрейм"] button')).toHaveCount(2);
  await expect(
    page.getByRole("link", { name: "Тест стратегии", exact: true }).first(),
  ).toBeVisible();
  check("common header, no templates/Smart Money, only 1h and 4h");
  for (const [name, def] of Object.entries(indicators)) {
    await select("Вход в позицию 1: индикатор", def.name);
    for (const measure of def.measures) {
      if (def.measures.length > 1)
        await select("Вход в позицию 1: расчёт", measure.label);
      const rule = makeRule(name, measure.value);
      if (hasWindow(rule)) {
        await field("Вход в позицию 1: окно в часах").fill("12");
        await expect(page.locator(".bt-rule-summary")).toContainText("12 ч");
      }
      if (def.period)
        await field("Вход в позицию 1: период в свечах").fill("20");
      if (measure.value !== "signal_cross")
        await field("Вход в позицию 1: порог").fill("0");
      await expect(page.locator(".bt-rule-summary")).not.toContainText(
        /undefined|NaN/,
      );
      await expect(page.locator(".bt-rule-error")).toHaveCount(0);
    }
  }
  check("all 17 indicator selectors and every measure/period/window/threshold");
  await select("Вход в позицию 1: индикатор", "Цена");
  await button("4 часа").click();
  await field("Вход в позицию 1: окно в часах").fill("3");
  await expect(page.locator(".bt-rule-error")).toContainText("кратно 4");
  await page
    .locator(".bt-window-presets button")
    .filter({ hasText: /^1 д$/ })
    .click();
  await expect(page.locator(".bt-rule-error")).toHaveCount(0);
  await button("1 час").click();
  for (const option of ["Снижение", "В любую сторону", "Рост"])
    await select("Вход в позицию 1: направление изменения", option);
  for (const option of [
    "> Больше",
    "≥ Больше или равно",
    "< Меньше",
    "≤ Меньше или равно",
    "Пересекает вверх",
    "Пересекает вниз",
  ])
    await select("Вход в позицию 1: сравнение", option);
  await button("Spot").click();
  await expect(button("Short (продажа)")).toBeDisabled();
  await field("Вход в позицию 1: индикатор").click();
  await expect(
    page.getByRole("option", {
      name: "Открытый интерес (OI) · только Futures",
      exact: true,
    }),
  ).toBeDisabled();
  await page.keyboard.press("Escape");
  await button("Futures").click();
  await button("Добавить группу").click();
  await select("Вход в позицию 2: логика", "Все условия (AND)");
  await button("Удалить вход в позицию 2").click();
  await expect(page.locator(".bt-nested-group")).toHaveCount(0);
  await button("Добавить условие").click();
  await select("Вход в позицию 2: индикатор", "RSI");
  await button("Удалить вход в позицию 1").click();
  await expect(page.locator(".bt-rule")).toHaveCount(1);
  await expect(field("Вход в позицию 1: индикатор")).toContainText("RSI");
  await select("Вход в позицию: логика", "Любое условие (OR)");
  await select("Вход в позицию: логика", "Все условия (AND)");
  await page.getByRole("checkbox", { name: "Выход по условию" }).check();
  await select("Условие выхода 1: индикатор", "MACD");
  await select("Условие выхода 1: расчёт", "Пересечение сигнальной линии");
  await page.getByRole("checkbox", { name: "Выход по условию" }).uncheck();
  check(
    "operators/directions, TF windows, Spot restrictions, nested AND/OR, stable deletion, exit controls",
  );
  for (const coin of ["ETHUSDT", "SOLUSDT", "XRPUSDT", "ADAUSDT"]) {
    await button("Добавить").click();
    await field("Поиск торговой пары").fill(coin);
    await button(coin).click();
  }
  await expect(button("Добавить")).toBeDisabled();
  for (const coin of ["ETHUSDT", "SOLUSDT", "XRPUSDT", "ADAUSDT"])
    await button("Удалить " + coin).click();
  await expect(button("Удалить BTCUSDT")).toBeDisabled();
  for (const preset of ["30 дней", "90 дней", "1 год"]) {
    await button(preset).click();
    await expect(button(preset)).toHaveAttribute("aria-pressed", "true");
  }
  check("symbol search/add/remove/max5 and all date presets");
  const importRequest = async (r) => {
    await field("Импорт стратегии").setInputFiles({
      name: "strategy.json",
      mimeType: "application/json",
      buffer: Buffer.from(JSON.stringify(r)),
    });
    await page.waitForTimeout(400);
  };
  await importRequest(base);
  let downloaded = page.waitForEvent("download");
  await button("Экспорт JSON").click();
  const stream = await (await downloaded).createReadStream(),
    chunks = [];
  for await (const c of stream) chunks.push(c);
  expect(JSON.parse(Buffer.concat(chunks).toString("utf8"))).toEqual(base);
  await field("Импорт стратегии").setInputFiles({
    name: "bad.json",
    mimeType: "application/json",
    buffer: Buffer.from('{"strategy":{}}'),
  });
  await expect(page.getByRole("alert")).toContainText("Некорректный формат");
  await importRequest(base);
  for (const [label, value] of [
    ["Начальный капитал, USDT", "12000"],
    ["Размер позиции, %", "25"],
    ["Комиссия, %", "0.12"],
    ["Проскальзывание, %", "0.08"],
    ["Тейк-профит (TP), %", "1.5"],
    ["Стоп-лосс (SL), %", "1.2"],
  ]) {
    await field(label).fill(value);
    await expect(field(label)).toHaveValue(value);
  }
  await field("Комиссия, %").fill("7");
  await button("Запустить тест").click();
  expect(
    await field("Комиссия, %").evaluate((e) => e.validity.rangeOverflow),
  ).toBe(true);
  await importRequest(base);
  check(
    "JSON roundtrip/errors and all capital/TP/SL/fee/slippage fields with validation",
  );
  await page.locator(".bt-strategy").scrollIntoViewIfNeeded();
  await page.screenshot({ path: path.join(out, "editor-desktop.png") });
  const created = page.waitForResponse(
    (r) =>
      r.url().endsWith("/api/backtests") && r.request().method() === "POST",
  );
  await button("Запустить тест").click();
  const response = await created;
  expect(response.status()).toBe(202);
  const id = (await response.json()).jobId;
  setActive(id);
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Результаты теста", exact: true }),
  ).toBeVisible({ timeout: 120000 });
  await expect(page.locator(".bt-equity-chart canvas").first()).toBeVisible();
  await expect(page.locator(".bt-trades tbody tr").first()).toContainText(
    "ADAUSDT",
  );
  const firstJob = await (
    await context.request.get(`http://localhost:3000/api/backtests/${id}`)
  ).json();
  expect(firstJob.result.engineVersion).toBe("2.0.0");
  jobs.push({ id, label: "UI price", ...firstJob.result.metrics });
  setActive(undefined);
  const next = button("Следующая страница сделок");
  if (await next.isEnabled()) {
    await next.click();
    await expect(page.locator(".bt-pagination")).toContainText("21–");
    await button("Предыдущая страница сделок").click();
  }
  downloaded = page.waitForEvent("download");
  await button("Скачать сделки").click();
  expect((await downloaded).suggestedFilename()).toMatch(/backtest-.*\.csv/);
  await page.locator(".bt-results").evaluate((element) => window.scrollTo(0, element.getBoundingClientRect().top + window.scrollY - 96));
  await page.waitForTimeout(500);
  await page.screenshot({ path: path.join(out, "results-desktop.png") });
  await button("История тестов").click();
  await expect(page.locator(".bt-history-list")).toContainText("ADAUSDT");
  await button("История тестов").click();
  check("real UI job, reload, results, portfolio, trade table/CSV and history");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.locator(".bt-strategy").scrollIntoViewIfNeeded();
  await page.waitForTimeout(400);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(
    390,
  );
  await page.screenshot({ path: path.join(out, "editor-mobile.png") });
  check("mobile 390px without horizontal overflow");
}
