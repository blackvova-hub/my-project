// src/pages/public/scanners/queries.ts
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  apiCreateScannerRule,
  apiDeleteScannerRule,
  apiListScannerRules,
  apiListSignals,
  apiUpdateScannerRule,
  apiListTrades,
  apiCreateTrade,
  apiCloseTrade,
  apiTradeStats,
  apiTradeStrategies,
  apiDeleteTradeStrategy,
  apiDeleteTradeStrategyPair,
  apiTradeMeta,
} from "./api";
import type {
  ApiCreateScannerRuleInput,
  ApiUpdateScannerRuleInput,
  ApiSignalRow,
  SignalRow,
  ApiTrade,
  ApiCreateTradeInput,
  ApiCloseTradeInput,
  ApiTradeStats,
  ApiTradeStrategy,
  ApiTradeMeta,
  ScannerExchange,
} from "./types";

const QK_SCANNER_RULES = "scannerRules" as const;
const QK_TRADES = "trades" as const;
const QK_TRADE_STATS = "tradeStats" as const;
const QK_TRADE_STRATEGIES = "tradeStrategies" as const;
const QK_TRADE_META = "tradeMeta" as const;

function qkScannerRules(userKey: string, slot?: string) {
  return slot
    ? ([QK_SCANNER_RULES, userKey, slot] as const)
    : ([QK_SCANNER_RULES, userKey] as const);
}

function qkSignals(userKey: string, limit: number, slot?: string) {
  return ["signals", userKey, limit, slot ?? ""] as const;
}

function qkTrades(userKey: string, status: string, limit?: number) {
  return typeof limit === "number"
    ? ([QK_TRADES, userKey, status, limit] as const)
    : ([QK_TRADES, userKey, status] as const);
}

function qkTradeStats(userKey: string) {
  return [QK_TRADE_STATS, userKey] as const;
}

function qkTradeMeta(userKey: string) {
  return [QK_TRADE_META, userKey] as const;
}

function qkTradeStrategies(userKey: string) {
  return [QK_TRADE_STRATEGIES, userKey] as const;
}

export function useScannerRules(userKey: string, slot?: string) {
  return useQuery({
    queryKey: qkScannerRules(userKey, slot),
    queryFn: () => apiListScannerRules(slot),
    staleTime: 3_000,
    refetchInterval: 10_000,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: true,
    enabled: userKey !== "anon",
  });
}

export function useCreateScannerRule(userKey: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ApiCreateScannerRuleInput) => apiCreateScannerRule(input),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: qkScannerRules(userKey) });
    },
  });
}

export function useUpdateScannerRule(userKey: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (p: { id: string; patch: ApiUpdateScannerRuleInput }) =>
      apiUpdateScannerRule(p.id, p.patch),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: qkScannerRules(userKey) });
    },
  });
}

export function useDeleteScannerRule(userKey: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => apiDeleteScannerRule(id),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: qkScannerRules(userKey) });
    },
  });
}

function labelForPath(path: string): string {
  const p = String(path ?? "");
  if (p.startsWith("price:")) return labelForIndicator(p);
  if (p === "price") return "Цена";
  if (p === "rsi") return "RSI";
  if (p === "oi") return "Открытый интерес";
  if (p === "funding") return "Фандинг";
  if (p === "ad") return "A/D";
  if (p === "ema200") return "EMA 200";
  if (p.startsWith("ema.")) return `EMA ${p.split(".")[1] ?? ""}`.trim();
  if (p.startsWith("sma.")) return `SMA ${p.split(".")[1] ?? ""}`.trim();
  if (p.startsWith("macd.")) return `MACD ${p.split(".")[1] ?? ""}`.trim();
  const indicatorLabel = labelForIndicator(p);
  if (indicatorLabel !== p) return indicatorLabel;
  return p;
}

