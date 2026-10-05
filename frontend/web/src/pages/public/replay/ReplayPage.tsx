import {
  useCallback,
  useEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
} from "react";
import { useQuery } from "@tanstack/react-query";
import { http } from "../../../shared/api/http";
import { fetchReplayRange, replayWindow } from "./archive";
import { ArchiveForm } from "./ArchiveForm";
import { ReplayChart } from "./ReplayChart";
import { ReplayControls } from "./ReplayControls";
import { PaperTradePanel, TradeJournal } from "./PaperTrading";
import { Icon } from "./Icons";
import {
  indexAtTime,
  initialState,
  intervals,
  replayReducer,
  type ArchiveQuery,
  type ArchiveSymbol,
} from "./model";
import "./replay.css";

function defaultQuery(): ArchiveQuery {
  return {
    exchange: "bybit",
    market: "linear",
    symbol: "BTCUSDT",
    timeframe: "5m",
  };
}

export default function ReplayPage() {
  const [query, setQuery] = useState(defaultQuery),
    [loaded, setLoaded] = useState<ArchiveQuery | null>(null);
  const [state, dispatch] = useReducer(replayReducer, initialState);
  const [loading, setLoading] = useState(false),
    [error, setError] = useState(""),
    [missing, setMissing] = useState(0),
    [returnToken, setReturnToken] = useState(0),
    [session, setSession] = useState(0);
  const request = useRef<AbortController | null>(null);
  const initialized = useRef(false);
  const loadingMore = useRef(false);
  const requestedEnd = useRef(0);
  const archiveEnd = useRef(0);
  const activeSymbol = useRef<ArchiveSymbol | null>(null);
  const activeMarket = useRef("");
  const dialog = useRef<HTMLDialogElement>(null);
  const catalog = useQuery({
    queryKey: ["replay-symbols", query.exchange, query.market],
    queryFn: ({ signal }) =>
      http<{ symbols: ArchiveSymbol[] }>(
        `/replay/symbols?exchange=${query.exchange}&market=${query.market}`,
        { signal },
      ),
    staleTime: 60000,
    retry: 1,
  });
  const visible = useMemo(
    () => state.candles.slice(0, state.cursor + 1),
    [state.candles, state.cursor],
  );
  const load = useCallback(async (nextQuery: ArchiveQuery, selectedTime?: number) => {
    const sameCatalog = query.exchange === nextQuery.exchange && query.market === nextQuery.market;
    const symbol = (sameCatalog ? catalog.data?.symbols.find((entry) => entry.symbol === nextQuery.symbol) : undefined)
      ?? (activeMarket.current === `${nextQuery.exchange}:${nextQuery.market}:${nextQuery.symbol}` ? activeSymbol.current : null);
    if (!symbol) { setError("Выберите инструмент из доступного архива"); return; }
    const anchor = selectedTime ?? (symbol.last / 1000 - 1000 * intervals[nextQuery.timeframe]);
    const range = replayWindow(nextQuery, symbol, anchor);
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    setLoading(true);
    setError("");
    try {
      const data = await fetchReplayRange(nextQuery, range.from, range.to, controller.signal);
      if (controller.signal.aborted) return;
      dispatch({ type: "load", candles: data.candles, startTime: range.anchor });
      setLoaded(nextQuery);
      activeSymbol.current = symbol;
      activeMarket.current = `${nextQuery.exchange}:${nextQuery.market}:${nextQuery.symbol}`;
      setMissing(data.missing);
      setSession((s) => s + 1);
      requestedEnd.current = range.to;
      archiveEnd.current = Math.floor(Math.min(symbol.last + intervals[nextQuery.timeframe] * 1000, Date.now()) / (intervals[nextQuery.timeframe] * 1000)) * intervals[nextQuery.timeframe] * 1000;
      dialog.current?.close();
    } catch (e) {
      if (!controller.signal.aborted) setError((e as Error).message);
    } finally {
      if (!controller.signal.aborted) setLoading(false);
    }
  }, [catalog.data, query.exchange, query.market]);
  useEffect(() => {
    if (!initialized.current && catalog.data) {
      initialized.current = true;
      const initial = catalog.data.symbols.find((item) => item.symbol === query.symbol);
      if (initial) void load(query);
      else setError("BTCUSDT пока нет в архиве. Выберите доступный инструмент.");
    }
  }, [catalog.data, load, query]);
  useEffect(() => () => request.current?.abort(), []);
  useEffect(() => {
    if (!loaded || loading || loadingMore.current || !state.candles.length || state.candles.length - state.cursor > 1000 || requestedEnd.current >= archiveEnd.current) return;
    const controller = request.current;
    if (!controller) return;
    loadingMore.current = true;
    const next = async () => {
      try {
        const step = intervals[loaded.timeframe] * 1000;
        while (!controller.signal.aborted && requestedEnd.current < archiveEnd.current) {
          const from = requestedEnd.current;
          const to = Math.min(archiveEnd.current, from + 2000 * step);
          const data = await fetchReplayRange(loaded, from, to, controller.signal);
          if (controller.signal.aborted) return;
          requestedEnd.current = to;
          if (data.candles.length) {
            dispatch({ type: "append", candles: data.candles.filter((candle) => candle.time > (state.candles.at(-1)?.time ?? 0)) });
            setMissing((count) => count + data.missing);
            return;
          }
        }
      } catch (e) {
        if (!controller.signal.aborted) setError((e as Error).message);
      } finally { loadingMore.current = false; }
    };
    void next();
  }, [loaded, loading, state.cursor, state.candles]);
  const openArchive = () => {
    if (state.mode === "playing") dispatch({ type: "toggle" });
    dialog.current?.showModal();
  };
  const start = useCallback((index: number) => {
    dispatch({ type: "start", index });
    setSession((s) => s + 1);
  }, []);
  const selectTime = useCallback(
    (time: number) => start(indexAtTime(state.candles, time)),
    [start, state.candles],
  );
  useEffect(() => {
    if (state.mode !== "playing" || loading) return;
    const timer = window.setInterval(
      () => dispatch({ type: "next" }),
      1000 / state.speed,
    );
    return () => clearInterval(timer);
  }, [state.mode, state.speed, loading]);
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (
        loading ||
        dialog.current?.open ||
        e.repeat ||
        e.ctrlKey ||
        e.metaKey ||
        e.altKey ||
        (e.target as HTMLElement).closest(
          'input,select,textarea,button,summary,[contenteditable="true"]',
        )
      )
        return;
      if (e.code === "Space") {
        e.preventDefault();
        dispatch({ type: "toggle" });
      }
      if (e.code === "ArrowRight" && state.mode === "paused") {
        e.preventDefault();
        dispatch({ type: "next" });
      }
      if (e.code === "Escape") dispatch({ type: "cancelSelect" });
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  }, [state.mode, loading]);
  const title = loaded
    ? `${loaded.symbol} · ${loaded.exchange === "bybit" ? "Bybit" : "Binance"} · ${loaded.market === "spot" ? "Spot" : "Futures"} · ${loaded.timeframe}`
    : "";
  return (
    <div className="rp-page">
      <h1 className="rp-sr-only">Бэктест</h1>
      <dialog
        ref={dialog}
        className="rp-archive-dialog"
        aria-labelledby="archive-title"
      >
        <div className="rp-dialog-title">
          <h2 id="archive-title">Выбрать инструмент</h2>
          <button
            className="rp-icon"
            aria-label="Закрыть выбор архива"
            onClick={() => dialog.current?.close()}
          >
            <Icon name="close" />
          </button>
        </div>
        <ArchiveForm
          query={query}
          onChange={(q) => {
            request.current?.abort();
            setLoading(false);
            setQuery(q);
            setError("");
          }}
          symbols={catalog.data?.symbols ?? []}
          catalogLoading={catalog.isPending}
          loading={loading}
          onSubmit={() => void load(query)}
        />
        {error || catalog.error ? (
          <div role="alert" className="rp-error">
            {error || catalog.error?.message}
            {catalog.error ? (
              <button onClick={() => catalog.refetch()}>Повторить</button>
            ) : null}
          </div>
        ) : null}
      </dialog>
      {error ? (
        <div role="alert" className="rp-error rp-page-error">
          {error}
          <button onClick={openArchive}>Выбрать инструмент</button>
        </div>
      ) : null}
      {loaded && visible.length ? (
        <div className="rp-workspace" aria-busy={loading}>
          <div className="rp-workspace-content" inert={loading}>
            <section
              className="rp-terminal"
              aria-label="Рабочее место Replay"
            >
              <div className="rp-session-grid">
                <div className="rp-chart-column">
                  <ReplayChart
                    candles={visible}
                    selecting={state.mode === "select"}
                    onSelect={selectTime}
                    title={title}
                    interval={intervals[loaded.timeframe]}
                    returnToken={returnToken}
                    session={session}
                    position={state.position}
                    onArchive={openArchive}
                  >
                    <ReplayControls
                      state={state}
                      dispatch={dispatch}
                      onChooseDate={(time) => { if (loaded) void load(loaded, time); }}
                      firstTime={activeSymbol.current?.first ?? 0}
                      lastTime={activeSymbol.current?.last ?? 0}
                      onReturn={() => setReturnToken((t) => t + 1)}
                    />
                  </ReplayChart>
                </div>
                <PaperTradePanel state={state} dispatch={dispatch} />
              </div>
            </section>
            <TradeJournal state={state} />
            {missing > 0 ? (
              <p className="rp-gap" role="status">
                В архиве отсутствует {missing} свечей. Пропуски не заполняются.
              </p>
            ) : null}
          </div>
          {loading ? (
            <div className="rp-loading-overlay" role="status">
              <div className="rp-loader" />
              <span>Открываем архив…</span>
            </div>
          ) : null}
        </div>
      ) : (
        <div className="rp-empty-chart" role="status">
          {loading ? (
            <>
              <div className="rp-loader" />
              <h2>Открываем архив</h2>
            </>
          ) : (
            <>
              <Icon name="indicators" />
              <h2>
                {loaded
                  ? "В этом периоде пока нет свечей"
                  : "Выберите инструмент"}
              </h2>
              <p>Выберите пару из скачанного архива.</p>
              <button className="rp-primary" onClick={openArchive}>
                Выбрать инструмент
              </button>
            </>
          )}
        </div>
      )}
    </div>
  );
}
