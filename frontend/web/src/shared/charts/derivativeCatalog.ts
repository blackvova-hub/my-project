import type { Instrument } from "./marketData";
const item = <T extends string>(
  id: T,
  label: string,
  description: string,
  market: "linear" | "all" = "linear",
  exchange: "both" | "binance" = "both",
) => ({ id, label, description, market, exchange, color: "#38bdf8" });
export const DERIVATIVE_PANELS = [
  item("oi", "Открытый интерес · базовый актив", "Снимки OI выбранной биржи"),
  item(
    "funding",
    "Funding · фактическая ставка, %",
    "Фактически выплаченные ставки; не прогноз",
  ),
  item(
    "funding_weighted",
    "Funding · OI-взвешенная Bybit + Binance, %",
    "Фактические ставки; веса OI в USDT на общих интервалах",
  ),
  item("basis", "Базис · mark/index, %", "(mark / index − 1) × 100"),
  item(
    "ratio",
    "Long/Short · отношение аккаунтов",
    "Количество аккаунтов; не объём позиций",
  ),
  item(
    "oi_delta",
    "Open Interest Delta",
    "Изменение между соседними снимками OI",
  ),
  item(
    "oi_candles",
    "Открытый интерес · свечи снимков",
    "OHLC наблюдений OI с исходным шагом 5м",
  ),
  item(
    "funding_candles",
    "Funding · свечи наблюдений, %",
    "OHLC фактических выплат в интервале",
  ),
  item("premium", "Premium Index, %", "Исторические premium index klines"),
  item(
    "index_price",
    "Индекс цен",
    "Историческая index price выбранного контракта",
  ),
  item(
    "oi_value",
    "Открытый интерес · стоимость, USDT",
    "sumOpenInterestValue из Binance API",
    "linear",
    "binance",
  ),
  item(
    "top_accounts",
    "Long/Short · аккаунты топ-трейдеров",
    "Binance topLongShortAccountRatio",
    "linear",
    "binance",
  ),
  item(
    "top_positions",
    "Long/Short · позиции топ-трейдеров",
    "Binance topLongShortPositionRatio",
    "linear",
    "binance",
  ),
  item(
    "taker_volume",
    "Taker Buy/Sell · объём",
    "Покупки и продажи агрессора в базовом активе",
    "all",
    "binance",
  ),
  item(
    "taker_delta",
    "Taker Volume Delta",
    "Buy volume − sell volume, базовый актив",
    "all",
    "binance",
  ),
  item(
    "taker_value",
    "Taker Buy/Sell · стоимость, USDT",
    "Фактический quote volume покупок/продаж",
    "all",
    "binance",
  ),
  item(
    "taker_value_delta",
    "Taker Value Delta, USDT",
    "Buy quote volume − sell quote volume",
    "all",
    "binance",
  ),
  item(
    "cvd",
    "CVD · накопленная taker delta",
    "Сумма дельт от первого загруженного бара, Binance",
    "all",
    "binance",
  ),
  item(
    "volume_ratio",
    "Futures/Spot Volume Ratio",
    "Отношение quote объёмов двух рынков выбранной биржи",
    "all",
  ),
  item(
    "aggregate_volume",
    "Bybit + Binance · совокупный объём, USDT",
    "Сумма quote объёмов совпадающих баров двух бирж",
    "all",
  ),
  item(
    "aggregate_oi",
    "Bybit + Binance · совокупный OI",
    "OI базового актива двух бирж, только общие наблюдения",
  ),
  item(
    "liquidations",
    "Ликвидации по символу · наблюдавшиеся, USDT",
    "События выбранной биржи из существующих сканеров, до 24ч",
  ),
  item(
    "aggregate_liquidations",
    "Совокупные ликвидации · наблюдавшиеся, USDT",
    "Bybit/Binance/OKX/Bitget/Gate · доступные события, до 24ч",
  ),
  item(
    "fear_greed",
    "Индекс страха и жадности · общий рынок",
    "Alternative.me · дневной ряд 90 дней через API проекта",
    "all",
  ),
] as const;
export type DerivativeId = (typeof DERIVATIVE_PANELS)[number]["id"];
export function derivativeAvailability(
  i: Pick<Instrument, "exchange" | "market">,
  id: DerivativeId,
) {
  const info = DERIVATIVE_PANELS.find((p) => p.id === id)!;
  if (info.market === "linear" && i.market !== "linear")
    return "Доступно на фьючерсах";
  if (info.exchange === "binance" && i.exchange !== "binance")
    return "Исторический ряд доступен в Binance";
  return "";
}
