export type RuleOperator =
  | "gt"
  | "gte"
  | "lt"
  | "lte"
  | "crosses_above"
  | "crosses_below";
export type IndicatorRule = {
  kind: "indicator";
  indicator: string;
  measure: string;
  operator: RuleOperator;
  threshold: number;
  direction?: "up" | "down" | "both";
  windowHours?: number;
  period?: number;
};
type Measure = { value: string; label: string; unit: string; hint: string };
type Definition = {
  name: string;
  measures: Measure[];
  period?: number;
  futures?: boolean;
  trades?: boolean;
};
const change: Measure = {
  value: "change_pct",
  label: "Изменение, %",
  unit: "%",
  hint: "Изменение относительно начала выбранного окна.",
};
const flowSum: Measure = {
  value: "sum",
  label: "Сумма дельты",
  unit: "USDT",
  hint: "Объём рыночных покупок минус объём продаж за выбранное окно. Положительное значение — перевес покупок.",
};
const imbalance: Measure = {
  value: "imbalance_pct",
  label: "Баланс объёма, %",
  unit: "%",
  hint: "(Покупки − продажи) / (покупки + продажи) × 100 за окно. От −100% до +100%.",
};
const oscillator: Measure = {
  value: "value",
  label: "Значение индикатора",
  unit: "пунктов",
  hint: "Значение на закрытии свечи, шкала от 0 до 100.",
};
export const indicators: Record<string, Definition> = {
  price: {
    name: "Цена",
    measures: [
      {
        ...change,
        hint: "Сравниваем цену закрытия сейчас с ценой закрытия в начале окна. Например: рост не менее 5% за 24 часа.",
      },
    ],
  },
  volume: {
    name: "Объём",
    measures: [
      {
        ...change,
        hint: "Сумма объёма за окно относительно суммы за предыдущее окно той же длины. Рост на 100% означает удвоение.",
      },
      {
        value: "relative",
        label: "Относительный объём",
        unit: "×",
        hint: "Объём за окно / объём за предыдущее окно. 2× означает удвоение.",
      },
      {
        value: "sum",
        label: "Суммарный объём",
        unit: "монет",
        hint: "Сумма объёма свечей за окно в базовой монете пары.",
      },
    ],
  },
  openInterest: {
    name: "Открытый интерес (OI)",
    futures: true,
    measures: [
      {
        ...change,
        hint: "Изменение количества открытых контрактов за окно. Источник — история OI Bybit; только Futures.",
      },
    ],
  },
  cvd: {
    name: "Кумулятивная дельта объёма (CVD)",
    trades: true,
    measures: [{ ...flowSum, label: "Изменение CVD за окно" }, imbalance],
  },
  delta: {
    name: "Дельта объёма",
    trades: true,
    measures: [flowSum, imbalance],
  },
  tradeBuyVolume: {
    name: "Объём рыночных покупок",
    trades: true,
    measures: [
      {
        ...change,
        hint: "Сумма покупок за окно относительно суммы за предыдущее окно той же длины.",
      },
      {
        value: "sum",
        label: "Объём за окно",
        unit: "USDT",
        hint: "Сумма price × size сделок с покупателем-инициатором.",
      },
    ],
  },
  tradeSellVolume: {
    name: "Объём рыночных продаж",
    trades: true,
    measures: [
      {
        ...change,
        hint: "Сумма продаж за окно относительно суммы за предыдущее окно той же длины.",
      },
      {
        value: "sum",
        label: "Объём за окно",
        unit: "USDT",
        hint: "Сумма price × size сделок с продавцом-инициатором.",
      },
    ],
  },
  rsi: {
    name: "RSI",
    period: 14,
    measures: [
      oscillator,
      {
        value: "change",
        label: "Изменение RSI",
        unit: "пунктов",
        hint: "RSI сейчас минус RSI в начале окна. Например: +10 означает рост с 30 до 40, а не рост на 10%.",
      },
    ],
  },
  macd: {
    name: "MACD",
    measures: [
      {
        value: "histogram",
        label: "Гистограмма MACD",
        unit: "USDT",
        hint: "MACD (EMA 12 − EMA 26) минус сигнальная EMA 9. Ноль — граница положительной и отрицательной гистограммы.",
      },
      {
        value: "line",
        label: "Линия MACD",
        unit: "USDT",
        hint: "Разница EMA 12 и EMA 26 на закрытии свечи.",
      },
      {
        value: "signal_cross",
        label: "Пересечение сигнальной линии",
        unit: "",
        hint: "Линия MACD пересекает сигнальную EMA 9 между двумя закрытыми свечами. Касание не считается пересечением.",
      },
    ],
  },
  ema: {
    name: "EMA",
    period: 20,
    measures: [
      {
        value: "distance_pct",
        label: "Отклонение цены от EMA",
        unit: "%",
        hint: "(Цена / EMA − 1) × 100. +2% — цена на 2% выше EMA; −2% — ниже. Ноль — сама средняя.",
      },
    ],
  },
  sma: {
    name: "SMA",
    period: 20,
    measures: [
      {
        value: "distance_pct",
        label: "Отклонение цены от SMA",
        unit: "%",
        hint: "(Цена / SMA − 1) × 100. Положительное значение — цена выше средней, отрицательное — ниже.",
      },
    ],
  },
  atr: {
    name: "ATR — диапазон движения",
    period: 14,
    measures: [
      {
        value: "value",
        label: "ATR относительно цены",
        unit: "%",
        hint: "ATR по Уайлдеру / цена закрытия × 100. Период задаётся в свечах выбранного таймфрейма.",
      },
    ],
  },
  stochastic: {
    name: "Стохастик (%K)",
    period: 14,
    measures: [
      {
        ...oscillator,
        hint: "Положение закрытия в диапазоне минимум–максимум за период. Быстрый %K, без дополнительного сглаживания.",
      },
    ],
  },
  mfi: {
    name: "MFI — денежный поток",
    period: 14,
    measures: [
      {
        ...oscillator,
        hint: "Осциллятор типичной цены и объёма, шкала 0–100. Период задаётся в свечах.",
      },
    ],
  },
  obv: {
    name: "OBV — баланс объёма",
    measures: [
      {
        ...imbalance,
        hint: "Изменение OBV / суммарный объём за окно × 100. Объём подписывается по изменению закрытия; это не дельта реальных сделок.",
      },
    ],
  },
  bollinger: {
    name: "Полосы Боллинджера",
    period: 20,
    measures: [
      {
        value: "position",
        label: "Положение цены (%B)",
        unit: "%",
        hint: "SMA ± 2 стандартных отклонения. 0% — нижняя полоса, 100% — верхняя. Выход за полосы допускается.",
      },
      {
        value: "width",
        label: "Ширина полос",
        unit: "%",
        hint: "(Верхняя полоса − нижняя) / SMA × 100. Множитель стандартного отклонения: 2.",
      },
    ],
  },
  volatility: {
    name: "Волатильность",
    measures: [
      {
        value: "value",
        label: "Волатильность доходности",
        unit: "%",
        hint: "Стандартное отклонение логарифмических доходностей свечей за окно, без пересчёта в годовую величину.",
      },
    ],
  },
};
export const hasWindow = (r: IndicatorRule) =>
  [
    "price",
    "volume",
    "openInterest",
    "cvd",
    "delta",
    "tradeBuyVolume",
    "tradeSellVolume",
    "obv",
    "volatility",
  ].includes(r.indicator) ||
  (r.indicator === "rsi" && r.measure === "change");
