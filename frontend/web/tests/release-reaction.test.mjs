import test from "node:test";
import assert from "node:assert/strict";
import { releaseReaction } from "../src/pages/public/news-macro/releaseReaction.ts";

const candles = Array.from({ length: 300 }, (_, i) => ({
  time: 900 + i * 300,
  close: 100 + i,
}));
test("release horizons use completed closes on both sides of the official schedule", () => {
  const result = releaseReaction(candles, 1200, 1800, 100000);
  assert.ok(Math.abs(result.value - 6) < 1e-9);
  assert.equal(releaseReaction(candles, 1200, 7200, 100000).value, 24);
});
test("missing history, price corruption and future horizons never become a return", () => {
  assert.equal(
    releaseReaction(
      candles.filter((p) => p.time !== 1800),
      1200,
      1800,
      100000,
    ).value,
    null,
  );
  assert.equal(
    releaseReaction(
      candles.filter((p) => p.time >= 1200),
      1200,
      1800,
      100000,
    ).value,
    null,
  );
  assert.equal(releaseReaction(candles, 1200, 86400, 2000).value, null);
  assert.equal(
    releaseReaction(
      candles.map((p) => (p.time === 900 ? { ...p, close: 0 } : p)),
      1200,
      1800,
      100000,
    ).value,
    null,
  );
});
test("an unfinished horizon candle cannot leak its provisional close", () => {
  assert.equal(releaseReaction(candles, 1200, 1800, 2999).value, null);
  assert.equal(
    releaseReaction(
      candles.filter((p) => p.time !== 2700),
      1200,
      1800,
      100000,
    ).value,
    null,
  );
});
