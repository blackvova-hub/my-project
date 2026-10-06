export const strategyOptions = [
  { value: "classic", label: "Обычный арбитраж" },
  { value: "cex-cex", label: "Мгновенный CEX↔CEX" },
  { value: "funding", label: "Funding Arbitrage" },
  { value: "xemm", label: "XEMM" },
  { value: "redemption", label: "Redemption / Depeg" },
  { value: "hedged-lp", label: "Hedged LP" },
];
export function classic(strategy: string) {
  return ["spot-perpetual", "perpetual-perpetual", "spot-spot"].includes(
    strategy,
  );
}
export function validAmount(text: string) {
  const value = Number(text.replace(",", "."));
  return (
    text.trim() !== "" &&
    Number.isFinite(value) &&
    value >= 10 &&
    value <= 1000000
  );
}
export function validAvailability(text: string) {
  const value = Number(text.replace(",", "."));
  return (
    text.trim() !== "" &&
    Number.isFinite(value) &&
    value >= 0 &&
    value <= 1000000
  );
}
export const strategyDescriptions: Record<string, string> = {
  "cex-cex":
    "Одновременная покупка и продажа спота. Средства нужны на обеих биржах; учтены глубина и оценка двух комиссий.",
  funding:
    "Long спот или фьючерс, short фьючерс. Считаем funding по расписанию каждой биржи за выбранный срок.",
  xemm: "Лимитная покупка по bid или продажа своего спота по ask, затем рыночный хедж на другой бирже. Исполнение лимитной заявки не гарантировано.",
  redemption:
    "Ищем отклонение USDC / USDe от номинала $1. USD-якорь отдельно; доступ к погашению нужно подтвердить.",
  "hedged-lp":
    "Позиция Uniswap v3 и short на CEX по текущей delta. APY пула исторический; нужны маржа и перебалансировка.",
};
export const executionLabels: Record<string, string> = {
  ready: "Сумма покрыта",
  "depth-insufficient": "Недостаточно известной глубины",
  "below-minimum": "Ниже минимума ордера",
  "spread-lost": "Разница исчезает на эту сумму",
  "costs-exceed-edge": "Расходы выше преимущества",
  "maker-conditional": "Ожидание maker-исполнения",
  "redemption-conditional": "Доступ к погашению не подтверждён",
  "lp-conditional": "Хедж требует перебалансировки",
  "pool-size-limit": "Сумма превышает 1% TVL пула",
};
export function marketLabel(market: string) {
  return (
    (
      {
        spot: "Спот",
        perpetual: "Фьючерс",
        lp: "Пул",
        redemption: "Номинал",
      } as Record<string, string>
    )[market] ?? market
  );
}
