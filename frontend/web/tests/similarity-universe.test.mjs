import test from "node:test";
import assert from "node:assert/strict";
import { majorSymbols, universeOptions, universeRequest, universeFromRequest } from "../src/shared/market/similarityUniverse.ts";

test("flat options include real sectors and preserve the server search contract", () => {
  const choices = universeOptions(["meme", "defi", "ai", "l1", "l2", "rwa", "gaming", "new_sector", "meme"]);
  assert.equal(choices.filter((choice) => choice.value === "sector:meme").length, 1);
  assert.equal(choices.some((choice) => choice.value === "sector"), false);
  assert.equal(choices.find((choice) => choice.value === "sector:meme").label, "Мемкоинов");
  for (const sector of ["meme", "defi", "ai", "l1", "l2", "rwa", "gaming", "new_sector"]) {
    const value = `sector:${sector}`;
    assert.ok(choices.some((choice) => choice.value === value));
    assert.deepEqual(universeRequest(value, "OLDUSDT"), { scope: "sector", sector });
  }
  for (const scope of ["same_asset", "all_crypto", "alts"]) assert.deepEqual(universeRequest(scope, "OLDUSDT"), { scope });
});

test("major preset and user list remain separate and contain no stale sector", () => {
  assert.deepEqual(universeRequest("majors", "OLDUSDT"), { scope: "custom", symbols: majorSymbols });
  assert.deepEqual(universeRequest("custom", "ethusdt, solusdt; BTCUSDT"), { scope: "custom", symbols: ["ETHUSDT", "SOLUSDT", "BTCUSDT"] });
  const copy = universeRequest("majors", "");
  copy.symbols.pop();
  assert.equal(majorSymbols.length, 5);
});

test("saved sector searches and sorted major presets restore their visible choice", () => {
  assert.equal(universeFromRequest({ scope: "sector", sector: "DEFI" }), "sector:defi");
  assert.equal(universeFromRequest({ scope: "custom", symbols: [...majorSymbols].sort() }), "majors");
  assert.equal(universeFromRequest({ scope: "custom", symbols: ["ETHUSDT"] }), "custom");
  assert.equal(universeFromRequest({ scope: "sector", sector: "" }), "same_asset");
  assert.equal(universeFromRequest(), "same_asset");
});
