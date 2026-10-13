import type { Candle } from "./engine";
import type { PerpStreamStatus } from "../market/perpMarketData";

export const LIVE_CANDLE_REFRESH_MS = 1000;

// One poller belongs to the shared instrument/interval feed, not to each window.
// REST snapshots supplement slow/disconnected sockets without rebuilding history.
export function startLiveCandlePolling(
  fetchRows: (signal: AbortSignal, limit: number) => Promise<Candle[]>,
  seconds: number,
  onCandles: (rows: Candle[]) => void,
  onStatus: (status: PerpStreamStatus) => void,
) {
  const abort = new AbortController();
  let busy = false,
    last: Candle | undefined,
    websocket:
      | { candle: Candle; received: number; generation: number }
      | undefined;
  let generation = 0,
    failures = 0,
    retryAt = 0;
  let recovering = false;
  const recover = async () => {
    if (recovering || abort.signal.aborted) return;
    recovering = true;
    try {
      const rows = await fetchRows(abort.signal, 1000);
      // A slow history page must not delay live prices or replace their tail.
      if (!abort.signal.aborted)
        emit(rows.filter((c) => !last || c.time < last.time));
    } catch {
      /* Keep the live tail; retry recovery on the next detected gap. */
    } finally {
      recovering = false;
    }
  };
  const visible = () =>
    typeof document === "undefined" || document.visibilityState === "visible";
  const emit = (rows: Candle[]) => {
    for (const c of rows) if (!last || c.time >= last.time) last = c;
    onCandles(rows);
  };
  const poll = async () => {
    const now = Date.now();
    if (
      abort.signal.aborted ||
      busy ||
      !visible() ||
      now < retryAt ||
      (websocket && now - websocket.received < LIVE_CANDLE_REFRESH_MS)
    )
      return;
    busy = true;
    const started = generation;
    try {
      const signal = AbortSignal.any([abort.signal, AbortSignal.timeout(2500)]);
      let rows = await fetchRows(signal, 2);
      if (abort.signal.aborted) return;
      if (!rows.length) throw new Error("Источник не вернул текущую свечу");
      const gap = last && rows[0].time > last.time + seconds;
      if (websocket && generation !== started)
        rows = rows.filter((c) => c.time !== websocket!.candle.time);
      failures = 0;
      retryAt = 0;
      if (rows.length) emit(rows);
      onStatus("live");
      if (gap) void recover();
    } catch {
      if (!abort.signal.aborted) {
        if (!websocket || Date.now() - websocket.received >= 3000)
          onStatus("offline");
        retryAt = Date.now() + Math.min(1000 * 2 ** ++failures, 30000);
      }
    } finally {
      busy = false;
    }
  };
  const timer = setInterval(() => {
    void poll();
  }, LIVE_CANDLE_REFRESH_MS);
  const resumed = () => {
    if (visible()) {
      retryAt = 0;
      void poll();
    }
  };
  if (typeof document !== "undefined")
    document.addEventListener("visibilitychange", resumed);
  return {
    websocket(c: Candle) {
      if (abort.signal.aborted) return;
      websocket = { candle: c, received: Date.now(), generation: ++generation };
      failures = 0;
      retryAt = 0;
      emit([c]);
      onStatus("live");
    },
    stop() {
      abort.abort();
      clearInterval(timer);
      if (typeof document !== "undefined")
        document.removeEventListener("visibilitychange", resumed);
    },
  };
}
