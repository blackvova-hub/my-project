// Exercises real studies and exchange/provider responses in the deployed UI.
import { chromium, expect } from "@playwright/test";
import { EXTRA_INDICATORS } from "../src/shared/market/indicatorCatalogExtra.ts";
import { DERIVATIVE_PANELS } from "../src/shared/charts/derivativeCatalog.ts";
import { mkdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
const url = process.env.CHARTS_E2E_URL ?? "http://127.0.0.1:5173/charts",
  base = new URL(url).origin,
  output = resolve(
    process.env.CHARTS_E2E_OUTPUT ?? "../../docs/charts/indicators-2026-10-12",
  );
mkdirSync(output, { recursive: true });
const browser = await chromium.launch({ channel: "msedge", headless: true }),
  context = await browser.newContext({
    viewport: { width: 1536, height: 1024 },
    reducedMotion: "reduce",
    ...(process.env.CHARTS_E2E_STORAGE
      ? { storageState: process.env.CHARTS_E2E_STORAGE }
      : {}),
  });
await context.addInitScript(() => {
  localStorage.setItem("cookie_consent", "essential");
  localStorage.setItem("mobile_notice_shown_v1", "1");
});
const page = await context.newPage(),
  result = {
    url,
    checks: [],
    studies: [],
    markets: [],
    sources: [],
    errors: [],
  },
  sources = new Set();
const workers = [];
page.on("worker", (worker) => workers.push(worker.url()));
page.on("pageerror", (e) => result.errors.push(e.message));
page.on("response", (r) => {
  if (
    r.ok() &&
    /api\.(bybit|binance)|fapi\.binance|\/api\/market\//.test(r.url())
  )
    sources.add(r.url());
});
const check = (name) => {
  result.checks.push(name);
  console.log("PASS", name);
};
const escaped = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
const menu = async (query) => {
  await page.getByRole("button", { name: /^Индикаторы/ }).click();
  await page.getByLabel("Поиск индикатора", { exact: true }).fill(query);
  return page.getByRole("dialog", { name: "Каталог индикаторов" });
};
const close = () =>
  page.getByRole("button", { name: "Закрыть каталог", exact: true }).click();
let original;
try {
  if (process.env.CHARTS_E2E_EMAIL && process.env.CHARTS_E2E_PASSWORD) {
    await page.goto(base + "/login");
    await page
      .getByLabel("Email", { exact: true })
      .fill(process.env.CHARTS_E2E_EMAIL);
    await page
      .getByLabel("Пароль", { exact: true })
      .fill(process.env.CHARTS_E2E_PASSWORD);
    await page.getByRole("button", { name: "Войти", exact: true }).click();
    await page.waitForURL((u) => !u.pathname.includes("login"), {
      timeout: 30000,
    });
    check("normal production login");
  }
  if (process.env.CHARTS_E2E_EMAIL || process.env.CHARTS_E2E_STORAGE) {
    const response = await context.request.get(base + "/api/chart/config");
    expect(response.status()).toBe(200);
    original = (await response.json()).config;
  }
  let bufferedRaceFrame = false;
  page.on("websocket", (socket) =>
    socket.on("framereceived", (event) => {
      try {
        const message = JSON.parse(String(event.payload));
        if (
          typeof message.topic === "string" &&
          message.topic.startsWith("kline.")
        )
          bufferedRaceFrame = true;
      } catch {
        /* Exchange control frames are not candles. */
      }
    }),
  );
  // Preserve the actual exchange payload and delay its delivery, never its values.
  await page.route("**/api.bybit.com/v5/market/kline?**", async (route) => {
    const response = await route.fetch();
    await new Promise((resolve) => setTimeout(resolve, 2500));
    await route.fulfill({ response });
  });
  await page.goto(url);
  const surface = page.locator("[data-chart-surface]").first();
  await expect
    .poll(
      async () => Number(await surface.getAttribute("data-visible-count")),
      { timeout: 45000 },
    )
    .toBeGreaterThan(150);
  await page.unroute("**/api.bybit.com/v5/market/kline?**");
  await expect
    .poll(async () => Number(await surface.getAttribute("data-range-from")), {
      timeout: 15000,
    })
    .toBeLessThan(
      Number(await surface.getAttribute("data-visible-count")) - 140,
    );
  result.startup = {
    websocketSeen: bufferedRaceFrame,
    visibleFrom: Number(await surface.getAttribute("data-range-from")),
    candles: Number(await surface.getAttribute("data-visible-count")),
  };
  check(
    "real delayed REST history keeps full initial viewport despite early stream",
  );
  await page.getByRole("button", { name: "Таймфрейм 1ч", exact: true }).click();
  await menu("");
  await page
    .getByRole("button", { name: "Выключить все", exact: true })
    .click();
  await close();
  for (const item of EXTRA_INDICATORS) {
    const dialog = await menu(item.label);
    await dialog
      .getByRole("switch", { name: new RegExp("^" + escaped(item.label)) })
      .click();
    await close();
    if (item.id.includes("volume_profile")) {
      await expect(surface.locator("[data-profile-bin]").first()).toBeVisible();
      result.studies.push({
        id: item.id,
        profileBins: await surface.locator("[data-profile-bin]").count(),
      });
    } else {
      await expect
        .poll(
          async () =>
            JSON.parse(
              (await surface.getAttribute("data-indicator-points")) ?? "{}",
            )[item.id] ?? 0,
          { timeout: 15000 },
        )
        .toBeGreaterThan(0);
      result.studies.push({
        id: item.id,
        points: JSON.parse(await surface.getAttribute("data-indicator-points"))[
          item.id
        ],
      });
    }
    check("render study " + item.id);
    await menu(item.label);
    await page
      .getByRole("button", { name: "Выключить все", exact: true })
      .click();
    await close();
  }
  await menu("ALMA");
  await page.getByRole("button", { name: /Добавить ALMA.*избранное/ }).click();
  await page.getByRole("button", { name: "Избранные", exact: true }).click();
  await expect(page.getByRole("switch", { name: /^ALMA/ })).toBeVisible();
  await page.getByRole("button", { name: "Все", exact: true }).click();
  await page.getByRole("switch", { name: /^ALMA/ }).click();
  await close();
  await page
    .getByRole("button", { name: "Настройки графика", exact: true })
    .click();
  const period = page.getByLabel(/ALMA.*период/);
  await expect(period).toHaveValue("20");
  await period.fill("35");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Закрыть", exact: true })
    .click();
  await expect
    .poll(
      async () =>
        JSON.parse(
          (await surface.getAttribute("data-indicator-points")) ?? "{}",
        ).alma,
      { timeout: 15000 },
    )
    .toBe(Number(await surface.getAttribute("data-visible-count")) - 34);
  check("ALMA settings change calculation period");
  await page.reload();
  await menu("ALMA");
  await expect(
    page.getByRole("button", { name: /Убрать ALMA.*из избранного/ }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: /Убрать ALMA.*из избранного/ })
    .click();
  await page
    .getByRole("button", { name: "Выключить все", exact: true })
    .click();
  await close();
  check("indicator favorites survive reload");
  await menu("CVD");
  await expect(page.getByRole("switch", { name: /^CVD/ })).toBeDisabled();
  await close();
  check("Bybit historical CVD restriction is explicit");
  await page.getByRole("button", { name: "Курсор", exact: true }).click();
  const hide = page.getByRole("button", {
    name: "Видимость разметки",
    exact: true,
  });
  if ((await hide.getAttribute("aria-pressed")) === "false") await hide.click();
  const bounds = await page.locator(".ct-chart-host").first().boundingBox();
  for (
    let attempt = 0;
    attempt < 16 &&
    Number(await surface.getAttribute("data-visible-count")) < 2000;
    attempt++
  ) {
    const y = Math.min(850, bounds.y + bounds.height * 0.35);
    await page.mouse.move(bounds.x + 150, y);
    await page.mouse.down();
    await page.mouse.move(bounds.x + bounds.width - 120, y, { steps: 30 });
    await page.mouse.up();
    await page.waitForTimeout(700);
  }
  await expect
    .poll(
      async () => Number(await surface.getAttribute("data-visible-count")),
      { timeout: 30000 },
    )
    .toBeGreaterThanOrEqual(2000);
  await menu("ALMA");
  await page.getByRole("switch", { name: /^ALMA/ }).click();
  await close();
  await expect
    .poll(() => workers.length, { timeout: 15000 })
    .toBeGreaterThan(0);
  await expect
    .poll(
      async () =>
        JSON.parse(
          (await surface.getAttribute("data-indicator-points")) ?? "{}",
        ).alma ?? 0,
      { timeout: 15000 },
    )
    .toBeGreaterThan(1900);
  result.workers = workers;
  check("real history backfill and background worker calculation");
  await menu("ALMA");
  await page
    .getByRole("button", { name: "Выключить все", exact: true })
    .click();
  await close();
  await page
    .getByRole("combobox", { name: "Биржа", exact: true })
    .selectOption("binance");
  const limited = [];
  for (const item of DERIVATIVE_PANELS) {
    if (
      !original &&
      ["liquidations", "aggregate_liquidations", "fear_greed"].includes(item.id)
    )
      continue;
    await menu(item.label);
    await page
      .getByRole("switch", { name: new RegExp("^" + escaped(item.label)) })
      .click();
    await close();
    await expect
      .poll(
        async () => ({
          points:
            JSON.parse(
              (await surface.getAttribute("data-market-points")) ?? "{}",
            )[item.id] ?? 0,
          error: await surface.locator(".ct-external-errors").count(),
        }),
        { timeout: 45000 },
      )
      .not.toEqual({ points: 0, error: 0 });
    const points =
        JSON.parse((await surface.getAttribute("data-market-points")) ?? "{}")[
          item.id
        ] ?? 0,
      error = (await surface.locator(".ct-external-errors").count())
        ? await surface.locator(".ct-external-errors").textContent()
        : null;
    result.markets.push({ id: item.id, points, error });
    if (error) {
      if (!["liquidations", "aggregate_liquidations"].includes(item.id))
        throw new Error(item.id + ": " + error);
      limited.push(item.id);
      check("explicit partial event history " + item.id);
    } else {
      expect(points).toBeGreaterThan(0);
      check("real market study " + item.id);
    }
    await menu(item.label);
    await page
      .getByRole("button", { name: "Выключить все", exact: true })
      .click();
    await close();
  }
  if (original) {
    await menu("VWMA");
    await page.getByRole("switch", { name: /^VWMA/ }).click();
    await close();
    await menu("Chop Zone");
    await page.getByRole("switch", { name: /^Chop Zone/ }).click();
    await close();
    await page.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(
      page.getByText("Сохранено в аккаунте", { exact: true }).first(),
    ).toBeVisible();
    const saved = (
      await (await context.request.get(base + "/api/chart/config")).json()
    ).config;
    expect(saved.active_indicator_ids).toContain("vwma");
    expect(saved.active_indicator_ids).toContain("chop_zone");
    check("new indicator cloud save and PostgreSQL round trip");
  }
  await menu("Spread");
  await page.getByRole("button", { name: /^Ratio \/ Spread/ }).click();
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
  for (const mode of ["ratio", "spread"]) {
    await page
      .getByLabel("Режим сравнения", { exact: true })
      .selectOption(mode);
    await expect(page.locator(".ct-comparison")).toHaveAttribute(
      "data-comparison-mode",
      mode,
    );
    await expect
      .poll(
        async () =>
          Number(
            await page
              .locator(".ct-comparison")
              .getAttribute("data-comparison-value"),
          ),
        { timeout: 15000 },
      )
      .toBeGreaterThan(0);
    check("real price comparison " + mode);
  }
  await page
    .getByRole("button", { name: "Закрыть сравнение", exact: true })
    .click();
  await menu("Visible Range");
  await page.getByRole("switch", { name: /^Volume Profile Visible/ }).click();
  await close();
  await expect(surface.locator("[data-profile=visible]")).toBeVisible();
  await expect.poll(async()=>JSON.parse(await surface.getAttribute("data-indicator-points")??"{}").visible_volume_profile,{timeout:15000}).toBe(0);
  for (const [theme, name] of [
    ["light", "Светлая тема"],
    ["dark", "Тёмная тема"],
    ["green", "Фирменная зелёная тема"],
  ]) {
    await page.getByRole("button", { name, exact: true }).click();
    await menu("");
    await page.screenshot({ path: resolve(output, `catalog-${theme}.png`) });
    await close();
  }
  check("catalog Light Dark Green");
  await page.setViewportSize({ width: 390, height: 844 });
  await menu("RSI");
  await page.screenshot({
    path: resolve(output, "catalog-mobile.png"),
    fullPage: true,
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
  ).toBe(false);
  await close();
  check("mobile catalog without overflow");
  expect(result.errors).toEqual([]);
  check("no browser runtime errors");
} finally {
  if (original) {
    const response = await context.request.put(base + "/api/chart/config", {
      data: original,
      headers: { Origin: base },
    });
    expect(response.status()).toBe(200);
  }
  result.sources = [...sources];
  writeFileSync(
    resolve(output, "browser-verification.json"),
    JSON.stringify(result, null, 2),
  );
  await browser.close();
}
