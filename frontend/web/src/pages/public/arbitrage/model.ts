export const exchanges = [
  "binance",
  "bybit",
  "okx",
  "bitget",
  "gateio",
  "kucoin",
] as const;
export const exchangeNames: Record<string, string> = {
  binance: "Binance",
  bybit: "Bybit",
  okx: "OKX",
  bitget: "Bitget",
  gateio: "Gate.io",
  kucoin: "KuCoin",
  uniswap: "Uniswap v3",
  Circle: "Circle",
  Ethena: "Ethena",
};
export function identityLabel(identity?: string): string | undefined {
  return identity === "UP:SUPERFORM"
    ? "Superform"
    : identity === "UP:UNITAS"
      ? "Unitas"
      : undefined;
}
export function validSearch(search: string): boolean {
  return (
    new TextEncoder().encode(search).length <= 30 &&
    /^[\p{L}\p{N}]*$/u.test(search)
  );
}
export type Settings = {
  minAvailable?: number;
  budget?: boolean;
  network?: string;
  lpRangePct?: number;
  simple?: boolean;
  search?: string;
  strategy: string;
  amount: number;
  minSpread: number;
  maxAgeMs: number;
  horizonHours: number;
  exitSpread: number;
  feeSpot?: number;
  feePerpetual?: number;
  exchanges: string[];
  assets: string[];
  interExchange: boolean;
  sort: string;
  limit: number;
};
export const defaults: Settings = {
  minAvailable: 0,
  budget: true,
  network: "all",
  lpRangePct: 10,
  simple: true,
  search: "",
  strategy: "spot-perpetual",
  amount: 100,
  minSpread: 0,
  maxAgeMs: 10000,
  horizonHours: 24,
  exitSpread: 0,
  exchanges: [...exchanges],
  assets: [],
  interExchange: true,
  sort: "spread",
  limit: 100,
};
// Old saved budgets/fees never silently restrict the simplified scanner.
export function simpleSettings(saved: Settings | null): Settings {
  const selected =
    saved?.exchanges.filter((e) =>
      exchanges.includes(e as (typeof exchanges)[number]),
    ) ?? [];
  return {
    ...defaults,
    minAvailable:
      Number.isFinite(saved?.minAvailable) &&
      saved!.minAvailable! >= 0 &&
      saved!.minAvailable! <= 1000000
        ? saved!.minAvailable
        : 0,
    exchanges:
      selected.length >=
      (["funding", "redemption", "hedged-lp"].includes(saved?.strategy ?? "")
        ? 1
        : 2)
        ? selected
        : [...exchanges],
    amount:
      saved?.budget &&
      Number.isFinite(saved.amount) &&
      saved.amount >= 10 &&
      saved.amount <= 1000000
        ? saved.amount
        : defaults.amount,
    horizonHours:
      saved?.budget && saved.horizonHours >= 1 && saved.horizonHours <= 168
        ? saved.horizonHours
        : 24,
    network: ["all", "Ethereum", "Arbitrum"].includes(saved?.network ?? "")
      ? saved!.network
      : "all",
    lpRangePct:
      saved?.lpRangePct && saved.lpRangePct > 0 && saved.lpRangePct <= 80
        ? saved.lpRangePct
        : 10,
    strategy: [
      "spot-perpetual",
      "perpetual-perpetual",
      "spot-spot",
      "cex-cex",
      "funding",
      "xemm",
      "redemption",
      "hedged-lp",
    ].includes(saved?.strategy ?? "")
      ? saved!.strategy
      : defaults.strategy,
    search: saved?.search ?? "",
  };
}
export function simpleQuery(
  s: Settings,
  offset = 0,
  routes?: string[],
): string {
  const p = new URLSearchParams({
    budget: "true",
    amount: String(s.amount),
    minAvailable: String(s.minAvailable ?? 0),
    horizonHours: String(s.horizonHours),
    network: s.network ?? "all",
    lpRangePct: String(s.lpRangePct ?? 10),
    simple: "true",
    strategy: s.strategy,
    exchanges: s.exchanges.join(","),
    search: s.search ?? "",
    offset: String(offset),
    limit: "100",
  });
  if (routes?.length) p.set("routes", routes.join(","));
  return p.toString();
}
export type Draft = Omit<
  Settings,
  "amount" | "minSpread" | "exitSpread" | "assets" | "feeSpot" | "feePerpetual"
