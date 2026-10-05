import { chromium, expect } from '@playwright/test';
import { readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

const access = process.env.LIVE_ADMIN_PASSWORD
  ? {url:process.env.LIVE_URL,email:process.env.LIVE_ADMIN_EMAIL,password:process.env.LIVE_ADMIN_PASSWORD}
  : JSON.parse(readFileSync('/run/admin-access.json', 'utf8'));
const report = name => join(process.env.LIVE_REPORT_DIR || '/reports', name);
const checks = [], errors = [], failures = [];
const browser = await chromium.launch({headless:true, executablePath:process.env.LIVE_BROWSER_EXECUTABLE || undefined, args:['--no-sandbox']});
let page;
try {
  const context = await browser.newContext({viewport:{width:1536,height:1024},reducedMotion:'reduce'});
  await context.addInitScript(() => {
    localStorage.setItem('cookie_consent','essential');
    localStorage.setItem('mobile_notice_shown_v1','1');
  });
  page = await context.newPage();
  page.on('pageerror',e=>errors.push(e.message));
  page.on('response',r=>{if(r.url().startsWith(access.url+'/api/') && r.status()>=500) failures.push({url:r.url(),status:r.status()});});
  await page.goto(access.url+'/login');
  await page.locator('input[type=email]').fill(access.email);
  await page.locator('input[type=password]').fill(access.password);
  await page.getByRole('button',{name:'Войти',exact:true}).click();
  await expect(page.getByRole('link',{name:'Тест стратегии',exact:true})).toBeVisible({timeout:30000});
  checks.push('Browser login and authenticated navigation');
  await page.goto(access.url+'/');
  await page.getByPlaceholder('Поиск монеты (Bybit)').fill('BTCUSDT');
  await page.getByRole('button',{name:'Добавить',exact:true}).click({timeout:45000});
  await page.screenshot({path:report('home-desktop.png'),fullPage:true});
  const coin=page.getByRole('button',{name:'BTCUSDT',exact:true}).first();
  const [candleResponse] = await Promise.all([
    page.waitForResponse(r => r.url().includes('/v5/market/kline') && r.url().includes('interval=1&') && r.ok(), {timeout:45000}),
    coin.click({timeout:30000}),
  ]);
  await expect(page.getByRole('button',{name:'1м',exact:true}).first()).toHaveAttribute('aria-pressed','true',{timeout:30000});
  await expect(page.locator('canvas').first()).toBeVisible({timeout:30000});
  const candleData = await candleResponse.json();
  expect(candleData.retCode).toBe(0);
  expect(candleData.result.list.length).toBeGreaterThan(1);
  await page.waitForTimeout(5000);
  await page.screenshot({path:report('coin-minute-chart.png'),fullPage:true});
  checks.push('Coin modal, minute interval and real chart canvas');
  await page.goto(access.url+'/backtest');
  await expect(page.getByRole('heading',{name:/Тест стратегии/i}).first()).toBeVisible({timeout:30000});
  await page.screenshot({path:report('backtest-desktop.png'),fullPage:true});
  checks.push('Backtest builder renders');
  await page.setViewportSize({width:390,height:844});
  await page.goto(access.url+'/');
  await expect(page.getByRole('heading',{name:'Лист ожидания',exact:true})).toBeVisible({timeout:30000});
  await expect(page.getByRole('button',{name:'BTCUSDT',exact:true}).first()).toBeVisible({timeout:30000});
  await page.screenshot({path:report('home-mobile.png'),fullPage:true});
  const overflow=await page.evaluate(()=>document.documentElement.scrollWidth>window.innerWidth+2);
  expect(overflow).toBe(false);
  checks.push('Mobile viewport has no horizontal overflow');
  expect(errors).toEqual([]);
  expect(failures).toEqual([]);
  writeFileSync(report('browser-checks.json'),JSON.stringify({checks,errors,failures},null,2));
  console.log(JSON.stringify({checks,errors,failures}));
} catch (error) {
  await page?.screenshot({path:report('browser-failure.png'),fullPage:true});
  writeFileSync(report('browser-failure.json'),JSON.stringify({checks,errors,failures,error:String(error)},null,2));
  throw error;
} finally { await browser.close(); }
