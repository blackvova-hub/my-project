import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Search,
  ChevronDown,
  Save,
  LayoutGrid,
  Camera,
  Maximize2,
  Minimize2,
  Star,
  X,
  Settings2,
  Plus,
  RefreshCw,
  Download,
  Upload,
  PanelRightClose,
  PanelRightOpen,
  Link2,
  ArrowUp,
  ArrowDown,
  CalendarDays,
  Radio,
} from "lucide-react";
import { useAuth } from "../../../shared/auth/AuthContext";
import { useTheme } from "../../../shared/theme/ThemeContext";
import { useDialogFocus } from "../../../shared/charts/useDialogFocus";
import {
  CHART_INTERVALS,
  fetchMarketTickers,
  type ChartInterval,
  type MarketTicker,
} from "../../../shared/charts/marketData";
import {
  DERIVATIVE_PANELS,
  derivativeAvailability,
  type DerivativeId,
} from "../../../shared/charts/derivatives";
import { downloadChartScreenshot } from "../../../shared/charts/screenshot";
import { IndicatorMenu } from "../../../shared/market/IndicatorMenu";
import {
  INDICATOR_CATALOG,
  defaultIndicatorPeriod,
  isClassicIndicatorId,
  type IndicatorId,
} from "../../../shared/market/indicatorTypes";
import { normalizeDrawingSets } from "../../../shared/charts/drawings/model";
import {
  logicalViewport,
  exactCandleAt,
  type TimeViewport,
} from "../../../shared/charts/navigation";
import { timestamp } from "../../../shared/charts/engine";
import { loadWatchlists, saveWatchlists } from "../../../shared/watchlist";
import { TerminalChart, type TerminalRuntime } from "./TerminalChart";
import {
  normalizeWorkspace,
  patchWindow,
  layoutShape,
  capturePresetDrawings,
  type ChartWindow,
  type TerminalWorkspace,
} from "./workspace";
import { useTerminalWorkspace } from "./useTerminalWorkspace";
import "../../../shared/charts/charts.css";
import "./terminal.css";

const fmt = (v: number | null | undefined, digits = 5) =>
  v == null
    ? "—"
    : v.toLocaleString("ru-RU", { maximumFractionDigits: digits });
const compact = (v: number | null | undefined) =>
  v == null
    ? "—"
    : v.toLocaleString("ru-RU", {
        notation: "compact",
        maximumFractionDigits: 2,
      });
type Dialog =
  | "symbol"
  | "compare"
  | "settings"
  | "presets"
  | "sources"
  | "date"
  | null;
