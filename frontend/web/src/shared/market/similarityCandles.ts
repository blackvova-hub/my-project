export type SimilarityCandle = {
  time: number;
  open: number;
  high: number;
  low: number;
  close: number;
};

export const similarityTimeframes = [
  { seconds: 300, label: "5м" },
  { seconds: 900, label: "15м" },
  { seconds: 3600, label: "1ч" },
  { seconds: 14400, label: "4ч" },
] as const;
export type SimilarityTimeframe = (typeof similarityTimeframes)[number]["seconds"];

// UTC candle buckets; incomplete candles at the edges retain their actual start.
// Never merge across a gap or mix the matched window with its subsequent move.
export function aggregateSimilarityCandles(
  candles: readonly SimilarityCandle[],
  seconds: SimilarityTimeframe,
  boundaryMs: number,
): SimilarityCandle[] {
  const result: SimilarityCandle[] = [];
  const boundary = boundaryMs / 1000;
  let previous = -Infinity;
  let bucket = -1;
  for (const candle of candles) {
    const nextBucket = Math.floor(candle.time / seconds);
    const last = result[result.length - 1];
    if (
      !last || nextBucket !== bucket || candle.time !== previous + 300 ||
      (previous < boundary && candle.time >= boundary)
    ) {
      result.push({ ...candle });
    } else {
      last.high = Math.max(last.high, candle.high);
      last.low = Math.min(last.low, candle.low);
      last.close = candle.close;
    }
    previous = candle.time;
    bucket = nextBucket;
  }
  return result;
}
