import type { UTCTimestamp } from "lightweight-charts";

import type { PerpCandle } from "./perpMarketData";
import type { SmartMoneyIndicatorId } from "./indicatorTypes";

export type SmartMoneyDirection = "bullish" | "bearish";
export type SmartMoneyZoneStatus = "active" | "mitigated" | "invalidated";

export type SmartMoneyMarker = {
  id: string;
  indicator: SmartMoneyIndicatorId;
  kind: "swing" | "sweep";
  direction: SmartMoneyDirection;
  time: UTCTimestamp;
  price: number;
  label: string;
};

export type SmartMoneyLevel = {
  id: string;
  indicator: SmartMoneyIndicatorId;
  direction: SmartMoneyDirection;
  startTime: UTCTimestamp;
  endTime?: UTCTimestamp;
  price: number;
  label: string;
  active: boolean;
};

export type SmartMoneyZone = {
  id: string;
  indicator: "order_blocks" | "fvg";
  direction: SmartMoneyDirection;
  startTime: UTCTimestamp;
  endTime?: UTCTimestamp;
  top: number;
  bottom: number;
  label: string;
  status: SmartMoneyZoneStatus;
};

export type SmartMoneyOutput = {
  markers: SmartMoneyMarker[];
  levels: SmartMoneyLevel[];
  zones: SmartMoneyZone[];
};

type PivotKind = "high" | "low";
type Pivot = {
  id: number;
  kind: PivotKind;
  index: number;
  confirmIndex: number;
  time: UTCTimestamp;
  price: number;
  atr: number;
  broken: boolean;
  swept: boolean;
  endTime?: UTCTimestamp;
};

type MutableLiquidityLevel = SmartMoneyLevel & {
  sourcePivotId?: number;
  swept: boolean;
  broken: boolean;
};

type StructureEvent = {
  index: number;
  direction: SmartMoneyDirection;
  source: Pivot;
  isChoch: boolean;
  isMss: boolean;
};

export const SMART_MONEY_RULES = Object.freeze({
  swingBars: 5,
  atrPeriod: 14,
  breakBufferAtr: 0.02,
  equalToleranceAtr: 0.1,
  fvgMinimumAtr: 0.15,
  fvgDisplacementAtr: 0.5,
  mssDisplacementAtr: 1,
  displacementBodyRatio: 0.6,
  maxVisiblePerKind: 24,
});

function directionForPivot(kind: PivotKind): SmartMoneyDirection {
  return kind === "high" ? "bearish" : "bullish";
}

function calculateAtr(candles: readonly PerpCandle[], period: number) {
  const result = new Array<number>(candles.length).fill(Number.NaN);
  if (candles.length === 0) return result;
  const trueRanges = candles.map((candle, index) => {
    const previousClose = candles[index - 1]?.close ?? candle.close;
    return Math.max(
      candle.high - candle.low,
      Math.abs(candle.high - previousClose),
      Math.abs(candle.low - previousClose),
    );
  });
  let running = 0;
  for (let index = 0; index < trueRanges.length; index += 1) {
    const trueRange = trueRanges[index];
    if (index < period) {
      running += trueRange;
      if (index === period - 1) result[index] = running / period;
      continue;
    }
    const previous = result[index - 1];
    result[index] = ((previous * (period - 1)) + trueRange) / period;
  }
  return result;
}

function isPivot(candles: readonly PerpCandle[], index: number, kind: PivotKind, bars: number) {
  const candidate = kind === "high" ? candles[index]?.high : candles[index]?.low;
  if (candidate === undefined) return false;
  // Left side is strict and right side inclusive. A flat plateau therefore
  // produces one stable pivot instead of repainting between equal candles.
  for (let offset = 1; offset <= bars; offset += 1) {
    const left = kind === "high" ? candles[index - offset]?.high : candles[index - offset]?.low;
    const right = kind === "high" ? candles[index + offset]?.high : candles[index + offset]?.low;
    if (left === undefined || right === undefined) return false;
    if (kind === "high") {
      if (candidate <= left || candidate < right) return false;
    } else if (candidate >= left || candidate > right) return false;
  }
  return true;
}

