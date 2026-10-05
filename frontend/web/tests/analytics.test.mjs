import test from "node:test";
import assert from "node:assert/strict";
import { makeDemo } from "../src/pages/analytics/demo.ts";
import {
  portfolio,
  stats,
  filterTrades,
  group,
  insights,
  csvExport,
  equitySeries,
  DAY,
} from "../src/pages/analytics/model.ts";
const data = makeDemo();
test("demo ledger independently reconciles every wallet snapshot", () => {
  for (const s of data.snapshots) {
    const opening = s.connectionId === "demo-bybit" ? 58000 : 34000;
    const changes = data.ledger
      .filter((l) => l.connectionId === s.connectionId && l.at <= s.at)
      .reduce((a, l) => a + l.amount, 0);
    assert.ok(Math.abs(s.wallet - opening - changes) < 1e-7);
    assert.ok(Math.abs(s.equity - s.wallet - s.unrealized) < 1e-7);
  }
});
test("fills and fees reconcile to every completed demo trade", () => {
  for (const t of data.trades) {
    const buys = t.fills
      .filter((f) => f.action === "Entry" || f.action === "Add")
      .reduce((n, f) => n + f.quantity, 0);
    const sells = t.fills
      .filter((f) => f.action === "Exit" || f.action === "Partial close")
      .reduce((n, f) => n + f.quantity, 0);
    assert.ok(Math.abs(buys - sells) < 1e-7);
    assert.ok(Math.abs(t.fills.reduce((n, f) => n + f.fee, 0) - t.fees) < 1e-7);
    assert.ok(Math.abs(t.gross - t.fees + t.funding - t.net) < 1e-7);
  }
});
test("account and period filters isolate all sources and alter results", () => {
  const a = portfolio(data, "demo-bybit", "7D"),
    b = portfolio(data, "demo-binance", "30D");
  assert.ok(a.trades.length < b.trades.length);
  assert.ok(a.trades.every((t) => t.connectionId === "demo-bybit"));
  assert.ok(a.ledger.every((t) => t.connectionId === "demo-bybit"));
  assert.notEqual(a.equity, b.equity);
});
test("groups sum to the same net performance", () => {
  const p = portfolio(data, "", "30D");
  for (const dimension of [
    "symbol",
    "side",
    "exchange",
    "weekday",
    "hour",
    "leverage",
    "duration",
    "size",
  ])
    assert.ok(
      Math.abs(
        group(p.trades, dimension).reduce((n, g) => n + g.net, 0) - p.stats.net,
      ) < 1e-7,
    );
});
test("deposits increase equity without creating return or drawdown recovery", () => {
  const snap = (at, equity) => ({
    connectionId: "x",
    at,
    equity,
    wallet: equity,
    btcPrice: 100,
  });
  const ledger = [
    {
      connectionId: "x",
      at: 2 * DAY,
      category: "deposit",
      currency: "USDT",
      amount: 100,
    },
  ];
  const s = equitySeries(
    [snap(DAY, 100), snap(2 * DAY, 200), snap(3 * DAY, 180)],
    ledger,
    1,
    0,
  );
  assert.equal(s[1].returnPct, 0);
  assert.equal(s[1].net, 0);
  assert.ok(Math.abs(s[2].drawdown + 10) < 1e-7);
});
test("incomplete and open trades never affect performance KPIs", () => {
  const t = data.trades[0];
  assert.equal(
    stats([
      { ...t, complete: false },
      { ...t, closedAt: null },
    ]).n,
    0,
  );
});
test("staggered account snapshots apply cash flows only when that wallet updates", () => {
  const snap = (connectionId, at, equity) => ({
    connectionId,
    at,
    equity,
    wallet: equity,
  });
  const points = equitySeries(
    [
      snap("a", 100, 100),
      snap("b", 110, 100),
      snap("a", 200, 100),
      snap("b", 210, 200),
    ],
    [
      {
        connectionId: "b",
        at: 190,
        category: "deposit",
        currency: "USDT",
        amount: 100,
      },
    ],
    2,
    0,
  );
  assert.equal(points.length, 3);
  for (const p of points) {
    assert.equal(p.net, 0);
    assert.equal(p.drawdown, 0);
  }
});
test("heatmap drilldowns combine UTC day and hour and insight ids isolate affected trades", () => {
  const p = portfolio(data, "", "90D");
  const matches = filterTrades(p.trades, { weekday: "Пн", hour: "00–04" });
  assert.ok(matches.length > 0);
  assert.ok(
    matches.every(
      (t) =>
        new Date(t.openedAt).getUTCDay() === 1 &&
        new Date(t.openedAt).getUTCHours() < 4,
    ),
  );
  for (const i of insights(data, p)) {
    assert.equal(
      filterTrades(p.trades, { ids: i.trades }).length,
      i.trades.filter((id) => p.trades.some((t) => t.id === id)).length,
    );
  }
});
test("CSV neutralizes spreadsheet formulas and quotes annotations", () => {
  const csv = csvExport([
    { ...data.trades[0], tag: '=HYPERLINK("bad")', strategy: "a,b\nnext" },
  ]);
  assert.ok(csv.includes("'="));
  assert.ok(csv.includes('""bad""'));
  assert.ok(csv.includes("a,b\nnext"));
});
