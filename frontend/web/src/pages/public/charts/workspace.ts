import {
  CHART_INTERVALS,
  type ChartInterval,
  type Instrument,
} from "../../../shared/charts/marketData";
import type { ChartMode } from "../../../shared/charts/ChartSurface";
import {
  DERIVATIVE_PANELS,
  type DerivativeId,
} from "../../../shared/charts/derivatives";
import {
  INDICATOR_CATALOG,
  isClassicIndicatorId,
  type IndicatorId,
} from "../../../shared/market/indicatorTypes";
import {
  normalizePanelLayout,
  normalizeStudySettings,
  type IndicatorInstance,
  type PanelLayout,
  type StudySettings,
} from "../../../shared/charts/studySettings";
import {
  normalizeDrawingSets,
  type Drawing,
} from "../../../shared/charts/drawings/model";

export type Comparison = Instrument & {
  id: string;
  mode: "price" | "percent" | "ratio" | "spread";
  color: string;
  hidden?: boolean;
  ownScale?: boolean;
};
export type ChartWindow = Instrument & {
  id: string;
  interval: ChartInterval;
  mode: ChartMode;
  indicators: IndicatorId[];
  instances: IndicatorInstance[];
  derivatives: DerivativeId[];
  periods: Partial<Record<IndicatorId, number>>;
  studySettings: Record<string, StudySettings>;
  panels: PanelLayout;
  volume: boolean;
  log: boolean;
  percent: boolean;
  autoScale: boolean;
  grid: boolean;
  follow: boolean;
  comparisons: Comparison[];
  view?: { from: number; to: number };
  priceView?: { from: number; to: number };
};
export type TerminalWorkspace = {
  version: 3;
  windows: ChartWindow[];
  layout: number;
  active: number;
  sidebarWidth: number;
  sidebarOpen: boolean;
  sync: {
    symbol: boolean;
    interval: boolean;
    crosshair: boolean;
    range: boolean;
  };
  favorites: string[];
  sort: {
    field: "manual" | "symbol" | "price" | "change" | "volume";
    descending: boolean;
  };
  presets: Record<
    string,
    {
      windows: ChartWindow[];
      layout: number;
      drawings?: Record<string, Drawing[]>;
    }
  >;
};
const SYMBOLS = [
  "BTCUSDT",
  "ETHUSDT",
  "SOLUSDT",
  "XRPUSDT",
  "DOGEUSDT",
  "BNBUSDT",
  "ADAUSDT",
  "LINKUSDT",
  "AVAXUSDT",
];
const ids = new Set(INDICATOR_CATALOG.map((i) => i.id));
const derivatives = new Set(DERIVATIVE_PANELS.map((i) => i.id));
const record = (v: unknown): Record<string, unknown> =>
  v && typeof v === "object" && !Array.isArray(v)
    ? (v as Record<string, unknown>)
    : {};
