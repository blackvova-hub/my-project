import assert from "node:assert/strict";
import { after, test } from "node:test";
import { fileURLToPath } from "node:url";

import { createServer } from "vite";

const root = fileURLToPath(new URL("../", import.meta.url));
const vite = await createServer({
  root,
  configFile: false,
  appType: "custom",
  logLevel: "silent",
  server: { middlewareMode: true },
});
const { ALERT_WINDOW_MAX_MINUTES, isValidAlertWindowMinutes } =
  await vite.ssrLoadModule("/src/pages/public/scanners/alertWindow.ts");

after(async () => {
  await vite.close();
});

test("alert window production boundaries are 1..1440", () => {
  assert.equal(ALERT_WINDOW_MAX_MINUTES, 1440);
  for (const boundary of [
    { value: 0, valid: false },
    { value: 1, valid: true },
    { value: 720, valid: true },
    { value: 721, valid: true },
    { value: 1440, valid: true },
    { value: 1441, valid: false },
  ]) {
    assert.equal(
      isValidAlertWindowMinutes(boundary.value, ALERT_WINDOW_MAX_MINUTES),
      boundary.valid,
      `unexpected result for ${boundary.value}`,
    );
  }
});
