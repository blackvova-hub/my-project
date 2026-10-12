import test from "node:test";
import assert from "node:assert/strict";
import { registerHooks } from "node:module";
// The project uses bundler-style TS imports; resolve those in the Node test runner.
registerHooks({
  resolve(specifier, context, next) {
    try {
      return next(specifier, context);
    } catch (error) {
      if (
        error.code === "ERR_MODULE_NOT_FOUND" &&
        specifier.startsWith(".") &&
        !/\.[a-z]+$/i.test(specifier)
      )
        return next(specifier + ".ts", context);
      throw error;
    }
  },
});
const math = await import("../src/shared/market/indicatorMath.ts");
const { calculateExtended } = await import(
  "../src/shared/market/indicatorExtended.ts"
);
const { EXTRA_INDICATORS } = await import(
  "../src/shared/market/indicatorCatalogExtra.ts"
);
const { INDICATOR_IDS, INDICATOR_CATALOG, defaultIndicatorPeriod } =
  await import("../src/shared/market/indicatorTypes.ts");
const { calculateVolumeProfile } = await import(
  "../src/shared/charts/volumeProfile.ts"
);
const { takerRows, bucketObservations, observationCandles, fetchMarketStudy } =
  await import("../src/shared/charts/marketStudyData.ts");
const { derivativeAvailability } = await import(
  "../src/shared/charts/derivativeCatalog.ts"
);
const { comparePrices } = await import("../src/shared/charts/comparison.ts");
const { calculateIndicatorBatch } = await import(
  "../src/shared/market/indicatorBatch.ts"
);
const candle = (i, close, volume = 10) => ({
  time: 1767225600 + i * 3600,
  open: close - 0.2,
  high: close + 1,
  low: close - 1,
  close,
  volume,
});
const source = Array.from({ length: 700 }, (_, i) =>
  candle(i, 100 + i * 0.03 + Math.sin(i * 0.5) * 3, 10 + (i % 20)),
);
const near = (actual, expected, tolerance = 1e-8) =>
  assert.ok(
    Math.abs(actual - expected) < tolerance,
    `${actual} != ${expected}`,
  );
