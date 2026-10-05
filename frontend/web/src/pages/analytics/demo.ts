import type { Candle, Dataset, Fill, Ledger, Position, Trade } from "./types";
import { DAY, sum } from "./model.ts";

// A reproducible, explicitly opt-in demonstration. No demo record is persisted
// as an exchange event or substituted for a failed API request.
export function makeDemo(): Dataset {
  const now = Date.UTC(2026, 8, 26, 23, 59),
    start = now - 90 * DAY;
  let seed = 712031;
  const random = () => {
    seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0;
    return seed / 4294967296;
  };
  const connections = ["bybit", "binance"].map((exchange, i) => ({
    id: `demo-${exchange}`,
    exchange,
    accountType: i ? "futures" : "unified",
    name: i ? "Фьючерсный счёт" : "Основной счёт",
    status: "ready" as const,
    error: "",
    warnings: [],
    coverageFrom: new Date(start).toISOString(),
    syncedThrough: new Date(now).toISOString(),
    lastSync: new Date(now).toISOString(),
  }));
  const trades: Trade[] = [],
    ledger: Ledger[] = [];
  const symbols = ["BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"];
  const prices = [63820, 3410, 146.8, 0.62, 0.128];
  const event = (
    connectionId: string,
    exchange: string,
    at: number,
    category: string,
    amount: number,
    symbol = "",
    tradeId = "",
  ) =>
    ledger.push({
      id: `demo-event-${ledger.length}`,
      connectionId,
      exchange,
      at,
      category,
      amount,
      amountExact: amount.toFixed(8),
      symbol,
      tradeId,
      currency: "USDT",
    });
  for (let day = 0; day < 90; day++) {
    for (let n = 0; n < 6; n++) {
      const index = Math.floor(random() * 5),
        symbol = symbols[index],
        account = connections[n % 2];
      const leverage = [2, 4, 7, 12, 15][Math.floor(random() * 5)];
      const side = random() > 0.28 ? "LONG" : "SHORT";
      const openedAt = start + day * DAY + n * 3.6 * 3600000;
      const duration = [3, 18, 75, 240, 530][Math.floor(random() * 5)];
      const closedAt = openedAt + duration * 60000;
      if (closedAt > now) continue;
      const entry = prices[index] * (0.93 + random() * 0.14);
      const quantity =
        (index === 0
          ? 0.1
          : index === 1
            ? 2
            : index === 2
              ? 45
              : index === 3
                ? 8000
                : 30000) *
        (0.6 + random());
      const fees = entry * quantity * 0.00065;
      const funding = -entry * quantity * (random() * 0.00035);
      const advantage =
        index === 0 ? 0.78 : index === 1 ? 0.7 : index === 2 ? 0.34 : 0.53;
      const win = random() < (leverage >= 10 ? advantage * 0.58 : advantage);
      const gross =
        (win ? 1 : -1) * (90 + random() * 640) * (index === 0 ? 1.6 : 1);
      const exit = entry + (gross / quantity) * (side === "LONG" ? 1 : -1);
      const id = `demo-trade-${day}-${n}`;
      const mfe = win ? gross * (1.1 + random() * 1.9) : 50 + random() * 200;
      const mae = win ? -random() * 260 : gross * (1.1 + random());
      const trade: Trade = {
        id,
        connectionId: account.id,
        exchange: account.exchange,
        symbol,
        market: "linear",
        side,
        openedAt,
        closedAt,
        entry,
        exit,
        quantity,
        remaining: 0,
        size: entry * quantity,
        gross,
        fees,
        funding,
        net: gross - fees + funding,
        duration,
        leverage,
        mfe,
        mae,
        captured: (gross / mfe) * 100,
        stopLoss: entry * (side === "LONG" ? 0.975 : 1.025),
        takeProfit: entry * (side === "LONG" ? 1.045 : 0.955),
        liquidation: entry * (1 + (side === "LONG" ? -1 : 1) / leverage),
        complete: true,
        tag: day % 3 === 0 ? "Пробой" : day % 3 === 1 ? "Откат" : "Диапазон",
        strategy: day % 2 ? "По тренду" : "Возврат к среднему",
        fills: [],
      };
      const definitions = [
        ["Entry", 0, 0.4, entry],
        ["Add", 0.2, 0.6, entry],
        ["Partial close", 0.72, 0.35, exit],
        ["Exit", 1, 0.65, exit],
      ] as const;
      trade.fills = definitions.map(
        ([action, portion, share, price], i): Fill => ({
          id: `${id}-fill-${i}`,
          connectionId: account.id,
          exchange: account.exchange,
          symbol,
          market: "linear",
          positionSide: side,
          side:
            i < 2
              ? side === "LONG"
                ? "BUY"
                : "SELL"
              : side === "LONG"
                ? "SELL"
                : "BUY",
          at: openedAt + (closedAt - openedAt) * portion,
          price,
          quantity: quantity * share,
          fee: (fees * share) / 2,
          feeCurrency: "USDT",
          feeKnown: true,
          maker: i === 1,
          action,
        }),
      );
      trade.fills.forEach((f) =>
        event(account.id, account.exchange, f.at, "fees", -f.fee, symbol, id),
      );
      event(
        account.id,
        account.exchange,
        closedAt,
        "trading",
        gross,
        symbol,
        id,
      );
      event(
        account.id,
        account.exchange,
        openedAt + (closedAt - openedAt) * 0.5,
        "funding",
        funding,
        symbol,
        id,
      );
      trades.push(trade);
    }
    if ([20, 62].includes(day))
      event("demo-bybit", "bybit", start + day * DAY, "deposit", 7500);
    if (day === 73)
      event("demo-binance", "binance", start + day * DAY, "withdrawal", -1500);
    if (day % 15 === 0)
      event("demo-bybit", "bybit", start + day * DAY + 1000, "rewards", 76);
  }
  const snapshots: Dataset["snapshots"] = [];
  for (let day = 0; day <= 90; day++)
    for (const [i, account] of connections.entries()) {
      const at = start + day * DAY;
      const wallet =
        (i ? 34000 : 58000) +
        sum(
          ledger.filter((l) => l.connectionId === account.id && l.at <= at),
          (l) => l.amount,
        );
      const unrealized = day === 0 ? 0 : Math.sin(day * 0.17) * (i ? 210 : 340);
      const equity = wallet + unrealized;
      const positions: Position[] =
        day === 0
          ? []
          : [
              {
                symbol: i ? "ETHUSDT" : "BTCUSDT",
                side: "LONG",
                quantity: i ? 22 : 2.1,
                entry: i ? 3290 : 63110,
                mark: i ? 3410 : 63820,
                notional: i ? 75020 : 134022,
                unrealized,
                leverage: 4,
                stopLoss: null,
                takeProfit: null,
                liquidation: null,
              },
              {
                symbol: "SOLUSDT",
                side: "SHORT",
                quantity: i ? 65 : 110,
                entry: 150,
                mark: 146.8,
                notional: (i ? 65 : 110) * 146.8,
                unrealized: 0,
                leverage: 3,
                stopLoss: null,
                takeProfit: null,
                liquidation: null,
              },
            ];
      snapshots.push({
        connectionId: account.id,
        at,
        equity,
        wallet,
        unrealized,
        margin: equity * 0.31,
        btcPrice: 61000 * (1 + day * 0.0005 + Math.sin(day * 0.13) * 0.032),
        assets: [
          { symbol: "BTC", value: equity * 0.48 },
          { symbol: "ETH", value: equity * 0.24 },
          { symbol: "SOL", value: equity * 0.13 },
          { symbol: "USDT", value: equity * 0.1 },
          { symbol: "Прочее", value: equity * 0.05 },
        ],
        positions,
      });
    }
  return {
    connections,
    trades: trades.sort((a, b) => b.openedAt - a.openedAt),
    ledger: ledger.sort((a, b) => a.at - b.at),
    snapshots,
    warnings: [],
    serverTime: now,
  };
}
export function demoCandles(t: Trade): Candle[] {
  const count = Math.max(12, Math.min(400, Math.ceil(t.duration))),
    seconds = (t.closedAt! - t.openedAt) / 1000 / count;
  return Array.from({ length: count }, (_, i) => {
    const progress = i / count,
      trend = t.entry + (t.exit - t.entry) * progress,
      wave =
        Math.sin(i * 0.76) * t.entry * 0.0015 * Math.sin(progress * Math.PI),
      open = trend + wave,
      close = open + Math.sin(i * 2.3) * t.entry * 0.0008;
    return {
      time: Math.floor(t.openedAt / 1000 + i * seconds),
      open,
      close,
      high: Math.max(open, close) + t.entry * 0.0008,
      low: Math.min(open, close) - t.entry * 0.0008,
    };
  });
}
