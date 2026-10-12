import { MarketChart } from "../charts/MarketChart";
import type { PrimaryExchange } from "../exchange/primaryExchange";
import { normalizeLinearPerpSymbol, type PerpInterval } from "./perpMarketData";
export function PerpChart({
  exchange,
  symbol,
  interval = "1m",
}: {
  exchange: PrimaryExchange;
  symbol: string;
  interval?: PerpInterval;
}) {
  return (
    <MarketChart
      exchange={exchange}
      market="linear"
      symbol={normalizeLinearPerpSymbol(symbol)}
      interval={interval}
      embedded
    />
  );
}