test("SMA, EMA and Wilder seed only from complete windows", () => {
  const x = [1, 2, 3, 4, 5];
  assert.ok(Number.isNaN(math.mean(x, 3)[1]));
  assert.deepEqual(math.mean(x, 3).slice(2), [2, 3, 4]);
  assert.deepEqual(math.smooth(x, 3).slice(2), [2, 3, 4]);
  near(math.smooth(x, 3, true)[3], 8 / 3);
  near(math.weighted(x, 3)[4], 26 / 6);
  assert.ok(Number.isNaN(math.mean([1, NaN, 3, 4], 3)[3]));
});
test("rolling variance remains stable around large market prices", () => {
  const x = [100000, 100000.01, 100000.02, 100000.03, 100000.04];
  near(math.deviation(x, 3)[4], Math.sqrt(2 / 3) * 0.01, 1e-6);
  near(math.deviation([1, 2, 3], 3, true)[2], 1);
  assert.deepEqual(
    math.deviation(Array(50).fill(100), 5).slice(4),
    Array(46).fill(0),
  );
});
test("all added studies preserve finite ordered points and warm-up boundaries", () => {
  for (const item of EXTRA_INDICATORS) {
    for (const data of [
      source,
      [],
      [candle(0, 100)],
      Array.from({ length: 70 }, (_, i) => ({
        ...candle(i, 100, 0),
        open: 100,
        high: 100,
        low: 100,
      })),
    ]) {
      const output = calculateExtended(data, item.id);
      assert.equal(output.placement, item.placement);
      for (const s of output.series) {
        let last = 0;
        for (const p of s.data) {
          assert.ok(Number.isFinite(p.value), item.id);
          assert.ok(p.time > last, item.id);
          last = p.time;
        }
      }
    }
    if (!item.id.includes("volume_profile"))
      assert.ok(
        calculateExtended(source, item.id).series.some((s) => s.data.length),
        `No data: ${item.id}`,
      );
  }
});
test("future bars do not change previous study values; fractals wait for two bars", () => {
  for (const item of EXTRA_INDICATORS) {
    const prefix = calculateExtended(source.slice(0, 350), item.id),
      full = calculateExtended(source, item.id),
      cutoff = source[item.id === "fractals" ? 347 : 349].time;
    for (const s of prefix.series) {
      const actual = full.series
        .find((v) => v.key === s.key)
        .data.filter((p) => p.time <= cutoff);
      const expected = s.data.filter((p) => p.time <= cutoff);
      assert.deepEqual(actual, expected, item.id);
    }
  }
});
test("OBV, ADL, BOP, VWMA and signed volume match independent hand calculations", () => {
  const data = [
    { ...candle(0, 2, 10), open: 1, high: 3, low: 1 },
    { ...candle(1, 3, 20), open: 2, high: 4, low: 2 },
    { ...candle(2, 2, 30), open: 3, high: 3, low: 1 },
  ];
  assert.deepEqual(
    calculateExtended(data, "obv").series[0].data.map((p) => p.value),
    [0, 20, -10],
  );
  assert.deepEqual(
    calculateExtended(data, "adl").series[0].data.map((p) => p.value),
    [0, 0, 0],
  );
  near(calculateExtended(data, "vwma", 2).series[0].data.at(-1).value, 2.4);
  assert.deepEqual(
    calculateExtended(data, "net_volume").series[0].data.map((p) => p.value),
    [20, -30],
  );
  near(calculateExtended(data, "bop").series[0].data.at(-1).value, -0.5);
});
test("Bollinger %B and width use population variance", () => {
  const data = [1, 2, 3].map((v, i) => candle(i, v));
  const sd = Math.sqrt(2 / 3);
  near(
    calculateExtended(data, "bb_percent", 3).series[0].data.at(-1).value,
    (1 + 2 * sd) / (4 * sd),
  );
  near(
    calculateExtended(data, "bb_width", 3).series[0].data.at(-1).value,
    200 * sd,
  );
});
test("volatility annualisation uses actual candle timeframe", () => {
  const hours = calculateExtended(
    source,
    "historical_volatility",
  ).series[0].data.at(-1).value;
  const days = calculateExtended(
    source.map((c, i) => ({ ...c, time: source[0].time + i * 86400 })),
    "historical_volatility",
  ).series[0].data.at(-1).value;
  near(hours / days, Math.sqrt(24));
});
test("zero-trend volatility includes a constant log-return trend", () => {
  const data = Array.from({ length: 100 }, (_, i) =>
    candle(i, 100 * Math.exp(i * 0.001)),
  );
  assert.ok(
    calculateExtended(data, "historical_volatility").series[0].data.at(-1)
      .value < 1e-5,
  );
  assert.ok(
    calculateExtended(data, "zero_trend_volatility").series[0].data.at(-1)
      .value > 1,
  );
});
test("volume profiles conserve source volume including zero-range candles", () => {
  const data = [
    candle(0, 100, 25),
    { ...candle(1, 100, 75), high: 100, low: 100, open: 100 },
  ];
  const bins = calculateVolumeProfile(data);
  near(
    bins.reduce((s, b) => s + b.volume, 0),
    100,
  );
  assert.equal(bins.length, 32);
  assert.deepEqual(calculateVolumeProfile([]), []);
});
test("catalogue has unique validated IDs and matching default periods", () => {
  assert.equal(new Set(INDICATOR_IDS).size, INDICATOR_IDS.length);
  assert.equal(INDICATOR_CATALOG.length, INDICATOR_IDS.length);
  assert.equal(defaultIndicatorPeriod("volume_profile"), 100);
  assert.equal(defaultIndicatorPeriod("ema"), 20);
  assert.equal(defaultIndicatorPeriod("macd"), undefined);
});
const klines = [
  [
    1767225600000,
    "100",
    "110",
    "90",
    "105",
    "10",
    1767229199999,
    "1000",
    50,
    "7",
    "720",
    0,
  ],
  [
    1767229200000,
    "105",
    "110",
    "100",
    "108",
    "20",
    1767232799999,
    "2100",
    60,
    "8",
    "850",
    0,
  ],
];

