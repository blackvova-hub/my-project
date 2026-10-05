export type PrimaryExchange = "bybit" | "binance";

const GUEST_PRIMARY_EXCHANGE_KEY = "primary_exchange_v1";

export function normalizePrimaryExchange(value: unknown): PrimaryExchange | null {
  return value === "bybit" || value === "binance" ? value : null;
}

export function readGuestPrimaryExchange(): PrimaryExchange | null {
  if (typeof window === "undefined") return null;
  try {
    return normalizePrimaryExchange(localStorage.getItem(GUEST_PRIMARY_EXCHANGE_KEY));
  } catch {
    return null;
  }
}

export function writeGuestPrimaryExchange(exchange: PrimaryExchange) {
  if (typeof window === "undefined") return;
  try {
    localStorage.setItem(GUEST_PRIMARY_EXCHANGE_KEY, exchange);
  } catch {
    // Local storage may be unavailable in private mode.
  }
}

export function normalizePerpetualSymbol(symbol: string) {
  const upper = symbol.toUpperCase().trim().replace(/[^A-Z0-9]/g, "");
  return upper.endsWith("PERP") ? upper.slice(0, -4) : upper;
}

export function tradingViewPerpetualSymbol(exchange: PrimaryExchange, symbol: string) {
  const normalized = normalizePerpetualSymbol(symbol);
  const withQuote =
    normalized.endsWith("USDT") || normalized.endsWith("USDC") || normalized.endsWith("USD")
      ? normalized
      : `${normalized}USDT`;
  const venue = exchange === "binance" ? "BINANCE" : "BYBIT";
  return `${venue}:${withQuote}.P`;
}

export function coinGlassTVURL(exchange: PrimaryExchange, symbol: string) {
  const venue = exchange === "binance" ? "Binance" : "Bybit";
  return `https://www.coinglass.com/tv/ru/${venue}_${normalizePerpetualSymbol(symbol)}`;
}

export function exchangeTradeURL(exchange: PrimaryExchange, symbol: string) {
  const normalized = normalizePerpetualSymbol(symbol);
  return exchange === "binance"
    ? `https://www.binance.com/en/futures/${normalized}`
    : `https://www.bybit.com/en-US/trade/usdt/${normalized}`;
}
