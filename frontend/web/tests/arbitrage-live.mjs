// Runs against a real local stack. Requires normal authenticated storageState.
// No mocked quotes, mocked authentication, orders or external notifications.
import { chromium, expect } from "@playwright/test";
import { mkdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

const base = process.env.ARBITRAGE_E2E_URL ?? "http://localhost:3000";
const storage = process.env.ARBITRAGE_E2E_STORAGE;
if (
  !storage &&
  !(process.env.ARBITRAGE_E2E_EMAIL && process.env.ARBITRAGE_E2E_PASSWORD)
)
  throw new Error(
    "Set ARBITRAGE_E2E_STORAGE, or ARBITRAGE_E2E_EMAIL and ARBITRAGE_E2E_PASSWORD for normal test-user login.",
  );
const output = resolve(
  process.env.ARBITRAGE_E2E_OUTPUT ?? "../../docs/arbitrage",
);
mkdirSync(output, { recursive: true });
const browser = await chromium.launch({
  channel: process.env.ARBITRAGE_E2E_CHANNEL ?? "msedge",
  headless: true,
});
const context = await browser.newContext({
  storageState: storage,
  viewport: { width: 1536, height: 1024 },
  reducedMotion: "reduce",
});
await context.addInitScript(() => {
  localStorage.setItem("cookie_consent", "essential");
  localStorage.setItem("mobile_notice_shown_v1", "1");
});
const page = await context.newPage();
const errors = [];
page.on("pageerror", (e) => errors.push(e.message));
const result = { checks: [], sources: [], themes: {}, errors };
let original;
const check = (name) => {
  result.checks.push(name);
  console.log(`PASS ${name}`);
};
try {
  if (!storage) {
    await page.goto(`${base}/login`);
    await page
      .getByLabel("Email", { exact: true })
      .fill(process.env.ARBITRAGE_E2E_EMAIL);
    await page
      .getByLabel("Пароль", { exact: true })
      .fill(process.env.ARBITRAGE_E2E_PASSWORD);
    await page.getByRole("button", { name: "Войти", exact: true }).click();
    await page.waitForURL((url) => !url.pathname.includes("login"), {
      timeout: 30000,
    });
  }
  const initial = await context.request.get(
    `${base}/api/arbitrage/preferences`,
  );
  expect(initial.status()).toBe(200);
  original = await initial.json();
  await page.goto(`${base}/arbitrage`);
  await expect(
    page.getByRole("heading", { name: "Настройки поиска" }),
  ).toBeVisible();
  const nav = page.getByRole("link", { name: "Арбитраж", exact: true });
  await expect(nav.first()).toHaveAttribute("href", "/arbitrage");
  check("authenticated page and desktop navigation");
  await page.getByRole("button", { name: "Сбросить", exact: true }).click();
  for (const [theme, label] of [
    ["light", "Светлая тема"],
    ["dark", "Тёмная тема"],
    ["green", "Фирменная зелёная тема"],
  ]) {
    await page.getByRole("button", { name: label, exact: true }).click();
    await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
    result.themes[theme] = await page.locator(".arb-page").evaluate((el) => {
      const panel = el.querySelector(".arb-panel"),
        input = el.querySelector('input[aria-label="Размер покупки, USDT"]');
      const p = panel.getBoundingClientRect(),
        i = input.getBoundingClientRect();
      return {
        pageWidth: el.getBoundingClientRect().width,
        panelX: p.x,
        panelWidth: p.width,
        panelRadius: getComputedStyle(panel).borderRadius,
        inputHeight: i.height,
        background: getComputedStyle(panel).backgroundColor,
      };
    });
    await page.screenshot({ path: resolve(output, `desktop-${theme}.png`) });
    check(`desktop ${theme} theme`);
  }
  const geometry = (t) => ({ ...t, background: undefined });
  expect(geometry(result.themes.light)).toEqual(geometry(result.themes.dark));
  expect(geometry(result.themes.dark)).toEqual(geometry(result.themes.green));
  check("theme changes preserve geometry");
  const sorting = page.getByRole("combobox", {
    name: "Сортировка",
    exact: true,
  });
  await sorting.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("listbox")).toBeVisible();
  await page.keyboard.press("End");
  await page.keyboard.press("Enter");
  await expect(sorting).toContainText("По длительности");
  await sorting.click();
  await page.getByRole("option", { name: "По спреду", exact: true }).click();
  check("shared dropdown mouse and keyboard interaction");
  const range = page.getByRole("slider", {
    name: "Ползунок минимального спреда",
  });
  await range.focus();
  const before = Number(await range.inputValue());
  await page.keyboard.press("ArrowRight");
  expect(Number(await range.inputValue())).toBeGreaterThan(before);
  check("slider keyboard input");
  const amount = page.getByRole("textbox", {
    name: "Размер покупки, USDT",
    exact: true,
  });
  await amount.fill("");
  await expect(
    page.getByRole("alert").filter({ hasText: "Размер покупки:" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Сохранить", exact: true }),
  ).toBeDisabled();
  await amount.fill("100");
  await page
    .getByRole("textbox", { name: "Минимальный спред, %", exact: true })
    .fill("-1");
  await page
    .getByRole("textbox", { name: "Монеты", exact: true })
    .fill("BTC, ETH, SOL");
  await expect(
    page
      .locator(".arb-table tbody tr")
      .first()
      .getByRole("button", { name: /^В избранное |^Убрать из избранного / }),
  ).toBeVisible({ timeout: 30000 });
  check("validation and real opportunities");
  let snapshot;
  await expect
    .poll(
      async () => {
        const live = await context.request.get(
          `${base}/api/arbitrage/opportunities?amount=100&minSpread=-1&assets=BTC,ETH,SOL`,
        );
        snapshot = await live.json();
        return ["binance", "bybit", "okx", "bitget", "gateio", "kucoin"].every(
          (e) =>
            snapshot.sources?.some(
              (s) => s.exchange === e && s.state === "live" && s.books > 0,
            ),
        );
      },
      { timeout: 60000, intervals: [1000, 2000, 3000] },
    )
    .toBeTruthy();
  expect(snapshot.transport).toBe("live");
  expect(snapshot.rows.length).toBeGreaterThan(0);
  result.sources = snapshot.sources;
  result.availableAssets = snapshot.assets;
  result.configuredAssets = snapshot.configuredAssets;
  for (const e of ["binance", "bybit", "okx", "bitget", "gateio", "kucoin"])
    expect(
      snapshot.sources.some(
        (s) => s.exchange === e && s.state === "live" && s.books > 0,
      ),
    ).toBeTruthy();
  check("six exchanges supply real quotes");
  await page.getByRole("button", { name: "Пауза", exact: true }).click();
  const firstRow = page.locator(".arb-table tbody tr").first();
  const asset = await firstRow.getByRole("button").last().textContent();
  await firstRow.getByRole("button").last().click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "История спреда лучших bid/ask" }),
  ).toBeVisible();
  await expect(
    page.getByRole("dialog").getByRole("link", { name: "Открыть инструмент" }),
  ).toHaveCount(2);
  await page.screenshot({ path: resolve(output, "route-details.png") });
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(firstRow.getByRole("button").last()).toBeFocused();
  check("route dialog, external instrument links and Escape focus restoration");
  const star = firstRow.getByRole("button").first();
  if ((await star.getAttribute("aria-pressed")) === "false") await star.click();
  await expect(star).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("button", { name: "Сохранить", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Настройки сохранены." }),
  ).toBeVisible();
  await page.goto(`${base}/news`);
  await page.goto(`${base}/arbitrage`);
  await expect(
    page.getByRole("textbox", { name: "Размер покупки, USDT", exact: true }),
  ).toHaveValue("100");
  await page.reload();
  await expect(
    page.getByRole("textbox", { name: "Размер покупки, USDT", exact: true }),
  ).toHaveValue("100");
  const saved = await (
    await context.request.get(`${base}/api/arbitrage/preferences`)
  ).json();
  expect(saved.settings.assets).toEqual(["BTC", "ETH", "SOL"]);
  expect(saved.favorites.length).toBeGreaterThan(0);
  check("filters and favorites survive navigation and reload");
  for (const [strategy, label] of [
    ["perpetual-perpetual", "Фьючерс–фьючерс"],
    ["spot-spot", "Спот–спот"],
  ]) {
    const response = page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname.endsWith("/arbitrage/opportunities") &&
        new URL(r.url()).searchParams.get("strategy") === strategy &&
        r.status() === 200,
    );
    await page.getByRole("button", { name: label, exact: true }).click();
    const payload = await (await response).json();
    expect(payload.rows.length).toBeGreaterThan(0);
    expect(payload.rows.every((r) => r.strategy === strategy)).toBeTruthy();
    await expect(
      page.locator(".arb-table tbody tr").first().locator("td").nth(3),
    ).not.toHaveText("—");
  }
  await expect(
    page.getByText("Спот–спот предполагает заранее размещённые средства", {
      exact: false,
    }),
  ).toBeVisible();
  check("all three arbitrage modes");
  await page.getByRole("button", { name: "Пауза", exact: true }).click();
  const pausedAt = performance.now();
  await expect
    .poll(async () => page.locator(".arb-table tbody").textContent(), {
      timeout: 15000,
    })
    .toContain("Устарело");
  expect(performance.now() - pausedAt).toBeGreaterThan(5000);
  check("paused quotes expire and calculated percentages disappear");
  await page.getByRole("button", { name: "Продолжить", exact: true }).click();
  await page.getByRole("button", { name: "Спот–фьючерс", exact: true }).click();
  await page.getByText("Комиссии и сценарий выхода", { exact: true }).click();
  await page
    .getByRole("checkbox", { name: "Задать комиссии вручную", exact: true })
    .check();
  await page
    .getByRole("textbox", { name: "Taker спот, %", exact: true })
    .fill("0,08");
  await page
    .getByRole("combobox", { name: "Горизонт funding", exact: true })
    .click();
  await page.getByRole("option", { name: "8 ч", exact: true }).click();
  await expect(
    page.getByRole("columnheader", { name: "Funding за 8 ч" }),
  ).toBeVisible();
  check("fee overrides and funding horizon controls");
  for (const [name, value] of [
    ["Максимальный возраст котировки", "5 с"],
    ["Число результатов", "50"],
  ]) {
    await page.getByRole("combobox", { name, exact: true }).click();
    await page.getByRole("option", { name: value, exact: true }).click();
  }
  await page
    .getByRole("checkbox", {
      name: "Только между разными биржами",
      exact: true,
    })
    .uncheck();
  await page
    .getByRole("textbox", { name: "Спред при закрытии, %", exact: true })
    .fill("0,2");
  await page.getByRole("button", { name: "Bybit", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Bybit", exact: true }),
  ).toHaveAttribute("aria-pressed", "false");
  check(
    "exchange selections, quote age, result limit, same-exchange toggle and exit scenario",
  );
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${base}/arbitrage`);
  await expect(
    page.getByRole("heading", { name: "Настройки поиска" }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
  ).toBeTruthy();
  for (const [theme, label] of [
    ["light", "Светлая тема"],
    ["dark", "Тёмная тема"],
    ["green", "Фирменная зелёная тема"],
  ]) {
    await page.getByRole("button", { name: label, exact: true }).click();
    await page.screenshot({
      path: resolve(output, `mobile-${theme}.png`),
      fullPage: false,
    });
  }
  await page.getByText("Меню", { exact: true }).click();
  const mobileNav = page.getByRole("navigation", {
    name: "Мобильная навигация",
  });
  await expect(
    mobileNav.getByRole("link", { name: "Арбитраж", exact: true }),
  ).toHaveAttribute("href", "/arbitrage");
  await mobileNav.getByRole("link", { name: "Арбитраж", exact: true }).click();
  check("390px layout, three mobile themes and navigation");
  await page
    .getByRole("textbox", { name: "Монеты", exact: true })
    .fill("UNKNOWN");
  const emptyMessage = page
    .getByRole("status")
    .filter({ hasText: "Связок по выбранным условиям нет" });
  await expect(emptyMessage).toBeVisible();
  await emptyMessage.scrollIntoViewIfNeeded();
  await expect(emptyMessage).toBeInViewport();
  const emptyBounds = await emptyMessage.boundingBox();
  expect(emptyBounds.x).toBeGreaterThanOrEqual(0);
  expect(emptyBounds.x + emptyBounds.width).toBeLessThanOrEqual(390);
  await page.screenshot({ path: resolve(output, "mobile-results.png") });
  check("mobile empty state stays visible outside the wide table");
  await page.setViewportSize({ width: 1536, height: 1024 });
  for (const [path, title] of [
    ["/scanners/1", "Крипто-сканер"],
    ["/backtest", "Тест стратегии"],
    ["/replay", "Бэктест"],
  ]) {
    await page.goto(base + path);
    await expect(
      page.getByRole("heading", { name: title, exact: true }).first(),
    ).toBeVisible();
  }
  await page.goto(base + "/account");
  await expect(page.locator("main").first()).toBeVisible();
  check("existing scanner, backtest, replay and account still render");
  const noAuth = await browser.newContext();
  const unauthorized = await noAuth.request.get(
    `${base}/api/arbitrage/opportunities`,
  );
  expect(unauthorized.status()).toBe(401);
  await noAuth.close();
  const csrf = await context.request.put(`${base}/api/arbitrage/preferences`, {
    headers: { Origin: "https://evil.test" },
    data: saved,
  });
  expect(csrf.status()).toBe(403);
  check("real gateway authentication and cross-site protection");
  expect(errors).toEqual([]);
  result.ok = true;
} catch (e) {
  result.ok = false;
  result.failure = e.message;
  await page.screenshot({
    path: resolve(output, "failure.png"),
    fullPage: false,
  });
  throw e;
} finally {
  if (original) {
    // API requires a complete valid settings object. Restore initially absent
    // preferences to defaults, preserving the user's original favorites.
    const settings = original.settings ?? {
      strategy: "spot-perpetual",
      amount: 1000,
      minSpread: 0.1,
      maxAgeMs: 10000,
      horizonHours: 24,
      exitSpread: 0,
      exchanges: ["binance", "bybit", "okx", "bitget", "gateio", "kucoin"],
      assets: [],
      interExchange: true,
      sort: "spread",
      limit: 100,
    };
    await context.request.put(`${base}/api/arbitrage/preferences`, {
      data: { settings, favorites: original.favorites },
      headers: { Origin: base },
    });
  }
  writeFileSync(
    resolve(output, "browser-verification.json"),
    JSON.stringify(result, null, 2),
  );
  await browser.close();
}
