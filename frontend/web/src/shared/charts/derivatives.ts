import { useQuery } from "@tanstack/react-query";
import { fetchMarketStudy, type ExternalPane } from "./marketStudyData";
import type { ChartInterval, Instrument } from "./marketData";
import { DERIVATIVE_PANELS, type DerivativeId } from "./derivativeCatalog";
export { DERIVATIVE_PANELS, derivativeAvailability } from "./derivativeCatalog";
export type { DerivativeId } from "./derivativeCatalog";
export type { ExternalPane } from "./marketStudyData";
async function get(url: string, signal: AbortSignal): Promise<unknown> {
  const response = await fetch(url, {
    signal: AbortSignal.any([signal, AbortSignal.timeout(15000)]),
  });
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  const data = await response.json();
  if (data.retCode !== undefined && data.retCode !== 0)
    throw new Error(data.retMsg ?? "Биржа не вернула данные");
  return data;
}
export function fetchDerivative(
  i: Instrument,
  interval: ChartInterval,
  id: DerivativeId,
  signal: AbortSignal,
) {
  return fetchMarketStudy(i, interval, id, (url) => get(url, signal));
}
export function useDerivativePanels(
  i: Instrument,
  interval: ChartInterval,
  ids: DerivativeId[],
  enabled = true,
) {
  return useQuery({
    queryKey: [
      "terminal-derivatives",
      i.exchange,
      i.market,
      i.symbol,
      interval,
      ids.join(","),
    ],
    enabled: enabled && !!ids.length,
    queryFn: async ({ signal }) => {
      // Families sharing a source reuse one request within this refresh.
      const requests = new Map<string, Promise<unknown>>();
      const request = (url: string) => {
        let pending = requests.get(url);
        if (!pending) {
          pending = get(url, signal);
          requests.set(url, pending);
        }
        return pending;
      };
      return Promise.all(
        ids.map(async (id) => {
          try {
            return await fetchMarketStudy(i, interval, id, request);
          } catch (error) {
            const info = DERIVATIVE_PANELS.find((p) => p.id === id)!;
            return {
              id,
              title: info.label,
              color: info.color,
              data: [],
              error:
                error instanceof Error ? error.message : "Ошибка источника",
            } satisfies ExternalPane;
          }
        }),
      );
    },
    staleTime: 60000,
    refetchInterval: 60000,
    refetchIntervalInBackground: false,
    retry: false,
  });
}