function labelForIndicator(indicator: string): string {
  const v = String(indicator ?? "");
  if (v === "price") return "Цена";
  if (v.startsWith("price:")) {
    const parts = v.split(":").map((p) => p.trim());
    if (parts.length === 3) {
      const start = parts[1];
      const end = parts[2];
      const map: Record<string, string> = {
        open: "открытие",
        high: "максимум",
        low: "минимум",
        close: "закрытие",
      };
      const startLabel = map[start] ?? start;
      const endLabel = map[end] ?? end;
      return `Цена (${startLabel} → ${endLabel})`;
    }
    return "Цена";
  }
  if (v === "openInterest") return "Открытый интерес";
  if (v === "volume") return "Объём";
  if (v === "delta") return "Дельта";
  if (v === "cvd") return "CVD";
  if (v === "orderbookBid") return "Стакан: биды";
  if (v === "orderbookAsk") return "Стакан: аски";
  if (v === "orderbookSpread") return "Стакан: спред";
  if (v === "orderbookBidDepth") return "Стакан: глубина бидов";
  if (v === "orderbookAskDepth") return "Стакан: глубина асков";
  if (v === "tradeBuyVolume") return "Покупки (объём)";
  if (v === "tradeSellVolume") return "Продажи (объём)";
  if (v === "longRatio") return "Long ratio";
  if (v === "shortRatio") return "Short ratio";
  if (v === "liquidations") return "Ликвидации";
  if (v === "liquidationsCombined") return "Совокупные ликвидации";
  if (v === "liquidationsLong") return "Ликвидации лонгов";
  if (v === "liquidationsShort") return "Ликвидации шортов";
  if (v === "price") return "Цена";
  return v || "Индикатор";
}

function fmtValue(v: unknown): string {
  if (v === null || v === undefined) return "—";
  if (typeof v === "number") {
    if (!Number.isFinite(v)) return "—";
    const abs = Math.abs(v);
    if (abs >= 1000) return v.toFixed(2);
    if (abs >= 1) return Number(v.toFixed(6)).toString();
    return Number(v.toPrecision(6)).toString();
  }
  return String(v);
}

function fmtPercentChange(value: unknown): string {
  if (typeof value !== "number" || !Number.isFinite(value)) return "—";
  const sign = value > 0 ? "+" : "";
  return `${sign}${fmtValue(value)}%`;
}

function fmtDollarChange(value: unknown): string {
  if (typeof value !== "number" || !Number.isFinite(value)) return "—";
  const sign = value < 0 ? "-" : "";
  return `${sign}$${fmtValue(Math.abs(value))}`;
}

function asObject(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" ? (value as Record<string, unknown>) : null;
}

function getString(obj: Record<string, unknown>, key: string): string | undefined {
  const v = obj[key];
  return typeof v === "string" ? v : undefined;
}

function getNumber(obj: Record<string, unknown>, key: string): number | undefined {
  const v = obj[key];
  return typeof v === "number" ? v : undefined;
}

