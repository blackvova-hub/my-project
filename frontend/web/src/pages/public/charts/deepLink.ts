/** Only supported exchange instruments may be opened from a URL. */
export function chartInstrumentFromSearch(search: string): { symbol: string; exchange: "bybit" | "binance"; market: "spot" | "linear" } | null {
  const params = new URLSearchParams(search);
  const symbol = (params.get("symbol") ?? "").trim().toUpperCase();
  const exchange = params.get("exchange") ?? "bybit";
  const market = params.get("market") ?? "linear";
  if (!/^[A-Z0-9]{2,24}USDT$/.test(symbol)) return null;
  if (exchange !== "bybit" && exchange !== "binance") return null;
  if (market !== "spot" && market !== "linear") return null;
  return { symbol, exchange, market };
}
