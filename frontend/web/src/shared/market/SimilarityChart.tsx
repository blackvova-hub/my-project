import { useMemo } from "react";
import type { Time } from "lightweight-charts";
import { ChartSurface } from "../charts/ChartSurface";
import type { SimilarityMatch } from "./SimilarityPanel";
import {
  aggregateSimilarityCandles,
  similarityTimeframes,
  type SimilarityCandle,
  type SimilarityTimeframe,
} from "./similarityCandles";
const EMPTY: SimilarityCandle[] = [];
export function SimilarityChart({
  match,
  candles,
  timeframe,
  loading,
}: {
  match: SimilarityMatch;
  candles?: SimilarityCandle[];
  timeframe: SimilarityTimeframe;
  loading: boolean;
}) {
  const bars = useMemo(
    () =>
      aggregateSimilarityCandles(candles ?? EMPTY, timeframe, match.end).map(
        (c) => ({ ...c, volume: 0 }),
      ),
    [candles, timeframe, match.end],
  );
  const markers = useMemo(() => {
    const future = bars.find((c) => c.time * 1000 >= match.end);
    return future
      ? [
          {
            time: future.time as Time,
            position: "aboveBar" as const,
            color: "#a1a1aa",
            shape: "circle" as const,
            text: "Далее",
          },
        ]
      : [];
  }, [bars, match.end]);
  const label = similarityTimeframes.find(
    (t) => t.seconds === timeframe,
  )?.label;
  return (
    <div
      data-chart-timeframe={timeframe}
      data-chart-bars={bars.length}
      aria-busy={loading}
    >
      <ChartSurface
        candles={bars}
        height={175}
        title={`${match.symbol} · ${label}`}
        interval={timeframe}
        markers={markers}
        loading={loading}
        volume={false}
        drawings={false}
        fit
      />
    </div>
  );
}