const dialogNames = {
  symbol: "Выбор инструмента",
  compare: "Сравнить инструмент",
  settings: "Настройки графика",
  presets: "Рабочие пространства",
  sources: "Источники данных",
  date: "Перейти к дате",
};
export default function ChartsPage() {
  const { user, isAuth } = useAuth();
  return (
    <Terminal
      key={user?.id ?? "guest"}
      owner={user?.id ?? "guest"}
      authenticated={isAuth}
    />
  );
}
function Terminal({
  owner,
  authenticated,
}: {
  owner: string;
  authenticated: boolean;
}) {
  const { workspace, update, save, status, conflict, loadRemote } =
      useTerminalWorkspace(owner, authenticated),
    { theme } = useTheme();
  const [dialog, setDialog] = useState<Dialog>(null),
    [search, setSearch] = useState(""),
    [tab, setTab] = useState("market"),
    [watchSearch, setWatchSearch] = useState(""),
    [layoutMenu, setLayoutMenu] = useState(false),
    [syncMenu, setSyncMenu] = useState(false),
    [maximized, setMaximized] = useState<number | null>(null),
    [notice, setNotice] = useState(""),
    [presetName, setPresetName] = useState("Моё рабочее пространство"),
    [date, setDate] = useState(new Date().toISOString().slice(0, 16)),
    [watchLimit, setWatchLimit] = useState(100);
  const root = useRef<HTMLDivElement>(null),
    file = useRef<HTMLInputElement>(null),
    runtimes = useRef(new Map<number, TerminalRuntime>()),
    live = useRef(workspace),
    syncing = useRef(false),
    lastSynced = useRef("");
  live.current = workspace;
  const [smallScreen, setSmallScreen] = useState(
      () => window.matchMedia("(max-width:900px)").matches,
    ),
    [mobileWatch, setMobileWatch] = useState(false);
  const showSidebar = smallScreen ? mobileWatch : workspace.sidebarOpen;
  useEffect(() => {
    const media = window.matchMedia("(max-width:900px)");
    const change = () => setSmallScreen(media.matches);
    media.addEventListener("change", change);
    return () => media.removeEventListener("change", change);
  }, []);
  const w = workspace.windows[workspace.active];
  const closeDialog = useCallback(() => setDialog(null), []);
  useDialogFocus(!!dialog, closeDialog);
  useEffect(() => {
    document.documentElement.classList.add("charts-terminal-page");
    const header = document.querySelector("[data-app-header]");
    const resize = () =>
      root.current?.style.setProperty(
        "--ct-header-height",
        `${header?.getBoundingClientRect().height ?? 60}px`,
      );
    const observer = new ResizeObserver(resize);
    if (header) observer.observe(header);
    resize();
    return () => {
      observer.disconnect();
      document.documentElement.classList.remove("charts-terminal-page");
    };
  }, []);
  const changeWindow = useCallback(
    (index: number, change: Partial<ChartWindow>) =>
      update((current) => {
        if (index === current.active) return patchWindow(current, change);
        return {
          ...current,
          windows: current.windows.map((w, n) =>
            n === index ? { ...w, ...change } : w,
          ),
        };
      }),
    [update],
  );
  const patch = (change: Partial<ChartWindow>) =>
    changeWindow(workspace.active, change);
  const runtime = useCallback(
    (index: number, value: TerminalRuntime | null) => {
      if (value) runtimes.current.set(index, value);
      else runtimes.current.delete(index);
    },
    [],
  );
  const syncViewport = useCallback((index: number, range: TimeViewport) => {
    const current = live.current;
    if (index !== current.active || !current.sync.range || syncing.current)
      return;
    const signature = `${Math.round(range.from)}:${Math.round(range.to)}`;
    if (signature === lastSynced.current) return;
    lastSynced.current = signature;
    syncing.current = true;
    for (const [n, r] of runtimes.current) {
      if (n === index || r.disposed || !r.candles.length) continue;
      const v = logicalViewport(r.candles, range, r.seconds);
      if (v) r.chart.timeScale().setVisibleLogicalRange(v);
    }
    requestAnimationFrame(() => {
      syncing.current = false;
    });
  }, []);
  const syncCrosshair = useCallback((index: number, time: number | null) => {
    if (
      index !== live.current.active ||
      !live.current.sync.crosshair ||
      syncing.current
    )
      return;
    syncing.current = true;
    for (const [n, r] of runtimes.current) {
      if (n === index || r.disposed) continue;
      const c =
        time === null ? null : exactCandleAt(r.candles, time, r.seconds);
      if (c) r.chart.setCrosshairPosition(c.close, timestamp(c.time), r.series);
      else r.chart.clearCrosshairPosition();
    }
    syncing.current = false;
  }, []);
  const maximize = useCallback(
    (index: number) => setMaximized((n) => (n === index ? null : index)),
    [],
  );
  const tickers = useQuery({
    queryKey: ["terminal-tickers", w.exchange, w.market],
    queryFn: ({ signal }) => fetchMarketTickers(w, signal),
    staleTime: 15000,
    refetchInterval: 15000,
    refetchIntervalInBackground: false,
    retry: 1,
  });
  const selected = tickers.data?.find((t) => t.symbol === w.symbol);
  const favorites = workspace.favorites;
  const rows = useMemo(() => {
    const list = (tickers.data ?? []).filter(
      (t) =>
        (tab === "market" || favorites.includes(t.symbol)) &&
        t.symbol.includes(watchSearch.toUpperCase()),
    );
    const field = workspace.sort.field;
    if (field === "manual")
      return list.sort(
        (a, b) => favorites.indexOf(a.symbol) - favorites.indexOf(b.symbol),
      );
    return list.sort((a, b) =>
      field === "symbol"
        ? (workspace.sort.descending ? -1 : 1) *
          a.symbol.localeCompare(b.symbol)
        : a[field === "price" ? "last" : field] == null
          ? 1
          : b[field === "price" ? "last" : field] == null
            ? -1
            : (workspace.sort.descending ? -1 : 1) *
              (a[field === "price" ? "last" : field]! -
                b[field === "price" ? "last" : field]!),
    );
  }, [tickers.data, tab, favorites, watchSearch, workspace.sort]);
  const candidates = (tickers.data ?? [])
    .filter((t) => t.symbol.includes(search.toUpperCase()))
    .slice(0, 150);
  const chooseSymbol = (symbol: string) => {
    patch({ symbol, autoScale: true, priceView: undefined });
    setDialog(null);
    setSearch("");
  };
  const toggleFavorite = (symbol: string) => {
    const next = favorites.includes(symbol)
      ? favorites.filter((s) => s !== symbol)
      : [...favorites, symbol];
    update((current) => ({ ...current, favorites: next }));
    saveWatchlists({ ...loadWatchlists(), hot: next });
  };
  const sort = (field: TerminalWorkspace["sort"]["field"]) =>
    update((current) => ({
      ...current,
      sort: {
        field,
        descending:
          current.sort.field === field
            ? !current.sort.descending
            : field !== "symbol",
      },
    }));
  const navigate = async (seconds: number | null, time?: number) => {
    const n = live.current.active;
    let r = runtimes.current.get(n);
    if (!r || r.disposed || !r.candles.length) return;
    try {
      if (time !== undefined) {
        if (time > Date.now() / 1000) {
          setNotice("Выберите дату в прошлом");
          return;
        }
        setNotice("Загружаем историю выбранной даты…");
        const candles = await r.loadAround(time);
        if (!candles.length) {
          setNotice("История выбранной даты недоступна");
          return;
        }
        await new Promise((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(resolve)),
        );
        r = runtimes.current.get(n);
        if (!r || r.disposed) return;
      }
      const end = r.candles.at(-1)!.time,
        range =
          time !== undefined
            ? { from: time - r.seconds * 90, to: time + r.seconds * 90 }
            : seconds === null
              ? { from: r.candles[0].time, to: end + r.seconds * 8 }
              : { from: end - seconds, to: end + r.seconds * 8 };
      const logical = logicalViewport(r.candles, range, r.seconds);
      if (logical) r.chart.timeScale().setVisibleLogicalRange(logical);
      changeWindow(n, { view: range, follow: time === undefined });
      setNotice("");
    } catch {
      setNotice("Не удалось загрузить историю · текущий график сохранён");
    }
  };
  const latest = () => {
    const r = runtimes.current.get(workspace.active);
    if (!r || r.disposed) return;
    r.chart.timeScale().scrollToRealTime();
    const scale = r.chart.timeScale().getVisibleLogicalRange();
    if (scale)
      syncViewport(workspace.active, {
        from: r.candles.at(-1)!.time - r.seconds * (scale.to - scale.from),
        to: r.candles.at(-1)!.time + r.seconds * 12,
      });
    patch({ follow: true, view: undefined });
  };
  const screenshot = () => {
    const r = runtimes.current.get(workspace.active),
      surface = root.current?.querySelector(
        `[data-window-id="${w.id}"] [data-chart-surface]`,
      ) as HTMLElement | null;
    if (r && surface && !r.disposed)
      void downloadChartScreenshot(r, surface).catch(() =>
        setNotice("Не удалось создать снимок"),
      );
  };
  const download = () => {
    const url = URL.createObjectURL(
      new Blob(
        [
          JSON.stringify(
            {
              workspace,
              drawings: normalizeDrawingSets(
                JSON.parse(
                  localStorage.getItem(`shortlong:charts:v2:${owner}`) ?? "{}",
                ),
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
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (
        (e.target as HTMLElement)?.closest(
          "input,textarea,select,[contenteditable=true]",
        ) ||
        dialog
      )
        return;
      if (e.key === "Escape") {
        setLayoutMenu(false);
        setSyncMenu(false);
        setMaximized(null);
      }
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") {
        e.preventDefault();
        void save();
      }
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  }, [save, dialog]);
  const shape = layoutShape(workspace.layout);
  return (
    <div
      className="ct-terminal ct-terminal-v3"
      ref={root}
      data-theme={theme}
      data-layout={workspace.layout}
      style={
        {
          "--ct-sidebar-width": `${workspace.sidebarWidth}px`,
          "--ct-columns": maximized !== null || smallScreen ? 1 : shape.columns,
          "--ct-rows": maximized !== null || smallScreen ? 1 : shape.rows,
        } as CSSProperties
      }
    >
      <h1 className="sr-only">Графики</h1>
      <div className="ct-toolbar ct-primary-toolbar">
        <button
          className="ct-instrument-button"
          onClick={() => {
            setDialog("symbol");
            setSearch("");
          }}
        >
          <Search size={14} />
          {w.symbol}
          <ChevronDown size={12} />
        </button>
        <select
          aria-label="Биржа"
          value={w.exchange}
          onChange={(e) =>
            patch({ exchange: e.target.value as ChartWindow["exchange"] })
          }
        >
          <option value="bybit">Bybit</option>
          <option value="binance">Binance</option>
        </select>
        <select
          aria-label="Рынок"
          value={w.market}
          onChange={(e) =>
            patch({ market: e.target.value as ChartWindow["market"] })
          }
        >
          <option value="linear">Фьючерсы</option>
          <option value="spot">Спот</option>
        </select>
        <span className="ct-toolbar-divider" />
        <div className="ct-timeframe-buttons">
          {CHART_INTERVALS.map((t) => (
            <button
              key={t.value}
              aria-label={`Таймфрейм ${t.label}`}
              aria-pressed={w.interval === t.value}
              onClick={() => patch({ interval: t.value })}
            >
              {t.label}
            </button>
          ))}
        </div>
        <select
          className="ct-mobile-timeframe"
          aria-label="Таймфрейм"
          value={w.interval}
          onChange={(e) => patch({ interval: e.target.value as ChartInterval })}
        >
          {CHART_INTERVALS.map((t) => (
            <option key={t.value} value={t.value}>
              {t.label}
            </option>
          ))}
        </select>
        <select
          aria-label="Тип графика"
          value={w.mode}
          onChange={(e) =>
            patch({ mode: e.target.value as ChartWindow["mode"] })
          }
        >
          <option value="candles">Свечи</option>
          <option value="bars">Бары</option>
          <option value="line">Линия</option>
          <option value="area">Область</option>
        </select>
        <IndicatorMenu
          active={w.indicators}
          extraCount={w.instances.length}
          onToggle={(id) =>
            patch({
              indicators: w.indicators.includes(id)
                ? w.indicators.filter((i) => i !== id)
                : [...w.indicators, id],
            })
          }
          onClear={() =>
            patch({ indicators: [], instances: [], derivatives: [] })
          }
          external={DERIVATIVE_PANELS.map((i) => ({
            ...i,
            unavailable: derivativeAvailability(w, i.id),
          }))}
          activeExternal={w.derivatives}
          onExternalToggle={(value) => {
            const id = value as DerivativeId;
            patch({
              derivatives: w.derivatives.includes(id)
                ? w.derivatives.filter((i) => i !== id)
                : [...w.derivatives, id],
            });
          }}
          onCompare={() => {
            setDialog("compare");
            setSearch("");
          }}
          volumeActive={w.volume}
          onVolumeToggle={() => patch({ volume: !w.volume })}
        />
        <button
          className="ct-compare-button"
          onClick={() => {
            setDialog("compare");
            setSearch("");
          }}
        >
          Сравнить
        </button>
        <span className="ct-toolbar-divider" />
        <div className="ct-toolbar-menu-anchor">
          <button
            aria-label="Количество графиков"
            aria-expanded={layoutMenu}
            onClick={() => {
              setLayoutMenu(!layoutMenu);
              setSyncMenu(false);
            }}
          >
            <LayoutGrid size={15} />
            {workspace.layout}
            <ChevronDown size={11} />
          </button>
          {layoutMenu && (
            <div
              className="ct-layout-menu"
              role="menu"
              aria-label="Раскладка графиков"
            >
              {Array.from({ length: 9 }, (_, n) => n + 1).map((n) => (
                <button
                  key={n}
                  role="menuitemradio"
                  aria-checked={workspace.layout === n}
                  aria-label={`${n} ${n === 1 ? "график" : n < 5 ? "графика" : "графиков"}`}
                  onClick={() => {
                    update((v) => ({
                      ...v,
                      layout: n,
                      active: Math.min(v.active, n - 1),
                    }));
                    setMaximized(null);
                    setLayoutMenu(false);
                  }}
                >
                  <span
                    className="ct-layout-preview"
                    style={{
                      gridTemplateColumns: `repeat(${layoutShape(n).columns},1fr)`,
                    }}
                  >
                    {Array.from({ length: n }, (_, i) => (
                      <i key={i} />
                    ))}
                  </span>
                  {n}
                </button>
              ))}
            </div>
          )}
        </div>
        <div className="ct-toolbar-menu-anchor">
          <button
            aria-label="Синхронизация графиков"
            aria-expanded={syncMenu}
            onClick={() => {
              setSyncMenu(!syncMenu);
              setLayoutMenu(false);
            }}
          >
            <Link2 size={15} />
          </button>
          {syncMenu && (
            <div
              className="ct-sync-menu"
              role="group"
              aria-label="Синхронизация"
            >
              {(
                [
                  ["symbol", "Инструмент и биржа"],
                  ["interval", "Таймфрейм"],
                  ["crosshair", "Перекрестие"],
                  ["range", "Диапазон времени"],
                ] as const
              ).map(([key, label]) => (
                <label key={key}>
                  <input
                    type="checkbox"
                    checked={workspace.sync[key]}
                    onChange={(e) => {
                      const checked = e.target.checked;
                      update((v) => {
                        const next = {
                          ...v,
                          sync: { ...v.sync, [key]: checked },
                        };
                        return checked &&
                          (key === "symbol" || key === "interval")
                          ? patchWindow(
                              next,
                              key === "symbol"
                                ? {
                                    symbol: w.symbol,
                                    exchange: w.exchange,
                                    market: w.market,
                                  }
                                : { interval: w.interval },
                            )
                          : next;
                      });
                    }}
                  />
                  {label}
                </label>
              ))}
            </div>
          )}
        </div>
        {smallScreen && workspace.layout > 1 && (
          <select
            aria-label="Активное окно"
            value={workspace.active}
            onChange={(e) =>
              update((v) => ({ ...v, active: Number(e.target.value) }))
            }
          >
            {workspace.windows.slice(0, workspace.layout).map((v, n) => (
              <option key={v.id} value={n}>
                {n + 1}: {v.symbol}
              </option>
            ))}
          </select>
        )}
        <button
          aria-label="Выбрать дату графика"
          onClick={() => setDialog("date")}
        >
          <CalendarDays size={15} />
        </button>
        <button
          aria-label="Настройки графика"
          onClick={() => setDialog("settings")}
        >
          <Settings2 size={15} />
        </button>
        <button
          aria-label="Рабочие пространства"
          title="Рабочие пространства"
          onClick={() => setDialog("presets")}
        >
          <LayoutGrid size={15} />
          <span className="ct-desktop-label">Разметки</span>
        </button>
        <button
          aria-label="Сохранить рабочее пространство"
          onClick={() => void save()}
        >
          <Save size={15} />
          <span className="ct-desktop-label">Сохранить</span>
        </button>
        <button aria-label="Снимок графика" onClick={screenshot}>
          <Camera size={15} />
        </button>
        <button
          aria-label="Полный экран терминала"
          onClick={() => {
            if (document.fullscreenElement) void document.exitFullscreen();
            else
              void root.current
                ?.requestFullscreen()
                .catch(() => setNotice("Полный экран недоступен"));
          }}
        >
          <Maximize2 size={15} />
        </button>
        <button
          aria-label="Показать список рынка"
          aria-pressed={showSidebar}
          onClick={() => {
            if (smallScreen) setMobileWatch((v) => !v);
            else update((v) => ({ ...v, sidebarOpen: !v.sidebarOpen }));
          }}
        >
          {showSidebar ? (
            <PanelRightClose size={15} />
          ) : (
            <PanelRightOpen size={15} />
          )}
        </button>
      </div>
      <div className={`ct-work-area ${showSidebar ? "" : "ct-sidebar-hidden"}`}>
        <div className="ct-chart-grid">
          {workspace.windows.slice(0, workspace.layout).map((window, index) => (
            <div
              key={window.id}
              className="ct-grid-cell"
              style={{
                display:
                  (maximized !== null && maximized !== index) ||
                  (smallScreen && index !== workspace.active)
                    ? "none"
                    : undefined,
              }}
              onPointerDown={() => {
                if (workspace.active !== index)
                  update((v) => ({ ...v, active: index }));
              }}
              onFocus={() => {
                if (workspace.active !== index)
                  update((v) => ({ ...v, active: index }));
              }}
            >
              <TerminalChart
                window={window}
                visible={
                  (!smallScreen || index === workspace.active) &&
                  (maximized === null || maximized === index)
                }
                index={index}
                active={workspace.active === index}
                onChange={changeWindow}
                onRuntime={runtime}
                onCrosshair={syncCrosshair}
                onViewport={syncViewport}
                onMaximize={maximize}
                onSave={() => void save()}
              />
            </div>
          ))}
        </div>
        {showSidebar && (
          <>
            <div
              className="ct-sidebar-resizer"
              role="separator"
              aria-label="Ширина списка рынка"
              aria-orientation="vertical"
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
                  e.preventDefault();
                  update((v) => ({
                    ...v,
                    sidebarWidth: Math.max(
                      220,
                      Math.min(
                        460,
                        v.sidebarWidth + (e.key === "ArrowLeft" ? 16 : -16),
                      ),
                    ),
                  }));
                }
              }}
              onPointerDown={(e) => {
                const start = e.clientX,
                  width = workspace.sidebarWidth;
                e.currentTarget.setPointerCapture(e.pointerId);
                const element = e.currentTarget;
                const move = (event: PointerEvent) => {
                  const next = Math.max(
                    220,
                    Math.min(460, width + start - event.clientX),
                  );
                  root.current?.style.setProperty(
                    "--ct-sidebar-width",
                    `${next}px`,
                  );
                };
                const end = (event: PointerEvent) => {
                  element.removeEventListener("pointermove", move);
                  element.removeEventListener("pointerup", end);
                  update((v) => ({
                    ...v,
                    sidebarWidth: Math.max(
                      220,
                      Math.min(460, width + start - event.clientX),
                    ),
                  }));
                };
                element.addEventListener("pointermove", move);
                element.addEventListener("pointerup", end, { once: true });
              }}
            />
            <aside className="ct-terminal-rail">
              <div className="ct-watch-tabs">
                <button
                  aria-pressed={tab === "market"}
                  onClick={() => {
                    setTab("market");
                    update((v) => ({
                      ...v,
                      sort: { field: "volume", descending: true },
                    }));
                  }}
                >
                  Рынок
                </button>
                <button
                  aria-pressed={tab === "favorites"}
                  onClick={() => {
                    setTab("favorites");
                    update((v) => ({
                      ...v,
                      sort: { field: "manual", descending: false },
                    }));
                  }}
                >
                  Избранное
                </button>
                <button
                  aria-label="Добавить инструмент"
                  onClick={() => {
                    setDialog("symbol");
                    setSearch("");
                  }}
                >
                  <Plus size={15} />
                </button>
              </div>
              <div className="ct-watch-search">
                <Search size={13} />
                <input
                  aria-label="Поиск в списке инструментов"
                  placeholder="Поиск инструментов…"
                  value={watchSearch}
                  onChange={(e) => {
                    setWatchSearch(e.target.value);
                    setWatchLimit(100);
                  }}
                />
              </div>
              <div
                className="ct-watch-table"
                onScroll={(e) => {
                  const el = e.currentTarget;
                  if (el.scrollHeight - el.scrollTop - el.clientHeight < 120)
                    setWatchLimit((n) => Math.min(rows.length, n + 100));
                }}
              >
                <table>
                  <thead>
                    <tr>
                      {(
                        [
                          ["symbol", "Инструмент"],
                          ["price", "Цена"],
                          ["change", "24ч"],
                          ["volume", "Объём"],
                        ] as const
                      ).map(([field, label]) => (
                        <th
                          key={field}
                          aria-sort={
                            workspace.sort.field === field
                              ? workspace.sort.descending
                                ? "descending"
                                : "ascending"
                              : "none"
                          }
                        >
                          <button onClick={() => sort(field)}>
                            {label}
                            {workspace.sort.field === field ? (
                              workspace.sort.descending ? (
                                <ArrowDown size={10} />
                              ) : (
                                <ArrowUp size={10} />
                              )
                            ) : null}
                          </button>
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {rows.slice(0, watchLimit).map((t) => (
                      <tr key={t.symbol} data-selected={t.symbol === w.symbol}>
                        <td>
                          <button
                            aria-label={`Избранное ${t.symbol}`}
                            aria-pressed={favorites.includes(t.symbol)}
                            onClick={() => toggleFavorite(t.symbol)}
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
                          <button
                            aria-label={`Открыть ${t.symbol}`}
                            onClick={() => chooseSymbol(t.symbol)}
                            onKeyDown={(e) => {
                              if (
                                e.key === "ArrowDown" ||
                                e.key === "ArrowUp"
                              ) {
                                e.preventDefault();
                                const n =
                                  rows.findIndex((i) => i.symbol === t.symbol) +
                                  (e.key === "ArrowDown" ? 1 : -1);
                                if (rows[n]) chooseSymbol(rows[n].symbol);
                              }
                            }}
                          >
                            {t.symbol}
                          </button>
                          {tab === "favorites" && (
                            <button
                              className="ct-favorite-move"
                              aria-label={`Поднять избранное ${t.symbol}`}
                              onClick={() => {
                                const list = [...favorites],
                                  n = list.indexOf(t.symbol);
                                if (n > 0) {
                                  [list[n - 1], list[n]] = [
                                    list[n],
                                    list[n - 1],
                                  ];
                                  update((v) => ({ ...v, favorites: list }));
                                  saveWatchlists({
                                    ...loadWatchlists(),
                                    hot: list,
                                  });
                                }
                              }}
                            >
                              <ArrowUp size={10} />
                            </button>
                          )}
                        </td>
                        <td>{fmt(t.last)}</td>
                        <td
                          className={(t.change ?? 0) >= 0 ? "ct-up" : "ct-down"}
                        >
                          {t.change == null
                            ? "—"
                            : `${t.change >= 0 ? "+" : ""}${t.change.toFixed(2)}%`}
                        </td>
                        <td>{compact(t.volume)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                {tickers.isLoading && (
                  <p className="ct-source-note">Загружаем инструменты…</p>
                )}
                {tickers.error && (
                  <p role="alert">
                    Каталог недоступен{" "}
                    <button onClick={() => void tickers.refetch()}>
                      Повторить
                    </button>
                  </p>
                )}
                {!tickers.isLoading && !tickers.error && !rows.length && (
                  <p className="ct-source-note">
                    Нет инструментов для выбранного фильтра
                  </p>
                )}
              </div>
              <InstrumentInfo
                window={w}
                ticker={selected}
                updated={tickers.dataUpdatedAt}
              />
            </aside>
          </>
        )}
      </div>
      <div className="ct-terminal-bottom ct-navigation-toolbar">
        <div className="ct-range-buttons">
          {(
            [
              ["1д", 86400],
              ["5д", 432000],
              ["1м", 2592000],
              ["3м", 7776000],
              ["6м", 15552000],
              ["Все", null],
            ] as const
          ).map(([label, seconds]) => (
            <button key={label} onClick={() => void navigate(seconds)}>
              {label}
            </button>
          ))}
        </div>
        <label className="ct-date-control">
          <CalendarDays size={13} />
          <input
            aria-label="Дата графика UTC"
            type="datetime-local"
            value={date}
            onChange={(e) => setDate(e.target.value)}
          />
          <button
            aria-label="Перейти к дате"
            onClick={() => void navigate(null, Date.parse(date + "Z") / 1000)}
          >
            Перейти
          </button>
        </label>
        <span role="status" className="ct-save-status" title={notice || status}>
          {notice || status}
        </span>
        {conflict && (
          <button
            onClick={() =>
              void loadRemote().catch(() =>
                setNotice("Не удалось загрузить версию аккаунта"),
              )
            }
          >
            Загрузить из аккаунта
          </button>
        )}
        {maximized !== null && (
          <button
            aria-label="Вернуть раскладку"
            onClick={() => setMaximized(null)}
          >
            <Minimize2 size={13} />
          </button>
        )}
        <button
          aria-label="Сброс масштаба"
          onClick={() => {
            void navigate(
              180 * (runtimes.current.get(workspace.active)?.seconds ?? 3600),
            );
            patch({ autoScale: true });
          }}
        >
          <RefreshCw size={13} />
        </button>
        <button
          aria-pressed={w.autoScale}
          onClick={() => patch({ autoScale: !w.autoScale })}
        >
          Auto
        </button>
        <button
          aria-pressed={w.log}
          onClick={() => patch({ log: !w.log, percent: false })}
        >
          Лог
        </button>
        <button
          aria-pressed={w.percent}
          onClick={() => patch({ percent: !w.percent, log: false })}
        >
          %
        </button>
        <span>UTC</span>
        <button
          className="ct-live-button"
          aria-pressed={w.follow}
          onClick={latest}
        >
          <Radio size={13} />К рынку
        </button>
      </div>
      {dialog && (
        <div className="ct-modal-backdrop" onClick={closeDialog}>
          <section
            className="ct-dialog"
            role="dialog"
            aria-modal="true"
            aria-label={dialogNames[dialog]}
            onClick={(e) => e.stopPropagation()}
          >
            <header>
              <strong>{dialogNames[dialog]}</strong>
              <button aria-label="Закрыть" onClick={closeDialog}>
                <X size={16} />
              </button>
            </header>
            {(dialog === "symbol" || dialog === "compare") && (
              <>
                <input
                  autoFocus
                  aria-label="Поиск инструмента"
                  placeholder="BTC, ETH, SOL…"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                />
                <p className="ct-source-note">
                  {w.exchange} ·{" "}
                  {w.market === "spot" ? "Спот" : "USDT фьючерсы"}
                </p>
                <div className="ct-instrument-list">
                  {candidates.map((t) => (
                    <button
                      key={t.symbol}
                      onClick={() => {
                        if (dialog === "compare") {
                          if (w.comparisons.length >= 4) {
                            setNotice("Доступно четыре сравнения на окно");
                            return;
                          }
                          patch({
                            comparisons: [
                              ...w.comparisons,
                              {
                                symbol: t.symbol,
                                exchange: w.exchange,
                                market: w.market,
                                id: `compare-${Date.now()}`,
                                mode: "percent",
                                color: "#38bdf8",
                              },
                            ],
                          });
                          setDialog(null);
                        } else chooseSymbol(t.symbol);
                      }}
                    >
                      <strong>{t.symbol}</strong>
                      <span>{fmt(t.last)}</span>
                      <span
                        className={(t.change ?? 0) >= 0 ? "ct-up" : "ct-down"}
                      >
                        {fmt(t.change, 2)}%
                      </span>
                    </button>
                  ))}
                </div>
                {tickers.error && (
                  <button onClick={() => void tickers.refetch()}>
                    Повторить загрузку каталога
                  </button>
                )}
              </>
            )}
            {dialog === "settings" && (
              <>
                <div className="ct-settings-toggles">
                  {(
                    [
                      ["grid", "Сетка"],
                      ["volume", "Объём"],
                      ["autoScale", "Автоматический масштаб"],
                      ["follow", "Следовать за рынком"],
                    ] as const
                  ).map(([key, label]) => (
                    <label key={key}>
                      <input
                        type="checkbox"
                        checked={w[key]}
                        onChange={(e) => patch({ [key]: e.target.checked })}
                      />
                      {label}
                    </label>
                  ))}
                </div>
                <h3>Активные индикаторы</h3>
                {w.indicators.map((id) => (
                  <label className="ct-form-row" key={id}>
                    <span>
                      {INDICATOR_CATALOG.find((i) => i.id === id)?.shortLabel}
                    </span>
                    {defaultIndicatorPeriod(id) !== undefined && (
                      <input
                        aria-label={id.toUpperCase()}
                        type="number"
                        min="2"
                        max="400"
                        value={
                          w.studySettings[id]?.period ??
                          w.periods[id] ??
                          defaultIndicatorPeriod(id)
                        }
                        onChange={(e) =>
                          patch({
                            periods: {
                              ...w.periods,
                              [id]: Number(e.target.value),
                            },
                          })
                        }
                      />
                    )}
                    <button
                      aria-label={`Добавить экземпляр ${id}`}
                      disabled={
                        !isClassicIndicatorId(id) || w.instances.length >= 16
                      }
                      onClick={() => {
                        if (isClassicIndicatorId(id))
                          patch({
                            instances: [
                              ...w.instances,
                              {
                                id,
                                key: `study-${Date.now()}`,
                                period:
                                  w.periods[id] ?? defaultIndicatorPeriod(id),
                              },
                            ],
                          });
                      }}
                    >
                      <Plus size={14} />
                    </button>
                  </label>
                ))}
                <label>
                  Добавить экземпляр
                  <select
                    aria-label="Дополнительный индикатор"
                    value=""
                    disabled={w.instances.length >= 16}
                    onChange={(e) =>
                      patch({
                        instances: [
                          ...w.instances,
                          {
                            id: e.target
                              .value as ChartWindow["instances"][number]["id"],
                            key: `study-${Date.now()}`,
                            period: defaultIndicatorPeriod(
                              e.target.value as IndicatorId,
                            ),
                          },
                        ],
                      })
                    }
                  >
                    <option value="">Выберите индикатор…</option>
                    {INDICATOR_CATALOG.filter((i) =>
                      isClassicIndicatorId(i.id),
                    ).map((i) => (
                      <option key={i.id} value={i.id}>
                        {i.label}
                      </option>
                    ))}
                  </select>
                </label>
                <h3>Шкалы панелей</h3>
                {[
                  "volume",
                  ...w.indicators.filter(
                    (id) =>
                      INDICATOR_CATALOG.find((i) => i.id === id)?.placement ===
                      "pane",
                  ),
                  ...w.instances
                    .filter(
                      (i) =>
                        INDICATOR_CATALOG.find((p) => p.id === i.id)
                          ?.placement === "pane",
                    )
                    .map((i) => i.key),
                  ...w.derivatives.map((id) => `external:${id}`),
                ].map((key) => (
                  <div className="ct-panel-scale-row" key={key}>
                    <span>{key}</span>
                    {(
                      [
                        ["auto", "Auto"],
                        ["log", "Лог"],
                        ["inverted", "Инверсия"],
                      ] as const
                    ).map(([option, label]) => (
                      <label key={option}>
                        <input
                          type="checkbox"
                          checked={
                            option === "auto"
                              ? w.panels.scales[key]?.auto !== false
                              : w.panels.scales[key]?.[option] === true
                          }
                          onChange={(e) =>
                            patch({
                              panels: {
                                ...w.panels,
                                scales: {
                                  ...w.panels.scales,
                                  [key]: {
                                    ...w.panels.scales[key],
                                    [option]: e.target.checked,
                                  },
                                },
                              },
                            })
                          }
                        />
                        {label}
                      </label>
                    ))}
                  </div>
                ))}
                <button onClick={() => setDialog("sources")}>
                  Источники и ограничения данных
                </button>
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
                    disabled={
                      !presetName.trim() ||
                      Object.keys(workspace.presets).length >= 12
                    }
                    onClick={() =>
                      update((v) => ({
                        ...v,
                        presets: {
                          ...v.presets,
                          [presetName.trim()]: {
                            windows: v.windows,
                            layout: v.layout,
                            drawings: capturePresetDrawings(
                              v,
                              JSON.parse(
                                localStorage.getItem(
                                  `shortlong:charts:v2:${owner}`,
                                ) ?? "{}",
                              ),
                            ),
                          },
                        },
                      }))
                    }
                  >
                    <Save size={14} />
                    Сохранить набор
                  </button>
                  <button onClick={download}>
                    <Download size={14} />
                    Экспорт JSON
                  </button>
                  <button onClick={() => file.current?.click()}>
                    <Upload size={14} />
                    Импорт JSON
                  </button>
                </div>
                {Object.entries(workspace.presets).map(([name, p]) => (
                  <div className="ct-object-row" key={name}>
                    <button
                      onClick={() => {
                        if (p.drawings) {
                          const stored = normalizeDrawingSets(
                            JSON.parse(
                              localStorage.getItem(
                                `shortlong:charts:v2:${owner}`,
                              ) ?? "{}",
                            ),
                          );
                          localStorage.setItem(
                            `shortlong:charts:v2:${owner}`,
                            JSON.stringify({ ...stored, ...p.drawings }),
                          );
                          window.dispatchEvent(
                            new Event("shortlong:drawings-restored"),
                          );
                        }
                        update((v) => ({
                          ...v,
                          windows: p.windows,
                          layout: p.layout,
                          active: 0,
                        }));
                        setDialog(null);
                        setMaximized(null);
                      }}
                    >
                      {name}
                    </button>
                    <button
                      aria-label={`Удалить набор ${name}`}
                      onClick={() =>
                        update((v) => ({
                          ...v,
                          presets: Object.fromEntries(
                            Object.entries(v.presets).filter(
                              ([key]) => key !== name,
                            ),
                          ),
                        }))
                      }
                    >
                      <X size={14} />
                    </button>
                  </div>
                ))}
              </>
            )}
            {dialog === "date" && (
              <>
                <label>
                  Дата и время UTC
                  <input
                    aria-label="Выбрать дату UTC"
                    type="datetime-local"
                    value={date}
                    onChange={(e) => setDate(e.target.value)}
                  />
                </label>
                <button
                  onClick={() => {
                    setDialog(null);
                    void navigate(null, Date.parse(date + "Z") / 1000);
                  }}
                >
                  Перейти
                </button>
              </>
            )}
            {dialog === "sources" && (
              <>
                <p>
                  Свечи и котировки: публичные API Bybit / Binance. История
                  графика — до 15 000 свечей. Текущая свеча обновляется из
                  потока биржи; при паузах потока резервная проверка — раз в
                  секунду. При ошибках источника повторные запросы замедляются.
                </p>
                <p>
                  Панели деривативов обновляются раз в минуту. Доступность
                  зависит от биржи, рынка и глубины истории провайдера. Значения
                  агрегатов относятся только к указанным источникам.
                </p>
                {DERIVATIVE_PANELS.filter((p) =>
                  w.derivatives.includes(p.id),
                ).map((p) => (
                  <p key={p.id}>
                    <strong>{p.label}</strong>
                    <br />
                    {p.description}
                  </p>
                ))}
                <ProviderAvailability />
                <p>
                  Профиль объёма по OHLC — оценка, не точный volume-at-price.
                  Long/Short — расчётная разметка; торговые ордера не
                  отправляются.
                </p>
              </>
            )}
          </section>
        </div>
      )}
      <input
        ref={file}
        type="file"
        accept="application/json,.json"
        hidden
        onChange={async (e) => {
          const f = e.target.files?.[0];
          if (!f) return;
          try {
            if (f.size > 1024 * 1024) throw new Error();
            const raw = JSON.parse(await f.text());
            const value = raw.workspace ?? raw;
            if (
              ![2, 3].includes(value?.version) ||
              (value.version === 3 && !Array.isArray(value.windows))
            )
              throw new Error();
            const next = normalizeWorkspace(value);
            if (raw.drawings) {
              localStorage.setItem(
                `shortlong:charts:v2:${owner}`,
                JSON.stringify(
                  normalizeDrawingSets(
                    typeof raw.drawings === "string"
                      ? JSON.parse(raw.drawings)
                      : raw.drawings,
                  ),
                ),
              );
              window.dispatchEvent(new Event("shortlong:drawings-restored"));
            }
            update(next);
            setMaximized(null);
            setDialog(null);
            setNotice("Рабочее пространство импортировано");
          } catch {
            setNotice("Некорректный JSON или файл больше 1 МБ");
          }
          e.target.value = "";
        }}
      />
    </div>
  );
}
function InstrumentInfo({
  window: w,
  ticker: t,
  updated,
}: {
  window: ChartWindow;
  ticker?: MarketTicker;
  updated: number;
}) {
  return (
    <section className="ct-instrument-info">
      <h2>{w.symbol}</h2>
      <p className="ct-source-note">
        {w.exchange === "bybit" ? "Bybit" : "Binance"} ·{" "}
        {w.market === "spot" ? "Спот" : "USDT фьючерсы"}
      </p>
      <p className="ct-big-price">
        {fmt(t?.last)} <small>USDT</small>
      </p>
      <p className={(t?.change ?? 0) >= 0 ? "ct-up" : "ct-down"}>
        {t?.change == null
          ? "—"
          : `${t.change > 0 ? "+" : ""}${t.change.toFixed(2)}% за 24ч`}
      </p>
      <dl>
        {[
          ["Максимум 24ч", fmt(t?.high)],
          ["Минимум 24ч", fmt(t?.low)],
          ["Объём 24ч, USDT", compact(t?.volume)],
          ["Открытый интерес, USDT", compact(t?.openInterest)],
          [
            "Финансирование",
            t?.funding == null ? "—" : (t.funding * 100).toFixed(4) + "%",
          ],
          [
            "Обновление · UTC",
            updated
              ? new Date(updated).toLocaleTimeString("ru-RU", {
                  timeZone: "UTC",
                })
              : "—",
          ],
        ].map(([label, value]) => (
          <div key={label}>
            <dt>{label}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}
function ProviderAvailability() {
  const query = useQuery({
    queryKey: ["chart-orderflow-capabilities"],
    queryFn: async ({ signal }) => {
      const response = await fetch("/api/chart/orderflow/capabilities", {
        signal,
        credentials: "include",
      });
      if (!response.ok) throw new Error();
      return response.json() as Promise<{
        layers: { id: string; available: boolean; requires: string }[];
      }>;
    },
    retry: false,
  });
  return (
    <div className="ct-provider-capabilities">
      <h3>Слои анализа потока</h3>
      {query.data?.layers.map((layer) => (
        <p key={layer.id}>
          <strong>{layer.id}</strong>:{" "}
          {layer.available ? "Источник подключён" : layer.requires}
        </p>
      )) ?? (
        <p>
          Liquidation Heatmap, Order Book Heatmap, Footprint и Large Trades
          требуют подключённого источника. Детали адаптера — в документации
          терминала.
        </p>
      )}
    </div>
  );
}