test("price ratios, spreads and returns only use real aligned observations", () => {
  const a = [
      { time: 1, close: 10 },
      { time: 2, close: 20 },
      { time: 3, close: 30 },
      { time: 4, close: 40 },
    ],
    b = [
      { time: 2, close: 5 },
      { time: 3, close: 6 },
      { time: 4, close: 0 },
    ];
  assert.deepEqual(comparePrices(a, b, "ratio"), [
    [
      { time: 2, value: 4 },
      { time: 3, value: 5 },
    ],
  ]);
  assert.deepEqual(comparePrices(a, b, "spread"), [
    [
      { time: 2, value: 15 },
      { time: 3, value: 24 },
    ],
  ]);
  const returns = comparePrices(a, b, "returns");
  near(returns[0][1].value, 50);
  near(returns[1][1].value, 20);
  assert.equal(returns[0].length, 2);
});
test("Chop Zone has neutral flat-price angles and directional color zones", () => {
  const flat = Array.from({ length: 70 }, (_, i) => candle(i, 100));
  const neutral = calculateExtended(flat, "chop_zone").series[0].data;
  assert.ok(neutral.length > 0);
  assert.ok(neutral.every((p) => p.value === 0 && p.color === "#fb923c"));
  const trend = Array.from({ length: 70 }, (_, i) => candle(i, 100 + i));
  assert.ok(
    calculateExtended(trend, "chop_zone").series[0].data.at(-1).value > 0,
  );
});
test("worker queue discards superseded queued snapshots and completes active work", async () => {
  const previous = globalThis.Worker,
    posted = [];
  let instance;
  class FakeWorker {
    constructor() {
      instance = this;
    }
    postMessage(value) {
      posted.push(value);
    }
    terminate() {}
  }
  globalThis.Worker = FakeWorker;
  try {
    const data = Array.from({ length: 2000 }, (_, i) =>
        candle(i, 100 + i * 0.01),
      ),
      a = new AbortController(),
      b = new AbortController(),
      c = new AbortController();
    const defs = (period) => [{ key: "wma", id: "wma", period }];
    const active = calculateIndicatorBatch(data, defs(3), a.signal, 3600);
    const stale = calculateIndicatorBatch(data, defs(4), b.signal, 3600).catch(
      (error) => error,
    );
    const latest = calculateIndicatorBatch(data, defs(5), c.signal, 3600);
    b.abort();
    a.abort();
    assert.equal((await stale).name, "AbortError");
    assert.equal(posted.length, 1);
    const reply = (request) =>
      instance.onmessage({
        data: {
          token: request.token,
          output: request.definitions.map((d) => ({
            key: d.key,
            indicator: calculateExtended(
              request.candles,
              d.id,
              d.period,
              request.barSeconds,
            ),
          })),
        },
      });
    reply(posted[0]);
    assert.equal((await active)[0].definition.period, 3);
    assert.equal(posted.length, 2);
    assert.equal(posted[1].definitions[0].period, 5);
    reply(posted[1]);
    near(
      (await latest)[0].indicator.series[0].data.at(-1).value,
      calculateExtended(data, "wma", 5).series[0].data.at(-1).value,
    );
  } finally {
    if (previous === undefined) delete globalThis.Worker;
    else globalThis.Worker = previous;
  }
});
test("taker volumes and values are read from exchange fields, never OHLC sign", () => {
  const rows = takerRows(klines);
  assert.deepEqual(
    rows.map((v) => [v.buy, v.sell, v.buyQuote, v.sellQuote]),
    [
      [7, 3, 720, 280],
      [8, 12, 850, 1250],
    ],
  );
  assert.throws(() => takerRows([klines[0].slice(0, 6)]));
});
test("missing provider values remain unavailable instead of becoming zero", async () => {
  const row = [...klines[0]];
  row[9] = null;
  assert.throws(() => takerRows([row]));
  await assert.rejects(
    () =>
      fetchMarketStudy(
        { exchange: "binance", market: "linear", symbol: "BTCUSDT" },
        "1h",
        "funding",
        async () => [{ fundingTime: 1767225600000, fundingRate: null }],
      ),
    /История отсутствует/,
  );
});
test("CVD accumulates true taker delta from first available candle", async () => {
  const i = { exchange: "binance", market: "spot", symbol: "BTCUSDT" };
  const cvd = await fetchMarketStudy(i, "1h", "cvd", async () => klines);
  assert.deepEqual(
    cvd.data.map((v) => v.value),
    [4, 0],
  );
  assert.match(cvd.note, /CVD от/);
  const values = await fetchMarketStudy(
    i,
    "1h",
    "taker_value_delta",
    async () => klines,
  );
  assert.deepEqual(
    values.data.map((v) => v.value),
    [440, -400],
  );
});
test("observed candle OHLC uses all snapshots while lines use last snapshot", () => {
  const data = [
    { time: 1767225600, value: 10 },
    { time: 1767225660, value: 15 },
    { time: 1767225720, value: 8 },
    { time: 1767225780, value: 12 },
  ];
  assert.deepEqual(observationCandles(data, 3600), [
    { time: 1767225600, open: 10, high: 15, low: 8, close: 12 },
  ]);
  assert.deepEqual(bucketObservations(data, 3600), [
    { time: 1767225600, value: 12 },
  ]);
});
test("OI delta is measured between aligned interval closes", async () => {
  const rows = [
    { timestamp: 1767225600000, sumOpenInterest: "100" },
    { timestamp: 1767225660000, sumOpenInterest: "110" },
    { timestamp: 1767229200000, sumOpenInterest: "130" },
  ];
  const result = await fetchMarketStudy(
    { exchange: "binance", market: "linear", symbol: "BTCUSDT" },
    "1h",
    "oi_delta",
    async () => rows,
  );
  assert.deepEqual(result.data, [{ time: 1767229200, value: 20 }]);
});
test("volume aggregation only joins actual common candles", async () => {
  const bybit = {
    result: {
      list: [
        [1767225600000, "100", "110", "90", "105", "10", "500"],
        [1767222000000, "100", "110", "90", "105", "10", "123"],
      ],
    },
  };
  const result = await fetchMarketStudy(
    { exchange: "bybit", market: "spot", symbol: "BTCUSDT" },
    "1h",
    "aggregate_volume",
    async (url) => (url.includes("bybit") ? bybit : klines),
  );
  assert.deepEqual(result.data, [{ time: 1767225600, value: 1500 }]);
});
test("liquidation events sum within intervals and never fill unobserved minutes", async () => {
  const raw = {
    points: [
      { time: 1767225600, value: 5 },
      { time: 1767225660, value: 8 },
      { time: 1767232800, value: 10 },
    ],
  };
  const result = await fetchMarketStudy(
    { exchange: "bybit", market: "linear", symbol: "BTCUSDT" },
    "1h",
    "liquidations",
    async () => raw,
  );
  assert.deepEqual(result.data, [
    { time: 1767225600, value: 13 },
    { time: 1767232800, value: 10 },
  ]);
  assert.match(result.note, /неполное покрытие/);
});
test("provider and market restrictions are explicit instead of synthetic fallback", async () => {
  const i = { exchange: "bybit", market: "spot", symbol: "BTCUSDT" };
  assert.match(derivativeAvailability(i, "oi"), /фьючерсах/);
  assert.match(derivativeAvailability(i, "cvd"), /Binance/);
  assert.equal(derivativeAvailability(i, "fear_greed"), "");
  await assert.rejects(
    () =>
      fetchMarketStudy(i, "1h", "cvd", async () => {
        throw new Error("Should not request");
      }),
    /Binance/,
  );
});
