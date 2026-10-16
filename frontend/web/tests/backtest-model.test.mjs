import test from "node:test";
import assert from "node:assert/strict";
import {
  defaultRequest,
  normalizeRequest,
  requestSchema,
  readDraft,
  saveDraft,
  validateRequest,
} from "../src/pages/public/backtest/model.ts";
import {
  indicators,
  makeRule,
  ruleError,
  hasWindow,
  describeRule,
} from "../src/pages/public/backtest/rules.ts";

test("every indicator and measure has a valid JSON contract", () => {
  assert.equal(Object.keys(indicators).length, 17);
  for (const [name, def] of Object.entries(indicators))
    for (const measure of def.measures) {
      const r = defaultRequest(),
        rule = makeRule(name, measure.value);
      r.strategy.entry.children = [rule];
      assert.equal(ruleError(rule), null, `${name}/${measure.value}`);
      assert.equal(validateRequest(r), null, `${name}/${measure.value}`);
      assert.deepEqual(requestSchema.parse(JSON.parse(JSON.stringify(r))), r);
      assert.equal(
        normalizeRequest(r).strategy.entry.children[0].threshold,
        rule.threshold,
      );
      assert.ok(!describeRule(rule).includes("undefined"));
    }
});
test("numeric-left legacy RSI comparisons migrate without changing meaning", () => {
  const reverse = {
    lt: "gt",
    gt: "lt",
    lte: "gte",
    gte: "lte",
    crosses_above: "crosses_below",
    crosses_below: "crosses_above",
  };
  for (const [operator, expected] of Object.entries(reverse)) {
    const r = defaultRequest();
    r.strategy.version = 1;
    r.strategy.entry = {
      kind: "compare",
      operator,
      left: { kind: "constant", value: 0 },
      right: { kind: "rsi", period: 14 },
    };
    const migrated = normalizeRequest(r),
      c = migrated.strategy.entry.children[0];
    assert.equal(c.kind, "indicator");
    assert.equal(c.indicator, "rsi");
    assert.equal(c.threshold, 0);
    assert.equal(c.operator, expected);
    assert.equal(validateRequest(migrated), null);
    assert.equal(r.strategy.entry.left.kind, "constant");
  }
});
test("legacy unsupported rules are preserved for review and cannot silently run", () => {
  for (const condition of [
    { kind: "smc", event: "bos", direction: "bullish", withinBars: 1 },
    {
      kind: "compare",
      operator: "gte",
      left: { kind: "close" },
      right: { kind: "constant", value: 100 },
    },
  ]) {
    const r = defaultRequest();
    r.strategy.entry = condition;
    const normalized = normalizeRequest(r);
    assert.deepEqual(normalized.strategy.entry.children[0], condition);
    assert.match(validateRequest(normalized), /прежнего формата/);
  }
});
test("window units, Spot OI and every indicator-specific bound are validated", () => {
  const r = defaultRequest();
  for (const hours of [1, 3, 7, 719, 720]) {
    r.strategy.entry.children[0].windowHours = hours;
    assert.equal(validateRequest(r), null, `${hours}-hour window`);
  }
  r.market = "spot";
  r.strategy.entry.children = [makeRule("openInterest")];
  assert.match(validateRequest(r), /Futures/);
  for (const [name, def] of Object.entries(indicators))
    for (const measure of def.measures) {
      const rule = makeRule(name, measure.value);
      if (hasWindow(rule)) assert.ok(ruleError({ ...rule, windowHours: 0 }));
      if (rule.period) assert.ok(ruleError({ ...rule, period: 1 }));
      assert.ok(ruleError({ ...rule, threshold: NaN }));
      if (rule.measure === "change_pct") {
        assert.ok(ruleError({ ...rule, threshold: -5 }));
        assert.ok(ruleError({ ...rule, direction: undefined }));
      }
    }
  assert.ok(ruleError({ ...makeRule("rsi"), threshold: 101 }));
  assert.ok(
    ruleError({ ...makeRule("cvd", "imbalance_pct"), threshold: -101 }),
  );
  assert.ok(
    ruleError({ ...makeRule("macd", "signal_cross"), operator: "gte" }),
  );
});
test("minute defaults, legacy imports and normalization leave saved jobs unchanged", () => {
  assert.equal(defaultRequest().timeframe, "1m");
  for (const timeframe of ["1m", "5m", "15m", "1h", "4h"]) {
    const savedJob = {
      request: { ...defaultRequest(), timeframe },
      result: { engineVersion: "saved", equity: [{ time: 123, value: 100 }] },
    };
    const before = JSON.stringify(savedJob);
    const imported = requestSchema.parse(JSON.parse(JSON.stringify(savedJob.request)));
    const next = normalizeRequest(imported);
    assert.equal(next.timeframe, "1m");
    assert.equal(validateRequest(next), null);
    assert.equal(JSON.stringify(savedJob), before);
    assert.equal(imported.timeframe, timeframe);
    if (timeframe !== "1m") assert.match(validateRequest(imported), /минутные свечи/);
  }
});

test("legacy local drafts restore and persist as minute requests without mutating the source", () => {
  const original = { ...defaultRequest(), timeframe: "4h" };
  const values = new Map([["backtest:draft:v1:test-user", JSON.stringify(original)]]);
  const priorStorage = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: {
      getItem: (key) => values.get(key) ?? null,
      setItem: (key, value) => values.set(key, value),
    },
  });
  try {
    assert.equal(readDraft("test-user").timeframe, "1m");
    saveDraft("test-user", original);
    assert.equal(JSON.parse(values.get("backtest:draft:v1:test-user")).timeframe, "1m");
    assert.equal(original.timeframe, "4h");
  } finally {
    if (priorStorage) Object.defineProperty(globalThis, "localStorage", priorStorage);
    else delete globalThis.localStorage;
  }
});

test("minute periods require closed aligned bars and at least one possible next-open entry", () => {
  const r = defaultRequest();
  r.to = new Date(Date.parse(r.from) + 2 * 60_000).toISOString();
  assert.equal(validateRequest(r), null);
  r.to = new Date(Date.parse(r.from) + 60_000).toISOString();
  assert.match(validateRequest(r), /завершённый период/);
  r.to = new Date(Date.parse(r.from) + 2 * 60_000 + 1000).toISOString();
  assert.match(validateRequest(r), /завершённый период/);
});

test("only native hourly sources are marked as unavailable between closed hourly boundaries", () => {
  assert.deepEqual(
    Object.entries(indicators).filter(([, value]) => value.hourly).map(([name]) => name),
    ["openInterest", "cvd", "delta", "tradeBuyVolume", "tradeSellVolume"],
  );
});

test("invalid grouping, exits, fees and Short restrictions", () => {
  const r = defaultRequest();
  r.strategy.entry.children = [];
  assert.ok(validateRequest(r));
  r.strategy.entry.children = [makeRule()];
  r.strategy.exit = {
    kind: "or",
    children: [makeRule("rsi"), makeRule("cvd")],
  };
  assert.equal(validateRequest(r), null);
  r.strategy.feePct = NaN;
  assert.ok(validateRequest(r));
  r.strategy.feePct = 0.1;
  r.market = "spot";
  r.strategy.direction = "short";
  assert.ok(validateRequest(r));
});
