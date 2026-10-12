import { useId } from "react";
export function DistributionBar({
  down,
  flat,
  up,
}: {
  down: number;
  flat: number;
  up: number;
}) {
  const total = Math.max(down + flat + up, 1);
  const downPct = (down / total) * 100;
  const flatPct = (flat / total) * 100;
  const upPct = (up / total) * 100;

  return (
    <div className="space-y-3">
      <div className="h-5 overflow-hidden rounded-full border border-border bg-background">
        <div className="flex h-full w-full">
          <div style={{ width: `${downPct}%` }} className="bg-rose-500/90" />
          <div
            style={{ width: `${flatPct}%` }}
            className="bg-muted-foreground/70"
          />
          <div style={{ width: `${upPct}%` }} className="bg-emerald-500/90" />
        </div>
      </div>
      <div className="grid grid-cols-3 gap-2 text-xs">
        <div className="rounded-xl border border-border bg-background px-4 py-3 text-destructive">
          <div className="text-2xl font-semibold leading-none">{down}</div>
          <div className="mt-0.5 text-[11px] text-destructive">
            {downPct.toFixed(1)}%
          </div>
          <div className="mt-1 text-muted-foreground">Падение</div>
        </div>
        <div className="rounded-xl border border-border bg-background px-4 py-3 text-foreground">
          <div className="text-2xl font-semibold leading-none">{flat}</div>
          <div className="mt-0.5 text-[11px] text-foreground">
            {flatPct.toFixed(1)}%
          </div>
          <div className="mt-1 text-muted-foreground">Без изменений</div>
        </div>
        <div className="rounded-xl border border-border bg-background px-4 py-3 text-primary">
          <div className="text-2xl font-semibold leading-none">{up}</div>
          <div className="mt-0.5 text-[11px] text-primary">
            {upPct.toFixed(1)}%
          </div>
          <div className="mt-1 text-muted-foreground">Рост</div>
        </div>
      </div>
    </div>
  );
}

function blendColor(
  a: [number, number, number],
  b: [number, number, number],
  t: number,
) {
  const safeT = Math.max(0, Math.min(1, t));
  const r = Math.round(a[0] + (b[0] - a[0]) * safeT);
  const g = Math.round(a[1] + (b[1] - a[1]) * safeT);
  const bch = Math.round(a[2] + (b[2] - a[2]) * safeT);
  return `rgb(${r}, ${g}, ${bch})`;
}

function sentimentTone(value: number) {
  const safe = Math.max(0, Math.min(100, value));
  const red: [number, number, number] = [239, 68, 68];
  const orange: [number, number, number] = [245, 158, 11];
  const green: [number, number, number] = [34, 197, 94];
  if (safe <= 50) {
    return blendColor(red, orange, safe / 50);
  }
  return blendColor(orange, green, (safe - 50) / 50);
}

export function SentimentGauge({
  value,
  label,
}: {
  value: number;
  label: string;
}) {
  const gradId = useId();
  const safeValue = Math.max(0, Math.min(100, value));
  const tone = sentimentTone(safeValue);
  const angle = 180 - (safeValue / 100) * 180;
  const rad = (angle * Math.PI) / 180;
  const cx = 100;
  const cy = 88;
  const needleRadius = 62;
  const nx = cx + Math.cos(rad) * needleRadius;
  const ny = cy - Math.sin(rad) * needleRadius;

  return (
    <div className="relative rounded-2xl border border-border bg-background p-4">
      <div className="text-xs uppercase tracking-wide text-muted-foreground">
        Настроение рынка
      </div>
      <svg viewBox="0 0 200 120" className="mt-2 h-28 w-full">
        <defs>
          <linearGradient id={gradId} x1="0" x2="1" y1="0" y2="0">
            <stop offset="0%" stopColor="#ef4444" />
            <stop offset="50%" stopColor="#f59e0b" />
            <stop offset="100%" stopColor="#22c55e" />
          </linearGradient>
        </defs>
        <path
          d="M20 88 A80 80 0 0 1 180 88"
          stroke={`url(#${gradId})`}
          strokeWidth="8"
          fill="none"
        />
        <path
          d="M30 88 A70 70 0 0 1 170 88"
          stroke="var(--border-strong)"
          strokeWidth="1"
          fill="none"
        />
        <line
          x1={cx}
          y1={cy}
          x2={nx}
          y2={ny}
          stroke="var(--foreground)"
          strokeWidth="2"
          strokeLinecap="round"
        />
        <circle
          cx={cx}
          cy={cy}
          r="3.5"
          fill="var(--foreground)"
          opacity="0.95"
        />
      </svg>
      <div className="-mt-8 text-center">
        <div className="text-4xl font-semibold text-foreground">
          {safeValue.toFixed(0)}
        </div>
        <div
          className="inline-block rounded-full border border-border bg-muted px-2.5 py-0.5 text-sm"
          style={{ color: tone }}
        >
          {label || "Настроение"}
        </div>
      </div>
    </div>
  );
}
