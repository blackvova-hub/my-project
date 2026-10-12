import { useCallback, useEffect, useRef, useState } from "react";
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
export function useMarketData(instrument: Instrument, interval: ChartInterval) {
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
    const abort = new AbortController();
    request.current = abort;
    page.current = false;
    exhausted.current = false;
    let disposed = false,
      stop = () => {},
      queued: Candle[] = [];
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
      setState((s) =>
        s.key === key
          ? {
              ...s,
              candles: normalizeCandles([...s.candles, ...data]).slice(-15000),
              updated: Date.now(),
              error: "",
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
    void Promise.allSettled([
      fetchMarketCandles(instrument, interval, abort.signal),
      fetchMarketPriceFormat(instrument, abort.signal),
    ]).then(([data, format]) => {
      if (disposed) return;
      if (data.status === "fulfilled") put(data.value);
      else
        setState((s) =>
          s.key === key
            ? {
                ...s,
                loading: false,
                error: data.reason?.message ?? "История недоступна",
              }
            : s,
        );
      if (format.status === "fulfilled")
        setState((s) => (s.key === key ? { ...s, format: format.value } : s));
    });
    const timer = setInterval(() => {
      if (document.visibilityState !== "visible") return;
      void fetchMarketCandles(instrument, interval, abort.signal)
        .then(put)
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
  }, [key, reload]);
  const loadHistory = useCallback(async () => {
    const s = current.current;
    if (
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
  }, [key, instrument, interval]);
  return {
    ...(state.key === key ? state : { ...state, candles: [], loading: true }),
    loadHistory,
    retry: () => setReload((n) => n + 1),
    seconds: CHART_INTERVALS.find((i) => i.value === interval)!.seconds,
  };
}
