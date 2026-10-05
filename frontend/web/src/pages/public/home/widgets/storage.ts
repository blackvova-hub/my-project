import type { SignalRow } from "../../scanners/types";

const SCANNER_CONFIG_KEY = "scanner_config_v1";      // как в ScannerConfig.tsx
const SCANNER_SIGNALS_KEY = "scanner_signals_v1";    // добавим для сигналов

export type StoredScannerConfig = {
  rules: Array<{
    id: string;
    indicator: string;
    operator: ">" | "<";
    value: number;
    timeframe: string;
  }>;
};

function isStoredRule(value: unknown): value is StoredScannerConfig["rules"][number] {
  if (!value || typeof value !== "object") return false;
  const rule = value as Record<string, unknown>;
  return (
    typeof rule.id === "string" &&
    typeof rule.indicator === "string" &&
    (rule.operator === ">" || rule.operator === "<") &&
    typeof rule.value === "number" &&
    Number.isFinite(rule.value) &&
    typeof rule.timeframe === "string"
  );
}

function isSignalRow(value: unknown): value is SignalRow {
  if (!value || typeof value !== "object") return false;
  const row = value as Record<string, unknown>;
  return (
    typeof row.id === "string" &&
    typeof row.symbol === "string" &&
    typeof row.criteria === "string" &&
    typeof row.changes === "string" &&
    typeof row.createdAt === "string"
  );
}

export function loadScannerConfig(): StoredScannerConfig | null {
  try {
    const raw = localStorage.getItem(SCANNER_CONFIG_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as { rules?: unknown };
    if (!Array.isArray(parsed?.rules)) return null;
    const rules = parsed.rules.filter(isStoredRule);
    return rules.length > 0 ? { rules } : null;
  } catch {
    return null;
  }
}

export function loadScannerSignals(): SignalRow[] {
  try {
    const raw = localStorage.getItem(SCANNER_SIGNALS_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as unknown;
    return Array.isArray(parsed) ? parsed.filter(isSignalRow).slice(0, 100) : [];
  } catch {
    return [];
  }
}

export function saveScannerSignals(rows: SignalRow[]) {
  try {
    localStorage.setItem(SCANNER_SIGNALS_KEY, JSON.stringify(rows.slice(0, 100)));
  } catch {
    // ignore
  }
}

export function prependScannerSignal(row: SignalRow) {
  const prev = loadScannerSignals();
  saveScannerSignals([row, ...prev].slice(0, 100));
}
