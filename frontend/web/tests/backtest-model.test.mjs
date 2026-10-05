import test from "node:test";
import assert from "node:assert/strict";
import {
  defaultRequest,
  normalizeRequest,
  requestSchema,
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
  r.timeframe = "4h";
  r.strategy.entry.children[0].windowHours = 3;
  assert.match(validateRequest(r), /кратно/);
  r.strategy.entry.children[0].windowHours = 4;
  assert.equal(validateRequest(r), null);
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
test("timeframes, invalid grouping, exits, fees and Short restrictions", () => {
  for (const timeframe of ["5m", "15m"]) {
    const r = { ...defaultRequest(), timeframe };
    assert.equal(normalizeRequest(r).timeframe, "1h");
    assert.ok(validateRequest(r));
  }
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
