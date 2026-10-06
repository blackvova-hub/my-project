import test from "node:test";
import assert from "node:assert/strict";
import { webcrypto } from "node:crypto";
import { createRequestKey } from "../src/pages/public/backtest/requestKey.ts";

test("HTTP crypto without randomUUID produces unique valid UUIDv4 request keys", () => {
  const source = { getRandomValues: webcrypto.getRandomValues.bind(webcrypto) };
  const keys = new Set(Array.from({ length: 1000 }, () => createRequestKey(source)));
  assert.equal(keys.size, 1000);
  for (const key of keys) assert.match(key, /^[\da-f]{8}-[\da-f]{4}-4[\da-f]{3}-[89ab][\da-f]{3}-[\da-f]{12}$/);
});

test("native randomUUID is called with its crypto receiver", () => {
  const key = "12345678-1234-4234-8234-123456789abc";
  const source = { randomUUID() { assert.equal(this, source); return key; } };
  assert.equal(createRequestKey(source), key);
});

test("missing secure random source fails clearly instead of using Math.random", () => {
  assert.throws(() => createRequestKey({}), /Браузер не поддерживает/);
});
