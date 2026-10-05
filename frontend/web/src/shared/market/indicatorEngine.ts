import type { HistogramData, LineData, UTCTimestamp } from 'lightweight-charts';
import type { PerpCandle } from './perpMarketData';
import type { ClassicIndicatorId, IndicatorId, IndicatorPlacement } from './indicatorTypes';

export type IndicatorLineOutput = {
  key: string;
  kind: 'line';
  title: string;
  color: string;
  lineWidth?: 1 | 2 | 3 | 4;
  lineStyle?: 0 | 1 | 2 | 3 | 4;
  data: LineData<UTCTimestamp>[];
};

export type IndicatorHistogramOutput = {
  key: string;
  kind: 'histogram';
  title: string;
  color: string;
  data: HistogramData<UTCTimestamp>[];
};

export type IndicatorSeriesOutput = IndicatorLineOutput | IndicatorHistogramOutput;
export type IndicatorOutput = {
  id: IndicatorId;
  placement: IndicatorPlacement;
  series: IndicatorSeriesOutput[];
};

let libraryPromise: Promise<typeof import('technicalindicators')> | null = null;

function loadLibrary() {
  libraryPromise ??= import('technicalindicators');
  return libraryPromise;
}

function candleArrays(candles: PerpCandle[]) {
  return {
    open: candles.map((candle) => candle.open),
    high: candles.map((candle) => candle.high),
    low: candles.map((candle) => candle.low),
    close: candles.map((candle) => candle.close),
    volume: candles.map((candle) => candle.volume),
  };
}

function alignNumbers(candles: PerpCandle[], values: Array<number | undefined>) {
  const offset = Math.max(0, candles.length - values.length);
  const result: LineData<UTCTimestamp>[] = [];
  for (let index = 0; index < values.length; index += 1) {
    const value = values[index];
    const candle = candles[offset + index];
    if (candle && typeof value === 'number' && Number.isFinite(value)) {
      result.push({ time: candle.time, value });
    }
  }
  return result;
}

function alignHistogram(
  candles: PerpCandle[],
  values: Array<number | undefined>,
  colorForValue: (value: number) => string,
) {
  const offset = Math.max(0, candles.length - values.length);
  const result: HistogramData<UTCTimestamp>[] = [];
  for (let index = 0; index < values.length; index += 1) {
    const value = values[index];
    const candle = candles[offset + index];
    if (candle && typeof value === 'number' && Number.isFinite(value)) {
      result.push({ time: candle.time, value, color: colorForValue(value) });
    }
  }
  return result;
}

function line(
  key: string,
  title: string,
  color: string,
  data: LineData<UTCTimestamp>[],
  options: Pick<IndicatorLineOutput, 'lineWidth' | 'lineStyle'> = {},
): IndicatorLineOutput {
  return { key, kind: 'line', title, color, data, ...options };
}

const UTC_SESSION_SECONDS = 24 * 60 * 60;

function utcSessionKey(candle: PerpCandle) {
  return Math.floor(Number(candle.time) / UTC_SESSION_SECONDS);
}

/**
 * Crypto VWAP is anchored to the UTC trading session and uses HLC3 as its
 * source. Resetting at each session also keeps historical and live values on
 * exactly the same anchor, regardless of how much older history is loaded.
 */
export function calculateSessionVwap(candles: readonly PerpCandle[]) {
  const data: LineData<UTCTimestamp>[] = [];
  let session = Number.NaN;
  let cumulativeTypicalVolume = 0;
  let cumulativeVolume = 0;

  for (const candle of candles) {
    const candleSession = utcSessionKey(candle);
    if (candleSession !== session) {
      session = candleSession;
      cumulativeTypicalVolume = 0;
      cumulativeVolume = 0;
    }

    if (Number.isFinite(candle.volume) && candle.volume > 0) {
      const typicalPrice = (candle.high + candle.low + candle.close) / 3;
      cumulativeTypicalVolume += typicalPrice * candle.volume;
      cumulativeVolume += candle.volume;
    }

    if (cumulativeVolume > 0) {
      data.push({ time: candle.time, value: cumulativeTypicalVolume / cumulativeVolume });
    }
  }

  return data;
}

