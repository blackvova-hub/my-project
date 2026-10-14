import { chromium, expect } from "@playwright/test";
import { readFileSync, mkdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

const base = process.env.LIVE_URL || "http://192.168.0.130";
const credentialFile = process.env.NEWS_MACRO_TEST_ACCESS;
if (!credentialFile)
  throw new Error(
    "Set NEWS_MACRO_TEST_ACCESS to a private JSON file containing email/password.",
  );
const credentials = JSON.parse(readFileSync(credentialFile, "utf8"));
const dir = resolve("../../docs/news-macro");
mkdirSync(dir, { recursive: true });
const browser = await chromium.launch({ channel: "msedge", headless: true });
const context = await browser.newContext({
  viewport: { width: 1536, height: 1024 },
  reducedMotion: "reduce",
});
const page = await context.newPage();
const errors = [],
  checks = [],
  layouts = [];
page.on("pageerror", (e) => errors.push(e.message));
async function check(name, run) {
  await run();
  checks.push(name);
  console.log(`PASS ${name}`);
}
async function navigate(path) {
  await page.goto(base + path);
  await page.locator(".nm-terminal").waitFor();
  await expect(page.locator(".nm-terminal .nm-skeleton")).toHaveCount(0, {
    timeout: 25000,
  });
}
async function closeDrawer() {
  await page
    .getByRole("button", { name: "Закрыть панель", exact: true })
    .click();
}
async function screenshot(name) {
  await page.screenshot({ path: resolve(dir, name + ".png") });
}
try {
  await check("Password login preserves requested terminal page", async () => {
    await page.goto(base + "/login?next=/news");
    await page.locator("input[type=email]").fill(credentials.email);
    await page.locator("input[type=password]").fill(credentials.password);
    await page.getByRole("button", { name: "Войти", exact: true }).click();
    await page.waitForURL((url) => url.pathname === "/news");
    await page.locator(".nm-terminal").waitFor();
    const response = await context.request.get(base + "/api/auth/me");
    expect(response.ok()).toBeTruthy();
    const payload = await response.json();
    expect(payload.user.email).toBe(credentials.email);
    expect(payload.user.plan).toBe("pro");
    expect(Boolean(payload.user.is_admin ?? payload.user.isAdmin)).toBe(false);
    if (
      await page
        .getByRole("button", { name: "Не разрешать", exact: true })
        .count()
    )
      await page
        .getByRole("button", { name: "Не разрешать", exact: true })
        .click();
    await expect(page.locator(".nm-terminal .nm-skeleton")).toHaveCount(0, {
      timeout: 25000,
    });
    await page
      .getByRole("button", { name: "Тёмная тема", exact: true })
      .click();
    await expect(page.locator(".nm-series-preview")).toHaveCount(4);
    await screenshot("dashboard-dark");
  });
  await check(
    "Live news filters, search, reading and persistent bookmarks",
    async () => {
      await navigate("/news/live");
      await page.getByLabel(/^Период/).selectOption("");
      await expect(page.locator(".nm-news-row").first()).toBeVisible();
      const row = page.locator(".nm-news-row").first();
      const headline = await row.locator(".nm-headline").innerText();
      await row
        .getByRole("button", { name: "Сохранить новость", exact: true })
        .click();
      await row.locator(".nm-headline").click();
      await expect(page.getByRole("dialog")).toBeVisible();
      await expect(
        page.getByRole("dialog").locator("a[target=_blank]").first(),
      ).toHaveAttribute("href", /^https:\/\//);
      await closeDrawer();
      await expect(row).toHaveClass(/nm-news-read/);
      await page.getByRole("button", { name: /^Избранное/ }).click();
      await expect(
        page.locator(".nm-headline").filter({ hasText: headline }).first(),
      ).toBeVisible();
      await page.reload();
      await expect(page.locator(".nm-terminal .nm-skeleton")).toHaveCount(0, {
        timeout: 25000,
      });
      await page.getByRole("button", { name: /^Избранное/ }).click();
      await expect(
        page.locator(".nm-headline").filter({ hasText: headline }).first(),
      ).toBeVisible();
      await page.getByRole("button", { name: /^Избранное/ }).click();
      await page.getByLabel(/^Период/).selectOption("");
      await page.getByLabel(/^Источник/).selectOption("federal_reserve");
      await expect(page.locator(".nm-news-row").first()).toBeVisible();
      await expect(
        page
          .locator(".nm-news-time small")
          .filter({ hasText: "Federal Reserve" })
          .first(),
      ).toBeVisible();
      await page.getByLabel("Фильтр по тикеру", { exact: true }).fill("USD");
      await expect(page.locator(".nm-news-row").first()).toBeVisible();
      await expect(
        page
          .locator(".nm-news-row")
          .filter({ hasNot: page.locator(".nm-ticker", { hasText: /^USD$/ }) }),
      ).toHaveCount(0);
      await page.getByLabel("Фильтр по тикеру", { exact: true }).fill("");
      await page.getByLabel(/^Источник/).selectOption("");
      await page
        .getByLabel("Поиск новостей", { exact: true })
        .fill("zzzz-no-such-publication-123456");
      await expect(page.locator(".nm-news-row")).toHaveCount(0);
      await expect(page.locator(".nm-empty")).toBeVisible();
      await page.getByLabel("Поиск новостей", { exact: true }).fill("");
      await expect(page.locator(".nm-news-row").first()).toBeVisible();
      await page
        .getByRole("combobox", { name: /^Важность/ })
        .selectOption("high");
      await expect(page.locator(".nm-news-row").first()).toBeVisible();
      await expect(
        page
          .locator(".nm-news-row")
          .filter({ hasNot: page.locator(".nm-importance-high") }),
      ).toHaveCount(0);
      await page
        .getByRole("combobox", { name: /^Важность/ })
        .selectOption("all");
      await page.getByLabel(/^Порядок/).selectOption("important");
      await expect(
        page.locator(".nm-news-row").first().locator(".nm-importance-high"),
      ).toBeVisible();
      await page.getByLabel(/^Порядок/).selectOption("latest");
      const next = page.getByRole("button", {
        name: "Следующая →",
        exact: true,
      });
      if (await next.isEnabled()) {
        await next.click();
        await expect(page.getByText(/Страница 2 ·/)).toBeVisible();
        await expect(page.locator(".nm-news-row").first()).toBeVisible();
        await page
          .getByRole("button", { name: "← Предыдущая", exact: true })
          .click();
        await expect(page.getByText(/Страница 1 ·/)).toBeVisible();
      }
      await screenshot("live-dark");
    },
  );
  await check(
    "Calendar periods, timezone, filters, official history and ICS export",
    async () => {
      await navigate("/news/calendar");
      await page.getByRole("button", { name: "Месяц", exact: true }).click();
      await page.getByLabel("Опорная дата календаря").fill("2026-10-14");
      await expect(page.locator(".nm-event-title").first()).toBeVisible();
      await page.getByLabel(/^Часовой пояс/).selectOption("UTC");
      const utcTime = await page
        .locator(".nm-calendar-table tbody")
        .innerText();
      await page.getByLabel(/^Часовой пояс/).selectOption("America/New_York");
      await expect(page.locator(".nm-calendar-table tbody")).not.toHaveText(
        utcTime,
      );
      await page
        .getByRole("combobox", { name: /^Важность/ })
        .selectOption("high");
      expect(await page.locator(".nm-event-title").count()).toBeGreaterThan(0);
      await page.getByRole("combobox", { name: /^Важность/ }).selectOption("");
      await screenshot("calendar-dark");
      await page
        .locator(".nm-event-title")
        .filter({ hasText: /Consumer Price Index/ })
        .first()
        .click();
      await expect(page.getByRole("dialog")).toBeVisible();
      await expect(
        page.getByRole("dialog").locator(".nm-values strong"),
      ).toHaveText(["—", "—", "—"]);
      const download = page.waitForEvent("download");
      await page
        .getByRole("button", {
          name: "Добавить в календарь (.ics)",
          exact: true,
        })
        .click();
      const file = await download;
      const text = readFileSync(await file.path(), "utf8");
      expect(text).toContain("BEGIN:VCALENDAR");
      expect(text).toContain("DTSTART:");
      expect(text).not.toContain("NaN");
      await page
        .getByRole("button", {
          name: "Загрузить историческую реакцию",
          exact: true,
        })
        .click();
      await expect(
        page.getByRole("dialog").locator(".nm-skeleton"),
      ).toHaveCount(0, { timeout: 25000 });
      const history = page
        .getByRole("dialog")
        .locator("section")
        .filter({ hasText: "Предыдущие выпуски и рынок" });
      expect(await history.innerText()).toMatch(
        /Плановое время|Нет сопоставимых/,
      );
      await closeDrawer();
      for (const period of ["Сегодня", "Завтра", "Неделя", "Месяц"]) {
        await page.getByRole("button", { name: period, exact: true }).click();
        await expect(
          page.getByRole("button", { name: period, exact: true }),
        ).toHaveAttribute("aria-pressed", "true");
      }
    },
  );
  await check(
    "Real macro chart, YoY, comparison and raw CSV export",
    async () => {
      await navigate("/news/macro?id=CPIAUCSL");
      await expect(
        page.locator(".nm-macro-chart-main svg").first(),
      ).toBeVisible();
      await page.getByRole("button", { name: "5 лет", exact: true }).click();
      await page
        .getByRole("button", { name: "Инфляция г/г", exact: true })
        .click();
      await expect(
        page
          .locator(".nm-note")
          .filter({ hasText: "Годовое изменение индекса" }),
      ).toBeVisible();
      const download = page.waitForEvent("download");
      await page
        .getByRole("button", { name: "Экспорт CSV", exact: true })
        .click();
      const file = await download;
      const rows = readFileSync(await file.path(), "utf8")
        .trim()
        .split(/\r?\n/);
      expect(rows[0]).toBe("date,value");
      expect(rows.length).toBeGreaterThan(40);
      await page.getByLabel(/^Сравнить с/).selectOption("PCEPILFE");
      await expect(page.locator(".nm-chart-legend span")).toHaveCount(2);
      await expect(
        page.locator(".nm-macro-chart-main svg").first(),
      ).toBeVisible();
      await screenshot("macro-dark");
      await page.getByLabel(/^Сравнить с/).selectOption("");
      await page.getByLabel(/^Страна/).selectOption("EA");
      await expect(page.getByLabel(/^Показатель/)).toContainText("ECB");
    },
  );
  await check(
    "Three themes and responsive routes preserve page bounds",
    async () => {
      const names = {
        dark: "Тёмная тема",
        light: "Светлая тема",
        green: "Фирменная зелёная тема",
      };
      for (const theme of ["dark", "light", "green"]) {
        await navigate("/news");
        await page
          .getByRole("button", { name: names[theme], exact: true })
          .click();
        await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
        layouts.push({
          theme,
          desktop: await page.locator(".nm-terminal").boundingBox(),
        });
        await screenshot("dashboard-" + theme);
      }
      await page.getByRole("button", { name: names.dark, exact: true }).click();
      for (const width of [768, 390]) {
        await page.setViewportSize({ width, height: 844 });
        for (const route of ["", "/live", "/calendar", "/macro"]) {
          await navigate("/news" + route);
          const overflow = await page.evaluate(
            () => document.documentElement.scrollWidth > innerWidth + 1,
          );
          expect(overflow, `${width}: ${route} overflow`).toBe(false);
          await screenshot((route.slice(1) || "dashboard") + "-" + width);
        }
      }
      expect(
        Math.max(...layouts.map((l) => l.desktop.width)) -
          Math.min(...layouts.map((l) => l.desktop.width)),
      ).toBeLessThan(1);
    },
  );
  await check(
    "Unavailable API shows retry and recovers without fabricated data",
    async () => {
      await page.setViewportSize({ width: 1536, height: 1024 });
      await page.route("**/api/macro/overview", (route) => route.abort());
      await page.goto(base + "/news/macro");
      await expect(
        page.getByText("Каталог макроданных не загрузился", { exact: true }),
      ).toBeVisible({ timeout: 25000 });
      await page.unroute("**/api/macro/overview");
      await page
        .getByRole("button", { name: "Повторить", exact: true })
        .click();
      await expect(
        page.locator(".nm-macro-chart-main svg").first(),
      ).toBeVisible({ timeout: 25000 });
    },
  );
  await check(
    "Market widget opens existing spot chart with the correct symbol",
    async () => {
      await navigate("/news");
      await page.locator(".nm-market-item").filter({ hasText: /^ETH/ }).click();
      await page.waitForURL((url) => url.pathname === "/charts");
      await expect(page.getByLabel("Рынок", { exact: true })).toHaveValue(
        "spot",
      );
      await expect(page.getByLabel("Биржа", { exact: true })).toHaveValue(
        "bybit",
      );
      await expect(page.locator(".ct-instrument-button")).toContainText(
        "ETHUSDT",
      );
    },
  );
  expect(errors).toEqual([]);
  writeFileSync(
    resolve(dir, "browser-verification.json"),
    JSON.stringify(
      {
        checkedAt: new Date().toISOString(),
        base,
        checks,
        layouts,
        pageErrors: errors,
      },
      null,
      2,
    ),
  );
} finally {
  await browser.close();
}
