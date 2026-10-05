export type Connection = {
  id: string;
  exchange: string;
  accountType: string;
  name: string;
  status: "queued" | "syncing" | "ready" | "error";
  error: string;
  warnings: string[];
  coverageFrom: string;
  syncedThrough: string | null;
  lastSync: string | null;
};
export type Fill = {
  id: string;
  connectionId: string;
  exchange: string;
  symbol: string;
  market: string;
  side: string;
  positionSide: string;
  at: number;
  price: number;
  quantity: number;
  fee: number;
  feeCurrency: string;
  feeKnown: boolean;
  maker: boolean;
  action?: string;
};
export type Ledger = {
  id: string;
  connectionId: string;
  exchange: string;
  at: number;
  category: string;
  symbol: string;
  currency: string;
  amount: number;
  amountExact: string;
  tradeId?: string;
};
export type Asset = { symbol: string; value: number };
export type Position = {
  symbol: string;
  side: string;
  quantity: number;
  entry: number;
  mark: number;
  notional: number;
  unrealized: number;
  leverage: number | null;
  stopLoss: number | null;
  takeProfit: number | null;
  liquidation: number | null;
};
export type Snapshot = {
  connectionId: string;
  at: number;
  equity: number;
  wallet: number;
  unrealized: number;
  margin: number;
  assets: Asset[];
  positions: Position[];
  btcPrice: number | null;
};
export type Trade = {
  id: string;
  connectionId: string;
  exchange: string;
  symbol: string;
  market: string;
  side: string;
  openedAt: number;
  closedAt: number | null;
  entry: number;
  exit: number;
  quantity: number;
  remaining: number;
  size: number;
  gross: number;
  fees: number;
  funding: number;
  net: number;
  duration: number;
  leverage: number | null;
  mfe: number | null;
  mae: number | null;
  captured: number | null;
  stopLoss: number | null;
  takeProfit: number | null;
  liquidation: number | null;
  complete: boolean;
  tag: string;
  strategy: string;
  fills: Fill[];
};
export type Candle = {
  time: number;
  open: number;
  high: number;
  low: number;
  close: number;
};
export type Dataset = {
  connections: Connection[];
  trades: Trade[];
  ledger: Ledger[];
  snapshots: Snapshot[];
  warnings: string[];
  serverTime: number;
};
export type Range = "7D" | "30D" | "90D" | "YTD" | "1Y" | "ALL";
export type Dimension =
  | "symbol"
  | "side"
  | "exchange"
  | "weekday"
  | "hour"
  | "leverage"
  | "duration"
  | "size";
export type TradeFilter = {
  exchange?: string;
  symbol?: string;
  side?: string;
  result?: string;
  leverage?: string;
  duration?: string;
  date?: string;
  tag?: string;
  strategy?: string;
  weekday?: string;
  hour?: string;
  size?: string;
  ids?: string[];
  category?: string;
};
export type Insight = {
  id: string;
  type: string;
  category: "Торговля" | "Расходы" | "Риски";
  title: string;
  text: string;
  evidence: string;
  confidence: string;
  sample: number;
  trades: string[];
  positive: boolean;
};
