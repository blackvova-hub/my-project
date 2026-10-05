export type TopMoverRow = {
  symbol: string;
  changePct: number;
};

// Fallback demo data. Real movers are rendered in `MarketInsightsWidget` from `/api/market/overview`.
export const GAINERS: TopMoverRow[] = [];
export const LOSERS: TopMoverRow[] = [];

