type Range = { from: number; to: number };
type Scale = {
  options(): { mode: number; minMove?: number };
  getVisibleRange(): Range | null;
  setVisibleRange(range: Range): void;
  applyOptions(options: { autoScale: boolean }): void;
};

// Lightweight Charts 5.2.1 reads logarithmic ranges in price units but its
// setter accepts log-space values. Convert at this boundary and verify the
// round trip, rather than exponentiating a saved price/volume a second time.
export function restorePriceRange(scale: Scale, range: Range) {
  try {
    let target = range;
    if (scale.options().mode === 1) {
      const current = scale.getVisibleRange();
      if (
        !current ||
        !Number.isFinite(current.from) ||
        !Number.isFinite(current.to)
      )
        throw new Error("Invalid log range");
      const diff = Math.abs(current.to - current.from);
      const offset =
        4 +
        (diff >= 1 || diff < 1e-15 ? 0 : Math.ceil(Math.abs(Math.log10(diff))));
      const coordinate = 10 ** -offset;
      const log = (price: number) =>
        Math.abs(price) < 1e-15
          ? 0
          : Math.sign(price) *
            (Math.log10(Math.abs(price) + coordinate) + offset);
      target = { from: log(range.from), to: log(range.to) };
    }
    scale.setVisibleRange(target);
    const restored = scale.getVisibleRange();
    const tolerance = Math.max(0.02, Math.abs(range.to - range.from) * 0.001);
    if (
      !restored ||
      !Number.isFinite(restored.from) ||
      !Number.isFinite(restored.to) ||
      Math.abs(restored.from - range.from) > tolerance ||
      Math.abs(restored.to - range.to) > tolerance
    )
      throw new Error("Range conversion mismatch");
    return true;
  } catch {
    scale.applyOptions({ autoScale: true });
    return false;
  }
}
