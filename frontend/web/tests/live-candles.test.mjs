import test from "node:test";
import assert from "node:assert/strict";
import { registerHooks } from "node:module";
registerHooks({
  resolve(specifier, context, next) {
    try {
      return next(specifier, context);
    } catch (e) {
      if (
        e.code === "ERR_MODULE_NOT_FOUND" &&
        specifier.startsWith(".") &&
        !/\.[a-z]+$/i.test(specifier)
      )
        return next(specifier + ".ts", context);
      throw e;
    }
  },
});
const { startLiveCandlePolling } = await import(
  "../src/shared/charts/liveCandlePolling.ts"
);
const { formatVolume, validVolumeRange } = await import(
  "../src/shared/charts/volumeScale.ts"
);
const { restorePriceRange } = await import(
  "../src/shared/charts/priceScaleRange.ts"
);
const candle = (time = 60, close = 11) => ({
  time,
  open: 10,
  high: Math.max(12, close),
  low: 9,
  close,
  volume: 5,
});
const flush = async () => {
  for (let n = 0; n < 12; n++) await Promise.resolve();
};
function clock(t) {
  let now = 0,
    tick;
  t.mock.method(Date, "now", () => now);
  t.mock.method(globalThis, "setInterval", (fn, delay) => {
    assert.equal(delay, 1000);
    tick = fn;
    return 123;
  });
  t.mock.method(globalThis, "clearInterval", () => {});
  return async (value) => {
    now = value;
    tick();
    await flush();
  };
}
test("live fallback requests only two current candles every second", async (t) => {
  const advance = clock(t),
    requests = [],
    output = [];
  const feed = startLiveCandlePolling(
    async (_signal, limit) => {
      requests.push([Date.now(), limit]);
      return [candle(60, 10 + Date.now() / 1000)];
    },
    60,
    (rows) => output.push(rows),
    () => {},
  );
  try {
    await advance(1000);
    await advance(2000);
    await advance(3000);
    assert.deepEqual(requests, [
      [1000, 2],
      [2000, 2],
      [3000, 2],
    ]);
    assert.equal(output.at(-1)[0].close, 13);
  } finally {
    feed.stop();
  }
});
test("pending snapshots do not overlap or overwrite a newer websocket candle", async (t) => {
  const advance = clock(t);
  let finish,
    requests = 0;
  const output = [];
  const feed = startLiveCandlePolling(
    () => {
      requests++;
      return new Promise((resolve) => (finish = resolve));
    },
    60,
    (rows) => output.push(...rows),
    () => {},
  );
  try {
    await advance(1000);
    await advance(2000);
    assert.equal(requests, 1);
    feed.websocket(candle(60, 22));
    finish([candle(60, 11)]);
    await flush();
    assert.equal(output.length, 1);
    assert.equal(output[0].close, 22);
    await advance(2500);
    assert.equal(requests, 1);
  } finally {
    feed.stop();
  }
});
test("stopping a live feed cancels a pending request and prevents late updates", async (t) => {
  const advance = clock(t);
  let finish, signal;
  const output = [];
  const feed = startLiveCandlePolling(
    (s) => {
      signal = s;
      return new Promise((resolve) => (finish = resolve));
    },
    60,
    (rows) => output.push(...rows),
    () => {},
  );
  await advance(1000);
  feed.stop();
  assert.equal(signal.aborted, true);
  finish([candle()]);
  await flush();
  assert.equal(output.length, 0);
});
test("missed bars trigger a history page and failed sources back off without emitting invented values", async (t) => {
  const advance = clock(t),
    limits = [],
    output = [];
  let fail = false;
  const feed = startLiveCandlePolling(
    async (_s, limit) => {
      limits.push(limit);
      if (fail) throw new Error("429");
      return [candle(600)];
    },
    60,
    (rows) => output.push(...rows),
    () => {},
  );
  try {
    feed.websocket(candle(60));
    await advance(1000);
    assert.deepEqual(limits, [2, 1000]);
    fail = true;
    await advance(2000);
    const size = output.length;
    await advance(3000);
    assert.equal(limits.length, 3);
    assert.equal(output.length, size);
    await advance(4000);
    assert.equal(limits.length, 4);
  } finally {
    feed.stop();
  }
});
test("volume axes reject damaged ranges and keep enormous labels bounded", () => {
  assert.equal(
    validVolumeRange({ from: 0, to: 1.4150746702e23 }, 10000),
    false,
  );
  assert.equal(validVolumeRange({ from: 0, to: 25000 }, 10000), true);
  assert.equal(validVolumeRange({ from: NaN, to: 10 }, 100), false);
  assert.equal(formatVolume(1250000000000), "1.25T");
  assert.equal(formatVolume(1.4150746702e23), "1.42e+23");
  assert.equal(formatVolume(0), "0");
});
test("manual logarithmic ranges round-trip in original units instead of exponentiating volumes", () => {
  let internal = { from: 0, to: Math.log10(20000 + 0.0001) + 4 },
    auto = false;
  const inverse = (v) =>
    v === 0 ? 0 : Math.sign(v) * (10 ** (Math.abs(v) - 4) - 0.0001);
  const scale = {
    options: () => ({ mode: 1 }),
    getVisibleRange: () => ({
      from: inverse(internal.from),
      to: inverse(internal.to),
    }),
    setVisibleRange: (r) => (internal = r),
    applyOptions: (o) => (auto = o.autoScale),
  };
  assert.equal(restorePriceRange(scale, { from: 0, to: 10000 }), true);
  assert.ok(Math.abs(scale.getVisibleRange().to - 10000) < 0.01);
  assert.equal(auto, false);
  const broken = {
    ...scale,
    setVisibleRange: () => (internal = { from: 0, to: Infinity }),
  };
  assert.equal(restorePriceRange(broken, { from: 0, to: 10000 }), false);
  assert.equal(auto, true);
});
