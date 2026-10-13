import { calculateIndicator } from "./indicatorEngine";
import type { PerpCandle } from "./perpMarketData";
import type { WorkerResponse } from "./indicatorBatch";
import type { StudyDefinition } from "./indicatorBatch";
self.addEventListener(
  "message",
  async (
    event: MessageEvent<{
      token: number;
      candles: PerpCandle[];
      barSeconds?: number;
      definitions: StudyDefinition[];
    }>,
  ) => {
    const { token, candles, definitions } = event.data;
    try {
      self.postMessage({
        token,
        output: await Promise.all(
          definitions.map(async (d) => ({
            key: d.key,
            indicator: await calculateIndicator(
              candles,
              d.id,
              d.period,
              event.data.barSeconds,
              d,
            ),
          })),
        ),
      } satisfies WorkerResponse);
    } catch (error) {
      self.postMessage({
        token,
        error: error instanceof Error ? error.message : "Ошибка расчёта",
      } satisfies WorkerResponse);
    }
  },
);