export function defaultWindow(index = 0): ChartWindow {
  return {
    id: `window-${index + 1}`,
    exchange: "bybit",
    market: "linear",
    symbol: SYMBOLS[index] ?? "BTCUSDT",
    interval: "1h",
    mode: "candles",
    indicators: ["ema", "rsi"],
    instances: [{ key: "ema200", id: "ema", period: 200, color: "#fbbf24" }],
    derivatives: [],
    periods: { ema: 50, rsi: 14 },
    studySettings: {},
    panels: { order: [], heights: {}, scales: {} },
    volume: true,
    log: false,
    percent: false,
    autoScale: true,
    grid: true,
    follow: true,
    comparisons: [],
  };
}
export function normalizeWindow(value: unknown, index = 0): ChartWindow {
  const raw = record(value),
    defaults = defaultWindow(index);
  const strings = (value: unknown) =>
    Array.isArray(value)
      ? value.filter((x): x is string => typeof x === "string")
      : [];
  const instrument = (v: Record<string, unknown>) => ({
    exchange:
      v.exchange === "binance" ? ("binance" as const) : ("bybit" as const),
    market: v.market === "spot" ? ("spot" as const) : ("linear" as const),
    symbol:
      typeof v.symbol === "string" && /^[A-Z0-9]{2,40}$/.test(v.symbol)
        ? v.symbol
        : defaults.symbol,
  });
  return {
    ...defaults,
    ...instrument(raw),
    id:
      typeof raw.id === "string" && /^[\w-]{1,40}$/.test(raw.id)
        ? raw.id
        : defaults.id,
    interval: CHART_INTERVALS.some((t) => t.value === raw.interval)
      ? (raw.interval as ChartInterval)
      : defaults.interval,
    mode: ["candles", "bars", "line", "area"].includes(String(raw.mode))
      ? (raw.mode as ChartMode)
      : "candles",
    indicators:
      raw.indicators === undefined
        ? defaults.indicators
        : ([
            ...new Set(
              strings(raw.indicators).filter((i) => ids.has(i as IndicatorId)),
            ),
          ] as IndicatorId[]),
    derivatives: strings(raw.derivatives).filter((i) =>
      derivatives.has(i as DerivativeId),
    ) as DerivativeId[],
    instances: Array.isArray(raw.instances)
      ? raw.instances
          .slice(0, 16)
          .filter((v) => {
            const i = record(v);
            return (
              typeof i.key === "string" &&
              /^[\w-]{1,40}$/.test(i.key) &&
              ids.has(i.id as IndicatorId) &&
              isClassicIndicatorId(i.id as IndicatorId)
            );
          })
          .map((v) => {
            const i = record(v);
            return {
              ...normalizeStudySettings(i),
              id: i.id as IndicatorInstance["id"],
              key: i.key as string,
            };
          })
          .filter(
            (i, n, all) =>
              !ids.has(i.key as IndicatorId) &&
              all.findIndex((p) => p.key === i.key) === n,
          )
      : defaults.instances,
    periods:
      raw.periods === undefined
        ? defaults.periods
        : Object.fromEntries(
            Object.entries(record(raw.periods)).filter(
              ([key, p]) =>
                ids.has(key as IndicatorId) &&
                Number.isInteger(p) &&
                Number(p) >= 2 &&
                Number(p) <= 400,
            ),
          ),
    studySettings: Object.fromEntries(
      Object.entries(record(raw.studySettings))
        .filter(([key]) => /^[\w:-]{1,100}$/.test(key))
        .slice(0, 120)
        .map(([key, v]) => [key, normalizeStudySettings(v)]),
    ),
    panels: normalizePanelLayout(raw.panels),
    volume: raw.volume !== false,
    log: raw.log === true,
    percent: raw.percent === true,
    autoScale: raw.autoScale !== false,
    grid: raw.grid !== false,
    follow: raw.follow !== false,
    comparisons: Array.isArray(raw.comparisons)
      ? raw.comparisons
          .slice(0, 4)
          .filter((v) => typeof record(v).id === "string")
          .map((v) => {
            const c = record(v);
            return {
              ...instrument(c),
              id: String(c.id).slice(0, 40),
              mode: ["price", "percent", "ratio", "spread"].includes(
                String(c.mode),
              )
                ? (c.mode as Comparison["mode"])
                : "percent",
              color: /^#[0-9a-f]{6}$/i.test(String(c.color))
                ? String(c.color)
                : "#38bdf8",
              hidden: c.hidden === true,
              ownScale: c.ownScale !== false,
            };
          })
      : [],
    view:
      typeof record(raw.view).from === "number" &&
      typeof record(raw.view).to === "number" &&
      Number(record(raw.view).from) > 0 &&
      Number(record(raw.view).to) > Number(record(raw.view).from)
        ? (raw.view as ChartWindow["view"])
        : undefined,
    priceView:
      Number.isFinite(record(raw.priceView).from) &&
      Number.isFinite(record(raw.priceView).to) &&
      Number(record(raw.priceView).to) > Number(record(raw.priceView).from)
        ? (raw.priceView as ChartWindow["priceView"])
        : undefined,
  };
}
export function normalizeWorkspace(value: unknown): TerminalWorkspace {
  const raw = record(value);
  const legacy = raw.version === 2;
  const candidates = legacy
    ? Array.from({ length: 9 }, (_, n) => ({
        ...raw,
        id: `window-${n + 1}`,
        symbol: Array.isArray(raw.symbols)
          ? (raw.symbols[n] ?? SYMBOLS[n])
          : SYMBOLS[n],
      }))
    : Array.isArray(raw.windows)
      ? raw.windows
      : [];
  const windows = Array.from({ length: 9 }, (_, n) =>
    normalizeWindow(candidates[n], n),
  );
  // Persistent IDs are identities, never duplicate imported windows.
  for (let n = 0; n < windows.length; n++) windows[n].id = `window-${n + 1}`;
  const layout =
    Number.isInteger(raw.layout) &&
    Number(raw.layout) >= 1 &&
    Number(raw.layout) <= 9
      ? Number(raw.layout)
      : 1;
  const sync = record(raw.sync),
    sort = record(raw.sort);
  return {
    version: 3,
    windows,
    layout,
    active:
      Number.isInteger(raw.active) &&
      Number(raw.active) >= 0 &&
      Number(raw.active) < layout
        ? Number(raw.active)
        : 0,
    sidebarWidth:
      typeof raw.sidebarWidth === "number"
        ? Math.max(220, Math.min(460, raw.sidebarWidth))
        : 280,
    sidebarOpen: raw.sidebarOpen !== false,
    sync: {
      symbol: sync.symbol === true,
      interval: sync.interval === true,
      crosshair: sync.crosshair !== false,
      range: sync.range === true,
    },
    favorites: Array.isArray(raw.favorites)
      ? [
          ...new Set(
            raw.favorites.filter(
              (s): s is string =>
                typeof s === "string" && /^[A-Z0-9]{2,40}$/.test(s),
            ),
          ),
        ].slice(0, 500)
      : ["BTCUSDT", "ETHUSDT"],
    sort: {
      field: ["manual", "symbol", "price", "change", "volume"].includes(
        String(sort.field),
      )
        ? (sort.field as TerminalWorkspace["sort"]["field"])
        : "volume",
      descending: sort.descending !== false,
    },
    presets: Object.fromEntries(
      Object.entries(record(raw.presets))
        .slice(0, 12)
        .filter(([name]) => name.length > 0 && name.length <= 60)
        .map(([name, value]) => {
          const p = record(value);
          return [
            name,
            {
              layout: Math.max(1, Math.min(9, Number(p.layout) || 1)),
              windows: Array.from({ length: 9 }, (_, n) =>
                normalizeWindow(
                  Array.isArray(p.windows) ? p.windows[n] : windows[n],
                  n,
                ),
              ),
              drawings: p.drawings
                ? normalizeDrawingSets(p.drawings)
                : undefined,
            },
          ];
        }),
    ),
  };
}
export function patchWindow(
  workspace: TerminalWorkspace,
  change: Partial<ChartWindow>,
): TerminalWorkspace {
  const windows = workspace.windows.map((w, n) => {
    if (n === workspace.active) return { ...w, ...change };
    const synced: Partial<ChartWindow> = {};
    if (workspace.sync.symbol)
      for (const key of ["symbol", "exchange", "market"] as const) {
        const value = change[key];
        if (value) Object.assign(synced, { [key]: value });
      }
    if (workspace.sync.interval && change.interval)
      synced.interval = change.interval;
    if (workspace.sync.range && "view" in change) {
      synced.view = change.view;
      if ("follow" in change) synced.follow = change.follow;
    }
    return { ...w, ...synced };
  });
  return { ...workspace, windows };
}
export function layoutShape(count: number) {
  const columns = count === 1 ? 1 : count <= 4 ? 2 : 3;
  return { columns, rows: Math.ceil(count / columns) };
}
export function capturePresetDrawings(
  workspace: TerminalWorkspace,
  stored: unknown,
) {
  const drawings = normalizeDrawingSets(stored),
    keys = workspace.windows.map(
      (w) => `${w.id}:${w.exchange}:${w.market}:${w.symbol}`,
    );
  return Object.fromEntries(
    Object.entries(drawings).filter(([key]) =>
      keys.some((prefix) => key === prefix || key.startsWith(prefix + ":")),
    ),
  );
}
