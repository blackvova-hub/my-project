import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  fetchMarketCandles,
  fetchMarketPriceFormat,
  subscribeMarketCandles,
  instrumentKey,
  CHART_INTERVALS,
  type ChartInterval,
  type Instrument,
} from "./marketData";
import { normalizeCandles, type Candle } from "./engine";
import type {
  PerpPriceFormat,
  PerpStreamStatus,
} from "../market/perpMarketData";
export function useMarketData(
  input: Instrument,
  interval: ChartInterval,
  enabled = true,
) {
  const instrument = useMemo(
    () => ({
      exchange: input.exchange,
      market: input.market,
      symbol: input.symbol,
    }),
    [input.exchange, input.market, input.symbol],
  );
  const key = `${instrumentKey(instrument)}:${interval}`;
  const [state, setState] = useState<{
    key: string;
    candles: Candle[];
    loading: boolean;
    error: string;
    format: PerpPriceFormat | null;
    status: PerpStreamStatus;
    updated: number;
  }>({
    key,
    candles: [],
    loading: true,
    error: "",
    format: null,
    status: "connecting",
    updated: 0,
  });
  const current = useRef(state),
    request = useRef<AbortController | null>(null),
    page = useRef(false),
    exhausted = useRef(false),
    [reload, setReload] = useState(0);
  useEffect(() => {
    current.current = state;
  }, [state]);
  useEffect(() => {
    if (!enabled) return;
    const abort = new AbortController();
    request.current = abort;
    page.current = false;
    exhausted.current = false;
    let disposed = false,
      primed = false,
      historyError = "",
      stop = () => {},
      queued: Candle[] = [];
    const bootstrap = new Map<number, Candle>();
    setState({
      key,
      candles: [],
      loading: true,
      error: "",
      format: null,
      status: "connecting",
      updated: 0,
    });
    const put = (data: Candle[]) => {
      if (disposed) return;
      if (!primed) {
        for (const candle of data) bootstrap.set(candle.time, candle);
        while (bootstrap.size > 1000)
          bootstrap.delete(bootstrap.keys().next().value!);
        return;
      }
      setState((s) =>
        s.key === key
          ? {
              ...s,
              candles: normalizeCandles([...s.candles, ...data]).slice(-15000),
              updated: Date.now(),
              error: historyError,
              loading: false,
            }
          : s,
      );
    };
    let raf: number | null = null;
    stop = subscribeMarketCandles(
      instrument,
      interval,
      (c) => {
        queued.push(c);
        if (raf === null)
          raf = requestAnimationFrame(() => {
            raf = null;
            const data = queued;
            queued = [];
            put(data);
          });
      },
      (status) => {
        if (!disposed) setState((s) => (s.key === key ? { ...s, status } : s));
      },
    );
    void fetchMarketCandles(instrument, interval, abort.signal)
      .then((rows) => {
        if (disposed) return;
        primed = true;
        put([...rows, ...bootstrap.values()]);
        bootstrap.clear();
      })
      .catch((error) => {
        if (disposed) return;
        primed = true;
        historyError = error?.message ?? "История недоступна";
        put([...bootstrap.values()]);
        bootstrap.clear();
      });
    void fetchMarketPriceFormat(instrument, abort.signal)
      .then((format) => {
        if (!disposed) setState((s) => (s.key === key ? { ...s, format } : s));
      })
      .catch(() => {
        /* Actual candle precision remains the fallback. */
      });
    const timer = setInterval(() => {
      if (document.visibilityState !== "visible") return;
      void fetchMarketCandles(instrument, interval, abort.signal)
        .then((rows) => {
          historyError = "";
          put(rows);
        })
        .catch(() => {
          if (!disposed) setState((s) => ({ ...s, status: "offline" }));
        });
    }, 30000);
    return () => {
      disposed = true;
      abort.abort();
      stop();
      clearInterval(timer);
      if (raf !== null) cancelAnimationFrame(raf);
    };
    // Primitive identity prevents reconnecting on parent renders.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, reload, enabled]);
  const loadHistory = useCallback(async () => {
    const s = current.current;
    if (
      !enabled ||
      s.key !== key ||
      s.loading ||
      !s.candles.length ||
      page.current ||
      exhausted.current ||
      s.candles.length >= 15000 ||
      !request.current
    )
      return;
    page.current = true;
    try {
      const before = s.candles[0].time,
        rows = await fetchMarketCandles(
          instrument,
          interval,
          request.current.signal,
          before,
        );
      if (!rows.length) exhausted.current = true;
      setState((s) =>
        s.key === key
          ? { ...s, candles: normalizeCandles([...rows, ...s.candles]) }
          : s,
      );
    } catch (e) {
      if ((e as Error).name !== "AbortError")
        setState((s) =>
          s.key === key
            ? {
                ...s,
                error:
                  "Более ранняя история недоступна. Текущие свечи сохранены.",
              }
            : s,
        );
    } finally {
      page.current = false;
    }
  }, [key, instrument, interval, enabled]);
  const loadAround = useCallback(
    async (time: number) => {
      const s = current.current;
      if (s.key !== key || !request.current) return [];
      if (
        s.candles.length &&
        time >= s.candles[0].time &&
        time <= s.candles.at(-1)!.time
      )
        return s.candles;
      const signal = request.current.signal;
      const seconds = CHART_INTERVALS.find(
        (i) => i.value === interval,
      )!.seconds;
      const rows = await fetchMarketCandles(
        instrument,
        interval,
        signal,
        time + seconds * 250,
      );
      if (signal.aborted || !rows.length) return [];
      const merged = normalizeCandles([...rows, ...s.candles.slice(-1000)]);
      setState((v) =>
        v.key === key ? { ...v, candles: merged, error: "" } : v,
      );
      return merged;
    },
    [key, instrument, interval],
  );
  const retry = useCallback(() => setReload((n) => n + 1), []);
  return {
    ...(state.key === key ? state : { ...state, candles: [], loading: true }),
    loadHistory,
    loadAround,
    retry,
    seconds: CHART_INTERVALS.find((i) => i.value === interval)!.seconds,
  };
}
