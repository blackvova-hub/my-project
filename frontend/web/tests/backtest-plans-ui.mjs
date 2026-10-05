// Mocked UI contracts; Go tests cover server authorization separately.
import { chromium, expect } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { defaultRequest } from "../src/pages/public/backtest/model.ts";

const base = process.env.BACKTEST_E2E_URL ?? "http://localhost:3000";
const out = "../../tmp/backtest-plans";
mkdirSync(out, { recursive: true });
const symbols = ["BTCUSDT", "ETHUSDT", ...Array.from({ length: 25 }, (_, i) => `COIN${i}USDT`)];
const browser = await chromium.launch({ channel: "msedge", headless: true });
try {
  for (const [plan, max] of [["free", 1], ["standard", 5], ["pro", 10]]) {
    const context = await browser.newContext({ viewport: { width: 1536, height: 1000 }, reducedMotion: "reduce" });
    await context.addInitScript(() => {
      localStorage.setItem("cookie_consent", "essential");
      localStorage.setItem("mobile_notice_shown_v1", "1");
    });
    let submitted;
    await context.route("**/api/**", async (route) => {
      const url = new URL(route.request().url());
      let body = {};
      if (url.pathname === "/api/auth/me") body = { user: { id: "plan-test", email: "plan@example.test", displayName: "Plan test", plan, emailVerified: true, primaryExchange: "bybit" } };
      else if (url.pathname === "/api/backtests/symbols") body = { symbols: url.searchParams.get("market") === "spot" ? ["BTCUSDT", "SPOTAAUSDT", "SPOTBBUSDT"] : symbols };
      else if (url.pathname === "/api/backtests" && route.request().method() === "GET") body = { jobs: [] };
      else if (url.pathname === "/api/backtests" && route.request().method() === "POST") {
        submitted = route.request().postDataJSON();
        await route.fulfill({ status: 422, json: { error: "UI test: request captured" } });
        return;
      }
      await route.fulfill({ json: body });
    });
    const page = await context.newPage();
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    const button = (name) => page.getByRole("button", { name, exact: true });
    const dialog = page.getByRole("dialog");
    const selected = () => page.locator(".bt-symbol").allTextContents();
    const closeDialog = async (text) => {
      await expect(dialog).toBeVisible();
      await expect(dialog).toContainText(text);
      await page.keyboard.press("Escape");
      await expect(dialog).not.toBeVisible();
      expect(await page.evaluate(() => document.body.style.overflow)).not.toBe("hidden");
    };
    await page.goto(`${base}/backtest`);
    await expect(button("Запустить тест")).toBeEnabled();
    await expect(page.locator(".bt-page")).not.toContainText(/Тариф (Free|Standard|Pro)|Условия: \d+ из|Лимит общий/);
    await expect(page.locator(".bt-symbol-field")).toContainText("из 15");
    await page.evaluate(() => window.scrollTo(0, 600));
    await page.evaluate(() => window.scrollTo(0, 300));
    expect(await page.locator("header").evaluate((el) => el.getBoundingClientRect().bottom)).toBeLessThan(0);
    await button("Рандом").click();
    const first = await selected();
    expect(first[0]).not.toContain("BTCUSDT");
    await button("Рандом").click();
    expect(await selected()).not.toEqual(first);
    await button("Spot").click();
    await button("Рандом").click();
    await expect(page.locator(".bt-symbol")).toContainText("SPOT");
    await button("Futures").click();
    for (const symbol of symbols.slice(1, max)) {
      await button("Добавить").click();
      await button(symbol).click();
    }
    await button("Добавить").click();
    await closeDialog(plan === "free" ? /Standard и Pro/ : plan === "standard" ? /в Pro/ : /до 10/);
    await expect(button("Добавить")).toBeFocused();
    const before = await selected();
    await button("Рандом").click();
    const after = await selected();
    expect(after).toHaveLength(max);
    expect(new Set(after).size).toBe(max);
    expect(after.some((s) => before.includes(s))).toBe(false);
    await button("Добавить группу").click();
    if (plan === "free") {
      await closeDialog(/Standard и Pro/);
      await button("Добавить условие").click();
      await button("Добавить условие").click();
      await button("Добавить условие").click();
      await closeDialog(/Дополнительные условия/);
      await expect(page.locator(".bt-rule")).toHaveCount(3);
      await page.getByRole("checkbox", { name: "Выход по условию" }).click();
      await closeDialog(/условия выхода/);
      await expect(page.getByRole("checkbox", { name: "Выход по условию" })).not.toBeChecked();
    } else {
      await page.locator(".bt-nested-group").getByRole("button", { name: "Добавить группу", exact: true }).click();
      if (plan === "standard") await closeDialog(/в Pro/);
      else await expect(page.locator(".bt-nested-group")).toHaveCount(2);
    }
    await button("Запустить тест").click();
    await expect(page.getByRole("alert")).toContainText("UI test: request captured");
    expect(submitted.symbols).toHaveLength(max);
    expect(submitted.plan).toBeUndefined();
    const invalid = defaultRequest();
    invalid.symbols = [...symbols.slice(0, max), "EXTRAUSDT"];
    await page.getByLabel("Импорт стратегии", { exact: true }).setInputFiles({ name: "over-limit.json", mimeType: "application/json", buffer: Buffer.from(JSON.stringify(invalid)) });
    if (plan === "pro") await expect(page.getByRole("alert")).toContainText("Некорректный формат");
    else await closeDialog(/максимум торговых пар/);
    await expect(page.locator(".bt-symbol")).toHaveCount(max);
    if (plan === "free") {
      await page.evaluate((r) => localStorage.setItem("backtest:draft:v1:plan-test", JSON.stringify(r)), invalid);
      await page.reload();
      await expect(page.locator(".bt-symbol")).toHaveCount(2);
      const previous = submitted;
      await button("Запустить тест").click();
      await closeDialog(/максимум торговых пар/);
      expect(submitted).toBe(previous);
    }
    await button("Сбросить настройки").click();
    if (plan === "free") {
      await button("Удалить BTCUSDT").click();
      await button("Добавить").click();
      await button("ETHUSDT").click();
      await expect(page.locator(".bt-symbol")).toHaveCount(1);
    }
    await page.screenshot({ path: `${out}/${plan}-desktop.png`, fullPage: true });
    await page.setViewportSize({ width: 390, height: 844 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    if (plan === "free") {
      await button("Добавить").click();
      await expect(dialog).toBeVisible();
      await page.screenshot({ path: `${out}/${plan}-modal.png` });
      await button("Понятно").click();
      await expect(dialog).not.toBeVisible();
    }
    expect(errors).toEqual([]);
    console.log(`PASS ${plan}: static header, limit dialogs, random without duplicates, submit/import, mobile`);
    await context.close();
  }
} finally {
  await browser.close();
}
