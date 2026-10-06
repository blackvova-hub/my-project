import test from "node:test";
import assert from "node:assert/strict";
import { verifiedNetProfit, verificationSnapshotFresh } from "../src/pages/public/arbitrage/verificationModel.ts";

test("metadata refresh and report receipt cannot freshen old quotes", () => {
  const now = 100000;
  const path = { checks: [{ key: "freshness", state: "pass", at: now - 1000 }] };
  assert.equal(verificationSnapshotFresh(now, path, now), true);
  assert.equal(verificationSnapshotFresh(now, { checks: [{ key: "freshness", state: "pass", at: now - 5001 }] }, now), false);
  assert.equal(verificationSnapshotFresh(now - 5001, path, now), false);
  assert.equal(verificationSnapshotFresh(now + 2001, path, now), false);
  assert.equal(verificationSnapshotFresh(now, { checks: [{ key: "freshness", state: "pass" }] }, now), false);
});

test("net profit stays hidden until every required check passes", () => {
  const keys = ["identity", "freshness", "trading", "wallet", "network", "delisting", "vwap", "fees", "account"];
  const path = { mode: "transfer", state: "verified", netProfit: 2, checks: keys.map((key) => ({ key, state: "pass" })) };
  assert.equal(verifiedNetProfit(path), 2);
  for (const key of keys) {
    for (const state of ["unknown", "fail", "na"]) {
      assert.equal(verifiedNetProfit({ ...path, checks: path.checks.map((c) => c.key === key ? { ...c, state } : c) }), null);
    }
    assert.equal(verifiedNetProfit({ ...path, checks: path.checks.filter((c) => c.key !== key) }), null);
  }
  assert.equal(verifiedNetProfit({ ...path, netProfit: null }), null);
  assert.equal(verifiedNetProfit({ ...path, netProfit: Infinity }), null);
  assert.equal(verifiedNetProfit({ ...path, state: "incomplete" }), null);
  assert.equal(verifiedNetProfit({ ...path, checks: [...path.checks, { key: "fees", state: "unknown" }] }), null);
  const prefunded = { ...path, mode: "prefunded", checks: path.checks.map((c) => ["wallet", "network"].includes(c.key) ? { ...c, state: "na" } : c) };
  assert.equal(verifiedNetProfit(prefunded), 2);
});
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
  assert.equal(q.get("amount"), "1000000");
  assert.equal(q.get("minAvailable"), "0");
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
test("availability filter survives preferences independently of calculation capital", () => {
  const s = simpleSettings({ ...defaults, minAvailable: 250.5, amount: 1000 });
  const q = new URLSearchParams(simpleQuery(s));
  assert.equal(s.minAvailable, 250.5);
  assert.equal(q.get("minAvailable"), "250.5");
  assert.equal(q.get("amount"), "1000");
  assert.deepEqual(
    simpleSettings({ ...defaults, strategy: "funding", exchanges: ["bybit"] })
      .exchanges,
    ["bybit"],
  );
  for (const value of [-1, NaN, Infinity, 1000001])
    assert.equal(
      simpleSettings({ ...defaults, minAvailable: value }).minAvailable,
      0,
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
