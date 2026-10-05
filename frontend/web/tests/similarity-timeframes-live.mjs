import { chromium, expect, request } from '@playwright/test';
import { readFileSync, mkdirSync, writeFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import path from 'node:path';
import { createHash } from 'node:crypto';

const root = path.resolve('../..');
const lines = readFileSync(path.join(root, 'README.md'), 'utf8').split(/\r?\n/);
const login = lines.findIndex((line) => line.trim() === 'admin@local.dev');
if (login < 0) throw Error('QA account missing');
// Reuse a real completed job; this check never submits a new similarity search.
const saved = JSON.parse(execFileSync('docker', ['exec', 'crypto_postgres', 'psql', '-U', 'postgres', '-d', 'alerts', '-At', '-c',
  "SELECT json_build_object('id',id,'owner',user_id,'request',request) FROM similarity_jobs WHERE status='completed' AND request->>'symbol'='BTCUSDT' AND request->>'windowBars'='2016' AND jsonb_array_length(result->'matches')=6 ORDER BY created_at DESC LIMIT 1"], { encoding: 'utf8' }).trim());
saved.request = { market: saved.request.market, symbol: saved.request.symbol, windowBars: saved.request.windowBars, scope: saved.request.scope, limit: saved.request.limit };
const out = path.join(root, 'docs/similarity/timeframes');
mkdirSync(out, { recursive: true });
const browser = await chromium.launch({ channel: 'msedge', headless: true });
const checks = [];
try {
  const context = await browser.newContext({ viewport: { width: 1536, height: 1024 }, reducedMotion: 'reduce' });
  await context.addInitScript(({ saved }) => {
    localStorage.setItem('cookie_consent', 'essential');
    localStorage.setItem('mobile_notice_shown_v1', '1');
    localStorage.setItem(`similarity:requested:v1:${saved.owner}:BTCUSDT`, JSON.stringify({ id: saved.id, request: saved.request }));
  }, { saved });
  const page = await context.newPage();
  const errors = [];
  const requests = [];
  page.on('pageerror', (error) => errors.push(error.message));
  page.on('request', (req) => { if (req.url().includes('/api/similarity/')) requests.push({ method: req.method(), url: req.url() }); });
  await page.goto('http://localhost:3000/login');
  await page.locator('input[type="email"]').fill(lines[login].trim());
  await page.locator('input[type="password"]').fill(lines[login + 1].trim());
  await page.getByRole('button', { name: 'Войти', exact: true }).click();
  await page.getByRole('link', { name: 'Тест стратегии', exact: true }).waitFor();
  const open = async () => {
    await page.goto('http://localhost:3000/');
    await page.getByRole('button', { name: 'BTCUSDT', exact: true }).first().click();
    const panel = page.getByRole('region', { name: 'Похожие ситуации', exact: true });
    await panel.scrollIntoViewIfNeeded();
    await expect(panel.locator('[data-similarity-match]')).toHaveCount(6, { timeout: 30000 });
    return panel;
  };
  let panel = await open();
  const raw = await context.request.get(`http://localhost:3000/api/similarity/jobs/${saved.id}/charts`);
  expect(raw.status()).toBe(200);
  const rawCharts = (await raw.json()).charts;
  expect(rawCharts).toHaveLength(6);
  expect(rawCharts.every((item) => item.candles.length > 1000)).toBe(true);
  for (const item of rawCharts) {
    expect(item.candles.every((c, i) => i === 0 || c.time - item.candles[i - 1].time >= 300)).toBe(true);
  }
  const charts = () => panel.locator('[data-chart-timeframe]');
  await expect(charts().first()).toHaveAttribute('aria-busy', 'false');
  await expect(panel.getByRole('group', { name: 'Таймфрейм всех графиков' }).getByRole('button', { name: '1ч', exact: true })).toHaveAttribute('aria-pressed', 'true');
  const counts = {};
  const beforeSwitchRequests = requests.length;
  for (const [label, seconds] of [['5м', 300], ['15м', 900], ['1ч', 3600], ['4ч', 14400]]) {
    await panel.getByRole('group', { name: 'Таймфрейм всех графиков' }).getByRole('button', { name: label, exact: true }).click();
    await expect.poll(() => charts().evaluateAll((nodes, interval) => nodes.every((n) => n.dataset.chartTimeframe === String(interval)), seconds)).toBe(true);
    counts[label] = await charts().evaluateAll((nodes) => nodes.map((n) => Number(n.dataset.chartBars)));
    expect(counts[label].every((n) => n > 0)).toBe(true);
  }
  expect(requests.length).toBe(beforeSwitchRequests);
  for (let i = 0; i < 6; i++) expect(counts['5м'][i]).toBeGreaterThan(counts['15м'][i]);
  for (let i = 0; i < 6; i++) expect(counts['15м'][i]).toBeGreaterThan(counts['1ч'][i]);
  for (let i = 0; i < 6; i++) expect(counts['1ч'][i]).toBeGreaterThan(counts['4ч'][i]);
  checks.push('four intervals update all six real charts with zero extra network requests');

  await panel.getByRole('group', { name: 'Таймфрейм всех графиков' }).getByRole('button', { name: '1ч', exact: true }).click();
  const hash = async (locator) => createHash('sha256').update(await locator.screenshot()).digest('hex');
  // Keep the top row visible and let chart rendering settle before image comparison.
  await charts().first().scrollIntoViewIfNeeded();
  await page.mouse.move(0, 0);
  await page.waitForTimeout(700);
  const before = [await hash(charts().nth(0)), await hash(charts().nth(1)), await hash(charts().nth(2))];
  const box = await charts().first().boundingBox();
  await page.mouse.move(box.x + box.width * .65, box.y + 80);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * .25, box.y + 80, { steps: 15 });
  await page.mouse.up();
  await page.mouse.move(0, 0);
  await page.waitForTimeout(700);
  const afterDrag = [await hash(charts().nth(0)), await hash(charts().nth(1)), await hash(charts().nth(2))];
  expect(afterDrag[0]).not.toBe(before[0]);
  expect(afterDrag.slice(1)).toEqual(before.slice(1));
  await panel.getByRole('button', { name: 'Сбросить масштаб графика BTCUSDT', exact: true }).first().click();
  await page.mouse.move(0, 0);
  await page.waitForTimeout(500);
  expect(await hash(charts().first())).toBe(before[0]);
  await page.mouse.move(box.x + box.width * .5, box.y + 80);
  await page.mouse.wheel(0, -450);
  await page.mouse.move(0, 0);
  await page.waitForTimeout(500);
  expect(await hash(charts().first())).not.toBe(before[0]);
  expect(await hash(charts().nth(1))).toBe(before[1]);
  await panel.getByRole('button', { name: 'Сбросить масштаб графика BTCUSDT', exact: true }).first().click();
  checks.push('dragging, wheel zoom and reset work independently; neighbouring chart images unchanged');
  await page.mouse.move(0, 0);
  await panel.screenshot({ path: path.join(out, 'desktop.png') });

  await panel.getByRole('group', { name: 'Таймфрейм всех графиков' }).getByRole('button', { name: '4ч', exact: true }).click();
  panel = await open();
  await expect(panel.getByRole('group', { name: 'Таймфрейм всех графиков' }).getByRole('button', { name: '4ч', exact: true })).toHaveAttribute('aria-pressed', 'true');
  checks.push('chosen timeframe survives page reload');
  await page.setViewportSize({ width: 390, height: 844 });
  await panel.getByRole('group', { name: 'Таймфрейм всех графиков' }).scrollIntoViewIfNeeded();
  await expect.poll(() => panel.evaluate((n) => n.scrollWidth <= n.clientWidth + 1)).toBe(true);
  const mobileButton = panel.getByRole('group', { name: 'Таймфрейм всех графиков' }).getByRole('button', { name: '15м', exact: true });
  await mobileButton.focus();
  await page.keyboard.press('Enter');
  await expect(mobileButton).toHaveAttribute('aria-pressed', 'true');
  await page.screenshot({ path: path.join(out, 'mobile.png') });
  checks.push('mobile layout without overflow and keyboard interval selection');

  const touch = await context.newCDPSession(page);
  await touch.send('Emulation.setTouchEmulationEnabled', { enabled: true, maxTouchPoints: 2 });
  await charts().first().scrollIntoViewIfNeeded();
  await page.mouse.move(0, 0);
  await page.waitForTimeout(500);
  const touchBefore = await hash(charts().first());
  const touchBox = await charts().first().boundingBox();
  const touchY = touchBox.y + 75;
  const touchX = touchBox.x + touchBox.width * .65;
  await touch.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: touchX, y: touchY }] });
  for (let i = 1; i <= 6; i++) {
    await touch.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: touchX - i * 12, y: touchY }] });
    await page.waitForTimeout(25);
  }
  await touch.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
  await page.waitForTimeout(1000);
  expect(await hash(charts().first())).not.toBe(touchBefore);
  await touch.send('Emulation.setTouchEmulationEnabled', { enabled: false });
  await touch.detach();
  checks.push('horizontal touch gesture moves the selected chart on mobile');

  await page.route(`**/api/similarity/jobs/${saved.id}/charts`, (route) => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'Проверка повторной загрузки' }) }), { times: 1 });
  panel = await open();
  await expect(panel.getByText('Проверка повторной загрузки', { exact: false })).toBeVisible();
  await panel.getByRole('button', { name: 'Повторить загрузку свечей', exact: true }).click();
  await expect(panel.locator('[data-chart-timeframe]').first()).toHaveAttribute('aria-busy', 'false');
  await expect.poll(() => panel.locator('[data-chart-timeframe]').first().getAttribute('data-chart-bars')).not.toBe('0');
  checks.push('failed candle load recovers using retry without resubmitting search');
  expect(requests.filter((r) => r.method === 'POST')).toHaveLength(0);
  expect(errors).toEqual([]);
  const anonymous = await request.newContext();
  expect((await anonymous.get(`http://localhost:3000/api/similarity/jobs/${saved.id}/charts`)).status()).toBe(401);
  await anonymous.dispose();
  writeFileSync(path.join(out, 'checks.json'), JSON.stringify({ checks, counts, errors, newSearches: 0, rawCandles: rawCharts.map((c) => c.candles.length) }, null, 2));
  console.log(JSON.stringify({ checks, counts, errors }, null, 2));
} finally {
  await browser.close();
}