function candleBodyRatio(candle: PerpCandle) {
  const range = candle.high - candle.low;
  return range <= 0 ? 0 : Math.abs(candle.close - candle.open) / range;
}

function isDisplacement(candle: PerpCandle, atr: number, direction: SmartMoneyDirection, atrMultiple: number) {
  if (!Number.isFinite(atr) || atr <= 0) return false;
  const body = Math.abs(candle.close - candle.open);
  const range = candle.high - candle.low;
  if (range <= 0 || body < atr * atrMultiple || body / range < SMART_MONEY_RULES.displacementBodyRatio) return false;
  if (direction === "bullish") {
    return candle.close > candle.open && (candle.high - candle.close) / range <= 0.25;
  }
  return candle.close < candle.open && (candle.close - candle.low) / range <= 0.25;
}

function closeBreaks(candle: PerpCandle, previous: PerpCandle | undefined, pivot: Pivot, atr: number) {
  const buffer = Number.isFinite(atr) ? atr * SMART_MONEY_RULES.breakBufferAtr : 0;
  if (pivot.kind === "high") {
    return candle.close > pivot.price + buffer && (previous?.close ?? pivot.price) <= pivot.price + buffer;
  }
  return candle.close < pivot.price - buffer && (previous?.close ?? pivot.price) >= pivot.price - buffer;
}

function findOrderBlockCandle(
  candles: readonly PerpCandle[],
  atr: readonly number[],
  event: StructureEvent,
) {
  const from = Math.max(event.source.index, event.index - 24);
  for (let index = event.index - 1; index >= from; index -= 1) {
    const candle = candles[index];
    const candleAtr = atr[index];
    if (!candle || !Number.isFinite(candleAtr) || candleAtr <= 0) continue;
    const isOpposing = event.direction === "bullish" ? candle.close < candle.open : candle.close > candle.open;
    if (!isOpposing) continue;
    const body = Math.abs(candle.close - candle.open);
    if (body >= candleAtr * 0.1 && candleBodyRatio(candle) >= 0.2) return { candle, index };
  }
  return null;
}

function lastUnbrokenPivot(pivots: readonly Pivot[], kind: PivotKind) {
  for (let index = pivots.length - 1; index >= 0; index -= 1) {
    const pivot = pivots[index];
    if (pivot?.kind === kind && !pivot.broken) return pivot;
  }
  return undefined;
}

function latestEligibleSweepLevel(levels: readonly MutableLiquidityLevel[], direction: SmartMoneyDirection) {
  for (let index = levels.length - 1; index >= 0; index -= 1) {
    const level = levels[index];
    if (level?.direction === direction && !level.swept && !level.broken && level.active) return level;
  }
  return undefined;
}

function appendMostRecent<T>(values: readonly T[], limit: number) {
  return values.length <= limit ? [...values] : values.slice(values.length - limit);
}

/**
 * Deterministic, close-confirmed SMC model.
 *
 * There is no single exchange-defined SMC standard. These rules deliberately
 * avoid repainting: pivots need five bars on both sides; structure requires a
 * close beyond the confirmed pivot; wick-only raids are sweeps, not BOS; MSS is
 * the stricter displacement-qualified subset of CHOCH; and zones are built and
 * mitigated only from already closed candles.
 */
