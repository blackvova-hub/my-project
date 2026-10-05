import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  testMatch: "backtest.e2e.spec.ts",
  fullyParallel: false,
  workers: 1,
  timeout: 180_000,
  expect: { timeout: 20_000 },
  outputDir: "../../tmp/backtest-playwright",
  use: {
    baseURL: process.env.BACKTEST_E2E_URL ?? "http://127.0.0.1:5173",
    viewport: { width: 1536, height: 1024 },
    channel: process.env.BACKTEST_E2E_CHANNEL ?? "msedge",
    headless: true,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
});
