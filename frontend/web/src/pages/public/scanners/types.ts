// src/pages/public/scanners/types.ts

/**
 * Типы и DTO для интеграции "Сканеры" (Frontend <-> Backend).
 *
 * Основано на текущем backend API (internal/scannerapi):
 *   - GET    /api/scanner/rules         -> { rules: AlertDTO[] }
 *   - POST   /api/scanner/rules         -> { rule: AlertDTO }
 *   - PATCH  /api/scanner/rules/:id     -> { rule: AlertDTO }
 *   - DELETE /api/scanner/rules/:id     -> { ok: true }
 *
 * Signals:
 *   - GET /api/signals?limit=N          -> { signals: row_to_json(signals)[] }
 */

export type LogicalOp = "AND" | "OR";

export type ScannerExchange = "bybit" | "binance";
export type ScannerExchangeSelection = ScannerExchange | "all";

export type Cmp = ">" | "<" | ">=" | "<=" | "==" | "!=";

export type ApiCondition = {
  path: string;
  cmp: Cmp;
  value: number | string;
};

export type ApiExpr = { op?: LogicalOp; conds: ApiCondition[] } | ApiCondition[];

export type ApiScannerRule = {
  id: number;
  exchange: ScannerExchange;
  market_type: "perpetual";
  indicator: string;
  symbol: string;
  window_minutes: number;
  threshold_percent?: number | null;
  threshold_amount?: number | null;
  direction: "up" | "down" | "both";
  cooldown_seconds: number;
  enabled: boolean;
  scanner_slot?: string;
  conditions?: ApiScannerCondition[];
  created_at: string;
  updated_at: string;
};

export type ApiScannerCondition = {
  indicator: string;
  direction: "up" | "down" | "both";
  threshold_percent?: number | null;
  threshold_amount?: number | null;
  negate?: boolean;
};

export type ApiCreateScannerRuleInput = {
  exchange: ScannerExchange;
  market_type: "perpetual";
  indicator: string;
  symbol: string;
  window_minutes: number;
  threshold_percent?: number | null;
  threshold_amount?: number | null;
  direction: "up" | "down" | "both";
  cooldown_seconds: number;
  enabled?: boolean;
  conditions?: ApiScannerCondition[];
  scanner_slot?: string;
};

export type ApiUpdateScannerRuleInput = {
  exchange?: ScannerExchange;
  market_type?: "perpetual";
  indicator?: string;
  symbol?: string;
  window_minutes?: number;
  threshold_percent?: number | null;
  threshold_amount?: number | null;
  direction?: "up" | "down" | "both";
  cooldown_seconds?: number;
  enabled?: boolean;
  conditions?: ApiScannerCondition[];
  scanner_slot?: string;
};

export type ApiSignalRow = {
  id: string;
  rule_id: number;
  user_id: number;
  exchange: ScannerExchange;
  market_type: "perpetual";
  symbol: string;
  tf: string;
  ts: number;
  payload: unknown;
  scanner_slot?: string;
  created_at: string;

  // чтобы не ломаться если добавятся новые поля
  [k: string]: unknown;
};

export type ApiTrade = {
  id: string;
  signal_id: string;
  user_id: number;
  symbol?: string;
  status: "OPEN" | "CLOSED";
  buy_at: string;
  sell_at?: string | null;
  duration_minutes?: number | null;
  profit_percent?: number | null;
  profit_usd?: number | null;
  exchange?: string | null;
  timeframe?: string | null;
  comment?: string | null;
  entry_basis?: string | null;
  entry_photos?: string[] | null;
  strategy_id?: string | null;
  strategy_name?: string | null;
  created_at: string;
  updated_at: string;
};

export type ApiTradeStats = {
  total_trades: number;
  open_trades: number;
  closed_trades: number;
  win_rate?: number | null;
  avg_profit_percent?: number | null;
  sum_profit_percent?: number | null;
  avg_profit_usd?: number | null;
  sum_profit_usd?: number | null;
  best_profit_percent?: number | null;
  worst_profit_percent?: number | null;
  best_profit_usd?: number | null;
  worst_profit_usd?: number | null;
  avg_duration_minutes?: number | null;
  max_duration_minutes?: number | null;
  min_duration_minutes?: number | null;
  first_buy_at?: string | null;
  last_sell_at?: string | null;
};

export type ApiCreateTradeInput = {
  signal_id: string;
  buy_at?: string;
};

export type ApiCloseTradeInput = {
  sell_at?: string;
  duration_minutes?: number;
  profit_percent?: number;
  profit_usd?: number;
  exchange?: string;
  timeframe?: string;
  comment?: string;
  entry_basis?: string;
  entry_photos?: string[];
  strategy?: string;
};

export type ApiTradeStrategyPair = {
  symbol: string;
  total_trades: number;
  closed_trades: number;
  win_rate?: number | null;
  avg_profit_percent?: number | null;
  sum_profit_percent?: number | null;
  avg_profit_usd?: number | null;
  sum_profit_usd?: number | null;
  avg_duration_minutes?: number | null;
};

export type ApiTradeStrategy = {
  id: string;
  name: string;
  total_trades: number;
  closed_trades: number;
  win_rate?: number | null;
  avg_profit_percent?: number | null;
  sum_profit_percent?: number | null;
  avg_profit_usd?: number | null;
  sum_profit_usd?: number | null;
  avg_duration_minutes?: number | null;
  pairs?: ApiTradeStrategyPair[];
};

export type ApiTradeMeta = {
  timeframe?: string | null;
  exchange?: string | null;
  strategy?: string | null;
};

export type SignalRow = {
  id: string;
  exchange?: ScannerExchange;
  combinedExchanges?: boolean;
  symbol: string;
  criteria: string;
  changes: string;
  createdAt: string;
  scannerSlot?: string;
};
