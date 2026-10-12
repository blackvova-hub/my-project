import { useDialogFocus } from "../../../shared/charts/useDialogFocus";
import {
  DERIVATIVE_PANELS,
  type DerivativeId,
} from "../../../shared/charts/derivatives";
import { useCallback, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Search,
  ChevronDown,
  Save,
  LayoutGrid,
  Camera,
  Maximize2,
  Star,
  X,
  Settings2,
  Plus,
  RefreshCw,
  Download,
  Upload,
} from "lucide-react";
import { MarketChart } from "../../../shared/charts/MarketChart";
import { TimeSeriesChart } from "../../../shared/charts/LineChart";
import {
  CHART_INTERVALS,
  fetchMarketTickers,
  type ChartInterval,
  type Instrument,
  type MarketTicker,
} from "../../../shared/charts/marketData";
import { useMarketData } from "../../../shared/charts/useMarketData";
import { useChartWorkspace } from "../../../shared/charts/useChartWorkspace";
import { downloadChartScreenshot } from "../../../shared/charts/screenshot";
import { IndicatorMenu } from "../../../shared/market/IndicatorMenu";
import {
  INDICATOR_CATALOG,
  type IndicatorId,
  isClassicIndicatorId,
} from "../../../shared/market/indicatorTypes";
import type {
  ChartMode,
  IndicatorInstance,
} from "../../../shared/charts/ChartSurface";
import type { DrawingRuntime } from "../../../shared/charts/drawings/DrawingEditor";
import { normalizeDrawingSets } from "../../../shared/charts/drawings/model";
import { useAuth } from "../../../shared/auth/AuthContext";
import { useTheme } from "../../../shared/theme/ThemeContext";
import {
  loadWatchlist,
  saveWatchlists,
  loadWatchlists,
} from "../../../shared/watchlist";
import "../../../shared/charts/charts.css";
type Workspace = {
  version: 2;
  symbols: string[];
  exchange: Instrument["exchange"];
  market: Instrument["market"];
  interval: ChartInterval;
  layout: 1 | 2 | 4;
  indicators: IndicatorId[];
  instances: IndicatorInstance[];
  derivatives: DerivativeId[];
  periods: Partial<Record<IndicatorId, number>>;
  mode: ChartMode;
  volume: boolean;
  log: boolean;
  grid: boolean;
};
const defaults: Workspace = {
  version: 2,
  symbols: ["BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT"],
  exchange: "bybit",
  market: "linear",
  interval: "1h",
  layout: 1,
  indicators: ["ema", "rsi"],
  instances: [{ key: "ema200", id: "ema", period: 200, color: "#fbbf24" }],
  derivatives: [],
  periods: { ema: 50, rsi: 14 },
  mode: "candles",
  volume: true,
  log: false,
  grid: true,
};
function normalizeWorkspace(value: unknown): Workspace {
  const raw = value as Partial<Workspace> | null;
  if (!raw || raw.version !== 2) return defaults;
  return {
    ...defaults,
    ...raw,
    symbols:
      Array.isArray(raw.symbols) &&
      raw.symbols.length > 0 &&
      raw.symbols.every(
        (s) => typeof s === "string" && /^[A-Z0-9]{2,40}$/.test(s),
      )
        ? Array.from(
            { length: 4 },
            (_, i) => raw.symbols?.[i] ?? defaults.symbols[i],
          )
        : defaults.symbols,
    exchange: raw.exchange === "binance" ? "binance" : "bybit",
    market: raw.market === "spot" ? "spot" : "linear",
    layout: raw.layout === 2 || raw.layout === 4 ? raw.layout : 1,
    interval: CHART_INTERVALS.some((t) => t.value === raw.interval)
      ? raw.interval!
      : "1h",
    mode: ["candles", "bars", "line", "area"].includes(raw.mode ?? "")
      ? raw.mode!
      : "candles",
    indicators: Array.isArray(raw.indicators)
      ? raw.indicators.filter((id) =>
          INDICATOR_CATALOG.some((i) => i.id === id),
        )
      : defaults.indicators,
    instances: Array.isArray(raw.instances)
      ? raw.instances
          .slice(0, 8)
          .filter(
            (i) =>
              i &&
              typeof i.key === "string" &&
              i.key.length <= 40 &&
              INDICATOR_CATALOG.some((p) => p.id === i.id) &&
              isClassicIndicatorId(i.id) &&
              (i.period === undefined ||
                (Number.isInteger(i.period) &&
                  Number(i.period) >= 2 &&
                  Number(i.period) <= 400)),
          )
          .map((i) => ({
            ...i,
            color: /^#[0-9a-f]{6}$/i.test(i.color ?? "") ? i.color : "#fbbf24",
          }))
      : [],
    volume: typeof raw.volume === "boolean" ? raw.volume : defaults.volume,
    log: raw.log === true,
    grid: raw.grid !== false,
    derivatives: Array.isArray(raw.derivatives)
      ? raw.derivatives.filter((id) =>
          DERIVATIVE_PANELS.some((p) => p.id === id),
        )
      : [],
    periods: Object.fromEntries(
      Object.entries(raw.periods ?? {}).filter(
        ([id, p]) =>
          INDICATOR_CATALOG.some((i) => i.id === id) &&
          Number.isInteger(p) &&
          p >= 2 &&
          p <= 400,
      ),
    ),
  };
}
const fmt = (v: number | null | undefined, digits = 6) =>
    v == null
      ? "—"
      : v.toLocaleString("ru-RU", { maximumSignificantDigits: digits }),
  compact = (v: number | null | undefined) =>
    v == null
      ? "—"
      : v.toLocaleString("ru-RU", {
          notation: "compact",
          maximumFractionDigits: 2,
        });