function normalizeSignal(dto: ApiSignalRow): SignalRow {
  const createdAtIso = typeof dto.created_at === "string" ? dto.created_at : "";
  const createdAt = createdAtIso
    ? new Date(createdAtIso).toLocaleString()
    : "—";

  const payload = asObject(dto.payload) ?? {};
  const payloadExchange = getString(payload, "exchange");
  const exchange: ScannerExchange =
    dto.exchange === "binance" || payloadExchange === "binance" ? "binance" : "bybit";
  const evalObj = asObject(payload.eval);
  const details = Array.isArray(evalObj?.details) ? evalObj?.details : [];
  const combinedExchanges =
    payload["combinedExchanges"] === true ||
    getString(payload, "indicator") === "liquidationsCombined" ||
    details.some((raw) => getString(asObject(raw) ?? {}, "path") === "liquidationsCombined");

  let criteria = "—";
  let changes = "—";

  if (details.length > 0) {
    const partsCriteria: string[] = [];
    const partsChanges: string[] = [];

    for (const raw of details) {
      const d = asObject(raw) ?? {};
      const rawPath = getString(d, "path") ?? "";
      const path = labelForPath(rawPath);
      const cmp = String(getString(d, "cmp") ?? "==");
      const want = fmtValue(d["want"]);
      const isAmount = String(rawPath).startsWith("liquidations");
      const wantText = isAmount ? `$${want}` : `${want}%`;
      const gotText = isAmount ? fmtDollarChange(d["got"]) : fmtPercentChange(d["got"]);

      if (rawPath === "liquidationsCombined") {
        partsCriteria.push("Совокупные ликвидации");
        partsChanges.push(gotText);
        continue;
      }
      partsCriteria.push(`${path} ${cmp} ${wantText}`);
      partsChanges.push(gotText);
    }

    const explain = getString(payload, "explain") ?? "";
    criteria = partsCriteria.join(" + ");
    if (explain) criteria = `${criteria} (${explain})`;
    changes = partsChanges.join(" · ");
  }

  if (criteria === "—") {
    const pct = getNumber(payload, "changePercent");
    const windowMinutes = getNumber(payload, "windowMinutes");
    const direction = getString(payload, "direction");
    const indicator = getString(payload, "indicator") ?? "price";
    const liquidationsUsd = getNumber(payload, "liquidationsUsd");
    if (indicator.startsWith("liquidations") && windowMinutes !== undefined && liquidationsUsd !== undefined) {
      if (indicator === "liquidationsCombined") {
        criteria = "Совокупные ликвидации";
      } else {
        const label = labelForIndicator(indicator);
        const liquidationsK = liquidationsUsd / 1000;
        criteria = `${label} ${fmtValue(liquidationsK)}k$ за ${windowMinutes}м`;
      }
      changes = fmtDollarChange(liquidationsUsd);
    }

    if (!indicator.startsWith("liquidations") && pct !== undefined && windowMinutes !== undefined) {
      const dirLabel = direction === "down" ? "упал" : direction === "up" ? "вырос" : "изменился";
      const label = labelForIndicator(indicator);
      criteria = `${label} ${dirLabel} на ${fmtValue(pct)}% за ${windowMinutes}м`;
      changes = fmtPercentChange(pct);
    }
  }

  return {
    id: dto.id,
    exchange,
    combinedExchanges,
    symbol: dto.symbol,
    criteria,
    changes,
    createdAt,
    scannerSlot: typeof dto.scanner_slot === "string" && dto.scanner_slot ? dto.scanner_slot : "SLOT_1",
  };
}

export function useSignals(userKey: string, limit = 50, slot?: string) {
  return useQuery({
    queryKey: qkSignals(userKey, limit, slot),
    queryFn: async () => {
      const dto = await apiListSignals(limit, slot);
      const seenCombined = new Set<string>();
      const rows: SignalRow[] = [];
      for (const signal of dto) {
        const row = normalizeSignal(signal);
        if (row.combinedExchanges) {
          const minute = Math.floor(Number(signal.ts || 0) / 60_000);
          const key = [
            signal.user_id,
            row.symbol,
            row.scannerSlot,
            minute,
            row.criteria,
          ].join("|");
          if (seenCombined.has(key)) continue;
          seenCombined.add(key);
        }
        rows.push(row);
      }
      return rows;
    },
    refetchInterval: 5_000,
    staleTime: 1_000,
    enabled: userKey !== "anon",
    refetchOnWindowFocus: true,
    refetchOnReconnect: true,
    retry: (failureCount, error) => {
      const err = error as { status?: number } | null;
      if (err?.status === 401 || err?.status === 403) return false;
      return failureCount < 5;
    },
    retryDelay: (attempt) => Math.min(1000 * 2 ** (attempt - 1), 10_000),
  });
}

