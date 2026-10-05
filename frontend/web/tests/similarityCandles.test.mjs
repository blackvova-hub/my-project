import { test } from 'node:test';
import assert from 'node:assert/strict';
import { aggregateSimilarityCandles } from '../src/shared/market/similarityCandles.ts';

const start = Date.UTC(2026, 8, 1) / 1000;
const candle = (i) => ({ time: start + i * 300, open: 100 + i, high: 105 + i, low: 98 + i, close: 101 + i });

test('all display intervals preserve actual OHLC and do not mutate source candles', () => {
  const source = Array.from({ length: 96 }, (_, i) => candle(i));
  const snapshot = structuredClone(source);
  for (const seconds of [300, 900, 3600, 14400]) {
    const bars = aggregateSimilarityCandles(source, seconds, (start + 14400) * 1000);
    const n = seconds / 300;
    assert.equal(bars.length, 96 / n);
    bars.forEach((bar, i) => assert.deepEqual(bar, {
      time: start + i * seconds,
      open: 100 + i * n,
      high: 104 + (i + 1) * n,
      low: 98 + i * n,
      close: 100 + (i + 1) * n,
    }));
  }
  assert.deepEqual(source, snapshot);
});

test('UTC alignment, partial edges and match boundary never leak future prices', () => {
  const source = Array.from({ length: 18 }, (_, i) => candle(i + 1));
  source[10] = { ...source[10], high: 999, close: 990 }; // first future bar 00:55
  const bars = aggregateSimilarityCandles(source, 3600, (start + 3300) * 1000);
  assert.deepEqual(bars.map((bar) => bar.time - start), [300, 3300, 3600]);
  assert.equal(bars[0].high, 115);
  assert.equal(bars[0].close, 111);
  assert.equal(bars[1].high, 999);
  assert.equal(bars[1].close, 990);
});

test('missing source candles are not joined across a gap', () => {
  const source = [candle(0), candle(1), candle(4), candle(5)];
  const bars = aggregateSimilarityCandles(source, 3600, (start + 3600) * 1000);
  assert.deepEqual(bars.map((bar) => bar.time - start), [0, 1200]);
  assert.equal(bars[0].close, 102);
  assert.equal(bars[1].open, 104);
  assert.deepEqual(aggregateSimilarityCandles([], 3600, 0), []);
});
