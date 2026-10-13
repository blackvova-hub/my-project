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
const { normalizeWorkspace, patchWindow, layoutShape } = await import(
  "../src/pages/public/charts/workspace.ts"
);
const { logicalViewport, temporalViewport, exactCandleAt, barCountdown } =
  await import("../src/shared/charts/navigation.ts");
const { normalizeStudySettings, priceSource } = await import(
  "../src/shared/charts/studySettings.ts"
);
const { calculateIndicator } = await import(
  "../src/shared/market/indicatorEngine.ts"
);
const { observationCandles, weightedFunding } = await import(
  "../src/shared/charts/marketStudyData.ts"
);
const { fetchMarketCandles, subscribeMarketCandles } = await import(
  "../src/shared/charts/marketData.ts"
);

test("legacy workspace migrates all chart settings to independent persistent window identities", () => {
  const old = {
    version: 2,
    layout: 4,
    symbols: ["BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT"],
    exchange: "binance",
    market: "spot",
    interval: "15m",
    indicators: ["ema", "macd"],
    instances: [{ key: "ema200", id: "ema", period: 200, color: "#fbbf24" }],
    periods: { ema: 50 },
    volume: false,
    log: true,
  };
  const next = normalizeWorkspace(old);
  assert.equal(next.version, 3);
  assert.equal(new Set(next.windows.map((w) => w.id)).size, 9);
  assert.deepEqual(
    next.windows.slice(0, 4).map((w) => w.symbol),
    old.symbols,
  );
  for (const w of next.windows) {
    assert.equal(w.interval, "15m");
    assert.equal(w.exchange, "binance");
    assert.equal(w.periods.ema, 50);
    assert.equal(w.volume, false);
    assert.equal(w.instances[0].period, 200);
  }
  const changed = patchWindow(next, { interval: "1d", symbol: "DOGEUSDT" });
  assert.equal(changed.windows[0].symbol, "DOGEUSDT");
  assert.equal(changed.windows[1].symbol, "ETHUSDT");
  assert.equal(changed.windows[1].interval, "15m");
});
test("sync changes only requested fields and leaves independent studies intact", () => {
  const initial = normalizeWorkspace(null);
  initial.layout = 4;
  initial.active = 2;
  initial.sync.symbol = true;
  initial.windows[1].indicators = ["macd"];
  const next = patchWindow(initial, {
    symbol: "LINKUSDT",
    exchange: "binance",
    interval: "4h",
    indicators: ["atr"],
  });
  assert.ok(
    next.windows.every(
      (w) => w.symbol === "LINKUSDT" && w.exchange === "binance",
    ),
  );
  assert.deepEqual(next.windows[1].indicators, ["macd"]);
  assert.equal(next.windows[1].interval, "1h");
  next.sync.interval = true;
  const synced = patchWindow(next, { interval: "5m" });
  assert.ok(synced.windows.every((w) => w.interval === "5m"));
});
test("complete workspace survives round trip and malformed imports cannot break the terminal", () => {
  const w = normalizeWorkspace(null);
  w.layout = 9;
  w.sidebarWidth = 350;
  w.favorites = [];
  w.windows[0].studySettings.macd = {
    parameters: { fast: 8, slow: 21, signal: 5 },
    components: { signal: { color: "#ff0000", hidden: true } },
    levels: [0],
  };
  w.windows[0].panels = {
    order: ["macd", "volume"],
    heights: { price: 400, macd: 130, volume: 70 },
    scales: { macd: { inverted: true } },
    maximized: "macd",
  };
  const result = normalizeWorkspace(JSON.parse(JSON.stringify(w)));
  assert.deepEqual(
    result.windows[0].studySettings.macd,
    w.windows[0].studySettings.macd,
  );
  assert.equal(result.windows[0].panels.heights.macd, 130);
  assert.deepEqual(result.favorites, []);
  const invalid = normalizeWorkspace({
    version: 3,
    layout: 100,
    active: -1,
    windows: [
      {
        id: "same",
        symbol: "../../bad",
        interval: "0m",
        instances: [
          { key: "ema", id: "ema" },
          { key: "x", id: "no" },
        ],
        studySettings: {
          rsi: { period: -5, color: "javascript:bad", width: 99 },
        },
        panels: { heights: { rsi: -1 } },
      },
    ],
  });
  assert.equal(invalid.layout, 1);
  assert.equal(invalid.windows[0].symbol, "BTCUSDT");
  assert.deepEqual(invalid.windows[0].instances, []);
  assert.deepEqual(invalid.windows[0].studySettings.rsi, {});
  assert.equal(normalizeWorkspace(null).windows[0].periods.ema, 50);
  for (let n = 1; n <= 9; n++) {
    const shape = layoutShape(n);
    assert.ok(shape.rows * shape.columns >= n);
  }
});
test("time synchronization round trips across gaps and different intervals without fabricated prices", () => {
  const candles = [
    { time: 1000, close: 10 },
    { time: 1060, close: 12 },
    { time: 1240, close: 14 },
  ];
  const range = { from: 0.5, to: 3 };
  const temporal = temporalViewport(candles, range, 60);
  assert.deepEqual(temporal, { from: 1030, to: 1300 });
  assert.deepEqual(logicalViewport(candles, temporal, 60), range);
  assert.equal(exactCandleAt(candles, 1180, 60), null);
  assert.equal(exactCandleAt(candles, 1070, 60).close, 12);
  assert.equal(barCountdown(1000, 60, 1020), "00:40");
  assert.equal(barCountdown(1000, 60, 2000), "закрыта");
});
test("MACD independent inputs and price sources affect calculated data", async () => {
  const candles = Array.from({ length: 100 }, (_, n) => ({
    time: 1000 + n * 60,
    open: 100 + n / 3,
    close: 101 + n / 3 + Math.sin(n),
    high: 104 + n / 3,
    low: 98 + n / 3,
    volume: 10,
  }));
  const standard = await calculateIndicator(candles, "macd");
  const changed = await calculateIndicator(candles, "macd", undefined, 60, {
    parameters: { fast: 8, slow: 21, signal: 5 },
  });
  assert.notDeepEqual(
    changed.series.find((s) => s.key === "macd").data,
    standard.series.find((s) => s.key === "macd").data,
  );
  const close = await calculateIndicator(candles, "sma", 10),
    open = await calculateIndicator(candles, "sma", 10, 60, { source: "open" });
  assert.notDeepEqual(close.series[0].data, open.series[0].data);
  assert.equal(priceSource({ open: 2, high: 6, low: 2, close: 4 }, "hlc3"), 4);
  assert.deepEqual(
    normalizeStudySettings({ period: -1, color: "bad", levels: [NaN, 30, 70] })
      .levels,
    [30, 70],
  );
});
test("one observation cannot masquerade as intrabar OHLC", () => {
  assert.deepEqual(observationCandles([{ time: 1000, value: 10 }], 3600), []);
  assert.deepEqual(
    observationCandles(
      [
        { time: 1000, value: 10 },
        { time: 1000, value: 20 },
      ],
      3600,
    ),
    [],
  );
  assert.equal(
    observationCandles(
      [
        { time: 1000, value: 10 },
        { time: 1060, value: 20 },
      ],
      3600,
    )[0].close,
    20,
  );
});
test("funding weights use quote OI and require full common source coverage", () => {
  assert.deepEqual(
    weightedFunding(
      [
        { time: 100, value: 0.01 },
        { time: 200, value: 0.5 },
      ],
      [{ time: 100, value: -0.02 }],
      [{ time: 100, value: 200 }],
      [{ time: 100, value: 100 }],
    ),
    [{ time: 100, value: 0 }],
  );
  assert.deepEqual(
    weightedFunding(
      [{ time: 100, value: 0.01 }],
      [{ time: 100, value: 0.02 }],
      [{ time: 100, value: 200 }],
      [],
    ),
    [],
  );
  assert.deepEqual(
    weightedFunding(
      [{ time: 100, value: 0.01 }],
      [{ time: 100, value: 0.02 }],
      [{ time: 100, value: NaN }],
      [{ time: 100, value: 100 }],
    ),
    [],
  );
});
test("parallel windows share one REST request without one cancellation aborting another", async () => {
  const original = globalThis.fetch;
  let finish,
    requests = 0,
    signal;
  globalThis.fetch = async (_url, init) => {
    requests++;
    signal = init.signal;
    return new Promise((resolve) => {
      finish = () =>
        resolve({
          ok: true,
          json: async () => ({
            result: { list: [[1000000, "10", "12", "9", "11", "5"]] },
          }),
        });
    });
  };
  try {
    const a = new AbortController(),
      b = new AbortController(),
      i = { exchange: "bybit", market: "linear", symbol: "BTCUSDT" };
    const first = fetchMarketCandles(i, "1h", a.signal),
      second = fetchMarketCandles(i, "1h", b.signal);
    a.abort();
    await assert.rejects(first, { name: "AbortError" });
    assert.equal(requests, 1);
    assert.equal(signal.aborted, false);
    finish();
    assert.equal((await second)[0].close, 11);
  } finally {
    globalThis.fetch = original;
  }
});
test("duplicate chart subscriptions share a socket until the last subscriber leaves", () => {
  const original = globalThis.WebSocket;
  let sockets = 0,
    closed = 0,
    transport;
  class Socket {
    static OPEN = 1;
    readyState = 1;
    constructor() {
      sockets++;
      transport = this;
    }
    send() {}
    close() {
      closed++;
      this.onclose?.();
    }
  }
  globalThis.WebSocket = Socket;
  let stopA, stopB;
  const a = [],
    b = [];
  try {
    const i = { exchange: "bybit", market: "linear", symbol: "ETHUSDT" };
    stopA = subscribeMarketCandles(
      i,
      "5m",
      (c) => a.push(c),
      () => {},
    );
    stopB = subscribeMarketCandles(
      i,
      "5m",
      (c) => b.push(c),
      () => {},
    );
    assert.equal(sockets, 1);
    transport.onopen();
    transport.onmessage({
      data: JSON.stringify({
        data: [
          {
            start: 1000000,
            open: "10",
            high: "12",
            low: "9",
            close: "11",
            volume: "5",
          },
        ],
      }),
    });
    assert.equal(a.length, 1);
    assert.equal(b.length, 1);
    stopA();
    stopA = null;
    assert.equal(closed, 0);
    stopB();
    stopB = null;
    assert.equal(closed, 1);
  } finally {
    stopA?.();
    stopB?.();
    globalThis.WebSocket = original;
  }
});
