/** Compact displays share one renderer; terminal-sized charts use ChartSurface. */
export function Sparkline({
  values,
  positive = true,
  height = 56,
  label = "Динамика показателя",
}: {
  values: readonly number[];
  positive?: boolean;
  height?: number;
  label?: string;
}) {
  const data = values.filter(Number.isFinite);
  if (data.length < 2)
    return (
      <div style={{ height }} className="ct-muted">
        Недостаточно данных
      </div>
    );
  let min = Infinity,
    max = -Infinity;
  for (const v of data) {
    min = Math.min(min, v);
    max = Math.max(max, v);
  }
  const spread = max - min || 1;
  const points = data
    .map(
      (v, i) =>
        `${(i / (data.length - 1)) * 300},${height - 4 - ((v - min) / spread) * (height - 8)}`,
    )
    .join(" ");
  return (
    <svg
      viewBox={`0 0 300 ${height}`}
      width="100%"
      height={height}
      role="img"
      aria-label={label}
      preserveAspectRatio="none"
    >
      <polyline
        points={points}
        fill="none"
        stroke={positive ? "var(--primary)" : "var(--destructive)"}
        strokeWidth={1.5}
        vectorEffect="non-scaling-stroke"
        strokeLinejoin="round"
      />
    </svg>
  );
}
