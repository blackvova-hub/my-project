import { chromium, expect } from "@playwright/test";
import { readFileSync, mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
const root = path.resolve("../..");
const lines = readFileSync(path.join(root, "README.md"), "utf8").split(/\r?\n/);
const i = lines.findIndex((l) => l.trim() === "admin@local.dev");
if (i < 0) throw Error("Local QA account missing");
const out = path.join(root, "docs/similarity");
mkdirSync(out, { recursive: true });
const browser = await chromium.launch({ channel: "msedge", headless: true });
try {
  const context = await browser.newContext({
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
  await page.goto("http://localhost:3000/login");
  await page.locator('input[type="email"]').fill(lines[i].trim());
  await page.locator('input[type="password"]').fill(lines[i + 1].trim());
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page
    .getByRole("link", { name: "Тест стратегии", exact: true })
    .waitFor();
  const options = await context.request.get(
    "http://localhost:3000/api/similarity/options",
  );
  expect(options.status()).toBe(200);
  const searchPosts = [];
  const statusTimes = [];
  page.on("request", (r) => {
    if (r.method() === "POST" && r.url().endsWith("/api/similarity/search"))
      searchPosts.push(Date.now());
    if (r.method() === "GET" && r.url().includes("/api/similarity/jobs/"))
      statusTimes.push(Date.now());
  });
  await page.goto("http://localhost:3000/");
  const coin = page
    .getByRole("button", { name: "BTCUSDT", exact: true })
    .first();
  await coin.waitFor({ timeout: 30000 });
  if ((await coin.count()) === 0) {
    console.log(
      "COIN_BUTTONS",
      await page.getByRole("button").allTextContents(),
    );
    throw Error("BTCUSDT not present in visible watchlist");
  }
  await coin.click();
  await page.screenshot({
    path: path.join(out, "existing-coin-reference.png"),
  });
  const panel = page.getByRole("region", {
    name: "Похожие ситуации",
    exact: true,
  });
  await panel.scrollIntoViewIfNeeded();
  await expect(
    panel.getByRole("button", { name: "Найти похожие", exact: true }),
  ).toBeEnabled();
  await page.waitForTimeout(6000);
  expect(searchPosts).toHaveLength(0);
  await expect(panel.locator("[data-similarity-match]")).toHaveCount(0);
  await panel.screenshot({ path: path.join(out, "on-demand-idle.png") });
  console.log("PASS opening/scrolling does not start a search");
  await panel.getByLabel("Окно похожих ситуаций", { exact: true }).click();
  await page.getByRole("option", { name: "1 час", exact: true }).click();
  await expect(panel.getByLabel("Область поиска", { exact: true })).toContainText(
    "Этой монеты",
  );
  const posted = page.waitForResponse(
    (r) =>
      r.request().method() === "POST" &&
      r.url().endsWith("/api/similarity/search"),
  );
  await panel
    .getByRole("button", { name: "Найти похожие", exact: true })
    .click();
  const response = await posted;
  expect(response.status()).toBe(202);
  const { jobId } = await response.json();
  await expect(panel.locator("[data-similarity-match]")).toHaveCount(6, {
    timeout: 180000,
  });
  const job = await (
    await context.request.get(
      "http://localhost:3000/api/similarity/jobs/" + jobId,
    )
  ).json();
  expect(job.status).toBe("completed");
  const result = job.result;
  expect(
    result.matches.every(
      (m) =>
        m.symbol === "BTCUSDT" && m.chart.length > 0 && m.outcomes.length > 0,
    ),
  ).toBe(true);
  const finishedPolls = statusTimes.length;
  await page.waitForTimeout(6000);
  expect(statusTimes.length).toBe(finishedPolls);
  console.log(
    "PASS real on-demand preparation, six matches, outcomes and polling stops",
  );
  await expect(panel.locator("canvas").first()).toBeVisible();
  await panel.screenshot({ path: path.join(out, "charts-desktop.png") });
  const boxes = await panel
    .locator("[data-similarity-match]")
    .evaluateAll((nodes) =>
      nodes.map((n) => {
        const r = n.getBoundingClientRect();
        return { x: r.x, y: r.y, width: r.width, height: r.height };
      }),
    );
  expect(Math.abs(boxes[0].y - boxes[2].y)).toBeLessThan(2);
  expect(boxes[3].y).toBeGreaterThan(boxes[0].y);
  expect(Math.abs(boxes[3].y - boxes[5].y)).toBeLessThan(2);
  await panel
    .getByRole("button", { name: /Сходство/ })
    .first()
    .click();
  await expect(panel.getByText("Волатильность", { exact: true })).toBeVisible();
  await panel
    .getByRole("button", { name: /Сходство/ })
    .first()
    .click();
  await panel
    .getByRole("button", { name: "Найти похожие", exact: true })
    .click();
  await expect(panel.locator("[data-similarity-match]").first()).toBeVisible({
    timeout: 30000,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await panel.evaluate((n) => {
    let p = n.parentElement;
    while (p && !/(auto|scroll)/.test(getComputedStyle(p).overflowY))
      p = p.parentElement;
    if (p)
      p.scrollTop +=
        n.getBoundingClientRect().top - p.getBoundingClientRect().top - 16;
  });
  await page.mouse.move(0, 0);
  await page.screenshot({ path: path.join(out, "charts-mobile.png") });
  const overflow = await panel.evaluate(
    (n) => n.scrollWidth > n.clientWidth + 1,
  );
  expect(overflow).toBe(false);
  await page.setViewportSize({ width: 1536, height: 1024 });
  await panel.getByLabel("Область поиска", { exact: true }).click();
  await page.getByRole("option", { name: "Всех монет", exact: true }).click();
  await panel.getByLabel("Окно похожих ситуаций", { exact: true }).click();
  await page.getByRole("option", { name: "3 часа", exact: true }).click();
  const cancelling = page.waitForResponse(
    (r) =>
      r.request().method() === "POST" &&
      r.url().endsWith("/api/similarity/search"),
  );
  await panel
    .getByRole("button", { name: "Найти похожие", exact: true })
    .click();
  const cancelResponse = await cancelling;
  expect(cancelResponse.status()).toBe(202);
  const cancelled = await cancelResponse.json();
  await panel
    .getByRole("button", { name: "Отменить поиск", exact: true })
    .click();
  await expect(panel).toContainText("Поиск отменён.", { timeout: 20000 });
  await page.reload();
  await page
    .getByRole("button", { name: "BTCUSDT", exact: true })
    .first()
    .click();
  const restored = page.getByRole("region", {
    name: "Похожие ситуации",
    exact: true,
  });
  await restored.scrollIntoViewIfNeeded();
  await expect(restored).toContainText("Поиск отменён.");
  expect(searchPosts).toHaveLength(3);
  writeFileSync(
    path.join(out, "on-demand-browser-check.json"),
    JSON.stringify(
      {
        jobId,
        cancelledJobId: cancelled.jobId,
        realSearch: {
          matches: result.matches.length,
          candidates: result.candidates,
        },
        desktop: boxes,
        mobileOverflow: overflow,
        pageErrors: errors,
        searchPosts: searchPosts.length,
        statusRequests: statusTimes.length,
        statusTimes,
      },
      null,
      2,
    ),
  );
  expect(errors).toEqual([]);
  console.log(
    "PASS explicit search, six charts 3x2, breakdown, reuse, mobile, cancellation, reload without restart",
  );
} finally {
  await browser.close();
}