export function useTrades(userKey: string, status = "OPEN", limit = 100) {
  return useQuery<ApiTrade[]>({
    queryKey: qkTrades(userKey, status, limit),
    queryFn: () => apiListTrades(status, limit),
    staleTime: 2_000,
    refetchInterval: 5_000,
    enabled: userKey !== "anon",
  });
}

export function useCreateTrade(userKey: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ApiCreateTradeInput) => apiCreateTrade(input),
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: qkTrades(userKey, "OPEN") }),
        qc.invalidateQueries({ queryKey: qkTrades(userKey, "ALL") }),
      ]);
    },
  });
}

export function useCloseTrade(userKey: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (p: { id: string; input: ApiCloseTradeInput }) => apiCloseTrade(p.id, p.input),
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: qkTrades(userKey, "OPEN") }),
        qc.invalidateQueries({ queryKey: qkTrades(userKey, "CLOSED") }),
        qc.invalidateQueries({ queryKey: qkTrades(userKey, "ALL") }),
        qc.invalidateQueries({ queryKey: qkTradeStats(userKey) }),
        qc.invalidateQueries({ queryKey: qkTradeStrategies(userKey) }),
        qc.invalidateQueries({ queryKey: qkTradeMeta(userKey) }),
      ]);
    },
  });
}

export function useTradeStats(userKey: string) {
  return useQuery<ApiTradeStats>({
    queryKey: qkTradeStats(userKey),
    queryFn: () => apiTradeStats(),
    staleTime: 5_000,
    refetchInterval: 10_000,
    enabled: userKey !== "anon",
  });
}

export function useSymbolTradeStats(symbol: string, enabled: boolean) {
  return useQuery<ApiTradeStats>({
    queryKey: [QK_TRADE_STATS, "symbol", symbol] as const,
    queryFn: () => apiTradeStats({ symbol }),
    staleTime: 30_000,
    enabled: enabled && Boolean(symbol),
    refetchOnWindowFocus: false,
    retry: 1,
  });
}

export function useAllTrades(userKey: string, status = "ALL") {
  return useQuery<ApiTrade[]>({
    queryKey: [...qkTrades(userKey, status), "all"],
    queryFn: async () => {
      const pageSize = 200;
      const trades: ApiTrade[] = [];
      let offset = 0;

      while (true) {
        const page = await apiListTrades(status, pageSize, offset);
        trades.push(...page);
        if (page.length < pageSize) break;
        offset += pageSize;
      }

      return trades;
    },
    staleTime: 5_000,
    refetchInterval: 15_000,
    enabled: userKey !== "anon",
  });
}

export function useTradeMeta(userKey: string) {
  return useQuery<ApiTradeMeta>({
    queryKey: qkTradeMeta(userKey),
    queryFn: () => apiTradeMeta(),
    staleTime: 60_000,
    enabled: userKey !== "anon",
  });
}

export function useTradeStrategies(userKey: string) {
  return useQuery<ApiTradeStrategy[]>({
    queryKey: qkTradeStrategies(userKey),
    queryFn: () => apiTradeStrategies(),
    staleTime: 5_000,
    refetchInterval: 10_000,
    enabled: userKey !== "anon",
  });
}

export function useDeleteTradeStrategy(userKey: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => apiDeleteTradeStrategy(id),
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: qkTradeStrategies(userKey) }),
        qc.invalidateQueries({ queryKey: qkTrades(userKey, "CLOSED") }),
        qc.invalidateQueries({ queryKey: qkTrades(userKey, "ALL") }),
      ]);
    },
  });
}

export function useDeleteTradeStrategyPair(userKey: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (p: { id: string; symbol: string }) =>
      apiDeleteTradeStrategyPair(p.id, p.symbol),
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: qkTradeStrategies(userKey) }),
        qc.invalidateQueries({ queryKey: qkTrades(userKey, "CLOSED") }),
        qc.invalidateQueries({ queryKey: qkTrades(userKey, "ALL") }),
      ]);
    },
  });
}
