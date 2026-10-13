import type { ClassicIndicatorId } from "../market/indicatorTypes";

export type PriceSource =
  | "close"
  | "open"
  | "high"
  | "low"
  | "hl2"
  | "hlc3"
  | "ohlc4";
export type ComponentStyle = {
  color?: string;
  width?: 1 | 2 | 3 | 4;
  dash?: 0 | 1 | 2;
  hidden?: boolean;
};
export type StudySettings = ComponentStyle & {
  representation?: "line" | "histogram" | "candles";
  period?: number;
  source?: PriceSource;
  parameters?: {
    fast?: number;
    slow?: number;
    signal?: number;
    deviation?: number;
    multiplier?: number;
  };
  components?: Record<string, ComponentStyle>;
  levels?: number[];
  fill?: boolean;
};
export type IndicatorInstance = StudySettings & {
  key: string;
  id: ClassicIndicatorId;
};
export type PanelLayout = {
  order: string[];
  heights: Record<string, number>;
  scales: Record<
    string,
    {
      auto?: boolean;
      log?: boolean;
      inverted?: boolean;
      range?: { from: number; to: number };
    }
  >;
  maximized?: string;
};
export const EMPTY_PANEL_LAYOUT: PanelLayout = {
  order: [],
  heights: {},
  scales: {},
};
const SOURCES = new Set([
  "close",
  "open",
  "high",
  "low",
  "hl2",
  "hlc3",
  "ohlc4",
]);
export function normalizeStudySettings(value: unknown): StudySettings {
  if (!value || typeof value !== "object") return {};
  const raw = value as StudySettings;
  const output: StudySettings = {};
  if (["line", "histogram", "candles"].includes(raw.representation ?? ""))
    output.representation = raw.representation;
  if (/^#[0-9a-f]{6}$/i.test(raw.color ?? "")) output.color = raw.color;
  if ([1, 2, 3, 4].includes(raw.width ?? 0)) output.width = raw.width;
  if ([0, 1, 2].includes(raw.dash ?? -1)) output.dash = raw.dash;
  if (typeof raw.hidden === "boolean") output.hidden = raw.hidden;
  if (typeof raw.fill === "boolean") output.fill = raw.fill;
  if (Number.isInteger(raw.period) && raw.period! >= 2 && raw.period! <= 400)
    output.period = raw.period;
  if (SOURCES.has(raw.source ?? "")) output.source = raw.source;
  if (raw.parameters && typeof raw.parameters === "object") {
    output.parameters = {};
    for (const name of [
      "fast",
      "slow",
      "signal",
      "deviation",
      "multiplier",
    ] as const) {
      const n = raw.parameters[name];
      if (typeof n === "number" && Number.isFinite(n) && n > 0 && n <= 400)
        output.parameters[name] = n;
    }
  }
  if (Array.isArray(raw.levels))
    output.levels = raw.levels
      .filter((n) => Number.isFinite(n) && Math.abs(n) <= 1e9)
      .slice(0, 12);
  if (raw.components && typeof raw.components === "object") {
    output.components = Object.fromEntries(
      Object.entries(raw.components)
        .slice(0, 16)
        .filter(([key]) => /^[\w-]{1,50}$/.test(key))
        .map(([key, style]) => {
          const { color, width, dash, hidden } = normalizeStudySettings(style);
          return [
            key,
            Object.fromEntries(
              Object.entries({ color, width, dash, hidden }).filter(
                ([, value]) => value !== undefined,
              ),
            ),
          ];
        }),
    );
  }
  return output;
}
export function normalizePanelLayout(value: unknown): PanelLayout {
  const raw = (
    value && typeof value === "object" ? value : {}
  ) as Partial<PanelLayout>;
  const validKey = (key: unknown): key is string =>
    typeof key === "string" && /^[\w:-]{1,100}$/.test(key);
  return {
    order: Array.isArray(raw.order)
      ? [...new Set(raw.order.filter(validKey))].slice(0, 120)
      : [],
    heights: Object.fromEntries(
      Object.entries(raw.heights ?? {})
        .filter(
          ([key, n]) =>
            validKey(key) && Number.isFinite(n) && n >= 20 && n <= 4000,
        )
        .slice(0, 120),
    ),
    scales: Object.fromEntries(
      Object.entries(raw.scales ?? {})
        .filter(([key, v]) => validKey(key) && v && typeof v === "object")
        .slice(0, 120)
        .map(([key, v]) => [
          key,
          {
            auto: v.auto !== false,
            log: v.log === true,
            inverted: v.inverted === true,
            range:
              v.range &&
              Number.isFinite(v.range.from) &&
              Number.isFinite(v.range.to) &&
              v.range.to > v.range.from
                ? v.range
                : undefined,
          },
        ]),
    ),
    maximized: validKey(raw.maximized) ? raw.maximized : undefined,
  };
}

export function priceSource(
  c: { open: number; high: number; low: number; close: number },
  source: PriceSource = "close",
) {
  if (source === "hl2") return (c.high + c.low) / 2;
  if (source === "hlc3") return (c.high + c.low + c.close) / 3;
  if (source === "ohlc4") return (c.open + c.high + c.low + c.close) / 4;
  return c[source];
}
