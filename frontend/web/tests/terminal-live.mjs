// Run against the deployed server. Candles, derivatives and tickers are real;
// fixtures below contain only workspace preferences, never market data.
import { chromium, expect } from "@playwright/test";
import { readFileSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { resolve } from "node:path";
import { registerHooks } from "node:module";
registerHooks({
  resolve(specifier, context, next) {
    try {
      return next(specifier, context);
    } catch (e) {
      if (
        e.code === "ERR_MODULE_NOT_FOUND" &&
        specifier.startsWith(".") &&
        !/\.[a-z]+$/i.test(specifier)
      )
        return next(specifier + ".ts", context);
      throw e;
    }
  },
});
const { normalizeWorkspace } = await import(
  "../src/pages/public/charts/workspace.ts"
);
const base = process.env.CHARTS_E2E_BASE ?? "http://192.168.0.130";
const output = resolve(
  process.env.CHARTS_E2E_OUTPUT ?? "../../docs/charts/terminal",
);
mkdirSync(output, { recursive: true });
rmSync(resolve(output, "failure.json"), { force: true });
const access = process.env.CHARTS_E2E_ACCESS_FILE
  ? JSON.parse(readFileSync(process.env.CHARTS_E2E_ACCESS_FILE, "utf8"))
  : {
      email: process.env.CHARTS_E2E_EMAIL,
      password: process.env.CHARTS_E2E_PASSWORD,
    };
if (!access.password) throw new Error("Server test credentials are required");
const browser = await chromium.launch({
  headless: true,
  ...(process.env.CHARTS_BROWSER_EXECUTABLE
    ? { executablePath: process.env.CHARTS_BROWSER_EXECUTABLE }
    : {}),
});
const context = await browser.newContext({
  viewport: { width: 1440, height: 900 },
  reducedMotion: "reduce",
  acceptDownloads: true,
});
await context.addInitScript(() =>
  localStorage.setItem("cookie_consent", "essential"),
);
const page = await context.newPage(),
  errors = [],
  checks = [],
  sources = new Set();
page.on("pageerror", (e) => {
  errors.push(e.stack ?? e.message);
  console.log("BROWSER_ERROR", e.stack ?? e.message);
});
page.on("response", (r) => {
  if (
    r.ok() &&
    /api\.bybit\.com|fapi\.binance\.com|api\.binance\.com/.test(r.url())
  )
    sources.add(new URL(r.url()).hostname);
});
const check = (name) => {
  checks.push(name);
  console.log("PASS", name);
};
let originalConfig, originalTerminal, owner;
const selectLayout = async (count) => {
  await page
    .getByRole("button", { name: "Количество графиков", exact: true })
    .click();
  await page
    .getByRole("menuitemradio", { name: new RegExp(`^${count} `) })
    .click();
};
const surface = () =>
  page.locator('[data-window-id="window-1"] [data-chart-surface]');
const workspace = () =>
  page.evaluate(
    (owner) =>
      JSON.parse(localStorage.getItem(`shortlong:terminal:v3:${owner}`))
        .workspace,
    owner,
  );
const waitCandles = async () =>
  expect
    .poll(
      async () => Number(await surface().getAttribute("data-visible-count")),
      { timeout: 60000 },
    )
    .toBeGreaterThan(100);
try {
  await page.goto(`${base}/login`);
  await expect(page.getByLabel("Email", { exact: true })).toBeVisible();
  await expect(page.getByLabel("Пароль", { exact: true })).toBeVisible();
  await page
    .getByLabel("Email", { exact: true })
    .fill(access.email ?? "admin@local.dev");
  await page.getByLabel("Пароль", { exact: true }).fill(access.password);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page.waitForURL((url) => !url.pathname.includes("login"), {
    timeout: 30000,
  });
  const me = await context.request.get(`${base}/api/auth/me`);
  expect(me.status()).toBe(200);
  const payload = await me.json();
  owner = (payload.user ?? payload).id;
  const oldConfig = await context.request.get(`${base}/api/chart/config`);
  expect(oldConfig.status()).toBe(200);
  originalConfig = (await oldConfig.json()).config;
  const oldTerminal = await context.request.get(`${base}/api/chart/terminal`);
  expect(oldTerminal.status()).toBe(200);
  originalTerminal = await oldTerminal.json();
  check("ordinary production authentication and protected workspace API");
  const initial = normalizeWorkspace(null);
  initial.windows[0].exchange = "binance";
  await page.goto(`${base}/charts`);
  await page.evaluate(
    ({ owner, initial, revision }) =>
      localStorage.setItem(
        `shortlong:terminal:v3:${owner}`,
        JSON.stringify({
          workspace: initial,
          saved: "test-preferences",
          revision,
        }),
      ),
    { owner, initial, revision: originalTerminal.revision },
  );
  await page.reload();
  await waitCandles();
  await expect(
    page.getByRole("button", { name: "Настроить rsi", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Настроить rsi", exact: true })
    .click();
  await page.getByLabel("Период rsi", { exact: true }).fill("21");
  await page.getByRole("button", { name: "Применить", exact: true }).click();
  await expect
    .poll(async () => (await workspace()).windows[0].studySettings.rsi.period)
    .toBe(21);
  await page
    .getByRole("button", { name: "Дублировать ema", exact: true })
    .click();
  await expect
    .poll(async () => (await workspace()).windows[0].instances.length)
    .toBe(2);
  await page
    .getByRole("button", { name: "Видимость rsi", exact: true })
    .click();
  await expect
    .poll(async () => (await workspace()).windows[0].studySettings.rsi.hidden)
    .toBe(true);
  await page
    .getByRole("button", { name: "Видимость rsi", exact: true })
    .click();
  await page.getByRole("button", { name: "Поднять rsi", exact: true }).click();
  await expect
    .poll(async () => (await workspace()).windows[0].panels.order[0])
    .toBe("rsi");
  await page
    .getByRole("button", { name: "Развернуть rsi", exact: true })
    .click();
  await expect
    .poll(async () => (await workspace()).windows[0].panels.maximized)
    .toBe("rsi");
  await page
    .getByRole("button", { name: "Развернуть rsi", exact: true })
    .click();
  check(
    "independent indicator instances, inputs, hide/show, pane order and maximize",
  );
  const resizeHandle = surface()
    .locator('div[style*="cursor: row-resize"][style*="position: absolute"]')
    .first();
  const handle = await resizeHandle.boundingBox();
  const paneTop = (await page.locator('[data-pane-key="rsi"]').boundingBox()).y;
  await page.mouse.move(
    handle.x + handle.width / 2,
    handle.y + handle.height / 2,
  );
  await page.mouse.down();
  await page.mouse.move(
    handle.x + handle.width / 2,
    handle.y + handle.height / 2 + 48,
    { steps: 12 },
  );
  await page.mouse.up();
  await expect
    .poll(
      async () =>
        (await page.locator('[data-pane-key="rsi"]').boundingBox()).y - paneTop,
    )
    .toBeGreaterThan(25);
  await expect
    .poll(async () => (await workspace()).windows[0].panels.heights.price ?? 0)
    .toBeGreaterThan(0);
  check("pane separator drag changes geometry and persists its height");
  await page
    .getByRole("button", { name: "Линии и каналы", exact: true })
    .click();
  await page.getByRole("menuitem", { name: /^Линия тренда/ }).click();
  const bounds = await surface().boundingBox();
  await page.mouse.click(bounds.x + 150, bounds.y + 180);
  await page.mouse.click(bounds.x + 370, bounds.y + 260);
  await expect(surface()).toHaveAttribute("data-drawing-count", "1");
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Отменить", exact: true }).click();
  await expect(surface()).toHaveAttribute("data-drawing-count", "0");
  await page.getByRole("button", { name: "Повторить", exact: true }).click();
  await expect(surface()).toHaveAttribute("data-drawing-count", "1");
  const canvas = await page
    .locator('[data-window-id="window-1"] .ct-chart-host canvas')
    .first()
    .elementHandle();
  await page
    .getByRole("button", { name: "Позиции и измерения", exact: true })
    .click();
  await page.getByRole("menuitem", { name: /^Длинная позиция/ }).click();
  await page.mouse.click(bounds.x + 450, bounds.y + 180);
  await page.mouse.click(bounds.x + 550, bounds.y + 240);
  await page.mouse.click(bounds.x + 620, bounds.y + 110);
  await expect(surface()).toHaveAttribute("data-drawing-count", "2");
  await expect(surface().locator('[data-drawing-type="long"]')).toBeVisible();
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Отменить", exact: true }).click();
  await expect(surface()).toHaveAttribute("data-drawing-count", "1");
  await page.mouse.move(bounds.x + 400, bounds.y + 160);
  await page.mouse.wheel(0, -250);
  await page.mouse.down();
  await page.mouse.move(bounds.x + 650, bounds.y + 160, { steps: 12 });
  await page.mouse.up();
  await expect
    .poll(async () => (await workspace()).windows[0].follow)
    .toBe(false);
  await page.getByRole("button", { name: "К рынку", exact: true }).click();
  await expect
    .poll(async () => (await workspace()).windows[0].follow)
    .toBe(true);
  check("long-position drawing, undo, cursor zoom and return to live market");
  await page
    .getByRole("button", { name: "Таймфрейм 15м", exact: true })
    .click();
  await waitCandles();
  await expect(surface()).toHaveAttribute("data-drawing-count", "1");
  expect(await canvas.evaluate((node) => node.isConnected)).toBe(true);
  check(
    "drawing undo/redo and timeframe change retain drawings and chart instance",
  );
  await page.getByRole("button", { name: "Таймфрейм 1ч", exact: true }).click();
  await waitCandles();
  await page.getByRole("button", { name: /^Индикаторы/ }).click();
  await page.getByRole("switch", { name: /^MACD/ }).click();
  await page.keyboard.press("Escape");
  await page
    .getByRole("button", { name: "Настроить macd", exact: true })
    .click();
  await page.getByLabel("Быстрый период", { exact: true }).fill("8");
  await page.getByLabel("Медленный период", { exact: true }).fill("21");
  await page.getByLabel("Сигнальный период", { exact: true }).fill("5");
  await page.getByRole("button", { name: "Применить", exact: true }).click();
  await expect
    .poll(
      async () =>
        (await workspace()).windows[0].studySettings.macd.parameters.fast,
    )
    .toBe(8);
  check("MACD inputs and component inspector");
  await page.getByRole("button", { name: /^Индикаторы/ }).click();
  for (const name of [
    /^Открытый интерес · базовый актив/,
    /^Funding · фактическая ставка/,
    /^CVD · накопленная/,
  ])
    await page.getByRole("switch", { name }).click();
  await page.keyboard.press("Escape");
  for (const key of ["oi", "funding", "cvd"])
    await expect(
      page.locator(`[data-pane-key="external:${key}"] .ct-pane-values`),
    ).not.toHaveText("", { timeout: 60000 });
  await page
    .getByRole("button", { name: "Поднять external:cvd", exact: true })
    .click();
  check("BTC with real Binance OI, paid Funding and candle-based CVD panels");
  await page.getByRole("button", { name: "Сравнить", exact: true }).click();
  await page.getByLabel("Поиск инструмента", { exact: true }).fill("ETHUSDT");
  await page
    .locator(".ct-instrument-list button")
    .filter({ hasText: "ETHUSDT" })
    .first()
    .click();
  await expect
    .poll(async () => (await workspace()).windows[0].comparisons.length)
    .toBe(1);
  await expect(
    page.getByRole("button", {
      name: "Настроить сравнение ETHUSDT",
      exact: true,
    }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Настроить сравнение ETHUSDT", exact: true })
    .click();
  await page.locator(".ct-comparison-settings select").selectOption("price");
  await expect
    .poll(async () => (await workspace()).windows[0].comparisons[0].mode)
    .toBe("price");
  await page
    .getByRole("button", { name: "Удалить сравнение ETHUSDT", exact: true })
    .click();
  await expect
    .poll(async () => (await workspace()).windows[0].comparisons.length)
    .toBe(0);
  await page.getByRole("button", { name: "Лог", exact: true }).click();
  await expect.poll(async () => (await workspace()).windows[0].log).toBe(true);
  await page.getByRole("button", { name: "%", exact: true }).click();
  await expect
    .poll(async () => (await workspace()).windows[0].percent)
    .toBe(true);
  expect((await workspace()).windows[0].log).toBe(false);
  await page.getByRole("button", { name: "%", exact: true }).click();
  check(
    "comparison overlay, independent scale, settings/removal and Log/% modes",
  );
  await page
    .getByRole("button", { name: "Настройки графика", exact: true })
    .click();
  await page
    .getByRole("button", {
      name: "Источники и ограничения данных",
      exact: true,
    })
    .click();
  const capabilities = await context.request.get(
    `${base}/api/chart/orderflow/capabilities`,
  );
  expect(capabilities.status()).toBe(200);
  const layers = await capabilities.json();
  expect(layers.layers.length).toBe(4);
  await page.keyboard.press("Escape");
  check(
    "provider capabilities expose missing sources without synthetic layers",
  );
  await selectLayout(4);
  await expect(page.locator("[data-chart-surface]")).toHaveCount(4);
  await expect
    .poll(async () =>
      page.evaluate(
        () => document.documentElement.scrollHeight <= innerHeight + 2,
      ),
    )
    .toBe(true);
  await page.locator('[data-window-id="window-2"] .ct-window-heading').click();
  await page.getByRole("button", { name: "Таймфрейм 4ч", exact: true }).click();
  await expect
    .poll(async () => (await workspace()).windows[1].interval)
    .toBe("4h");
  expect((await workspace()).windows[0].interval).toBe("1h");
  await page
    .getByRole("button", { name: "Синхронизация графиков", exact: true })
    .click();
  await page
    .locator(".ct-sync-menu")
    .getByLabel("Таймфрейм", { exact: true })
    .check();
  await page
    .getByRole("button", { name: "Синхронизация графиков", exact: true })
    .click();
  await page.getByRole("button", { name: "Таймфрейм 5м", exact: true }).click();
  await expect
    .poll(async () =>
      (await workspace()).windows.slice(0, 4).every((w) => w.interval === "5m"),
    )
    .toBe(true);
  await selectLayout(9);
  await expect(page.locator("[data-chart-surface]")).toHaveCount(9);
  await expect
    .poll(async () =>
      page.evaluate(
        () => document.documentElement.scrollHeight <= innerHeight + 2,
      ),
    )
    .toBe(true);
  check(
    "layouts 1–9 fit desktop viewport; independent windows and optional timeframe sync",
  );
  await page
    .getByRole("button", { name: "Развернуть окно 1", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Вернуть раскладку", exact: true })
    .click();
  await selectLayout(1);
  await page.getByRole("button", { name: "Таймфрейм 1ч", exact: true }).click();
  await waitCandles();
  for (const [label, file] of [
    ["Светлая тема", "light"],
    ["Тёмная тема", "dark"],
    ["Фирменная зелёная тема", "green"],
  ]) {
    await page.getByRole("button", { name: label, exact: true }).click();
    await page.screenshot({ path: resolve(output, `desktop-${file}.png`) });
    expect(await canvas.evaluate((node) => node.isConnected)).toBe(true);
  }
  check("Light/Dark/Green preserve chart instance and viewport geometry");
  const separator = page.getByRole("separator", {
    name: "Ширина списка рынка",
    exact: true,
  });
  await separator.focus();
  await page.keyboard.press("ArrowLeft");
  await expect.poll(async () => (await workspace()).sidebarWidth).toBe(296);
  await page
    .getByRole("button", { name: "Рабочие пространства", exact: true })
    .click();
  await page.getByLabel("Название", { exact: true }).fill("QA terminal");
  await page
    .getByRole("button", { name: "Сохранить набор", exact: true })
    .click();
  await expect
    .poll(async () => !!(await workspace()).presets["QA terminal"]?.drawings)
    .toBe(true);
  await page.getByRole("button", { name: "Закрыть", exact: true }).click();
  await page
    .getByRole("button", {
      name: "Сохранить рабочее пространство",
      exact: true,
    })
    .click();
  await expect(page.locator(".ct-save-status")).toHaveText(
    "Сохранено в аккаунте",
    { timeout: 30000 },
  );
  const saved = await context.request.get(`${base}/api/chart/terminal`);
  expect(saved.status()).toBe(200);
  const snapshot = await saved.json();
  expect(snapshot.workspace.windows[0].studySettings.rsi.period).toBe(21);
  expect(snapshot.workspace.sidebarWidth).toBe(296);
  expect(
    Object.values(snapshot.drawing_sets).some((list) =>
      list.some((d) => d.type === "trend"),
    ),
  ).toBe(true);
  const stale = await context.request.put(`${base}/api/chart/terminal`, {
    data: {
      workspace: snapshot.workspace,
      revision: originalTerminal.revision,
      drawing_sets: snapshot.drawing_sets,
    },
  });
  expect(stale.status()).toBe(409);
  await page.reload();
  await waitCandles();
  await expect(surface()).toHaveAttribute("data-drawing-count", "1");
  expect((await workspace()).windows[0].studySettings.rsi.period).toBe(21);
  expect((await workspace()).presets["QA terminal"].windows[0].interval).toBe(
    "1h",
  );
  check(
    "atomic server save, drawings, full reload restoration and stale-save rejection",
  );
  await context.setOffline(true);
  await page.waitForTimeout(2000);
  await expect(surface()).toHaveAttribute("data-drawing-count", "1");
  await context.setOffline(false);
  await waitCandles();
  check(
    "network interruption keeps history and drawings; recovery remains usable",
  );
  await page.setViewportSize({ width: 1920, height: 1080 });
  await page.screenshot({ path: resolve(output, "desktop-1920.png") });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollHeight <= innerHeight + 2,
    ),
  ).toBe(true);
  await page.setViewportSize({ width: 1584, height: 992 });
  await page.screenshot({ path: resolve(output, "concept-size.png") });
  await page.setViewportSize({ width: 1024, height: 768 });
  await page.screenshot({ path: resolve(output, "tablet.png") });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: resolve(output, "mobile.png") });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page
    .getByRole("button", { name: "Показать список рынка", exact: true })
    .click();
  await expect(page.getByLabel("Поиск в списке инструментов")).toBeVisible();
  await page
    .getByRole("button", { name: "Показать список рынка", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Выбрать дату графика", exact: true })
    .click();
  await expect(
    page.getByLabel("Выбрать дату UTC", { exact: true }),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  check(
    "1920 desktop, native concept size, tablet and mobile controls have no horizontal overflow",
  );
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(`${base}/account?demo=1`);
  await expect(page.locator("canvas").first()).toBeVisible({ timeout: 30000 });
  check("analytics demo charts still render through shared system");
  await page.goto(`${base}/replay`);
  await expect(page.getByText("Бэктест", { exact: true }).first()).toBeVisible({
    timeout: 30000,
  });
  check("replay route remains available");
  expect(errors).toEqual([]);
  expect(sources.size).toBeGreaterThan(0);
} catch (e) {
  writeFileSync(
    resolve(output, "failure.json"),
    JSON.stringify(
      { error: e.message, checks, errors, sources: [...sources] },
      null,
      2,
    ),
  );
  throw e;
} finally {
  if (originalConfig && originalTerminal) {
    const currentResponse = await context.request.get(
      `${base}/api/chart/terminal`,
    );
    const current = await currentResponse.json();
    if (originalTerminal.workspace) {
      const restored = await context.request.put(`${base}/api/chart/terminal`, {
        data: {
          workspace: originalTerminal.workspace,
          revision: current.revision,
          drawing_sets:
            originalTerminal.drawing_sets ?? originalConfig.drawing_sets,
        },
      });
      expect(restored.status()).toBe(200);
    } else if (current.workspace) {
      const cleared = await context.request.delete(
        `${base}/api/chart/terminal`,
        { data: { revision: current.revision } },
      );
      expect(cleared.status()).toBe(200);
    }
    const restoredConfig = await context.request.put(
      `${base}/api/chart/config`,
      { data: originalConfig },
    );
    expect(restoredConfig.status()).toBe(200);
  }
  writeFileSync(
    resolve(output, "browser-verification.json"),
    JSON.stringify(
      {
        checks,
        errors,
        sources: [...sources],
        server: base,
        marketData: "real exchange feeds",
        preferencesRestored: !!originalConfig,
      },
      null,
      2,
    ),
  );
  await context.request.post(`${base}/api/auth/logout`).catch(() => {});
  await browser.close();
}
