export type VerificationCheck = { key: string; label: string; state: "pass" | "fail" | "unknown" | "na"; detail: string; at?: number };
export type VerificationPath = {
  id?: string; mode: string; network?: string; networkLabel?: string; state: string; checks: VerificationCheck[];
  netProfit: number | null;
  calculation: {
    buyVwap: number | null; sellVwap: number | null;
    buyQuantity: number | null; sellQuantity: number | null;
    grossUsdt: number | null; tradingFeesEstimate: number | null; withdrawalFeeEstimate: number | null;
    execution?: { requestedUsdt: number; filledUsdt: number; full: boolean; buyLevels: number; sellLevels: number };
  };
};
export type VerificationReport = { at: number; amount: number; paths: VerificationPath[] };
export const verificationLabels: Record<VerificationCheck["state"], string> = {
  pass: "Подтверждено", fail: "Не пройдено", unknown: "Неизвестно", na: "Не требуется",
};
export function verificationSnapshotFresh(at: number, path: VerificationPath | undefined, now: number) {
  const quote = path?.checks.find((c) => c.key === "freshness");
  return Number.isFinite(at) && at > 0 && now - at <= 5000 && at <= now + 2000 && quote?.state === "pass" && !!quote.at && now - quote.at <= 5000 && quote.at <= now + 2000;
}
// A fail-closed display also protects against incomplete responses or old servers.
export function verifiedNetProfit(path: VerificationPath | undefined): number | null {
  const required = ["identity", "freshness", "trading", "wallet", "network", "delisting", "vwap", "fees", "account"];
  if (!path || path.state !== "verified" || path.netProfit === null || !Number.isFinite(path.netProfit)) return null;
  if (!["prefunded", "transfer"].includes(path.mode) || path.checks.some((c) => c.state === "fail" || c.state === "unknown")) return null;
  if (required.some((key) => !path.checks.some((c) => c.key === key && (c.state === "pass" || (path.mode === "prefunded" && ["wallet", "network"].includes(key) && c.state === "na"))))) return null;
  return path.netProfit;
}
