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
  const defaults = {
    simple: true,
    search: "",
    strategy: "spot-perpetual",
    amount: 1000,
    minSpread: 0,
    maxAgeMs: 10000,
    horizonHours: 24,
    exitSpread: 0,
    exchanges: ["binance", "bybit", "okx", "bitget", "gateio", "kucoin"],
    assets: [],
    interExchange: true,
    sort: "spread",
    limit: 100,
  };
  expect(
    (
      await context.request.put(`${base}/api/arbitrage/preferences`, {
        data: { settings: defaults, favorites: [] },
        headers: { Origin: base },
      })
    ).status(),
  ).toBe(200);
  await page.goto(`${base}/arbitrage`);
  await expect(
    page.getByRole("heading", { name: "Арбитраж", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Арбитраж", exact: true }).first(),
  ).toHaveAttribute("href", "/arbitrage");
  await expect(
    page.getByRole("textbox", { name: "Найти монету" }),
  ).toBeVisible();
  await expect(page.getByRole("slider")).toHaveCount(0);
  await expect(page.getByRole("combobox")).toHaveCount(0);
  await expect(
    page.getByText("Комиссии и сценарий выхода", { exact: true }),
  ).toHaveCount(0);
  check("simple authenticated page: only modes, exchanges and coin search");
  let snapshot;
  await expect
    .poll(
      async () => {
        const r = await context.request.get(
          `${base}/api/arbitrage/opportunities?simple=true&limit=100`,
        );
        if (r.status() !== 200) return false;
        snapshot = await r.json();
        return defaults.exchanges.every((e) =>
          ["spot", "perpetual"].every((k) =>
            snapshot.sources.some(
              (s) =>
                s.exchange === e &&
                s.market === k &&
                s.state === "live" &&
                s.books > 0 &&
                (!s.connections || s.connected === s.connections) &&
                (e !== "gateio" || s.books > 50),
            ),
          ),
        );
      },
      { timeout: 120000, intervals: [2000, 5000] },
    )
    .toBeTruthy();
  expect(snapshot.assets.length).toBeGreaterThan(30);
  expect(snapshot.universeMode).toBe("all");
  expect(snapshot.rows.length).toBeGreaterThan(0);
  result.sources = snapshot.sources;
  result.availableAssets = snapshot.assets;
  result.total = snapshot.total;
  for (const o of snapshot.rows) {
    expect(o.entrySpreadPct).toBeGreaterThan(0);
    expect(o.entrySpreadPct).toBeCloseTo(
      (o.sell.price / o.buy.price - 1) * 100,
      8,
    );
    expect(o.depthUsdt).toBeCloseTo(o.quantity * o.buy.price, 6);
    expect(o.buy.instrument.exchange).not.toBe(o.sell.instrument.exchange);
  }
  for (const ticker of ["UP", "EWT", "哈基米"]) {
    const r = await context.request.get(
      `${base}/api/arbitrage/opportunities?simple=true&search=${encodeURIComponent(ticker)}&limit=300`,
    );
    expect(r.status()).toBe(200);
    for (const o of (await r.json()).rows) {
      expect(o.asset).toContain(ticker);
      expect(o.buy.instrument.assetIdentity).toBe(
        o.sell.instrument.assetIdentity,
      );
      if (ticker === "EWT")
        for (const leg of [o.buy, o.sell])
          expect(
            `${leg.instrument.exchange}:${leg.instrument.market}`,
          ).not.toBe("gateio:perpetual");
    }
  }
  check(
    "all six spot and futures sources, expanded coin universe, real raw bid/ask and matched volume",
  );
  await expect(page.locator(".arb-table tbody tr").first()).toBeVisible();
  for (const [theme, label] of [
    ["light", "Светлая тема"],
    ["dark", "Тёмная тема"],
    ["green", "Фирменная зелёная тема"],
  ]) {
    await page.getByRole("button", { name: label, exact: true }).click();
    await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
    result.themes[theme] = await page.locator(".arb-page").evaluate((el) => {
      const p = el.querySelector(".arb-panel"),
        i = el.querySelector("input"),
        r = p.getBoundingClientRect();
      return {
        pageWidth: el.getBoundingClientRect().width,
        panelX: r.x,
        panelWidth: r.width,
        panelRadius: getComputedStyle(p).borderRadius,
        inputHeight: i.getBoundingClientRect().height,
        background: getComputedStyle(p).backgroundColor,
      };
    });
    await page.screenshot({
      path: resolve(output, `simple-desktop-${theme}.png`),
    });
    check(`desktop ${theme}`);
  }
  const geometry = (t) => ({ ...t, background: undefined });
  expect(geometry(result.themes.light)).toEqual(geometry(result.themes.dark));
  expect(geometry(result.themes.dark)).toEqual(geometry(result.themes.green));
  check("three themes preserve geometry");
  await page.getByRole("button", { name: "Пауза", exact: true }).click();
  const row = page.locator(".arb-table tbody tr").first();
  const asset = await row
    .locator("td")
    .first()
    .getByRole("button")
    .last()
    .textContent();
  const detail = row.getByRole("button", { name: /^Подробности/ });
  await detail.click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "История разницы цен" }),
  ).toBeVisible();
  await expect(
    page.getByRole("dialog").getByRole("link", { name: "Открыть инструмент" }),
  ).toHaveCount(2);
  await expect(
    page.getByRole("dialog").getByText("Комиссии входа", { exact: true }),
  ).toHaveCount(0);
  await page.screenshot({ path: resolve(output, "simple-route-details.png") });
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(detail).toBeFocused();
  check(
    "simple route details, book levels, history, two exchange links and focus restoration",
  );
  const star = row.getByRole("button").first();
  await star.click();
  await expect(star).toHaveAttribute("aria-pressed", "true");
  const favoriteId = (
    await (
      await context.request.get(`${base}/api/arbitrage/preferences`)
    ).json()
  ).favorites[0];
  const favoriteResponse = await context.request.get(
    `${base}/api/arbitrage/opportunities?simple=true&routes=${favoriteId}`,
  );
  expect(
    (await favoriteResponse.json()).rows.every((o) => o.id === favoriteId),
  ).toBeTruthy();
  await page.getByRole("button", { name: "Продолжить", exact: true }).click();
  await page.getByRole("button", { name: "Избранное", exact: true }).click();
  await expect
    .poll(async () => {
      const rows = page.locator(".arb-table tbody tr");
      return (await rows.count()) <= 1;
    })
    .toBeTruthy();
  await page.getByRole("button", { name: "Все", exact: true }).click();
  check("favorites saved and queried independently of top-result pagination");
  const search = page.getByRole("textbox", { name: "Найти монету" });
  await search.fill(asset);
  await search.blur();
  await expect
    .poll(async () => {
      const r = await context.request.get(`${base}/api/arbitrage/preferences`);
      return (await r.json()).settings.search;
    })
    .toBe(asset);
  await page.reload();
  await expect(search).toHaveValue(asset);
  await search.fill("");
  await search.blur();
  check("coin search and automatic preferences survive reload");
  for (const [strategy, label] of [
    ["perpetual-perpetual", "Фьючерс–фьючерс"],
    ["spot-spot", "Спот–спот"],
    ["spot-perpetual", "Спот–фьючерс"],
  ]) {
    const response = page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname.endsWith("/arbitrage/opportunities") &&
        new URL(r.url()).searchParams.get("strategy") === strategy &&
        r.status() === 200,
    );
    await page.getByRole("button", { name: label, exact: true }).click();
    const d = await (await response).json();
    expect(d.rows.length).toBeGreaterThan(0);
    expect(d.rows.every((o) => o.strategy === strategy)).toBeTruthy();
  }
  check("all three live arbitrage modes");
  const bybit = page.getByRole("button", { name: "Bybit", exact: true });
  await bybit.click();
  await expect(bybit).toHaveAttribute("aria-pressed", "false");
  await expect
    .poll(async () => {
      const p = await (
        await context.request.get(`${base}/api/arbitrage/preferences`)
      ).json();
      return p.settings.exchanges.includes("bybit");
    })
    .toBe(false);
  await bybit.click();
  await expect(bybit).toHaveAttribute("aria-pressed", "true");
  check("exchange selection saved automatically");
  const paging = await context.request.get(
    `${base}/api/arbitrage/opportunities?simple=true&offset=100&limit=100`,
  );
  const second = await paging.json();
  expect(second.offset).toBe(
    second.total === 0
      ? 0
      : Math.min(100, Math.floor((second.total - 1) / 100) * 100),
  );
  expect(second.rows.length).toBeLessThanOrEqual(100);
  if (
    await page.getByRole("button", { name: "Далее", exact: true }).isVisible()
  ) {
    await page.getByRole("button", { name: "Далее", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Назад", exact: true }),
    ).toBeEnabled();
    await page.getByRole("button", { name: "Назад", exact: true }).click();
  }
  check("complete result count and bounded pagination");
  await page.getByRole("button", { name: "Пауза", exact: true }).click();
  await expect(
    page.locator(".arb-table tbody tr").first().locator("td").nth(3),
  ).toHaveText("—", { timeout: 15000 });
  await expect(page.locator(".arb-table tbody")).toContainText("Устарело");
  await page.getByRole("button", { name: "Продолжить", exact: true }).click();
  check("paused quotes expire, percentages and available volume disappear");
  await page.locator(".arb-sources summary").click();
  await expect(page.getByText(/монет с котировками/)).toBeVisible();
  await page.locator(".arb-sources summary").click();
  check("source coverage and connection details disclosure");
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(
    page.locator(".arb-table tbody tr").first().locator("td").nth(3),
  ).not.toHaveText("—");
  for (const [theme, label] of [
    ["light", "Светлая тема"],
    ["dark", "Тёмная тема"],
    ["green", "Фирменная зелёная тема"],
  ]) {
    await page.getByRole("button", { name: label, exact: true }).click();
    await page.evaluate(() => window.scrollTo(0, 0));
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    ).toBeTruthy();
    await page.screenshot({
      path: resolve(output, `simple-mobile-${theme}.png`),
    });
  }
  await page.getByText("Меню", { exact: true }).click();
  await expect(
    page
      .getByRole("navigation", { name: "Мобильная навигация" })
      .getByRole("link", { name: "Арбитраж", exact: true }),
  ).toHaveAttribute("href", "/arbitrage");
  await page
    .getByRole("navigation", { name: "Мобильная навигация" })
    .getByRole("link", { name: "Арбитраж", exact: true })
    .click();
  check(
    "390px mobile, all three themes, scrollable table and existing navigation",
  );
  await search.fill("NONEXISTINGCOINZZZ");
  await expect(
    page.getByText("Связок не найдено", { exact: true }),
  ).toBeVisible();
  await page.screenshot({ path: resolve(output, "simple-mobile-empty.png") });
  await search.fill("BTC/USDT");
  await expect(
    page.getByRole("alert").filter({ hasText: "Введите тикер" }),
  ).toBeVisible();
  await search.fill("");
  await search.blur();
  check("empty search and invalid ticker states");
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
  check("existing scanner, backtest, replay and account render");
  const noAuth = await browser.newContext();
  expect(
    (await noAuth.request.get(`${base}/api/arbitrage/opportunities`)).status(),
  ).toBe(401);
  await noAuth.close();
  const csrf = await context.request.put(`${base}/api/arbitrage/preferences`, {
    headers: { Origin: "https://evil.test" },
    data: { settings: defaults, favorites: [] },
  });
  expect(csrf.status()).toBe(403);
  check("gateway authentication and cross-site write protection");
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
