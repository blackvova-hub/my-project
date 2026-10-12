import { logicalIndexAtTime } from "../src/shared/charts/seriesData.ts";
import test from "node:test";
import assert from "node:assert/strict";
import {
  TOOLS,
  normalizeDrawing,
  positionMetrics,
} from "../src/shared/charts/drawings/model.ts";
import { drawingGeometry } from "../src/shared/charts/drawings/geometry.ts";
import {
  splitLineData,
  alignBarTime,
} from "../src/shared/charts/seriesData.ts";
const project = (p) => ({ x: ((p.time - 1000) / 60) * 8, y: 500 - p.price });
test("weekly derivative observations align to exchange Monday UTC candles", () => {
  const monday = Date.parse("2026-10-12T00:00:00Z") / 1000;
  assert.equal(alignBarTime(monday + 3 * 86400, 604800), monday);
  assert.equal(alignBarTime(monday + 3599, 3600), monday);
});
test("historical gaps and unavailable balances split real line segments instead of joining them", () => {
  assert.deepEqual(
    splitLineData(
      [
        { time: 100, value: 2 },
        { time: 130, value: 3 },
        { time: 200, value: null },
        { time: 400, value: 5 },
        { time: 600, value: 6 },
      ],
      90,
    ),
    [
      [
        { time: 100, value: 2 },
        { time: 130, value: 3 },
      ],
      [{ time: 400, value: 5 }],
      [{ time: 600, value: 6 }],
    ],
  );
});
test("all drawing families remain finite during partial and completed interactions", () => {
  for (const tool of TOOLS) {
    const count = Math.max(2, tool.points),
      points = Array.from({ length: count }, (_, i) => ({
        time: 1000 + i * 60,
        price: 100 + i * 7 + (i % 2) * 12,
      }));
    const drawing = {
      id: 1,
      type: tool.id,
      points: tool.points === 1 ? points.slice(0, 1) : points,
      color: "#61d99b",
      sample: Array.from({ length: 20 }, (_, i) => ({
        time: 1000 + i * 60,
        open: 100 + i,
        close: 101 + i,
        high: 105 + i,
        low: 98 + i,
        volume: 10 + i,
      })),
    };
    for (let n = 1; n <= drawing.points.length; n++) {
      const geo = drawingGeometry(
        { ...drawing, points: drawing.points.slice(0, n) },
        project,
        900,
        500,
      );
      for (const shape of geo.shapes) {
        assert.ok(
          !/(NaN|Infinity|undefined)/.test(shape.path ?? ""),
          `${tool.id}: nonfinite path at ${n} points`,
        );
        for (const key of ["x", "y", "rx", "ry"])
          if (shape[key] !== undefined)
            assert.ok(Number.isFinite(shape[key]), `${tool.id}: ${key}`);
      }
    }
    assert.ok(
      drawingGeometry(drawing, project, 900, 500).shapes.length,
      `${tool.id} rendered`,
    );
  }
});
test("long and short risk-reward use direction and quantity and reject inverted protection", () => {
  const long = {
    id: 1,
    type: "long",
    points: [
      { time: 1000, price: 100 },
      { time: 1060, price: 120 },
      { time: 1120, price: 90 },
    ],
    quantity: 3,
  };
  assert.deepEqual(positionMetrics(long), {
    reward: 20,
    risk: 10,
    valid: true,
    ratio: 2,
    profit: 60,
    loss: 30,
  });
  assert.equal(positionMetrics({ ...long, type: "short" }).valid, false);
  assert.equal(
    positionMetrics({
      ...long,
      type: "short",
      points: [
        long.points[0],
        { time: 1060, price: 80 },
        { time: 1120, price: 110 },
      ],
    }).ratio,
    2,
  );
});
test("Fibonacci extension starts at the third price and restored drawings reject invalid geometry", () => {
  const d = {
    id: 2,
    type: "fib-extension",
    points: [
      { time: 1000, price: 100 },
      { time: 1060, price: 120 },
      { time: 1120, price: 130 },
    ],
    levels: [1],
  };
  const label = drawingGeometry(d, project, 900, 500).shapes.find(
    (s) => s.kind === "text",
  );
  assert.match(label.text, /150/);
  assert.equal(
    normalizeDrawing({ ...d, points: [{ time: NaN, price: 100 }] }),
    null,
  );
  assert.equal(normalizeDrawing({ ...d, type: "unknown" }), null);
  assert.equal(
    normalizeDrawing({
      id: 1,
      type: "trend",
      points: [{ time: 1000, price: 20 }],
    }),
    null,
  );
});

test("drawing timestamps inside missing history project between actual adjacent bars", () => {
  const bars = [{ time: 1000 }, { time: 1060 }, { time: 1600 }];
  assert.equal(logicalIndexAtTime(bars, 1330, 60), 1.5);
  assert.equal(logicalIndexAtTime(bars, 1660, 60), 3);
  assert.equal(logicalIndexAtTime(bars, 940, 60), -1);
});
