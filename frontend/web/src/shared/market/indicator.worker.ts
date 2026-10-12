import { calculateExtended } from "./indicatorExtended";
import type { ExtraIndicatorId } from "./indicatorCatalogExtra";
import type { PerpCandle } from "./perpMarketData";
import type { WorkerResponse } from "./indicatorBatch";
self.addEventListener(
  "message",
  (
    event: MessageEvent<{
      token: number;
      candles: PerpCandle[];
      barSeconds?: number;
      definitions: { key: string; id: ExtraIndicatorId; period?: number }[];
    }>,
  ) => {
    const { token, candles, definitions } = event.data;
    try {
      self.postMessage({
        token,
        output: definitions.map((d) => ({
          key: d.key,
          indicator: calculateExtended(
            candles,
            d.id,
            d.period,
            event.data.barSeconds,
          ),
        })),
      } satisfies WorkerResponse);
    } catch (error) {
      self.postMessage({
        token,
        error: error instanceof Error ? error.message : "Ошибка расчёта",
      } satisfies WorkerResponse);
    }
  },
);