export default function ChartsPage() {
  const { user } = useAuth(),
    { theme } = useTheme(),
    storage = `shortlong:terminal:v2:${user?.id ?? "guest"}`;
  const [workspace, setWorkspace] = useState<Workspace>(() => {
      try {
        return normalizeWorkspace(
          JSON.parse(localStorage.getItem(storage) ?? "null"),
        );
      } catch {
        return defaults;
      }
    }),
    [active, setActive] = useState(0),
    [dialog, setDialog] = useState<
      "symbol" | "compare" | "settings" | "presets" | "derivatives" | null
    >(null),
    [search, setSearch] = useState(""),
    [favorites, setFavorites] = useState(loadWatchlist),
    [tab, setTab] = useState("favorites"),
    [watchSearch, setWatchSearch] = useState(""),
    [compare, setCompare] = useState<string | null>(null),
    [reset, setReset] = useState(0),
    [notice, setNotice] = useState(""),
    [presetName, setPresetName] = useState("Рабочая разметка");
  const [presets, setPresets] = useState<
    Record<string, { workspace: Workspace; drawings: unknown }>
  >(() => {
    try {
      return JSON.parse(localStorage.getItem(storage + ":presets") ?? "{}");
    } catch {
      return {};
    }
  });
  const closeDialog = useCallback(() => setDialog(null), []);
  useDialogFocus(!!dialog, closeDialog);
  const modalTitle = dialog
    ? {
        symbol: "Выбор инструмента",
        compare: "Сравнить инструмент",
        settings: "Настройки графика",
        presets: "Рабочие пространства",
        derivatives: "Деривативные панели",
      }[dialog]
    : "";
  const root = useRef<HTMLDivElement>(null),
    api = useRef<Map<number, DrawingRuntime>>(new Map()),
    file = useRef<HTMLInputElement>(null);
  const instrument: Instrument = {
    exchange: workspace.exchange,
    market: workspace.market,
    symbol: workspace.symbols[active] ?? "BTCUSDT",
  };
  const tickers = useQuery({
    queryKey: ["terminal-tickers", instrument.exchange, instrument.market],
    queryFn: ({ signal }) => fetchMarketTickers(instrument, signal),
    staleTime: 15000,
    refetchInterval: 15000,
    refetchIntervalInBackground: false,
    retry: 1,
  });
  const cloud = useChartWorkspace(
      `${instrument.exchange}:${instrument.market}:${instrument.symbol}:${workspace.interval}`,
    ),
    selected = tickers.data?.find((t) => t.symbol === instrument.symbol);
  const patch = useCallback(
    (change: Partial<Workspace>) =>
      setWorkspace((w) => {
        const next = { ...w, ...change };
        try {
          localStorage.setItem(storage, JSON.stringify(next));
        } catch {
          setNotice("Не удалось сохранить настройки браузера");
        }
        return next;
      }),
    [storage],
  );
  const selectSymbol = (symbol: string) => {
    const symbols = [...workspace.symbols];
    symbols[active] = symbol;
    patch({ symbols });
    setDialog(null);
    setSearch("");
  };
  const toggleFavorite = (symbol: string) => {
    const next = favorites.includes(symbol)
      ? favorites.filter((s) => s !== symbol)
      : [...favorites, symbol];
    setFavorites(next);
    saveWatchlists({ ...loadWatchlists(), hot: next });
  };
  const candidates = useMemo(
    () =>
      (tickers.data ?? [])
        .filter((t) => t.symbol.includes(search.toUpperCase()))
        .slice(0, 100),
    [tickers.data, search],
  );
  const rows = useMemo(
    () =>
      (tickers.data ?? [])
        .filter(
          (t) =>
            (tab === "market" || favorites.includes(t.symbol)) &&
            t.symbol.includes(watchSearch.toUpperCase()),
        )
        .sort((a, b) =>
          tab === "favorites"
            ? favorites.indexOf(a.symbol) - favorites.indexOf(b.symbol)
            : (b.volume ?? 0) - (a.volume ?? 0),
        )
        .slice(0, 100),
    [tickers.data, tab, favorites, watchSearch],
  );
  const save = async () => {
    try {
      localStorage.setItem(storage, JSON.stringify(workspace));
      const message = await cloud.save(
        workspace.interval,
        workspace.indicators,
      );
      setNotice(message);
    } catch {
      setNotice("Хранилище браузера недоступно");
    }
  };
  const chooseLayout = () => {
    const layout = workspace.layout === 1 ? 2 : workspace.layout === 2 ? 4 : 1;
    patch({ layout });
    setActive(0);
  };
  const height =
    workspace.layout === 1
      ? "max(440px, calc(100svh - 280px))"
      : workspace.layout === 2
        ? "max(430px, calc(100svh - 280px))"
        : 430;
  const download = () => {
    const url = URL.createObjectURL(
      new Blob(
        [
          JSON.stringify(
            {
              workspace,
              drawings: localStorage.getItem(
                `shortlong:charts:v2:${user?.id ?? "guest"}`,
              ),
            },
            null,
            2,
          ),
        ],
        { type: "application/json" },
      ),
    );
    const a = document.createElement("a");
    a.href = url;
    a.download = "shortlong-workspace.json";
    a.click();
    URL.revokeObjectURL(url);
  };
  return (
    <div className="ct-terminal" ref={root} data-theme={theme}>
      <div className="ct-terminal-heading">
        <div>
          <h1>Графики</h1>
          <p>Профессиональный терминал · публичные данные бирж</p>
        </div>
        <div className="ct-terminal-actions">
          <button
            onClick={() => setDialog("presets")}
            title="Сохранённые рабочие пространства"
          >
            <LayoutGrid size={15} />
            <span>Разметки</span>
          </button>
          <button onClick={chooseLayout} aria-label="Количество графиков">
            <LayoutGrid size={15} />
            {workspace.layout}
          </button>
          <button onClick={() => void save()}>
            <Save size={15} />
            <span>Сохранить</span>
          </button>
          <button
            aria-label="Снимок графика"
            onClick={() => {
              const selectedApi = api.current.get(active),
                surface = root.current?.querySelectorAll<HTMLElement>(
                  "[data-chart-surface]",
                )[active];
              if (selectedApi && surface)
                void downloadChartScreenshot(selectedApi, surface).catch(() =>
                  setNotice("Не удалось создать снимок"),
                );
            }}
          >
            <Camera size={15} />
          </button>
          <button
            aria-label="Полный экран терминала"
            onClick={() => {
              if (document.fullscreenElement) void document.exitFullscreen();
              else
                void root.current
                  ?.requestFullscreen()
                  .catch(() =>
                    setNotice("Полный экран недоступен в этом браузере"),
                  );
            }}
          >
            <Maximize2 size={15} />
          </button>
        </div>
      </div>
      <div className="ct-terminal-layout">
        <div className="ct-terminal-center">
          <div className="ct-toolbar">
            <button
              className="ct-instrument-button"
              onClick={() => {
                setDialog("symbol");
                setSearch("");
              }}
            >
              <Search size={14} />
              {instrument.symbol}
              <ChevronDown size={13} />
            </button>
            <select
              aria-label="Биржа"
              value={workspace.exchange}
              onChange={(e) =>
                patch({ exchange: e.target.value as Instrument["exchange"] })
              }
            >
              <option value="bybit">Bybit</option>
              <option value="binance">Binance</option>
            </select>
            <select
              aria-label="Рынок"
              value={workspace.market}
              onChange={(e) =>
                patch({ market: e.target.value as Instrument["market"] })
              }
            >
              <option value="linear">Фьючерсы</option>
              <option value="spot">Спот</option>
            </select>
            {CHART_INTERVALS.map((t) => (
              <button
                key={t.value}
                aria-label={`Таймфрейм ${t.label}`}
                aria-pressed={workspace.interval === t.value}
                onClick={() => patch({ interval: t.value })}
              >
                {t.label}
              </button>
            ))}
            <select
              aria-label="Тип графика"
              value={workspace.mode}
              onChange={(e) => patch({ mode: e.target.value as ChartMode })}
            >
              <option value="candles">Свечи</option>
              <option value="bars">Бары</option>
              <option value="line">Линия</option>
              <option value="area">Область</option>
            </select>
            <IndicatorMenu
              active={workspace.indicators}
              extraCount={workspace.instances.length}
              onToggle={(id) =>
                patch({
                  indicators: workspace.indicators.includes(id)
                    ? workspace.indicators.filter((i) => i !== id)
                    : [...workspace.indicators, id],
                })
              }
              onClear={() => patch({ indicators: [], instances: [] })}
              volumeActive={workspace.volume}
              onVolumeToggle={() => patch({ volume: !workspace.volume })}
            />
            <button
              disabled={workspace.market !== "linear"}
              onClick={() => setDialog("derivatives")}
            >
              Деривативы
            </button>
            <button
              onClick={() => {
                setDialog("compare");
                setSearch("");
              }}
            >
              Сравнить
            </button>
            <button
              aria-label="Настройки графика"
              onClick={() => setDialog("settings")}
            >
              <Settings2 size={15} />
            </button>
          </div>
          <div
            className={`ct-terminal-main ${workspace.layout > 1 ? "ct-terminal-grid" : ""}`}
          >
            {workspace.symbols
              .slice(0, workspace.layout)
              .map((symbol, index) => (
                <div
                  key={`${index}:${symbol}:${workspace.exchange}:${workspace.market}:${workspace.interval}`}
                  data-active={active === index}
                  onFocus={() => setActive(index)}
                  onPointerDown={() => setActive(index)}
                >
                  <MarketChart
                    exchange={workspace.exchange}
                    market={workspace.market}
                    symbol={symbol}
                    interval={workspace.interval}
                    mode={workspace.mode}
                    indicators={workspace.indicators}
                    periods={workspace.periods}
                    instances={workspace.instances}
                    derivatives={workspace.derivatives}
                    volume={workspace.volume}
                    height={height}
                    log={workspace.log}
                    grid={workspace.grid}
                    resetToken={reset}
                    onReady={(runtime) => {
                      if (runtime) api.current.set(index, runtime);
                      else api.current.delete(index);
                    }}
                  />
                </div>
              ))}
          </div>
          {compare && (
            <Comparison
              primary={instrument}
              symbol={compare}
              interval={workspace.interval}
              onClose={() => setCompare(null)}
            />
          )}
          <div className="ct-terminal-bottom">
            <span role="status">{notice || cloud.status}</span>
            <button onClick={() => setReset((n) => n + 1)}>
              <RefreshCw size={12} /> Сброс масштаба
            </button>
            <button
              aria-pressed={workspace.log}
              onClick={() => patch({ log: !workspace.log })}
            >
              Лог
            </button>
            <span>UTC</span>
          </div>
        </div>
        <aside className="ct-terminal-rail">
          <div className="ct-watch-tabs">
            <button
              aria-pressed={tab === "favorites"}
              onClick={() => setTab("favorites")}
            >
              Избранное
            </button>
            <button
              aria-pressed={tab === "market"}
              onClick={() => setTab("market")}
            >
              Рынок
            </button>
            <button
              aria-label="Добавить инструмент"
              onClick={() => setDialog("symbol")}
            >
              <Plus size={16} />
            </button>
          </div>
          <div className="ct-watch-search">
            <Search size={14} />
            <input
              aria-label="Поиск в списке инструментов"
              placeholder="Поиск инструментов…"
              value={watchSearch}
              onChange={(e) => setWatchSearch(e.target.value)}
            />
          </div>
          <div className="ct-watch-table">
            <table>
              <thead>
                <tr>
                  <th>Инструмент</th>
                  <th>Цена</th>
                  <th>Изм. 24ч</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((t) => (
                  <tr
                    key={t.symbol}
                    data-active={instrument.symbol === t.symbol}
                    onClick={() => selectSymbol(t.symbol)}
                  >
                    <td>
                      <button
                        aria-label={`Избранное ${t.symbol}`}
                        aria-pressed={favorites.includes(t.symbol)}
                        onClick={(e) => {
                          e.stopPropagation();
                          toggleFavorite(t.symbol);
                        }}
                      >
                        <Star
                          size={12}
                          fill={
                            favorites.includes(t.symbol)
                              ? "currentColor"
                              : "none"
                          }
                        />
                      </button>
                      <button onClick={() => selectSymbol(t.symbol)}>
                        {t.symbol}
                      </button>
                    </td>
                    <td>{fmt(t.last)}</td>
                    <td className={(t.change ?? 0) >= 0 ? "ct-up" : "ct-down"}>
                      {t.change === null
                        ? "—"
                        : `${t.change > 0 ? "+" : ""}${t.change.toFixed(2)}%`}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {tickers.isPending && (
              <p className="ct-source-note">Загружаем инструменты…</p>
            )}
            {tickers.isError && (
              <p className="ct-source-note">
                Источник недоступен{" "}
                <button onClick={() => void tickers.refetch()}>
                  Повторить
                </button>
              </p>
            )}
            {!tickers.isPending && !tickers.isError && !rows.length && (
              <p className="ct-source-note">
                Нет инструментов. Добавьте их из вкладки «Рынок».
              </p>
            )}
          </div>
          <InstrumentInfo
            ticker={selected}
            instrument={instrument}
            updated={tickers.dataUpdatedAt}
          />
        </aside>
      </div>
      {dialog && (
        <div className="ct-modal-backdrop" onClick={() => setDialog(null)}>
          <div
            className="ct-dialog"
            role="dialog"
            aria-modal="true"
            aria-label={modalTitle}
            onClick={(e) => e.stopPropagation()}
          >
            <header>
              {modalTitle}
              <button aria-label="Закрыть" onClick={() => setDialog(null)}>
                <X size={18} />
              </button>
            </header>
            {(dialog === "symbol" || dialog === "compare") && (
              <>
                <label>
                  Инструмент
                  <input
                    autoFocus
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    placeholder="BTC, ETH, SOL…"
                  />
                </label>
                <p className="ct-muted">
                  {workspace.exchange === "bybit" ? "Bybit" : "Binance"} ·{" "}
                  {workspace.market === "spot" ? "Спот" : "Фьючерсы"}
                </p>
                <div className="ct-instrument-list">
                  {candidates.map((t) => (
                    <button
                      key={t.symbol}
                      onClick={() => {
                        if (dialog === "compare") {
                          setCompare(t.symbol);
                          setDialog(null);
                        } else selectSymbol(t.symbol);
                      }}
                    >
                      <strong>{t.symbol}</strong>
                      <span>{fmt(t.last)}</span>
                    </button>
                  ))}
                </div>
                {tickers.isError && (
                  <p>
                    Каталог биржи недоступен.{" "}
                    <button onClick={() => void tickers.refetch()}>
                      Повторить
                    </button>
                  </p>
                )}
              </>
            )}
            {dialog === "derivatives" && (
              <>
                <h3>Деривативные панели</h3>
                {DERIVATIVE_PANELS.map((p) => (
                  <label key={p.id}>
                    <span>
                      <input
                        type="checkbox"
                        checked={workspace.derivatives.includes(p.id)}
                        onChange={(e) =>
                          patch({
                            derivatives: e.target.checked
                              ? [...workspace.derivatives, p.id]
                              : workspace.derivatives.filter(
                                  (id) => id !== p.id,
                                ),
                          })
                        }
                      />{" "}
                      {p.label}
                    </span>
                  </label>
                ))}
                <p className="ct-source-note">
                  Последние доступные снимки API: OI Bybit до 200, Binance до
                  500; funding до 200 / 1000. Обновление раз в минуту. Для 1м OI
                  и L/S доступны с шагом 5м. На крупных таймфреймах отображается
                  последнее наблюдение в интервале. Ставки funding не
                  суммируются. OI — базовый актив, L/S — аккаунты, не объём
                  позиций.
                </p>
              </>
            )}
            {dialog === "settings" && (
              <>
                <label>
                  <input
                    type="checkbox"
                    checked={workspace.grid}
                    onChange={(e) => patch({ grid: e.target.checked })}
                  />{" "}
                  Сетка
                </label>
                <label>
                  <input
                    type="checkbox"
                    checked={workspace.volume}
                    onChange={(e) => patch({ volume: e.target.checked })}
                  />{" "}
                  Панель объёма
                </label>
                <label>
                  <input
                    type="checkbox"
                    checked={workspace.log}
                    onChange={(e) => patch({ log: e.target.checked })}
                  />{" "}
                  Логарифмическая шкала
                </label>
                {workspace.indicators
                  .filter(
                    (id) =>
                      !["macd", "ichimoku", "vwap"].includes(id) &&
                      INDICATOR_CATALOG.find((i) => i.id === id)?.group !==
                        "smart-money",
                  )
                  .map((id) => (
                    <label key={id}>
                      {INDICATOR_CATALOG.find((i) => i.id === id)?.label} ·
                      период
                      <input
                        type="number"
                        min={2}
                        max={400}
                        value={
                          workspace.periods[id] ??
                          (["ema", "sma", "bollinger", "cci"].includes(id)
                            ? 20
                            : id === "roc"
                              ? 12
                              : id === "supertrend"
                                ? 10
                                : 14)
                        }
                        onChange={(e) => {
                          const n = Number(e.target.value);
                          if (Number.isInteger(n) && n >= 2 && n <= 400)
                            patch({
                              periods: { ...workspace.periods, [id]: n },
                            });
                        }}
                      />
                    </label>
                  ))}
                <h3>Дополнительные экземпляры индикаторов</h3>
                {workspace.instances.map((instance) => (
                  <div className="ct-form-row" key={instance.key}>
                    <label>
                      {instance.id.toUpperCase()}
                      {["macd", "ichimoku", "vwap"].includes(instance.id) ? (
                        <span>
                          {instance.id === "macd"
                            ? "12 / 26 / 9"
                            : instance.id === "ichimoku"
                              ? "9 / 26 / 52"
                              : "Сессия UTC"}
                        </span>
                      ) : (
                        <input
                          type="number"
                          min={2}
                          max={400}
                          value={instance.period ?? 20}
                          onChange={(e) => {
                            const n = Number(e.target.value);
                            if (Number.isInteger(n) && n >= 2 && n <= 400)
                              patch({
                                instances: workspace.instances.map((i) =>
                                  i.key === instance.key
                                    ? { ...i, period: n }
                                    : i,
                                ),
                              });
                          }}
                        />
                      )}
                    </label>
                    <input
                      aria-label={`Цвет ${instance.key}`}
                      type="color"
                      value={instance.color ?? "#fbbf24"}
                      onChange={(e) =>
                        patch({
                          instances: workspace.instances.map((i) =>
                            i.key === instance.key
                              ? { ...i, color: e.target.value }
                              : i,
                          ),
                        })
                      }
                    />
                    <button
                      aria-label={`Удалить ${instance.key}`}
                      onClick={() =>
                        patch({
                          instances: workspace.instances.filter(
                            (i) => i.key !== instance.key,
                          ),
                        })
                      }
                    >
                      <X size={14} />
                    </button>
                  </div>
                ))}
                <label>
                  Добавить индикатор
                  <select
                    aria-label="Дополнительный индикатор"
                    value=""
                    disabled={workspace.instances.length >= 8}
                    onChange={(e) => {
                      const id = e.target.value as IndicatorInstance["id"];
                      patch({
                        instances: [
                          ...workspace.instances,
                          {
                            key: `${id}-${Date.now()}`,
                            id,
                            period: ["macd", "ichimoku", "vwap"].includes(id)
                              ? undefined
                              : ["sma", "ema", "bollinger", "cci"].includes(id)
                                ? 20
                                : id === "roc"
                                  ? 12
                                  : id === "supertrend"
                                    ? 10
                                    : 14,
                            color: "#fbbf24",
                          },
                        ],
                      });
                    }}
                  >
                    <option value="">Выберите из каталога…</option>
                    {INDICATOR_CATALOG.filter((i) =>
                      isClassicIndicatorId(i.id),
                    ).map((i) => (
                      <option value={i.id} key={i.id}>
                        {i.label}
                      </option>
                    ))}
                  </select>
                </label>
                <p className="ct-source-note">
                  Максимум 15 000 загруженных свечей на график. История
                  подгружается при прокрутке влево. Паттерны и волны — ручная
                  разметка. Угол и окружность измеряются в координатах экрана.
                  Профиль объёма оценивает распределение по OHLC; это не
                  биржевой volume-at-price.
                </p>
                <p className="ct-source-note">
                  Закрытые ликвидационные карты CoinGlass и индикаторы их
                  платного API не подключены. Отправки торговых ордеров нет;
                  Long/Short — расчётные инструменты.
                </p>
              </>
            )}
            {dialog === "presets" && (
              <>
                <label>
                  Название
                  <input
                    maxLength={60}
                    value={presetName}
                    onChange={(e) => setPresetName(e.target.value)}
                  />
                </label>
                <div className="ct-object-actions">
                  <button
                    onClick={() => {
                      if (!presetName.trim()) return;
                      const next = {
                        ...presets,
                        [presetName.trim()]: {
                          workspace,
                          drawings: JSON.parse(
                            localStorage.getItem(
                              `shortlong:charts:v2:${user?.id ?? "guest"}`,
                            ) ?? "{}",
                          ),
                        },
                      };
                      setPresets(next);
                      localStorage.setItem(
                        storage + ":presets",
                        JSON.stringify(next),
                      );
                      void save();
                    }}
                  >
                    Сохранить разметку
                  </button>
                  <button onClick={download}>
                    <Download size={14} /> Экспорт JSON
                  </button>
                  <button onClick={() => file.current?.click()}>
                    <Upload size={14} /> Импорт JSON
                  </button>
                </div>
                <div className="ct-instrument-list">
                  {Object.entries(presets).map(([name, value]) => (
                    <div className="ct-object-row" key={name}>
                      <button
                        onClick={() => {
                          localStorage.setItem(
                            `shortlong:charts:v2:${user?.id ?? "guest"}`,
                            JSON.stringify(
                              normalizeDrawingSets(value.drawings),
                            ),
                          );
                          patch(normalizeWorkspace(value.workspace ?? value));
                          setActive(0);
                          setDialog(null);
                          window.location.reload();
                        }}
                      >
                        {name}
                      </button>
                      <button
                        aria-label={`Удалить ${name}`}
                        onClick={() => {
                          const next = { ...presets };
                          delete next[name];
                          setPresets(next);
                          localStorage.setItem(
                            storage + ":presets",
                            JSON.stringify(next),
                          );
                        }}
                      >
                        <X size={14} />
                      </button>
                    </div>
                  ))}
                </div>
              </>
            )}
          </div>
        </div>
      )}
      <input
        ref={file}
        hidden
        type="file"
        accept=".json,application/json"
        onChange={async (e) => {
          const f = e.target.files?.[0];
          if (!f || f.size > 1024 * 1024) return;
          try {
            const data = JSON.parse(await f.text());
            if (data.workspace?.version !== 2) throw new Error();
            const imported = normalizeDrawingSets(
              typeof data.drawings === "string"
                ? JSON.parse(data.drawings)
                : data.drawings,
            );
            localStorage.setItem(
              `shortlong:charts:v2:${user?.id ?? "guest"}`,
              JSON.stringify(imported),
            );
            patch(normalizeWorkspace(data.workspace));
            setActive(0);
            setDialog(null);
            window.location.reload();
          } catch {
            setNotice("Файл рабочего пространства некорректен");
          }
          e.target.value = "";
        }}
      />
    </div>
  );
}
function InstrumentInfo({
  ticker,
  instrument,
  updated,
}: {
  ticker?: MarketTicker;
  instrument: Instrument;
  updated: number;
}) {
  return (
    <section className="ct-instrument-info">
      <h2>{instrument.symbol}</h2>
      <p className="ct-source-note">
        {instrument.exchange === "bybit" ? "Bybit" : "Binance"} ·{" "}
        {instrument.market === "spot" ? "Спот" : "USDT фьючерсы"}
      </p>
      <p className="ct-big-price">
        {fmt(ticker?.last, 8)} <small>USDT</small>
      </p>
      <p className={(ticker?.change ?? 0) >= 0 ? "ct-up" : "ct-down"}>
        {ticker?.change == null ? "—" : `${ticker.change.toFixed(2)}% за 24ч`}
      </p>
      <dl>
        <dt>Максимум 24ч</dt>
        <dd>{fmt(ticker?.high)}</dd>
        <dt>Минимум 24ч</dt>
        <dd>{fmt(ticker?.low)}</dd>
        <dt>Объём 24ч, USDT</dt>
        <dd>{compact(ticker?.volume)}</dd>
        {instrument.market === "linear" && (
          <>
            <dt>Открытый интерес, USDT</dt>
            <dd>{compact(ticker?.openInterest)}</dd>
            <dt>Финансирование</dt>
            <dd>
              {ticker?.funding == null
                ? "—"
                : (ticker.funding * 100).toFixed(4) + "%"}
            </dd>
          </>
        )}
        <dt>Обновление · UTC</dt>
        <dd>
          {updated
            ? new Date(updated).toLocaleTimeString("ru-RU", { timeZone: "UTC" })
            : "—"}
        </dd>
      </dl>
      <p className="ct-source-note">
        Котировки и свечи поступают напрямую с выбранной биржи. Пустое значение
        означает, что источник его не предоставил.
      </p>
    </section>
  );
}
function Comparison({
  primary,
  symbol,
  interval,
  onClose,
}: {
  primary: Instrument;
  symbol: string;
  interval: ChartInterval;
  onClose: () => void;
}) {
  const a = useMarketData(primary, interval),
    b = useMarketData({ ...primary, symbol }, interval);
  const lines = useMemo(() => {
    const common = new Set(b.candles.map((c) => c.time)),
      start = a.candles.find((c) => common.has(c.time))?.time,
      firstA = a.candles.find((c) => c.time === start)?.close,
      firstB = b.candles.find((c) => c.time === start)?.close;
    if (!start || !firstA || !firstB) return [];
    return [
      {
        name: primary.symbol,
        color: "var(--primary)",
        data: a.candles
          .filter((c) => c.time >= start)
          .map((c) => ({ time: c.time, value: (c.close / firstA - 1) * 100 })),
      },
      {
        name: symbol,
        color: "var(--chart-3)",
        data: b.candles
          .filter((c) => c.time >= start)
          .map((c) => ({ time: c.time, value: (c.close / firstB - 1) * 100 })),
      },
    ];
  }, [a.candles, b.candles, primary.symbol, symbol]);
  return (
    <section className="ct-comparison">
      <header>
        Сравнение доходности · общая начальная свеча · {primary.symbol} /{" "}
        {symbol}
        <button aria-label="Закрыть сравнение" onClick={onClose}>
          <X size={15} />
        </button>
      </header>
      {lines.length ? (
        <TimeSeriesChart
          lines={lines}
          height={210}
          label="Сравнение рыночных цен"
          percent
        />
      ) : (
        <p className="ct-source-note">
          {a.error ||
            b.error ||
            (a.loading || b.loading
              ? "Загружаем данные для сравнения…"
              : "Нет общего периода для сравнения")}
        </p>
      )}
    </section>
  );
}
