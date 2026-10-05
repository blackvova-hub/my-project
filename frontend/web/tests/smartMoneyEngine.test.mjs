import assert from "node:assert/strict";
import test from "node:test";

import { calculateSmartMoney } from "../src/shared/market/smartMoneyEngine.ts";

function sweepFixture(closeBeyondLevel) {
  const candles = [];
  for (let index = 0; index < 35; index += 1) {
    let close = 100;
    let high = 101;
    let low = 99;
    if (index >= 15 && index <= 20) {
      high = 101 + (index - 15) * 1.8;
      close = high - 0.8;
      low = close - 1;
    }
    if (index > 20 && index <= 25) {
      high = 110 - (index - 20) * 1.4;
      close = high - 0.8;
      low = close - 1;
    }
    if (index === 26) {
      high = closeBeyondLevel ? 112 : 111.2;
      close = closeBeyondLevel ? 111.5 : 109.4;
      low = 108.8;
    }
    candles.push({ time: index + 1, open: close - 0.2, high, low, close, volume: 100 });
  }
  return candles;
}

function structureFixture() {
  const candles = [];
  for (let index = 0; index <= 26; index += 1) {
    let close = 100;
    let high = 101;
    let low = 99;
    let open = 99.8;
    if (index >= 15 && index <= 20) {
      high = 101 + (index - 15) * 1.8;
      close = high - 0.8;
      low = close - 1;
      open = close + 0.8;
    }
    if (index > 20 && index <= 25) {
      high = 110 - (index - 20) * 1.4;
      close = high - 0.8;
      low = close - 1;
      open = close + 0.8;
    }
    if (index === 26) {
      open = 108.8;
      high = 112;
      low = 108.5;
      close = 111.5;
    }
    candles.push({ time: index + 1, open, high, low, close, volume: 100 });
  }
  for (const close of [111, 110, 109, 108, 107, 108, 109, 110, 111, 112]) {
    candles.push({ time: candles.length + 1, open: close - 0.3, high: close + 0.5, low: close - 0.5, close, volume: 100 });
  }
  candles.push({ time: candles.length + 1, open: 112, high: 112.2, low: 103.5, close: 104, volume: 500 });
  return candles;
}

test("wick rejection is a liquidity sweep, not a BOS", () => {
  const output = calculateSmartMoney(sweepFixture(false));
  assert.equal(output.markers.filter((item) => item.indicator === "liquidity_sweeps").length, 1);
  assert.equal(output.levels.some((item) => item.indicator === "bos"), false);
});

test("a confirmed close through the same swing creates BOS", () => {
  const output = calculateSmartMoney(sweepFixture(true));
  assert.equal(output.levels.some((item) => item.indicator === "bos" && item.direction === "bullish"), true);
  assert.equal(output.markers.some((item) => item.indicator === "liquidity_sweeps"), false);
});

test("counter-trend displacement upgrades CHOCH to MSS and creates structure-gated OBs", () => {
  const output = calculateSmartMoney(structureFixture());
  assert.equal(output.levels.some((item) => item.indicator === "choch" && item.direction === "bearish"), true);
  assert.equal(output.levels.some((item) => item.indicator === "mss" && item.direction === "bearish"), true);
  assert.equal(output.zones.some((item) => item.indicator === "order_blocks" && item.direction === "bullish"), true);
  assert.equal(output.zones.some((item) => item.indicator === "order_blocks" && item.direction === "bearish"), true);
});

test("equal highs require two distinct confirmed swings inside ATR tolerance", () => {
  const values = [];
  for (let index = 0; index < 55; index += 1) {
    let close = 100;
    if (index >= 15 && index <= 20) close = 100 + (index - 15) * 2;
    if (index > 20 && index <= 30) close = 110 - (index - 20);
    if (index > 30 && index <= 40) close = 100 + (index - 30) * 0.99;
    if (index > 40 && index <= 50) close = 109.9 - (index - 40);
    values.push(close);
  }
  const candles = values.map((close, index) => ({
    time: index + 1,
    open: close - 0.1,
    high: close + 0.5,
    low: close - 0.5,
    close,
    volume: 100,
  }));
  const output = calculateSmartMoney(candles);
  const equalHigh = output.levels.find((item) => item.indicator === "equal_high_low" && item.label === "EQH");
  assert.ok(equalHigh);
  assert.equal(equalHigh.active, true);
});

test("FVG uses the three-candle wick gap and ends only after a full wick fill", () => {
  const candles = Array.from({ length: 26 }, (_, index) => ({
    time: index + 1, open: 100, high: 101, low: 99, close: 100, volume: 100,
  }));
  candles.push({ time: 27, open: 100, high: 104, low: 99.8, close: 103.8, volume: 500 });
  candles.push({ time: 28, open: 103.5, high: 104, low: 102, close: 103, volume: 100 });
  candles.push({ time: 29, open: 103, high: 104, low: 101.5, close: 102.5, volume: 100 });
  candles.push({ time: 30, open: 102.5, high: 103, low: 100.5, close: 101, volume: 100 });
  const output = calculateSmartMoney(candles);
  const gap = output.zones.find((item) => item.indicator === "fvg" && item.direction === "bullish");
  assert.ok(gap);
  assert.equal(gap.bottom, 101);
  assert.equal(gap.top, 102);
  assert.equal(gap.status, "mitigated");
  assert.equal(gap.endTime, 30);
});