export function makeRule(indicator = "price", measure?: string): IndicatorRule {
  const definition = indicators[indicator];
  const selected = measure ?? definition.measures[0].value;
  const r: IndicatorRule = {
    kind: "indicator",
    indicator,
    measure: selected,
    operator: "gte",
    threshold: selected === "change_pct" ? 5 : selected === "relative" ? 2 : 0,
  };
  if (definition.period) r.period = definition.period;
  if (selected === "change_pct") r.direction = "up";
  if (hasWindow(r)) r.windowHours = 24;
  if (
    ["rsi", "mfi", "stochastic"].includes(indicator) &&
    selected === "value"
  ) {
    r.operator = "lte";
    r.threshold = 30;
  }
  if (selected === "signal_cross") r.operator = "crosses_above";
  return r;
}
export function thresholdBounds(r: IndicatorRule): [number, number] {
  if (
    ["rsi", "mfi", "stochastic"].includes(r.indicator) &&
    r.measure === "value"
  )
    return [0, 100];
  if (
    r.measure === "imbalance_pct" ||
    (r.indicator === "rsi" && r.measure === "change")
  )
    return [-100, 100];
  if (
    r.measure === "change_pct" ||
    ["relative", "width"].includes(r.measure) ||
    ["atr", "volatility"].includes(r.indicator) ||
    (r.measure === "sum" && !["cvd", "delta"].includes(r.indicator))
  )
    return [0, 1e12];
  return [-1e12, 1e12];
}
export function ruleError(r: IndicatorRule): string | null {
  const def = indicators[r.indicator];
  if (!def || !def.measures.some((m) => m.value === r.measure))
    return "Выберите индикатор и способ расчёта";
  if (
    hasWindow(r)
      ? !Number.isInteger(r.windowHours) ||
        r.windowHours! < 1 ||
        r.windowHours! > 720
      : r.windowHours !== undefined
  )
    return "Окно условия — от 1 до 720 часов";
  if (
    def.period
      ? !Number.isInteger(r.period) || r.period! < 2 || r.period! > 400
      : r.period !== undefined
  )
    return "Период индикатора — от 2 до 400 свечей";
  const [min, max] = thresholdBounds(r);
  if (!Number.isFinite(r.threshold) || r.threshold < min || r.threshold > max)
    return `Порог: от ${min} до ${max}`;
  if (
    r.measure === "change_pct"
      ? !["up", "down", "both"].includes(r.direction ?? "")
      : r.direction !== undefined
  )
    return "Выберите направление изменения";
  if (
    r.measure === "signal_cross" &&
    (r.threshold !== 0 || !r.operator.startsWith("crosses"))
  )
    return "MACD: выберите пересечение сигнальной линии";
  return null;
}
const relations: Record<RuleOperator, string> = {
  gt: "больше",
  gte: "не меньше",
  lt: "меньше",
  lte: "не больше",
  crosses_above: "пересекает вверх",
  crosses_below: "пересекает вниз",
};
export function describeRule(r: IndicatorRule): string {
  const def = indicators[r.indicator];
  const measure = def?.measures.find((m) => m.value === r.measure);
  if (!def || !measure) return "Выберите индикатор";
  const number = Number.isFinite(r.threshold) ? String(r.threshold) : "…";
  const window = hasWindow(r) ? ` за ${r.windowHours ?? "…"} ч` : "";
  const period = r.period ? ` (${r.period})` : "";
  if (r.measure === "signal_cross")
    return `MACD (12, 26, 9) ${relations[r.operator]} сигнальную линию`;
  if (r.measure === "change_pct")
    return `${def.name}: ${r.direction === "down" ? "снижение" : r.direction === "both" ? "изменение в любую сторону" : "рост"} ${relations[r.operator]} ${number}%${window}`;
  return `${def.name}${period}: ${measure.label.toLowerCase()} ${relations[r.operator]} ${number} ${measure.unit}${window}`;
}
