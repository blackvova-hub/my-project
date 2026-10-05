import { chromium, expect } from "@playwright/test";
import { mkdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

const base = process.env.LIVE_URL || "http://192.168.0.124";
const dir = resolve("../../docs/analytics/ru");
mkdirSync(dir, { recursive: true });
const browser = await chromium.launch({
  headless: true,
  executablePath:
    process.env.LIVE_BROWSER_EXECUTABLE ||
    "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe",
});
const page = await browser.newPage({
  viewport: { width: 1536, height: 1100 },
  reducedMotion: "reduce",
});
const errors = [];
page.on("pageerror", (e) => errors.push(e.message));
const checks = [];
const copyAudit = [];
async function auditRussian() {
  const values = await page
    .locator(".an-root")
    .first()
    .evaluate((root) => {
      const texts = [root.innerText];
      for (const node of root.querySelectorAll(
        "[title],[aria-label],[placeholder]",
      )) {
        for (const name of ["title", "aria-label", "placeholder"])
          if (node.getAttribute(name)) texts.push(node.getAttribute(name));
      }
      return texts.join("\n");
    });
  const allowed = new Set([
    "Short",
    "Long",
    "Bybit",
    "Binance",
    "BYBIT",
    "BINANCE",
    "BYB",
    "I",
    "T",
    "Server",
    "Admin",
    "SE",
    "SA",
    "API",
    "CSV",
    "UTC",
    "USD",
    "USD-M",
    "USDT",
    "USDC",
    "BTC",
    "ETH",
    "SOL",
    "XRP",
    "DOGE",
  ]);
  const unexpected = [
    ...new Set(values.match(/[A-Za-z][A-Za-z-]*/g) || []),
  ].filter((word) => !allowed.has(word) && !/^[A-Z]+USDT$/.test(word));
  expect(unexpected, "Untranslated interface words").toEqual([]);
}
async function auditCopy(title) {
  const subtitles = {
    Обзор: "Весь счёт, каждая сделка и понятный итог.",
    Результаты: "Доходность и качество ваших торговых результатов.",
    Сделки: "От позиции до каждого исполнения ордера.",
    Эффективность: "Узнайте, какие сделки приносят вам прибыль.",
    Расходы: "Все комиссии и расходы на торговлю.",
    Риски: "Оцените нагрузку на капитал и открытые позиции.",
    Закономерности: "Закономерности вашей торговли с подтверждением в данных.",
    Подключения: "Ваши биржи и счета в одном месте.",
  };
  const navigation = (
    await page.locator(".an-nav-link span").allTextContents()
  ).map((s) => s.trim());
  expect(navigation).toEqual([
    "Обзор",
    "Результаты",
    "Сделки",
    "Эффективность",
    "Расходы",
    "Риски",
    "Закономерности",
    "Подключения",
  ]);
  expect(await page.locator(".an-page-header p").textContent()).toBe(
    subtitles[title],
  );
  const typography = await page.locator(".an-page-header h1").evaluate((e) => ({
    size: getComputedStyle(e).fontSize,
    weight: getComputedStyle(e).fontWeight,
  }));
  copyAudit.push({
    page: title,
    navigation,
    subtitle: subtitles[title],
    headings: await page.locator("h2").allTextContents(),
    typography,
    diff: [],
  });
  await auditRussian();
}
async function section(title) {
  await page.getByRole("link", { name: title, exact: true }).click();
  await expect(
    page.getByRole("heading", { name: title, exact: true }),
  ).toBeVisible();
}
async function noOverflow() {
  const sizes = await page.evaluate(() => ({
    scroll: document.documentElement.scrollWidth,
    width: innerWidth,
  }));
  expect(sizes.scroll).toBeLessThanOrEqual(sizes.width + 1);
}
try {
  await page.goto(base + "/account");
  await page
    .locator("input[type=email]")
    .fill(process.env.LIVE_ADMIN_EMAIL || "admin@local.dev");
  await page
    .locator("input[type=password]")
    .fill(process.env.LIVE_ADMIN_PASSWORD);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page.waitForURL("**/account", { timeout: 30000 });
  if (process.env.ANALYTICS_QA_MODE === "reference") {
    await page
      .getByRole("button", { name: "Не разрешать", exact: true })
      .click({ timeout: 4000 })
      .catch(() => {});
    await page.screenshot({
      path: resolve(dir, "site-reference-account.png"),
      fullPage: true,
    });
    await page.goto(base + "/");
    await page
      .getByRole("heading", { name: "Лист ожидания", exact: true })
      .waitFor({ timeout: 30000 });
    await page.screenshot({
      path: resolve(dir, "site-reference-home.png"),
      fullPage: false,
    });
    const styles = await page.evaluate(() => ({
      body: getComputedStyle(document.body).fontFamily,
      header: [...document.querySelectorAll("header")].map((e) => ({
        background: getComputedStyle(e).background,
        font: getComputedStyle(e).fontFamily,
        height: e.getBoundingClientRect().height,
      })),
      surfaces: [...document.querySelectorAll("[class*=rounded]")]
        .slice(0, 25)
        .map((e) => ({
          text: e.textContent.slice(0, 45),
          background: getComputedStyle(e).background,
          border: getComputedStyle(e).borderColor,
          radius: getComputedStyle(e).borderRadius,
        })),
    }));
    writeFileSync(
      resolve(dir, "site-reference-styles.json"),
      JSON.stringify(styles, null, 2),
    );
    console.log(JSON.stringify({ reference: "captured", errors }));
  } else {
    await expect(
      page.getByRole("heading", { name: "Обзор", exact: true }),
    ).toBeVisible();
    await expect(page.locator(".an-loading")).toHaveCount(0);
    await page.screenshot({
      path: resolve(dir, "live-empty.png"),
      fullPage: true,
    });
    await page.goto(base + "/account?demo=1");
    await expect(
      page.getByRole("heading", { name: "Динамика портфеля" }),
    ).toBeVisible();
    await page.screenshot({
      path: resolve(dir, "render-overview.png"),
      fullPage: true,
    });
    checks.push("Real account is isolated from explicitly selected demonstration data");
    await auditCopy("Обзор");
    for (const title of [
      "Результаты",
      "Сделки",
      "Эффективность",
      "Расходы",
      "Риски",
      "Закономерности",
    ]) {
      await page.getByRole("link", { name: title, exact: true }).click();
      await expect(
        page.getByRole("heading", { name: title, exact: true }),
      ).toBeVisible();
      await auditCopy(title);
      await page.screenshot({
        path: resolve(dir, `render-${title.toLowerCase()}.png`),
        fullPage: true,
      });
    }
    checks.push("Seven analytics sections");
    const live = await page.request.get(base + "/api/analytics");
    expect(live.status()).toBe(200);
    const liveData = await live.json();
    expect(liveData.connections.every((c) => !c.id.startsWith("demo-"))).toBe(
      true,
    );
    const foreign = await page.request.put(base + "/api/analytics/notes", {
      headers: { Origin: "https://other.invalid" },
      data: { tradeId: "none", tag: "x", strategy: "" },
    });
    expect(foreign.status()).toBe(403);
    const anonymous = await browser.newContext();
    expect(
      (await anonymous.request.get(base + "/api/analytics")).status(),
    ).toBe(401);
    await anonymous.close();
    checks.push(
      "Authenticated dataset, no fabricated live history, CSRF rejection and unauthenticated rejection",
    );
    await section("Обзор");
    await page.getByRole("combobox", { name: "Период", exact: true }).click();
    await expect(page.getByRole("listbox", { name: "Период" })).toBeVisible();
    await page.screenshot({
      path: resolve(dir, "dropdown.png"),
      fullPage: false,
    });
    await page.getByRole("option", { name: "30 дней", exact: true }).click();
    const kpi30 = await page.locator(".an-metrics").first().textContent();
    const period = page.getByRole("combobox", { name: "Период", exact: true });
    await period.focus();
    await page.keyboard.press("ArrowDown");
    await page.keyboard.press("ArrowDown");
    await page.keyboard.press("Enter");
    await expect(period).toContainText("90 дней");
    await period.click();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("listbox")).toHaveCount(0);
    expect(await page.locator(".an-metrics").first().textContent()).not.toBe(
      kpi30,
    );
    const chart = page.locator(".an-chart-canvas").first();
    await chart.hover({ position: { x: 200, y: 80 } });
    await expect(page.locator(".an-chart-tooltip")).toBeVisible();
    await page.getByRole("button", { name: /^Торговые комиссии/ }).click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await page.screenshot({
      path: resolve(dir, "render-ledger.png"),
      fullPage: true,
    });
    await page
      .getByRole("dialog")
      .getByRole("button", { name: /BTCUSDT/ })
      .click();
    await expect(
      page.getByRole("heading", { name: "Сделки", exact: true }),
    ).toBeVisible();
    await page.locator(".an-symbol-button").first().click();
    await expect(page.locator(".an-candles canvas").first()).toBeVisible();
    await page.screenshot({
      path: resolve(dir, "render-trade-detail.png"),
      fullPage: true,
    });
    await page.locator(".an-annotation input").first().fill("Проверено");
    await page.getByRole("button", { name: "Сохранить пометку" }).click();
    await expect(page.getByText("Сохранено", { exact: true })).toBeVisible();
    const downloaded = page.waitForEvent("download");
    await page
      .getByRole("button", { name: "Скачать CSV", exact: true })
      .click();
    const download = await downloaded;
    expect(download.suggestedFilename()).toContain("demo-trades");
    const stream = await download.createReadStream();
    const chunks = [];
    for await (const c of stream) chunks.push(c);
    expect(Buffer.concat(chunks).toString()).toContain("BTCUSDT");
    expect(Buffer.concat(chunks).toString()).toContain("Инструмент");
    expect(Buffer.concat(chunks).toString()).toContain("Лонг");
    checks.push(
      "Period filter, interactive chart, category → symbol → trade → executions, annotation, CSV",
    );
    await section("Результаты");
    await page.locator(".an-calendar-cell:not([disabled])").first().click();
    await expect(
      page.getByRole("heading", { name: "Сделки", exact: true }),
    ).toBeVisible();
    await section("Эффективность");
    await page.locator(".an-heatmap > button:not([disabled])").first().click();
    await expect(
      page.getByRole("heading", { name: "Сделки", exact: true }),
    ).toBeVisible();
    await section("Закономерности");
    await page
      .getByRole("button", { name: "Разобрать закономерность", exact: true })
      .first()
      .click();
    await expect(
      page.getByRole("heading", { name: "Основание расчёта" }),
    ).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(0);
    checks.push(
      "Calendar and heatmap drilldowns, insight evidence dialog and Escape",
    );
    await section("Подключения");
    await auditCopy("Подключения");
    await page.screenshot({
      path: resolve(dir, "render-connections.png"),
      fullPage: true,
    });
    await page
      .getByRole("button", { name: "Подключить биржу", exact: true })
      .click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Проверить и подключить" }),
    ).toBeDisabled();
    await page.getByRole("radio", { name: "Binance", exact: true }).check();
    await expect(page.getByLabel("Тип счёта", { exact: true })).toHaveValue(
      "Фьючерсы USD-M · контракты USDT",
    );
    await page.screenshot({
      path: resolve(dir, "render-connection-form.png"),
      fullPage: true,
    });
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await page.getByRole("button", { name: "Профиль", exact: true }).click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await expect(
      page.getByLabel("Электронная почта", { exact: true }),
    ).toHaveValue(process.env.LIVE_ADMIN_EMAIL || "admin@local.dev");
    await page.keyboard.press("Escape");
    checks.push(
      "Real connection form, exchange switch, read-only consent gate, existing profile",
    );
    await page.goto(base + "/account?demo=1");
    await expect(
      page.getByRole("heading", { name: "Динамика портфеля" }),
    ).toBeVisible();
    const styles = await page.evaluate(() => {
      const root = getComputedStyle(document.querySelector(".an-root"));
      const panel = getComputedStyle(document.querySelector(".an-panel"));
      return {
        background: root.backgroundImage,
        font: root.fontFamily,
        accent: root.getPropertyValue("--an-accent"),
        surface: panel.backgroundColor,
        radius: panel.borderRadius,
        brand: document.querySelector(".an-root > header a").textContent,
      };
    });
    expect(styles.background).toContain("rgb(31, 51, 32)");
    expect(styles.accent.trim()).toBe("#34d399");
    expect(styles.radius).toBe("16px");
    expect(styles.brand).toBe("Short&Long");
    const accountHeader = await page.locator(".an-root > header").evaluate((header) => ({
      height: header.getBoundingClientRect().height,
      position: getComputedStyle(header).position,
      navigation: [...header.querySelectorAll("nav a")].map((link) => link.textContent?.trim()),
    }));
    expect(accountHeader.position).toBe("sticky");
    writeFileSync(
      resolve(dir, "render-styles.json"),
      JSON.stringify(styles, null, 2),
    );
    await page.setViewportSize({ width: 1482, height: 1061 });
    await noOverflow();
    await page.screenshot({
      path: resolve(dir, "render-overview-native.png"),
      fullPage: false,
    });
    await page.setViewportSize({ width: 390, height: 844 });
    for (const title of [
      "Обзор",
      "Результаты",
      "Сделки",
      "Эффективность",
      "Расходы",
      "Риски",
      "Закономерности",
    ]) {
      await section(title);
      await noOverflow();
      await page.screenshot({
        path: resolve(dir, `mobile-${title.toLowerCase()}.png`),
        fullPage: true,
      });
    }
    await section("Подключения");
    await noOverflow();
    await page.screenshot({
      path: resolve(dir, "mobile-connections.png"),
      fullPage: true,
    });
    await page
      .getByRole("button", { name: "Подключить биржу", exact: true })
      .click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await noOverflow();
    await page.screenshot({
      path: resolve(dir, "mobile-connection-form.png"),
      fullPage: true,
    });
    await page.keyboard.press("Escape");
    checks.push(
      "Site palette/font/radius/brand; concept native viewport; all eight sections and form at 390px",
    );
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    await page.goto(base + '/account?demo=1');
    await expect(page.locator('.an-metrics').first()).toBeVisible();
    expect(await page.locator('.an-metrics').first().evaluate(e=>getComputedStyle(e).animationName)).toBe('an-content-enter');
    const mobilePeriod = page.getByRole('combobox',{name:'Период',exact:true});
    await mobilePeriod.click();
    await expect(page.getByRole('listbox',{name:'Период'})).toBeVisible();
    await noOverflow();
    await expect.poll(() => page.getByRole('listbox').evaluate(e => getComputedStyle(e).opacity)).toBe('1');
    await page.screenshot({path:resolve(dir,'mobile-dropdown.png'),fullPage:false});
    await page.keyboard.press('Tab');
    await expect(page.getByRole('listbox')).toHaveCount(0);
    await page.emulateMedia({reducedMotion:'reduce'});
    expect(await page.locator('.an-metrics').first().evaluate(e=>getComputedStyle(e).animationName)).toBe('none');
    checks.push('Custom dropdown selection, keyboard navigation, Escape/Tab, mobile portal, section animation and reduced motion');
    await page
      .locator(".an-root > header")
      .getByText("Меню", { exact: true })
      .click();
    await expect(
      page.getByRole("navigation", { name: "Мобильная навигация" }),
    ).toBeVisible();
    await page
      .getByRole("navigation", { name: "Мобильная навигация" })
      .getByRole("link", { name: "Главная", exact: true })
      .click();
    await page.waitForURL(base + "/");
    await page.setViewportSize({ width: 1536, height: 1100 });
    await page.evaluate(() => {
      localStorage.setItem("cookie_consent", "essential");
      localStorage.setItem("mobile_notice_shown_v1", "1");
    });
    for (const route of [
      "/",
      "/scanners/1",
      "/scanners/2",
      "/scanners/3",
      "/backtest",
      "/replay",
      "/news",
      "/community",
    ]) {
      await page.goto(base + route);
      await expect(page.locator("header[data-app-header]")).toBeVisible();
      await expect(page.locator("main")).toBeVisible();
      await expect(page.locator("main")).not.toBeEmpty();
      expect(new URL(page.url()).pathname).toBe(route);
      expect(await page.locator(".an-root").count()).toBe(0);
      if (route === "/news") {
        const siteHeader = await page.locator("header[data-app-header]").evaluate((header) => ({
          height: header.getBoundingClientRect().height,
          position: getComputedStyle(header).position,
          navigation: [...header.querySelectorAll("nav a")].map((link) => link.textContent?.trim()),
        }));
        expect(siteHeader).toEqual(accountHeader);
      }
    }
    checks.push(
      "Existing home, scanners, strategy testing, replay, news and community routes restored; mobile header navigation works",
    );
    expect(errors).toEqual([]);
    writeFileSync(
      resolve(dir, "browser-verification.json"),
      JSON.stringify(
        {
          base,
          checks,
          errors,
          viewports: ["1536x1100", "1482x1061", "390x844"],
        },
        null,
        2,
      ),
    );
    writeFileSync(
      resolve(dir, "copy-audit.json"),
      JSON.stringify(copyAudit, null, 2),
    );
    console.log(JSON.stringify({ checks, errors }));
  }
} catch (e) {
  await page.screenshot({ path: resolve(dir, "failure.png"), fullPage: true });
  throw e;
} finally {
  await browser.close();
}
