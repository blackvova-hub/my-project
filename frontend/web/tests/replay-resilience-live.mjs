import { chromium, expect } from "@playwright/test";
import { writeFileSync } from "node:fs";
import { resolve } from "node:path";
const base = process.env.LIVE_URL || "http://192.168.0.124";
const out = resolve("../../docs/replay/rework");
const browser = await chromium.launch({
  headless: true,
  executablePath:
    process.env.LIVE_BROWSER_EXECUTABLE ||
    "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe",
});
const context = await browser.newContext({
  viewport: { width: 1469, height: 1071 },
});
await context.addInitScript(() => {
  localStorage.setItem("cookie_consent", "essential");
  localStorage.setItem("mobile_notice_shown_v1", "1");
});
const page = await context.newPage(),
  errors = [],
  checks = [];
page.on("pageerror", (e) => errors.push(e.message));
try {
  const login = await context.request.post(base + "/api/auth/login", {
    data: {
      email: process.env.LIVE_ADMIN_EMAIL || "admin@local.dev",
      password: process.env.LIVE_ADMIN_PASSWORD,
    },
  });
  expect(login.ok()).toBe(true);
  const to = Date.parse(new Date().toISOString().slice(0, 10)),
    from = to - 86400000;
  for (const exchange of ["bybit", "binance"])
    for (const market of ["linear", "spot"]) {
      const symbols = await context.request.get(
        `${base}/api/replay/symbols?exchange=${exchange}&market=${market}`,
      );
      expect(symbols.ok()).toBe(true);
      expect(
        (await symbols.json()).symbols.some((s) => s.symbol === "BTCUSDT"),
      ).toBe(true);
      let minutes;
      for (const [timeframe, n] of [
        ["1m", 1],
        ["5m", 5],
        ["15m", 15],
        ["1h", 60],
        ["4h", 240],
      ]) {
        const r = await context.request.get(
          `${base}/api/replay/candles?${new URLSearchParams({ exchange, market, symbol: "BTCUSDT", timeframe, from: String(from), to: String(to) })}`,
        );
        expect(r.ok()).toBe(true);
        const data = await r.json();
        expect(data.missing).toBe(0);
        expect(data.candles.length).toBe(1440 / n);
        if (n === 1) minutes = data.candles;
        else {
          const first = data.candles[0],
            source = minutes.slice(0, n);
          expect(first.open).toBe(source[0].open);
          expect(first.close).toBe(source.at(-1).close);
          expect(first.high).toBe(Math.max(...source.map((c) => c.high)));
          expect(first.low).toBe(Math.min(...source.map((c) => c.low)));
          expect(first.volume).toBeCloseTo(
            source.reduce((sum, c) => sum + c.volume, 0),
            4,
          );
        }
      }
      checks.push(
        `${exchange}/${market}: actual 1m archive and exact 5m/15m/1h/4h aggregation`,
      );
    }
  expect(errors).toEqual([]);
  writeFileSync(
    out + "/archive-api-checks.json",
    JSON.stringify({ checks, errors }, null, 2),
  );
  console.log(JSON.stringify({ checks, errors }, null, 2));
} finally {
  await browser.close();
}
