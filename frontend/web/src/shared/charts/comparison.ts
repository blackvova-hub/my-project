export type ComparisonMode = "returns" | "ratio" | "spread";
export function comparePrices(
  a: readonly { time: number; close: number }[],
  b: readonly { time: number; close: number }[],
  mode: ComparisonMode,
) {
  const right = new Map(b.map((p) => [p.time, p.close]));
  const pairs = a
    .flatMap((p) => {
      const other = right.get(p.time);
      return Number.isFinite(p.time) &&
        p.time > 0 &&
        Number.isFinite(p.close) &&
        p.close > 0 &&
        other !== undefined &&
        Number.isFinite(other) &&
        other > 0
        ? [{ time: p.time, a: p.close, b: other }]
        : [];
    })
    .sort((x, y) => x.time - y.time);
  if (!pairs.length) return [];
  if (mode === "returns")
    return [
      pairs.map((p) => ({ time: p.time, value: (p.a / pairs[0].a - 1) * 100 })),
      pairs.map((p) => ({ time: p.time, value: (p.b / pairs[0].b - 1) * 100 })),
    ];
  return [
    pairs.map((p) => ({
      time: p.time,
      value: mode === "ratio" ? p.a / p.b : p.a - p.b,
    })),
  ];
}
