// src/pages/public/scanners/api.ts
import { http } from "../../../shared/api/http";
import type {
  ApiCreateScannerRuleInput,
  ApiScannerRule,
  ApiSignalRow,
  ApiUpdateScannerRuleInput,
  ApiTrade,
  ApiCreateTradeInput,
  ApiCloseTradeInput,
  ApiTradeStats,
  ApiTradeStrategy,
  ApiTradeMeta,
} from "./types";

function normalizeRulesResponse(res: unknown): ApiScannerRule[] {
  if (Array.isArray(res)) return res as ApiScannerRule[];
  if (res && typeof res === "object" && Array.isArray((res as { rules?: unknown }).rules)) {
    return (res as { rules: ApiScannerRule[] }).rules;
  }
  return [];
}

function normalizeSignalsResponse(res: unknown): ApiSignalRow[] {
  if (Array.isArray(res)) return res as ApiSignalRow[];
  if (res && typeof res === "object" && Array.isArray((res as { signals?: unknown }).signals)) {
    return (res as { signals: ApiSignalRow[] }).signals;
  }
  return [];
}

function normalizeTradesResponse(res: unknown): ApiTrade[] {
  if (Array.isArray(res)) return res as ApiTrade[];
  if (res && typeof res === "object" && Array.isArray((res as { trades?: unknown }).trades)) {
    return (res as { trades: ApiTrade[] }).trades;
  }
  return [];
}

export async function apiListScannerRules(slot?: string): Promise<ApiScannerRule[]> {
  const qs = new URLSearchParams();
  if (slot) {
    qs.set("slot", slot);
  }
  const res = await http<unknown>(`/api/scanner/rules${qs.toString() ? `?${qs}` : ""}`);
  return normalizeRulesResponse(res);
}

export async function apiCreateScannerRule(
  input: ApiCreateScannerRuleInput
): Promise<ApiScannerRule> {
  const res = await http<unknown>("/api/scanner/rules", {
    method: "POST",
    body: JSON.stringify(input),
  });

  // backend -> { rule: {...} }
  if (res && typeof res === "object" && (res as { rule?: unknown }).rule) {
    return (res as { rule: ApiScannerRule }).rule;
  }
  return res as ApiScannerRule;
}

export async function apiUpdateScannerRule(
  id: string,
  patch: ApiUpdateScannerRuleInput
): Promise<ApiScannerRule> {
  const res = await http<unknown>(`/api/scanner/rules/${id}`, {
    method: "PATCH",
    body: JSON.stringify(patch),
  });

  if (res && typeof res === "object" && (res as { rule?: unknown }).rule) {
    return (res as { rule: ApiScannerRule }).rule;
  }
  return res as ApiScannerRule;
}

export async function apiDeleteScannerRule(id: string): Promise<void> {
  await http<{ ok: true }>(`/api/scanner/rules/${id}`, {
    method: "DELETE",
  });
}

export async function apiListSignals(limit = 50, slot?: string): Promise<ApiSignalRow[]> {
  const qs = new URLSearchParams();
  qs.set("limit", String(limit));
  if (slot) {
    qs.set("slot", slot);
  }

  const res = await http<unknown>(`/api/signals?${qs.toString()}`);
  return normalizeSignalsResponse(res);
}

export async function apiListTrades(status = "OPEN", limit = 100, offset = 0): Promise<ApiTrade[]> {
  const qs = new URLSearchParams();
  if (status) qs.set("status", status);
  qs.set("limit", String(limit));
  if (offset > 0) qs.set("offset", String(offset));
  const res = await http<unknown>(`/api/trades?${qs.toString()}`);
  return normalizeTradesResponse(res);
}

export async function apiCreateTrade(input: ApiCreateTradeInput): Promise<ApiTrade> {
  const res = await http<unknown>("/api/trades", {
    method: "POST",
    body: JSON.stringify(input),
  });
  if (res && typeof res === "object" && (res as { trade?: unknown }).trade) {
    return (res as { trade: ApiTrade }).trade;
  }
  return res as ApiTrade;
}

export async function apiCloseTrade(id: string, input: ApiCloseTradeInput): Promise<ApiTrade> {
  const res = await http<unknown>(`/api/trades/${id}`, {
    method: "PATCH",
    body: JSON.stringify(input),
  });
  if (res && typeof res === "object" && (res as { trade?: unknown }).trade) {
    return (res as { trade: ApiTrade }).trade;
  }
  return res as ApiTrade;
}

export async function apiTradeStats(params?: { symbol?: string }): Promise<ApiTradeStats> {
  const qs = new URLSearchParams();
  if (params?.symbol) qs.set("symbol", params.symbol);
  const url = qs.toString() ? `/api/trades/stats?${qs.toString()}` : "/api/trades/stats";
  const res = await http<unknown>(url);
  if (res && typeof res === "object" && (res as { stats?: unknown }).stats) {
    return (res as { stats: ApiTradeStats }).stats;
  }
  return res as ApiTradeStats;
}

export async function apiTradeMeta(): Promise<ApiTradeMeta> {
  const res = await http<unknown>("/api/trades/meta");
  if (res && typeof res === "object" && (res as { meta?: unknown }).meta) {
    return (res as { meta: ApiTradeMeta }).meta;
  }
  return res as ApiTradeMeta;
}

export async function apiTradeStrategies(): Promise<ApiTradeStrategy[]> {
  const res = await http<unknown>("/api/trades/strategies");
  if (res && typeof res === "object" && Array.isArray((res as { strategies?: unknown }).strategies)) {
    return (res as { strategies: ApiTradeStrategy[] }).strategies;
  }
  if (Array.isArray(res)) return res as ApiTradeStrategy[];
  return [];
}

export async function apiDeleteTradeStrategy(id: string): Promise<void> {
  await http<{ ok: true }>(`/api/trades/strategies/${id}`,
    {
      method: "DELETE",
    }
  );
}

export async function apiDeleteTradeStrategyPair(id: string, symbol: string): Promise<void> {
  await http<{ ok: true }>(`/api/trades/strategies/${id}/pairs/${encodeURIComponent(symbol)}`,
    {
      method: "DELETE",
    }
  );
}
