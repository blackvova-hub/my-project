import test from "node:test";
import assert from "node:assert/strict";
import {
  initialState,
  replayReducer as reduce,
  floatingPnl,
  indexAtTime,
  replayIndexAtOrAfter,
  validateCandles,
} from "../src/pages/public/replay/model.ts";
const candles = Array.from({ length: 100 }, (_, i) => ({
  time: 1700000000 + i * 60,
  open: 100 + i,
  high: 102 + i,
  low: 99 + i,
  close: 101 + i,
  volume: 10,
}));
const loaded = () => reduce(initialState, { type: "load", candles });
const started = () => reduce(loaded(), { type: "start", index: 40 });

test("start hides entire future; step adds exactly one; pause does not advance; end is bounded", () => {
  let s = started();
  assert.equal(s.candles.slice(0, s.cursor + 1).length, 41);
  s = reduce(s, { type: "next" });
  assert.equal(s.cursor, 41);
  s = reduce(s, { type: "prev" });
  assert.equal(s.cursor, 40);
  s = reduce(s, { type: "toggle" });
  assert.equal(s.mode, "playing");
  s = reduce(s, { type: "toggle" });
  assert.equal(s.mode, "paused");
  assert.equal(s.cursor, 40);
  for (let i = 0; i < 200; i++) s = reduce(s, { type: "next" });
  assert.equal(s.cursor, 99);
  assert.equal(s.mode, "paused");
  assert.equal(reduce(s, { type: "toggle" }).mode, "paused");
});
test("background archive extension preserves the replay cursor, position and session", () => {
  let state = reduce(started(), { type: "open", side: "long", amount: 1000 });
  const extra = Array.from({ length: 20 }, (_, index) => ({ ...candles.at(-1), time: candles.at(-1).time + (index + 1) * 60 }));
  const oldPosition = state.position;
  state = reduce(state, { type: "append", candles: extra });
  assert.equal(state.candles.length, 120);
  assert.equal(state.cursor, 40);
  assert.equal(state.position, oldPosition);
  assert.equal(state.balance, 10000);
  assert.equal(reduce(state, { type: "append", candles: extra }), state);
});
test("selection cancellation keeps future hidden including at end; invalid starts and speeds are rejected", () => {
  for (const index of [0, 40, 99]) {
    const s = reduce(loaded(), { type: "start", index });
    const selected = reduce(s, { type: "select" });
    assert.equal(selected.cursor, index);
    assert.equal(reduce(selected, { type: "cancelSelect" }).mode, "paused");
  }
  const s = started();
  for (const index of [-1, 100, NaN, 0.5])
    assert.equal(reduce(s, { type: "start", index }), s);
  assert.equal(reduce(s, { type: "speed", value: 999 }), s);
});
test("long and short use only visible close; PnL has no hidden fees; one position and cash bound", () => {
  for (const side of ["long", "short"]) {
    let s = started();
    s = reduce(s, { type: "open", side, amount: 1410 });
    assert.equal(s.position.entry, 141);
    assert.equal(s.position.quantity, 10);
    assert.equal(s.balance, 10000);
    assert.equal(reduce(s, { type: "open", side, amount: 1 }), s);
    assert.equal(reduce(s, { type: "prev" }), s);
    s = reduce(s, { type: "next" });
    assert.equal(
      floatingPnl(s.position, candles[s.cursor].close),
      side === "long" ? 10 : -10,
    );
    s = reduce(s, { type: "close" });
    assert.equal(s.position, null);
    assert.equal(s.trades[0].exit, 142);
    assert.equal(s.trades[0].pnl, side === "long" ? 10 : -10);
    assert.equal(s.balance, side === "long" ? 10010 : 9990);
    assert.equal(reduce(s, { type: "prev" }), s);
  }
  for (const amount of [-1, 0, Infinity, 10001])
    assert.equal(
      reduce(started(), { type: "open", side: "long", amount })
        .position,
      null,
    );
  assert.equal(
    reduce(reduce(loaded(), { type: "exit" }), {
      type: "open",
      side: "long",
      amount: 10,
    }).position,
    null,
  );
});
test("opening an archive immediately enables replay with future bars; live orders pause at the displayed close", () => {
  let s = loaded();
  assert.equal(s.mode, "paused");
  assert.ok(s.cursor >= 0 && s.cursor < candles.length - 1);
  s = reduce(s, { type: "toggle" });
  assert.equal(s.mode, "playing");
  s = reduce(s, { type: "next" });
  const visible = candles[s.cursor];
  s = reduce(s, { type: "open", side: "long", amount: 1000 });
  assert.equal(s.mode, "paused");
  assert.equal(s.position.entry, visible.close);
  s = reduce(s, { type: "toggle" });
  s = reduce(s, { type: "next" });
  s = reduce(s, { type: "close" });
  assert.equal(s.mode, "paused");
  assert.equal(s.trades[0].exit, candles[s.cursor].close);
  for (const count of [0, 1, 2]) {
    const tiny = reduce(initialState, {
      type: "load",
      candles: candles.slice(0, count),
    });
    assert.equal(tiny.cursor, count ? 0 : -1);
  }
});
test("new start/load/exit reset paper account to prevent trading across timelines", () => {
  const s = reduce(started(), {
    type: "open",
    side: "short",
    amount: 1000,
  });
  for (const action of [
    { type: "start", index: 10 },
    { type: "load", candles },
    { type: "exit" },
  ]) {
    const next = reduce(s, action);
    assert.equal(next.position, null);
    assert.equal(next.balance, 10000);
    assert.deepEqual(next.trades, []);
  }
});
test("archive validation rejects duplicate/out-of-order/nonfinite/malformed candles; date search handles gaps", () => {
  assert.equal(validateCandles(candles), candles);
  for (const c of [
    { ...candles[1], time: candles[0].time },
    { ...candles[1], close: NaN },
    { ...candles[1], low: -1 },
    { ...candles[1], high: 1 },
  ])
    assert.throws(() => validateCandles([candles[0], c]));
  assert.equal(indexAtTime(candles, candles[10].time + 30), 10);
  assert.equal(replayIndexAtOrAfter(candles, candles[10].time + 30), 11);
  assert.equal(replayIndexAtOrAfter(candles, candles[10].time), 10);
  assert.equal(indexAtTime(candles, 1), -1);
});
