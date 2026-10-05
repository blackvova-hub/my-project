import test from "node:test";
import assert from "node:assert/strict";
import { defaultRequest, validateRequest } from "../src/pages/public/backtest/model.ts";
import { backtestLimits, conditionUsage, canAdd } from "../src/pages/public/backtest/limits.ts";
import { makeRule } from "../src/pages/public/backtest/rules.ts";

test("symbol limits and unknown-plan fallback", () => {
  for (const [plan, count] of [["free", 1], [undefined, 1], ["unknown", 1], ["standard", 5], ["standart", 5], [" PRO ", 10]]) {
    const r = defaultRequest();
    r.symbols = Array.from({ length: count }, (_, i) => `COIN${i}USDT`);
    assert.equal(validateRequest(r, plan), null);
    r.symbols.push("EXTRAUSDT");
    assert.ok(validateRequest(r, plan));
  }
});

test("Free allows three rules across entry and exit, no nested groups", () => {
  const r = defaultRequest();
  r.strategy.entry.children.push(makeRule());
  r.strategy.exit = { kind: "and", children: [makeRule()] };
  assert.equal(validateRequest(r, "free"), null);
  const usage = conditionUsage(r.strategy.entry, r.strategy.exit);
  assert.deepEqual(usage, { conditions: 3, groups: 2, nodes: 5, depth: 0 });
  assert.equal(canAdd(usage, backtestLimits("free")), false);
  r.strategy.exit.children.push(makeRule());
  assert.match(validateRequest(r, "free"), /условий/);
  r.strategy.exit = undefined;
  r.strategy.entry.children = [{ kind: "or", children: [makeRule()] }];
  assert.match(validateRequest(r, "free"), /вложенные группы/);
  assert.equal(validateRequest(r, "standard"), null);
});

test("Standard budget includes exit rules, group count and nesting", () => {
  const r = defaultRequest();
  r.strategy.entry.children = Array.from({ length: 6 }, () => makeRule());
  r.strategy.exit = { kind: "or", children: Array.from({ length: 6 }, () => makeRule()) };
  assert.equal(validateRequest(r, "standard"), null);
  r.strategy.exit.children.push(makeRule());
  assert.match(validateRequest(r, "standard"), /условий/);
  assert.equal(validateRequest(r, "pro"), null);
  r.strategy.exit = undefined;
  r.strategy.entry.children = Array.from({ length: 6 }, () => ({ kind: "or", children: [makeRule()] }));
  assert.match(validateRequest(r, "standard"), /групп/);
  r.strategy.entry.children = [{ kind: "or", children: [{ kind: "and", children: [makeRule()] }] }];
  assert.match(validateRequest(r, "standard"), /уровней/);
  assert.equal(validateRequest(r, "pro"), null);
});

test("Pro retains 32-element budget and reserves both nodes for a new group", () => {
  const limits = backtestLimits("pro");
  assert.equal(canAdd({ conditions: 25, groups: 6, nodes: 31, depth: 2 }, limits), true);
  assert.equal(canAdd({ conditions: 25, groups: 6, nodes: 31, depth: 2 }, limits, true), false);
  assert.equal(canAdd({ conditions: 26, groups: 6, nodes: 32, depth: 2 }, limits), false);
  const r = defaultRequest();
  r.strategy.entry.children = Array.from({ length: 4 }, () => ({ kind: "or", children: Array.from({ length: 7 }, () => makeRule()) }));
  assert.ok(validateRequest(r, "pro"));
  r.strategy.entry.children[3].children.pop();
  assert.equal(validateRequest(r, "pro"), null);
});