function currentUtcSessionCandles(candles: PerpCandle[]) {
  const latest = candles[candles.length - 1];
  if (!latest) return candles;
  const latestSession = utcSessionKey(latest);
  let firstIndex = candles.length - 1;
  while (firstIndex > 0 && utcSessionKey(candles[firstIndex - 1]) === latestSession) {
    firstIndex -= 1;
  }
  return candles.slice(firstIndex);
}

function supertrendData(candles: PerpCandle[], atrValues: number[], multiplier: number) {
  const offset = candles.length - atrValues.length;
  const values: Array<number | undefined> = [];
  let previousUpper = 0;
  let previousLower = 0;
  let previousTrend = 0;
  for (let index = 0; index < atrValues.length; index += 1) {
    const candleIndex = offset + index;
    const candle = candles[candleIndex];
    const previousClose = candles[candleIndex - 1]?.close ?? candle?.close ?? 0;
    if (!candle) {
      values.push(undefined);
      continue;
    }
    const midpoint = (candle.high + candle.low) / 2;
    const basicUpper = midpoint + multiplier * atrValues[index];
    const basicLower = midpoint - multiplier * atrValues[index];
    const upper = index === 0 || basicUpper < previousUpper || previousClose > previousUpper ? basicUpper : previousUpper;
    const lower = index === 0 || basicLower > previousLower || previousClose < previousLower ? basicLower : previousLower;
    let trend = previousTrend;
    if (index === 0) trend = candle.close <= upper ? upper : lower;
    else if (previousTrend === previousUpper) trend = candle.close <= upper ? upper : lower;
    else trend = candle.close >= lower ? lower : upper;
    previousUpper = upper;
    previousLower = lower;
    previousTrend = trend;
    values.push(trend);
  }
  return alignNumbers(candles, values);
}

