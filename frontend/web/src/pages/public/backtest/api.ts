import { http } from "../../../shared/api/http";
import type { BacktestRequest, Job, Trade } from "./model";

export const backtestApi = {
  symbols: (market: string, signal?: AbortSignal) =>
    http<{ symbols: string[] }>(`/backtests/symbols?market=${market}`, {
      signal,
    }),
  list: (signal?: AbortSignal) =>
    http<{ jobs: Job[] }>("/backtests", { signal }),
  get: (id: string, signal?: AbortSignal) =>
    http<Job>(`/backtests/${id}`, { signal }),
  create: (request: BacktestRequest, requestKey: string) =>
    http<{ jobId: string }>("/backtests", {
      method: "POST",
      body: JSON.stringify({ ...request, requestKey }),
    }),
  cancel: (id: string) =>
    http<{ ok: boolean }>(`/backtests/${id}/cancel`, {
      method: "POST",
      body: "{}",
    }),
  trades: (id: string, page: number, signal?: AbortSignal) =>
    http<{ trades: Trade[]; total: number; page: number }>(
      `/backtests/${id}/trades?page=${page}`,
      { signal },
    ),
};
export function csvURL(id: string) {
  const base = (import.meta.env.VITE_API_BASE ?? "/api").replace(/\/+$/, "");
  return `${base}/backtests/${id}/trades.csv`;
}
