import test from "node:test";
import assert from "node:assert/strict";
import {
  defaults,
  toDraft,
  parseDraft,
  queryString,
  isStale,
  duration,
  hydrateDataset,
  simpleSettings,
  simpleQuery,
  validSearch,
} from "../src/pages/public/arbitrage/model.ts";
test("search accepts real Unicode tickers and rejects separators and oversized input", () => {
  for (const value of ["", "BTC", "1INCH", "哈基米"])
    assert.equal(validSearch(value), true);
  for (const value of ["BTC/USDT", "BTC ETH", "?", "哈".repeat(11)])
    assert.equal(validSearch(value), false);
});
test("old preferences cannot silently hide simple arbitrage opportunities", () => {
  const s = simpleSettings({
    ...defaults,
    amount: 1000000,
    minSpread: 90,
    feeSpot: 1,
    sort: "net",
    assets: ["BTC"],
    exchanges: ["binance", "bybit"],
  });
  assert.equal(s.simple, true);
  assert.equal(s.minSpread, 0);
  assert.equal(s.sort, "spread");
  assert.deepEqual(s.assets, []);
  assert.equal(s.feeSpot, undefined);
  assert.deepEqual(s.exchanges, ["binance", "bybit"]);
  const q = new URLSearchParams(
    simpleQuery({ ...s, search: "INCH" }, 100, ["a".repeat(24)]),
  );
  assert.equal(q.get("search"), "INCH");
  assert.equal(q.get("offset"), "100");
  assert.equal(q.has("amount"), false);
  assert.equal(q.has("feeSpot"), false);
  assert.equal(
    simpleSettings({ ...defaults, exchanges: ["binance"] }).exchanges.length,
    6,
  );
});
test("invalid and empty numeric input never becomes a zero budget", () => {
  for (const amount of ["", " ", "NaN", "Infinity", "9", "1000001"])
    assert.ok(parseDraft({ ...toDraft(defaults), amount }).error);
  assert.equal(
    parseDraft({ ...toDraft(defaults), amount: "1000,5" }).settings.amount,
    1000.5,
  );
});
test("repeated legs reuse the same book while retaining route-specific prices", () => {
  const books = {
    a: {
      bids: [{ price: 99, quantity: 2 }],
      asks: [{ price: 100, quantity: 3 }],
    },
    b: {
      bids: [{ price: 101, quantity: 2 }],
      asks: [{ price: 102, quantity: 3 }],
    },
  };
  const rows = [1, 2].map((id) => ({
    id,
    buy: { instrument: { id: "a" }, price: 100 },
    sell: { instrument: { id: "b" }, price: 101 },
  }));
  const d = hydrateDataset({ books, rows });
  assert.equal(d.rows[0].buy.asks, d.rows[1].buy.asks);
  assert.equal(d.rows[0].sell.price, 101);
  assert.equal(rows[0].buy.asks, undefined);
  assert.throws(() => hydrateDataset({ books: {}, rows }), /стаканы/);
});
test("all six exchanges, modes, asset parsing and manual fees", () => {
  assert.equal(defaults.exchanges.length, 6);
  const result = parseDraft({
    ...toDraft(defaults),
    assets: "btc; ETH, btc sol",
    manualFees: true,
    feeSpot: "0",
    feePerpetual: "0,055",
  }).settings;
  assert.deepEqual(result.assets, ["BTC", "ETH", "SOL"]);
  assert.equal(result.feeSpot, 0);
  assert.equal(result.feePerpetual, 0.055);
  assert.equal(
    new URLSearchParams(queryString(result)).get("exchanges"),
    defaults.exchanges.join(","),
  );
  assert.ok(parseDraft({ ...toDraft(defaults), exchanges: [] }).error);
  assert.ok(parseDraft({ ...toDraft(defaults), assets: "BTC/USDT" }).error);
  assert.ok(
    parseDraft({ ...toDraft(defaults), manualFees: true, feeSpot: "" }).error,
  );
});
test("pause does not keep quotes fresh", () => {
  const row = {
    buy: { bookAt: 1000 },
    sell: { bookAt: 1100 },
    calculatedAt: 1200,
  };
  assert.equal(isStale(row, 10000, 10000), false);
  assert.equal(isStale(row, 11001, 10000), true);
  assert.equal(isStale(row, -1000, 10000), true);
  assert.equal(duration(0, 10000), "—");
  assert.equal(duration(1000, 62000), "1 мин");
});
test("saved settings round trip retains fee overrides and omits UI-only state", () => {
  const s = {
    ...defaults,
    strategy: "perpetual-perpetual",
    feeSpot: 0.08,
    feePerpetual: 0.05,
    assets: ["BTC"],
  };
  assert.deepEqual(parseDraft(toDraft(s)).settings, s);
  assert.equal(queryString(s).includes("manualFees"), false);
});
