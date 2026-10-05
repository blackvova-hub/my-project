export const INDICATOR_IDS = [
  "sma",
  "ema",
  "bollinger",
  "vwap",
  "supertrend",
  "ichimoku",
  "rsi",
  "macd",
  "atr",
  "stochastic",
  "adx",
  "cci",
  "roc",
  "swing_points",
  "bos",
  "choch",
  "mss",
  "order_blocks",
  "fvg",
  "liquidity",
  "equal_high_low",
  "liquidity_sweeps",
] as const;

export type IndicatorId = (typeof INDICATOR_IDS)[number];
export type IndicatorPlacement = "overlay" | "pane";

export type IndicatorCatalogItem = {
  id: IndicatorId;
  label: string;
  shortLabel: string;
  description: string;
  placement: IndicatorPlacement;
  color: string;
  group?: "classic" | "smart-money" | "oscillator";
};

export const INDICATOR_CATALOG: readonly IndicatorCatalogItem[] = [
  { id: "sma", label: "SMA · Простая средняя", shortLabel: "SMA 20", description: "Период 20", placement: "overlay", color: "#4ade80" },
  { id: "ema", label: "EMA · Экспоненциальная", shortLabel: "EMA 20", description: "Период 20", placement: "overlay", color: "#38bdf8" },
  { id: "bollinger", label: "Полосы Боллинджера", shortLabel: "BB 20", description: "20 · отклонение 2", placement: "overlay", color: "#a78bfa" },
  { id: "vwap", label: "VWAP", shortLabel: "VWAP", description: "Средняя по объёму", placement: "overlay", color: "#fbbf24" },
  { id: "supertrend", label: "Supertrend", shortLabel: "ST 10×3", description: "ATR 10 · множитель 3", placement: "overlay", color: "#2dd4bf" },
  { id: "ichimoku", label: "Облако Ишимоку", shortLabel: "Ichimoku", description: "9 · 26 · 52", placement: "overlay", color: "#fb7185" },
  { id: "rsi", label: "RSI", shortLabel: "RSI 14", description: "Индекс относительной силы", placement: "pane", color: "#c084fc" },
  { id: "macd", label: "MACD", shortLabel: "MACD", description: "12 · 26 · 9", placement: "pane", color: "#38bdf8" },
  { id: "atr", label: "ATR", shortLabel: "ATR 14", description: "Средний истинный диапазон", placement: "pane", color: "#fb923c" },
  { id: "stochastic", label: "Stochastic", shortLabel: "Stoch 14", description: "%K 14 · %D 3", placement: "pane", color: "#22d3ee" },
  { id: "adx", label: "ADX", shortLabel: "ADX 14", description: "Сила и направление тренда", placement: "pane", color: "#facc15" },
  { id: "cci", label: "CCI", shortLabel: "CCI 20", description: "Индекс товарного канала", placement: "pane", color: "#f472b6" },
  { id: "roc", label: "ROC", shortLabel: "ROC 12", description: "Скорость изменения", placement: "pane", color: "#a3e635" },
  { id: "swing_points", label: "Swing High / Swing Low", shortLabel: "Swing H/L", description: "Подтверждённые фракталы 5 + 5", placement: "overlay", color: "#e2e8f0", group: "smart-money" },
  { id: "bos", label: "BOS · Break of Structure", shortLabel: "BOS", description: "Закрытие за структурным swing по тренду", placement: "overlay", color: "#22c55e", group: "smart-money" },
  { id: "choch", label: "CHOCH · Change of Character", shortLabel: "CHOCH", description: "Первый подтверждённый слом против тренда", placement: "overlay", color: "#f59e0b", group: "smart-money" },
  { id: "mss", label: "MSS · Market Structure Shift", shortLabel: "MSS", description: "CHOCH с ATR-displacement и сильным закрытием", placement: "overlay", color: "#a78bfa", group: "smart-money" },
  { id: "order_blocks", label: "Bullish / Bearish Order Blocks", shortLabel: "Order Blocks", description: "Последняя встречная свеча перед structural break", placement: "overlay", color: "#2dd4bf", group: "smart-money" },
  { id: "fvg", label: "Fair Value Gaps · FVG", shortLabel: "FVG", description: "3-свечный imbalance, фильтр 0.15 ATR", placement: "overlay", color: "#38bdf8", group: "smart-money" },
  { id: "liquidity", label: "Buy-Side / Sell-Side Liquidity", shortLabel: "BSL / SSL", description: "Неснятые подтверждённые swing-уровни", placement: "overlay", color: "#facc15", group: "smart-money" },
  { id: "equal_high_low", label: "Equal Highs / Equal Lows", shortLabel: "EQH / EQL", description: "Swing-уровни в допуске 0.10 ATR", placement: "overlay", color: "#fb923c", group: "smart-money" },
  { id: "liquidity_sweeps", label: "Liquidity Sweeps", shortLabel: "Sweeps", description: "Прокол уровня тенью с возвратом закрытия", placement: "overlay", color: "#f472b6", group: "smart-money" },
];

export const SMART_MONEY_INDICATOR_IDS = [
  "swing_points",
  "bos",
  "choch",
  "mss",
  "order_blocks",
  "fvg",
  "liquidity",
  "equal_high_low",
  "liquidity_sweeps",
] as const satisfies readonly IndicatorId[];

export type SmartMoneyIndicatorId = (typeof SMART_MONEY_INDICATOR_IDS)[number];
export type ClassicIndicatorId = Exclude<IndicatorId, SmartMoneyIndicatorId>;

const SMART_MONEY_INDICATOR_ID_SET = new Set<IndicatorId>(SMART_MONEY_INDICATOR_IDS);

export function isSmartMoneyIndicatorId(value: IndicatorId): value is SmartMoneyIndicatorId {
  return SMART_MONEY_INDICATOR_ID_SET.has(value);
}

export function isClassicIndicatorId(value: IndicatorId): value is ClassicIndicatorId {
  return !SMART_MONEY_INDICATOR_ID_SET.has(value);
}

const INDICATOR_ID_SET = new Set<string>(INDICATOR_IDS);

export function isIndicatorId(value: string): value is IndicatorId {
  return INDICATOR_ID_SET.has(value);
}
