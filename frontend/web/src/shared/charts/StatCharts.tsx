import type { ReactNode } from "react";
export function StackedBar({
  segments,
  total,
  className = "an-allocation-bar",
  label,
}: {
  segments: { value: number; color: string; title?: string }[];
  total?: number;
  className?: string;
  label: string;
}) {
  const sum = total ?? segments.reduce((s, p) => s + Math.max(0, p.value), 0);
  return (
    <div className={className} role="img" aria-label={label}>
      {segments.map((p, i) => (
        <span
          key={i}
          title={p.title}
          style={{
            display: "block",
            height: "100%",
            width: `${sum > 0 ? (Math.max(0, p.value) / sum) * 100 : 0}%`,
            background: p.color,
          }}
        />
      ))}
    </div>
  );
}
export function MetricBar({ value, max }: { value: number; max: number }) {
  return (
    <span className="an-mini-bar" role="img" aria-label={`Значение ${value}`}>
      <i
        style={{
          width: `${Math.max(2, (Math.abs(value) / Math.max(max, 1)) * 100)}%`,
          background: value >= 0 ? "var(--primary)" : "var(--destructive)",
        }}
      />
    </span>
  );
}
export function HeatmapChart({
  columns,
  rows,
  cells,
  onSelect,
}: {
  columns: string[];
  rows: string[];
  cells: {
    row: string;
    column: string;
    value: number | null;
    label: ReactNode;
    title: string;
    ariaLabel: string;
  }[];
  onSelect: (row: string, column: string) => void;
}) {
  return (
    <div className="an-heatmap">
      <span />
      {columns.map((c) => (
        <span key={c}>{c}</span>
      ))}
      {rows.flatMap((row) => [
        <span className="an-hour" key={row}>
          {row}
        </span>,
        ...columns.map((column) => {
          const cell = cells.find((c) => c.row === row && c.column === column);
          return (
            <button
              key={row + column}
              disabled={cell?.value == null}
              aria-label={cell?.ariaLabel}
              title={cell?.title}
              style={{
                background:
                  cell?.value == null
                    ? undefined
                    : `color-mix(in srgb, ${cell.value >= 0 ? "var(--primary)" : "var(--destructive)"} ${Math.min(50, 12 + Math.abs(cell.value) / 100)}%, var(--card))`,
              }}
              onClick={() => onSelect(row, column)}
            >
              {cell?.label ?? "—"}
            </button>
          );
        }),
      ])}
    </div>
  );
}