> & {
  amount: string;
  minSpread: string;
  exitSpread: string;
  assets: string;
  feeSpot: string;
  feePerpetual: string;
  manualFees: boolean;
};
export function toDraft(s: Settings): Draft {
  return {
    ...s,
    amount: String(s.amount),
    minSpread: String(s.minSpread),
    exitSpread: String(s.exitSpread),
    assets: s.assets.join(", "),
    feeSpot: String(s.feeSpot ?? 0.1),
    feePerpetual: String(s.feePerpetual ?? 0.06),
    manualFees: s.feeSpot !== undefined || s.feePerpetual !== undefined,
  };
}
export function parseDraft(d: Draft): { settings?: Settings; error?: string } {
  const number = (v: string) =>
    v.trim() === "" ? NaN : Number(v.replace(",", "."));
  const amount = number(d.amount),
    minSpread = number(d.minSpread),
    exitSpread = number(d.exitSpread);
  if (!Number.isFinite(amount) || amount < 10 || amount > 1e6)
    return { error: "Размер покупки: от 10 до 1 000 000 USDT." };
  if (!Number.isFinite(minSpread) || minSpread < -10 || minSpread > 100)
    return { error: "Минимальный спред: от −10 до 100 %." };
  if (!Number.isFinite(exitSpread) || Math.abs(exitSpread) > 50)
    return { error: "Спред выхода: от −50 до 50 %." };
  if (!d.exchanges.length) return { error: "Выберите хотя бы одну биржу." };
  const assets = [
    ...new Set(
      d.assets
        .toUpperCase()
        .split(/[\s,;]+/)
        .filter(Boolean),
    ),
  ];
  if (assets.length > 100 || assets.some((a) => !/^[A-Z0-9]{1,30}$/.test(a)))
    return { error: "Укажите тикеры монет через пробел или запятую." };
  const s: Settings = {
    budget: d.budget,
    minAvailable: d.minAvailable,
    network: d.network,
    lpRangePct: d.lpRangePct,
    simple: d.simple,
    search: d.search,
    strategy: d.strategy,
    amount,
    minSpread,
    maxAgeMs: d.maxAgeMs,
    horizonHours: d.horizonHours,
    exitSpread,
    exchanges: [...d.exchanges],
    assets,
    interExchange: d.interExchange,
    sort: d.sort,
    limit: d.limit,
  };
  if (d.manualFees) {
    s.feeSpot = number(d.feeSpot);
    s.feePerpetual = number(d.feePerpetual);
    if (
      [s.feeSpot, s.feePerpetual].some(
        (v) => !Number.isFinite(v) || v < 0 || v > 2,
      )
    )
      return { error: "Комиссии taker: от 0 до 2 %." };
  }
  return { settings: s };
}
export function queryString(s: Settings): string {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(s)) {
    if (v !== undefined && (!Array.isArray(v) || v.length))
      p.set(k, Array.isArray(v) ? v.join(",") : String(v));
  }
  return p.toString();
}
export type Level = { price: number; quantity: number };
export type Funding = {
  rate: number;
  nextAt: number;
  intervalMs: number;
  observedAt: number;
};
export type Instrument = {
  id: string;
  exchange: string;
  market: string;
  nativeSymbol: string;
  asset: string;
  assetIdentity?: string;
  quantityStep: number;
  minQuantity: number;
  minNotional: number;
  takerFee: number;
  tradeUrl: string;
};
export type Leg = {
  instrument: Instrument;
  price: number;
  notional: number;
  fee: number;
  funding?: Funding;
  bookAt: number;
  bids: Level[];
  asks: Level[];
};
export type Opportunity = {
  execution?: {
    requestedUsdt: number;
    filledUsdt: number;
    capacityUsdt: number;
    fillPct: number;
    full: boolean;
    status: string;
    buyLevels: number;
    sellLevels: number;
    buySlippagePct: number;
    sellSlippagePct: number;
    depthAt: number;
  };
  metrics?: {
    kind: string;
    valuePct: number;
    cashUsdt: number;
    grossUsdt: number;
    costsUsdt: number;
    fundingUsdt: number;
    fundingEvents: number;
    horizonHours: number;
    nextAt?: number;
    annualFundingPct?: number;
    makerSide?: string;
    condition: string;
    url?: string;
    network?: string;
    pool?: string;
    poolAddress?: string;
    tvlUsd?: number;
    historicalApy?: number;
    lpRangePct?: number;
    priceLower?: number;
    priceUpper?: number;
    hedgeQuantity?: number;
    residualQuantity?: number;
    observedAt: number;
    referenceUsd?: number;
    discountPct?: number;
    eligibility?: string;
  };
  horizonHours: number;
  exitSpreadPct: number;
  id: string;
  asset: string;
  strategy: string;
  buy: Leg;
  sell: Leg;
  quantity: number;
  entrySpreadPct: number;
  quoteSpreadPct: number;
  entryFees: number;
  netEntryPct: number;
  exitFeesEstimate: number;
  fundingEstimate: number | null;
  fundingEvents: number;
  scenarioPnl: number | null;
  spotCyclePnl: number | null;
  depthUsdt: number;
  ageMs: number;
  since: number;
  calculatedAt: number;
};
export type Source = {
  fundingBooks?: number;
  connections?: number;
  connected?: number;
  exchange: string;
  market: string;
  state: string;
  message?: string;
  instruments: number;
  books: number;
  updatedAt: number;
  lastBookAt: number;
};
export type Dataset = {
  externalSources?: {
    name: string;
    state: string;
    message?: string;
    observedAt: number;
    count: number;
  }[];
  total: number;
  offset: number;
  universeMode: string;
  books: Record<string, { bids: Level[]; asks: Level[] }>;
  rows: Opportunity[];
  sources: Source[];
  at: number;
  assets: string[];
  configuredAssets: string[];
  transport: string;
};
type WireLeg = Omit<Leg, "bids" | "asks">;
export type WireDataset = Omit<Dataset, "rows"> & {
  rows: (Omit<Opportunity, "buy" | "sell"> & { buy: WireLeg; sell: WireLeg })[];
};
export function hydrateDataset(d: WireDataset): Dataset {
  return {
    ...d,
    rows: d.rows.map((row) => {
      const buy = d.books[row.buy.instrument.id],
        sell = d.books[row.sell.instrument.id];
      if (!buy || !sell)
        throw new Error("Не получены стаканы для расчётного снимка.");
      return {
        ...row,
        buy: { ...row.buy, ...buy },
        sell: { ...row.sell, ...sell },
      };
    }),
  };
}
export type Preferences = { settings: Settings | null; favorites: string[] };
export type History = {
  points: { at: number; spread: number }[];
  intervalMs: number;
  retentionHours: number;
  sampled: boolean;
};
export function isStale(
  row: Opportunity,
  now: number,
  maxAge: number,
): boolean {
  if (row.strategy === "hedged-lp")
    return (
      now - row.sell.bookAt > maxAge ||
      now - row.buy.bookAt > 60000 ||
      now < row.calculatedAt - 2000
    );
  if (row.strategy === "redemption")
    return (
      now - row.buy.bookAt > maxAge ||
      now - row.sell.bookAt > 30000 ||
      now < row.calculatedAt - 2000
    );
  return (
    now - Math.min(row.buy.bookAt, row.sell.bookAt) > maxAge ||
    now < row.calculatedAt - 2000
  );
}
export function fmt(v: number | null | undefined, digits = 2): string {
  return v == null || !Number.isFinite(v)
    ? "—"
    : v.toLocaleString("ru-RU", {
        maximumFractionDigits: digits,
        minimumFractionDigits: digits,
      });
}
export function duration(since: number, now: number): string {
  if (!since) return "—";
  const s = Math.max(0, Math.floor((now - since) / 1000));
  return s < 60
    ? `${s} с`
    : s < 3600
      ? `${Math.floor(s / 60)} мин`
      : `${Math.floor(s / 3600)} ч ${Math.floor((s % 3600) / 60)} мин`;
}