export async function calculateIndicator(candles: PerpCandle[], id: ClassicIndicatorId): Promise<IndicatorOutput> {
  if (id === 'vwap') {
    return { id, placement: 'overlay', series: [
      line('vwap', 'VWAP', '#fbbf24', calculateSessionVwap(candles)),
    ] };
  }

  const indicators = await loadLibrary();
  const values = candleArrays(candles);
  switch (id) {
    case 'sma':
      return { id, placement: 'overlay', series: [
        line('sma', 'SMA 20', '#4ade80', alignNumbers(candles, indicators.sma({ period: 20, values: values.close }))),
      ] };
    case 'ema':
      return { id, placement: 'overlay', series: [
        line('ema', 'EMA 20', '#38bdf8', alignNumbers(candles, indicators.ema({ period: 20, values: values.close }))),
      ] };
    case 'bollinger': {
      const output = indicators.bollingerbands({ period: 20, stdDev: 2, values: values.close });
      return { id, placement: 'overlay', series: [
        line('upper', 'BB Upper', '#a78bfa', alignNumbers(candles, output.map((item) => item.upper))),
        line('middle', 'BB Basis', 'rgba(196,181,253,.72)', alignNumbers(candles, output.map((item) => item.middle)), { lineStyle: 2 }),
        line('lower', 'BB Lower', '#a78bfa', alignNumbers(candles, output.map((item) => item.lower))),
      ] };
    }
    case 'supertrend': {
      const atrValues = indicators.atr({ high: values.high, low: values.low, close: values.close, period: 10 });
      return { id, placement: 'overlay', series: [
        line('supertrend', 'Supertrend 10x3', '#2dd4bf', supertrendData(candles, atrValues, 3), { lineWidth: 2 }),
      ] };
    }
    case 'ichimoku': {
      const output = indicators.ichimokucloud({
        high: values.high,
        low: values.low,
        conversionPeriod: 9,
        basePeriod: 26,
        spanPeriod: 52,
        displacement: 26,
      });
      return { id, placement: 'overlay', series: [
        line('conversion', 'Tenkan 9', '#fb7185', alignNumbers(candles, output.map((item) => item.conversion))),
        line('base', 'Kijun 26', '#60a5fa', alignNumbers(candles, output.map((item) => item.base))),
        line('spanA', 'Span A', 'rgba(74,222,128,.78)', alignNumbers(candles, output.map((item) => item.spanA)), { lineStyle: 2 }),
        line('spanB', 'Span B', 'rgba(248,113,113,.78)', alignNumbers(candles, output.map((item) => item.spanB)), { lineStyle: 2 }),
      ] };
    }
    case 'rsi':
      return { id, placement: 'pane', series: [
        line('rsi', 'RSI 14', '#c084fc', alignNumbers(candles, indicators.rsi({ period: 14, values: values.close })), { lineWidth: 2 }),
      ] };
    case 'macd': {
      const output = indicators.macd({
        values: values.close,
        fastPeriod: 12,
        slowPeriod: 26,
        signalPeriod: 9,
        SimpleMAOscillator: false,
        SimpleMASignal: false,
      });
      return { id, placement: 'pane', series: [
        {
          key: 'histogram',
          kind: 'histogram',
          title: 'MACD Histogram',
          color: '#34d399',
          data: alignHistogram(
            candles,
            output.map((item) => item.histogram),
            (value) => value >= 0 ? 'rgba(52,211,153,.48)' : 'rgba(251,113,133,.48)',
          ),
        },
        line('macd', 'MACD', '#38bdf8', alignNumbers(candles, output.map((item) => item.MACD))),
        line('signal', 'Signal', '#fb923c', alignNumbers(candles, output.map((item) => item.signal))),
      ] };
    }
    case 'atr':
      return { id, placement: 'pane', series: [
        line('atr', 'ATR 14', '#fb923c', alignNumbers(candles, indicators.atr({
          high: values.high, low: values.low, close: values.close, period: 14,
        })), { lineWidth: 2 }),
      ] };
    case 'stochastic': {
      const output = indicators.stochastic({
        high: values.high, low: values.low, close: values.close, period: 14, signalPeriod: 3,
      });
      return { id, placement: 'pane', series: [
        line('k', 'Percent K', '#22d3ee', alignNumbers(candles, output.map((item) => item.k)), { lineWidth: 2 }),
        line('d', 'Percent D', '#f472b6', alignNumbers(candles, output.map((item) => item.d))),
      ] };
    }
    case 'adx': {
      const output = indicators.adx({
        high: values.high, low: values.low, close: values.close, period: 14,
      });
      return { id, placement: 'pane', series: [
        line('adx', 'ADX 14', '#facc15', alignNumbers(candles, output.map((item) => item.adx)), { lineWidth: 2 }),
        line('pdi', 'Plus DI', '#4ade80', alignNumbers(candles, output.map((item) => item.pdi))),
        line('mdi', 'Minus DI', '#fb7185', alignNumbers(candles, output.map((item) => item.mdi))),
      ] };
    }
    case 'cci':
      return { id, placement: 'pane', series: [
        line('cci', 'CCI 20', '#f472b6', alignNumbers(candles, indicators.cci({
          high: values.high, low: values.low, close: values.close, period: 20,
        })), { lineWidth: 2 }),
      ] };
    case 'roc':
      return { id, placement: 'pane', series: [
        line('roc', 'ROC 12', '#a3e635', alignNumbers(candles, indicators.roc({
          period: 12, values: values.close,
        })), { lineWidth: 2 }),
      ] };
  }
}

const LIVE_LOOKBACK = 600;

export async function calculateIndicatorTail(candles: PerpCandle[], id: ClassicIndicatorId) {
  const sourceCandles = id === 'vwap'
    ? currentUtcSessionCandles(candles)
    : candles.slice(-LIVE_LOOKBACK);
  const output = await calculateIndicator(sourceCandles, id);
  return {
    ...output,
    series: output.series.map((series) => ({ ...series, data: series.data.slice(-1) })),
  };
}
