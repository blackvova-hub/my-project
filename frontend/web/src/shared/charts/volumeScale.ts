export type PriceRange = { from: number; to: number };

// Manual zoom is retained while it remains related to the actual volume data.
// Damaged persisted/log-converted ranges must not render astronomical axes.
export function validVolumeRange(
  range: PriceRange | null | undefined,
  maximum: number,
) {
  if (!range) return true;
  const extent = Math.max(1, maximum);
  return (
    Number.isFinite(range.from) &&
    Number.isFinite(range.to) &&
    range.to > range.from &&
    range.to > 0 &&
    range.from <= extent &&
    Math.max(Math.abs(range.from), Math.abs(range.to), range.to - range.from) <=
      extent * 1000
  );
}

export function formatVolume(value: number) {
  if (!Number.isFinite(value)) return "—";
  const amount = Math.abs(value);
  // A scientific representation bounds label width beyond ordinary T units.
  if (amount >= 1e15) return value.toExponential(2);
  for (const [scale, suffix] of [
    [1e12, "T"],
    [1e9, "B"],
    [1e6, "M"],
    [1e3, "K"],
  ] as const)
    if (amount >= scale)
      return (value / scale).toFixed(2).replace(/\.?0+$/, "") + suffix;
  return value.toFixed(2).replace(/\.?0+$/, "");
}
