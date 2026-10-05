export type Candle = {
  time: number;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
};
export type ArchiveSymbol = { symbol: string; first: number; last: number };
export const intervals = {
  "1m": 60,
  "5m": 300,
  "15m": 900,
  "1h": 3600,
  "4h": 14400,
} as const;
export type ArchiveQuery = {
  exchange: "bybit" | "binance";
  market: "spot" | "linear";
  symbol: string;
  timeframe: keyof typeof intervals;
};
export const speeds = [0.25, 0.5, 1, 2, 5, 10, 20];
export type Position = {
  side: "long" | "short";
  entry: number;
  quantity: number;
  time: number;
  notional: number;
};
export type Trade = Position & {
  exit: number;
  exitTime: number;
  pnl: number;
};
export type ReplayState = {
  candles: Candle[];
  cursor: number;
  mode: "browse" | "select" | "paused" | "playing";
  speed: number;
  selectionOrigin: "browse" | "paused";
  balance: number;
  position: Position | null;
  trades: Trade[];
  error: string;
};
export const initialState: ReplayState = {
  candles: [],
  cursor: -1,
  mode: "browse",
  speed: 1,
  selectionOrigin: "browse",
  balance: 10000,
  position: null,
  trades: [],
  error: "",
};
export type Action =
  | { type: "load"; candles: Candle[]; startTime?: number }
  | { type: "append"; candles: Candle[] }
  | { type: "select" | "cancelSelect" | "toggle" | "prev" | "next" | "exit" | "close" }
  | { type: "start"; index: number }
  | { type: "speed"; value: number }
  | { type: "open"; side: "long" | "short"; amount: number };

export function floatingPnl(position: Position | null, price: number) {
  return position
    ? (price - position.entry) *
        position.quantity *
        (position.side === "long" ? 1 : -1)
    : 0;
}

export const replayStartIndex = (length: number) =>
  length ? Math.min(length - 1, Math.max(0, Math.floor(length * 0.35))) : -1;

export function replayIndexAtOrAfter(candles: Candle[], time: number) {
  if (!candles.length) return -1;
  const prior = indexAtTime(candles, time);
  return Math.min(candles.length - 1, Math.max(0, prior + (candles[prior]?.time < time ? 1 : 0)));
}

export function replayReducer(state: ReplayState, action: Action): ReplayState {
  switch (action.type) {
    case "load":
      return {
        ...initialState,
        candles: action.candles,
        cursor: action.startTime === undefined ? replayStartIndex(action.candles.length) : replayIndexAtOrAfter(action.candles, action.startTime),
        mode: action.candles.length ? "paused" : "browse",
        speed: state.speed,
      };
    case "append":
      return action.candles.length && action.candles[0].time > (state.candles.at(-1)?.time ?? 0)
        ? { ...state, candles: [...state.candles, ...action.candles] }
        : state;
    case "select":
      return state.candles.length
        ? {
            ...state,
            mode: "select",
            selectionOrigin: state.mode === "browse" ? "browse" : "paused",
          }
        : state;
    case "cancelSelect":
      return state.mode === "select"
        ? { ...state, mode: state.selectionOrigin }
        : state;
    case "start":
      return Number.isInteger(action.index) &&
        action.index >= 0 &&
        action.index < state.candles.length
        ? {
            ...initialState,
            candles: state.candles,
            cursor: action.index,
            mode: "paused",
            speed: state.speed,
          }
        : state;
    case "toggle":
      return state.mode === "playing"
        ? { ...state, mode: "paused" }
        : state.mode === "paused" && state.cursor < state.candles.length - 1
          ? { ...state, mode: "playing" }
          : state;
    case "next": {
      if (state.mode !== "playing" && state.mode !== "paused") return state;
      const cursor = Math.min(state.candles.length - 1, state.cursor + 1);
      return {
        ...state,
        cursor,
        mode: cursor === state.candles.length - 1 ? "paused" : state.mode,
      };
    }
    case "prev":
      return state.mode === "paused" && state.cursor > 0 && !state.position && state.trades.length === 0
        ? { ...state, cursor: state.cursor - 1 }
        : state;
    case "exit":
      return {
        ...state,
        mode: "browse",
        cursor: state.candles.length - 1,
        position: null,
        balance: 10000,
        trades: [],
        error: "",
      };
    case "speed":
      return speeds.includes(action.value)
        ? { ...state, speed: action.value }
        : state;
    case "open": {
      const candle = state.candles[state.cursor];
      if (
        (state.mode !== "paused" && state.mode !== "playing") ||
        !candle ||
        state.position
      )
        return state;
      if (
        !Number.isFinite(action.amount) ||
        action.amount <= 0 ||
        action.amount > state.balance
      ) {
        return {
          ...state,
          error: "Укажите размер позиции больше нуля и не выше доступного баланса.",
        };
      }
      return {
        ...state,
        error: "",
        mode: "paused",
        position: {
          side: action.side,
          entry: candle.close,
          quantity: action.amount / candle.close,
          time: candle.time,
          notional: action.amount,
        },
      };
    }
    case "close": {
      const candle = state.candles[state.cursor],
        p = state.position;
      if (
        (state.mode !== "paused" && state.mode !== "playing") ||
        !candle ||
        !p
      )
        return state;
      const gross = floatingPnl(p, candle.close);
      return {
        ...state,
        position: null,
        mode: "paused",
        balance: state.balance + gross,
        error: "",
        trades: [
          ...state.trades,
          {
            ...p,
            exit: candle.close,
            exitTime: candle.time,
            pnl: gross,
          },
        ],
      };
    }
  }
}

export function validateCandles(rows: Candle[]): Candle[] {
  let previous = -1;
  for (const c of rows) {
    if (
      ![c.time, c.open, c.high, c.low, c.close, c.volume].every(
        Number.isFinite,
      ) ||
      !Number.isInteger(c.time) ||
      c.time <= previous ||
      c.time <= 0 ||
      c.low <= 0 ||
      c.volume < 0 ||
      c.high < Math.max(c.open, c.close) ||
      c.low > Math.min(c.open, c.close)
    )
      throw new Error("Архив содержит некорректные свечи");
    previous = c.time;
  }
  return rows;
}
export function indexAtTime(candles: Candle[], time: number) {
  let low = 0,
    high = candles.length;
  while (low < high) {
    const mid = (low + high) >>> 1;
    if (candles[mid].time <= time) low = mid + 1;
    else high = mid;
  }
  return low - 1;
}
export const utc = (time: number) =>
  new Date(time * 1000).toLocaleString("ru-RU", {
    timeZone: "UTC",
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
export const price = (value: number) =>
  value.toLocaleString("en-US", {
    maximumFractionDigits: Math.abs(value) < 1 ? 8 : 4,
  });