export function calculateSmartMoney(candlesInput: readonly PerpCandle[]): SmartMoneyOutput {
  const candles = candlesInput.filter((candle) => (
    Number.isFinite(candle.open)
    && Number.isFinite(candle.high)
    && Number.isFinite(candle.low)
    && Number.isFinite(candle.close)
    && candle.high >= candle.low
  ));
  const { swingBars, atrPeriod } = SMART_MONEY_RULES;
  if (candles.length < swingBars * 2 + atrPeriod) return { markers: [], levels: [], zones: [] };

  const atr = calculateAtr(candles, atrPeriod);
  const pivotCandidates = new Map<number, Array<Omit<Pivot, "broken" | "swept">>>();
  let pivotId = 0;
  for (let index = swingBars; index < candles.length - swingBars; index += 1) {
    for (const kind of ["high", "low"] as const) {
      if (!isPivot(candles, index, kind, swingBars)) continue;
      const candle = candles[index];
      const pivotAtr = atr[index];
      if (!candle || !Number.isFinite(pivotAtr)) continue;
      const confirmIndex = index + swingBars;
      const bucket = pivotCandidates.get(confirmIndex) ?? [];
      bucket.push({
        id: pivotId += 1,
        kind,
        index,
        confirmIndex,
        time: candle.time,
        price: kind === "high" ? candle.high : candle.low,
        atr: pivotAtr,
      });
      pivotCandidates.set(confirmIndex, bucket);
    }
  }

  const pivots: Pivot[] = [];
  const liquidityLevels: MutableLiquidityLevel[] = [];
  const equalLevels: MutableLiquidityLevel[] = [];
  const markers: SmartMoneyMarker[] = [];
  const structureLevels: SmartMoneyLevel[] = [];
  const zones: SmartMoneyZone[] = [];
  let bias = 0;

  const finishZones = (index: number) => {
    const candle = candles[index];
    if (!candle) return;
    for (const zone of zones) {
      if (zone.status !== "active" || Number(zone.startTime) >= Number(candle.time)) continue;
      if (zone.indicator === "fvg") {
        const filled = zone.direction === "bullish" ? candle.low <= zone.bottom : candle.high >= zone.top;
        if (filled) {
          zone.status = "mitigated";
          zone.endTime = candle.time;
        }
        continue;
      }
      const invalidated = zone.direction === "bullish" ? candle.close < zone.bottom : candle.close > zone.top;
      const touched = zone.direction === "bullish" ? candle.low <= zone.top : candle.high >= zone.bottom;
      if (invalidated) zone.status = "invalidated";
      else if (touched) zone.status = "mitigated";
      if (invalidated || touched) zone.endTime = candle.time;
    }
  };

  for (let index = 0; index < candles.length; index += 1) {
    const candle = candles[index];
    if (!candle) continue;
    finishZones(index);

    const newlyConfirmed = pivotCandidates.get(index) ?? [];
    for (const candidate of newlyConfirmed) {
      const pivot: Pivot = { ...candidate, broken: false, swept: false };
      const previousSameKind = [...pivots].reverse().find((item) => item.kind === pivot.kind);
      pivots.push(pivot);
      const direction = directionForPivot(pivot.kind);
      markers.push({
        id: `swing-${pivot.id}`,
        indicator: "swing_points",
        kind: "swing",
        direction,
        time: pivot.time,
        price: pivot.price,
        label: pivot.kind === "high" ? "SH" : "SL",
      });
      liquidityLevels.push({
        id: `liquidity-${pivot.id}`,
        indicator: "liquidity",
        direction,
        startTime: pivot.time,
        price: pivot.price,
        label: pivot.kind === "high" ? "BSL" : "SSL",
        active: true,
        sourcePivotId: pivot.id,
        swept: false,
        broken: false,
      });

      if (previousSameKind && !previousSameKind.swept && !previousSameKind.broken) {
        const tolerance = Math.max(pivot.atr, previousSameKind.atr) * SMART_MONEY_RULES.equalToleranceAtr;
        const enoughSeparation = pivot.index - previousSameKind.index >= swingBars * 2;
        if (enoughSeparation && Math.abs(pivot.price - previousSameKind.price) <= tolerance) {
          const level = (pivot.price + previousSameKind.price) / 2;
          equalLevels.push({
            id: `equal-${previousSameKind.id}-${pivot.id}`,
            indicator: "equal_high_low",
            direction,
            startTime: previousSameKind.time,
            price: level,
            label: pivot.kind === "high" ? "EQH" : "EQL",
            active: true,
            swept: false,
            broken: false,
          });
        }
      }
    }

    const currentAtr = atr[index];
    if (!Number.isFinite(currentAtr) || currentAtr <= 0) continue;

    // A sweep must trade through a confirmed level but close back on the
    // original side. A close beyond it is handled below as structure break.
    for (const direction of ["bearish", "bullish"] as const) {
      const pivotLevel = latestEligibleSweepLevel(liquidityLevels, direction);
      const equalLevel = latestEligibleSweepLevel(equalLevels, direction);
      const candidates = [equalLevel, pivotLevel].filter((level): level is MutableLiquidityLevel => level !== undefined);
      let emittedSweep = false;
      for (const level of candidates) {
        if (Number(level.startTime) >= Number(candle.time)) continue;
        const buffer = currentAtr * SMART_MONEY_RULES.breakBufferAtr;
        const swept = direction === "bearish"
          ? candle.high > level.price + buffer && candle.close <= level.price
          : candle.low < level.price - buffer && candle.close >= level.price;
        if (!swept) continue;
        level.swept = true;
        level.active = false;
        level.endTime = candle.time;
        const sourcePivot = pivots.find((pivot) => pivot.id === level.sourcePivotId);
        if (sourcePivot) {
          sourcePivot.swept = true;
          sourcePivot.endTime = candle.time;
        }
        if (!emittedSweep) {
          markers.push({
            id: `sweep-${level.id}-${Number(candle.time)}`,
            indicator: "liquidity_sweeps",
            kind: "sweep",
            direction,
            time: candle.time,
            price: direction === "bearish" ? candle.high : candle.low,
            label: direction === "bearish" ? "BSL sweep" : "SSL sweep",
          });
          emittedSweep = true;
        }
      }
    }

    const highPivot = lastUnbrokenPivot(pivots, "high");
    const lowPivot = lastUnbrokenPivot(pivots, "low");
    const brokenPivot = highPivot && closeBreaks(candle, candles[index - 1], highPivot, currentAtr)
      ? highPivot
      : lowPivot && closeBreaks(candle, candles[index - 1], lowPivot, currentAtr)
        ? lowPivot
        : undefined;
    if (brokenPivot) {
      const direction: SmartMoneyDirection = brokenPivot.kind === "high" ? "bullish" : "bearish";
      const numericDirection = direction === "bullish" ? 1 : -1;
      const isChoch = bias !== 0 && bias !== numericDirection;
      const isMss = isChoch && isDisplacement(candle, currentAtr, direction, SMART_MONEY_RULES.mssDisplacementAtr);
      const event: StructureEvent = { index, direction, source: brokenPivot, isChoch, isMss };
      if (!isChoch) {
        structureLevels.push({
          id: `bos-${brokenPivot.id}-${index}`,
          indicator: "bos",
          direction,
          startTime: brokenPivot.time,
          endTime: candle.time,
          price: brokenPivot.price,
          label: "BOS",
          active: false,
        });
      } else {
        structureLevels.push({
          id: `choch-${brokenPivot.id}-${index}`,
          indicator: "choch",
          direction,
          startTime: brokenPivot.time,
          endTime: candle.time,
          price: brokenPivot.price,
          label: "CHOCH",
          active: false,
        });
        if (isMss) {
          structureLevels.push({
            id: `mss-${brokenPivot.id}-${index}`,
            indicator: "mss",
            direction,
            startTime: brokenPivot.time,
            endTime: candle.time,
            price: brokenPivot.price,
            label: "MSS",
            active: false,
          });
        }
      }

      for (const pivot of pivots) {
        const crossed = direction === "bullish"
          ? pivot.kind === "high" && candle.close > pivot.price
          : pivot.kind === "low" && candle.close < pivot.price;
        if (crossed) {
          pivot.broken = true;
          pivot.endTime = candle.time;
        }
      }
      for (const level of [...liquidityLevels, ...equalLevels]) {
        const crossed = direction === "bullish"
          ? level.direction === "bearish" && candle.close > level.price
          : level.direction === "bullish" && candle.close < level.price;
        if (crossed) {
          level.broken = true;
          level.active = false;
          level.endTime = candle.time;
        }
      }
      bias = numericDirection;

      const displacementQualified = isDisplacement(candle, currentAtr, direction, SMART_MONEY_RULES.fvgDisplacementAtr)
        || Math.abs(candle.close - brokenPivot.price) >= currentAtr * 0.5;
      if (displacementQualified) {
        const origin = findOrderBlockCandle(candles, atr, event);
        if (origin) {
          const top = direction === "bullish" ? Math.max(origin.candle.open, origin.candle.close) : origin.candle.high;
          const bottom = direction === "bullish" ? origin.candle.low : Math.min(origin.candle.open, origin.candle.close);
          zones.push({
            id: `ob-${direction}-${origin.index}-${index}`,
            indicator: "order_blocks",
            direction,
            startTime: origin.candle.time,
            top,
            bottom,
            label: direction === "bullish" ? "Bull OB" : "Bear OB",
            status: "active",
          });
        }
      }
    }

    if (index >= 2) {
      const first = candles[index - 2];
      const middle = candles[index - 1];
      if (!first || !middle) continue;
      const gapThreshold = currentAtr * SMART_MONEY_RULES.fvgMinimumAtr;
      const middleAtr = atr[index - 1];
      const bullishGap = candle.low - first.high;
      const bearishGap = first.low - candle.high;
      const bullishDisplacement = isDisplacement(middle, middleAtr, "bullish", SMART_MONEY_RULES.fvgDisplacementAtr);
      const bearishDisplacement = isDisplacement(middle, middleAtr, "bearish", SMART_MONEY_RULES.fvgDisplacementAtr);
      if (bullishGap >= gapThreshold && bullishDisplacement) {
        zones.push({
          id: `fvg-bullish-${index}`,
          indicator: "fvg",
          direction: "bullish",
          startTime: first.time,
          bottom: first.high,
          top: candle.low,
          label: "Bull FVG",
          status: "active",
        });
      } else if (bearishGap >= gapThreshold && bearishDisplacement) {
        zones.push({
          id: `fvg-bearish-${index}`,
          indicator: "fvg",
          direction: "bearish",
          startTime: first.time,
          bottom: candle.high,
          top: first.low,
          label: "Bear FVG",
          status: "active",
        });
      }
    }
  }

  // Keep historical signals useful without letting a long-loaded chart turn
  // into an unreadable wall of annotations.
  const recentStructure = appendMostRecent(structureLevels, SMART_MONEY_RULES.maxVisiblePerKind);
  const recentLiquidity = appendMostRecent(
    liquidityLevels.filter((level) => level.active || level.swept),
    12,
  );
  const recentEquals = appendMostRecent(
    equalLevels.filter((level) => level.active || level.swept),
    10,
  );
  const recentZones = ["order_blocks", "fvg"].flatMap((indicator) => {
    const matching = zones.filter((zone) => zone.indicator === indicator);
    const active = matching.filter((zone) => zone.status === "active").slice(-6);
    const finished = matching.filter((zone) => zone.status !== "active").slice(-2);
    return [...finished, ...active];
  });

  return {
    markers: [
      ...markers.filter((marker) => marker.indicator === "swing_points").slice(-32),
      ...markers.filter((marker) => marker.indicator === "liquidity_sweeps").slice(-16),
    ],
    levels: [...recentStructure, ...recentLiquidity, ...recentEquals],
    zones: recentZones,
  };
}
