// Real local login/options; intercept search submissions to inspect the payload
// without creating a separate history job for every UI option.
import { chromium, expect } from "@playwright/test";
import { readFileSync, mkdirSync } from "node:fs";
import { universeOptions, universeRequest } from "../src/shared/market/similarityUniverse.ts";

const lines = readFileSync("../../README.md", "utf8").split(/\r?\n/);
const login = lines.findIndex((line) => line.trim() === "admin@local.dev");
if (login < 0) throw Error("Local QA account missing");
const out = "../../tmp/similarity-universe";
mkdirSync(out, { recursive: true });
const browser = await chromium.launch({ channel: "msedge", headless: true });
try {
  const context = await browser.newContext({ viewport: { width: 1536, height: 1024 } });
  await context.addInitScript(() => {
    localStorage.setItem("cookie_consent", "essential");
    localStorage.setItem("mobile_notice_shown_v1", "1");
  });
  const page = await context.newPage();
  const errors = [];
  const sent = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await context.route("**/api/similarity/search", async (route) => {
    sent.push(route.request().postDataJSON());
    await route.fulfill({ status: 422, json: { error: "UI test: request captured" } });
  });
  await page.goto("http://localhost:3000/login");
  await page.locator('input[type="email"]').fill(lines[login].trim());
  await page.locator('input[type="password"]').fill(lines[login + 1].trim());
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page.getByRole("link", { name: "Тест стратегии", exact: true }).waitFor();
  const response = await context.request.get("http://localhost:3000/api/similarity/options");
  expect(response.status()).toBe(200);
  const options = await response.json();
  await page.goto("http://localhost:3000/");
  await page.getByRole("button", { name: "BTCUSDT", exact: true }).first().click();
  const panel = page.getByRole("region", { name: "Похожие ситуации", exact: true });
  await panel.scrollIntoViewIfNeeded();
  const scope = panel.getByLabel("Область поиска", { exact: true });
  await expect(scope).toBeEnabled();
  await expect(panel.locator("select")).toHaveCount(0);
  await expect(panel.getByLabel("Категория похожих ситуаций", { exact: true })).toHaveCount(0);
  const choose = async (label, choice) => {
    const trigger = panel.getByLabel(label, { exact: true });
    await trigger.scrollIntoViewIfNeeded();
    await page.waitForTimeout(350);
    try {
      await expect(async () => {
        if (await trigger.getAttribute("aria-expanded") !== "true") await trigger.click();
        await page.getByRole("option").filter({ has: page.getByText(choice, { exact: true }) }).click({ timeout: 1500 });
      }).toPass({ timeout: 12000 });
      await expect(page.getByRole("listbox")).toHaveCount(0);
    } catch (error) {
      await page.screenshot({ path: `${out}/failure.png` });
      throw error;
    }
  };
  await scope.click();
  await expect(page.getByRole("listbox")).toBeVisible();
  await expect(page.getByRole("option", { name: "Категории", exact: true })).toHaveCount(0);
  await expect(page.getByRole("option", { name: "Мемкоинов", exact: true })).toBeVisible();
  await page.waitForTimeout(250);
  await page.screenshot({ path: `${out}/desktop.png` });
  await page.keyboard.press("Escape");
  await expect(page.getByRole("listbox")).toHaveCount(0);
  expect(sent).toHaveLength(0);
  for (const option of universeOptions(options.sectors)) {
    await choose("Область поиска", option.label);
    if (option.value === "custom") await panel.getByLabel("Монеты для сравнения").fill("ETHUSDT, SOLUSDT");
    else await expect(panel.getByLabel("Монеты для сравнения")).toHaveCount(0);
    const count = sent.length;
    await panel.getByRole("button", { name: "Найти похожие", exact: true }).click();
    await expect.poll(() => sent.length).toBe(count + 1);
    const expected = universeRequest(option.value, "ETHUSDT, SOLUSDT");
    const actual = sent.at(-1);
    expect(actual.scope).toBe(expected.scope);
    expect(actual.sector).toBe(expected.sector);
    expect(actual.symbols).toEqual(expected.symbols);
    expect(actual.limit).toBe(6);
  }
  await choose("Рынок похожих ситуаций", "Spot");
  await expect(panel.getByLabel("Рынок похожих ситуаций")).toContainText("Spot");
  await choose("Область поиска", "Мемкоинов");
  await panel.getByLabel("Окно похожих ситуаций").click();
  await expect(page.getByRole("listbox")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("listbox")).toHaveCount(0);
  await page.setViewportSize({ width: 390, height: 844 });
  await scope.scrollIntoViewIfNeeded();
  await scope.click();
  await expect(page.getByRole("listbox")).toBeVisible();
  expect(await page.getByRole("listbox").evaluate((el) => { const r = el.getBoundingClientRect(); return r.left >= 0 && r.right <= innerWidth && r.top >= 0 && r.bottom <= innerHeight; })).toBe(true);
  await page.waitForTimeout(250);
  await page.screenshot({ path: `${out}/mobile.png` });
  await page.keyboard.press("End");
  await page.keyboard.press("Enter");
  await expect(scope).toContainText("Своего списка");
  await expect(panel.getByLabel("Монеты для сравнения")).toBeVisible();
  expect(errors).toEqual([]);
  console.log(`PASS ${sent.length} scopes: correct payloads, no native category selector, desktop/mobile menu, keyboard, market, no automatic searches`);
} finally {
  await browser.close();
}
