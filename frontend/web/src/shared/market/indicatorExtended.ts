import type { PerpCandle } from "./perpMarketData";
import type { ExtraIndicatorId } from "./indicatorCatalogExtra";
import { EXTRA_INDICATORS } from "./indicatorCatalogExtra";
import type { IndicatorOutput, IndicatorSeriesOutput } from "./indicatorEngine";
import {
  gaps,
  mean,
  sum,
  smooth,
  weighted,
  extrema,
  deviation,
  change,
  ratio,
  diff,
  rsi,
  roc,
  trueRange,
  cumulative,
  kernel,
} from "./indicatorMath";

function candleSeconds(c: readonly PerpCandle[]) {
  let step = Infinity;
  for (let i = 1; i < c.length; i++) {
    const difference = Number(c[i].time) - Number(c[i - 1].time);
    if (difference > 0) step = Math.min(step, difference);
  }
  return Number.isFinite(step) ? step : 86400;
}

export function calculateExtended(
  c: readonly PerpCandle[],
  id: ExtraIndicatorId,
  period?: number,
  barSeconds?: number,
): IndicatorOutput {
  const info = EXTRA_INDICATORS.find((s) => s.id === id)!;
  const n = Math.max(
    2,
    Math.min(400, Math.round(period ?? info.defaultPeriod ?? 14)),
  );
  const close = c.map((v) => v.close),
    high = c.map((v) => v.high),
    low = c.map((v) => v.low),
    volume = c.map((v) => v.volume);
  const hl2 = high.map((v, i) => (v + low[i]) / 2),
    hlc3 = high.map((v, i) => (v + low[i] + close[i]) / 3);
  const range = diff(high, low),
    tr = trueRange(high, low, close),
    atr = smooth(tr, n, true),
    delta = change(close);
  const series: IndicatorSeriesOutput[] = [];
  const plot = (
    key: string,
    data: readonly number[],
    color: string = info.color,
    title = key,
    histogram = false,
    histogramColor?: (value: number) => string,
  ) => {
    const points = data.flatMap((value, i) =>
      Number.isFinite(value) && c[i]
        ? [
            {
              time: c[i].time,
              value,
              ...(histogram
                ? {
                    color:
                      histogramColor?.(value) ??
                      (value >= 0 ? "#34d399" : "#fb7185"),
                  }
                : {}),
            },
          ]
        : [],
    );
    if (histogram)
      series.push({ key, kind: "histogram", title, color, data: points });
    else
      series.push({
        key,
        kind: "line",
        title,
        color,
        data: points,
        ...(id === "fractals" || id === "psar"
          ? { lineVisible: false, pointMarkersVisible: true }
          : {}),
      });
  };
  const basic = (data: readonly number[]) =>
    plot(
      id,
      data,
      info.color,
      `${info.shortLabel}${info.defaultPeriod ? ` ${n}` : ""}`,
    );
  const pair = (
    a: readonly number[],
    b: readonly number[],
    aName = "Fast",
    bName = "Slow",
  ) => {
    plot(aName, a, "#38bdf8", aName);
    plot(bName, b, "#fb923c", bName);
  };
  const adl = cumulative(
    c.map((v, i) =>
      range[i] === 0
        ? 0
        : ((2 * v.close - v.high - v.low) / range[i]) * v.volume,
    ),
  );
  const bands = (
    upper: readonly number[],
    middle: readonly number[],
    lower: readonly number[],
  ) => {
    plot("upper", upper, "#38bdf8", "Upper");
    plot("middle", middle, "#fbbf24", "Basis");
    plot("lower", lower, "#38bdf8", "Lower");
  };
  switch (id) {
    case "chop_zone": {
      const ema = smooth(close, n),
        h = extrema(high, 30),
        l = extrema(low, 30, false),
        angle = ema.map((v, i) =>
          i &&
          Number.isFinite(h[i]) &&
          Number.isFinite(v) &&
          Number.isFinite(ema[i - 1])
            ? h[i] === l[i]
              ? 0
              : (Math.atan(
                  (((v - ema[i - 1]) / hlc3[i]) * 25 * l[i]) / (h[i] - l[i]),
                ) *
                  180) /
                Math.PI
            : Number.NaN,
        );
      plot(id, angle, info.color, "EMA angle, °", true, (value) =>
        Math.abs(value) < 0.71
          ? "#fb923c"
          : value >= 5
            ? "#38bdf8"
            : value > 0
              ? "#34d399"
              : value <= -5
                ? "#fb7185"
                : "#c084fc",
      );
      break;
    }
    case "wma":
      basic(weighted(close, n));
      break;
    case "vwma":
      basic(
        ratio(
          sum(
            close.map((v, i) => v * volume[i]),
            n,
          ),
          sum(volume, n),
        ),
      );
      break;
    case "rma":
      basic(smooth(close, n, true));
      break;
    case "dema":
    case "tema": {
      const a = smooth(close, n),
        b = smooth(a, n),
        d = smooth(b, n);
      basic(
        a.map((v, i) =>
          id === "dema" ? 2 * v - b[i] : 3 * v - 3 * b[i] + d[i],
        ),
      );
      break;
    }
    case "trima":
      basic(mean(mean(close, Math.ceil(n / 2)), Math.floor(n / 2) + 1));
      break;
    case "hma":
      basic(
        weighted(
          diff(
            weighted(close, Math.max(1, Math.floor(n / 2))).map((v) => 2 * v),
            weighted(close, n),
          ),
          Math.max(1, Math.floor(Math.sqrt(n))),
        ),
      );
      break;
    case "alma":
      basic(
        kernel(
          close,
          Array.from({ length: n }, (_, j) =>
            Math.exp(-((j - 0.85 * (n - 1)) ** 2) / (2 * (n / 6) ** 2)),
          ),
        ),
      );
      break;
    case "hamming":
      basic(
        kernel(
          close,
          Array.from(
            { length: n },
            (_, j) => 0.54 - 0.46 * Math.cos((2 * Math.PI * j) / (n - 1)),
          ),
        ),
      );
      break;
    case "kama": {
      const movement = sum(delta.map(Math.abs), n),
        out = gaps(c.length);
      let last = mean(close, n)[n - 1];
      for (let i = n; i < c.length; i++) {
        const er =
            movement[i] === 0
              ? 0
              : Math.abs(close[i] - close[i - n]) / movement[i],
          sc = (er * (2 / 3 - 2 / 31) + 2 / 31) ** 2;
        last += sc * (close[i] - last);
        out[i] = last;
      }
      basic(out);
      break;
    }
    case "ma_cross":
      pair(mean(close, n), mean(close, 2 * n), `SMA ${n}`, `SMA ${2 * n}`);
      break;
    case "ema_cross":
      pair(smooth(close, n), smooth(close, 2 * n), `EMA ${n}`, `EMA ${2 * n}`);
      break;
    case "bb_percent": {
      const m = mean(close, n),
        sd = deviation(close, n);
      basic(
        close.map((v, i) =>
          Number.isFinite(sd[i])
            ? sd[i] === 0
              ? 0.5
              : (v - m[i] + 2 * sd[i]) / (4 * sd[i])
            : Number.NaN,
        ),
      );
      break;
    }
    case "bb_width":
      basic(
        ratio(
          deviation(close, n).map((v) => 4 * v * 100),
          mean(close, n),
        ),
      );
      break;
    case "ao":
    case "ac": {
      const ao = diff(mean(hl2, 5), mean(hl2, 34)),
        v = id === "ao" ? ao : diff(ao, mean(ao, 5));
      plot(id, v, info.color, id.toUpperCase(), true);
      break;
    }
    case "alligator":
      for (const [length, shift, color, name] of [
        [13, 8, "#38bdf8", "Jaw"],
        [8, 5, "#fb7185", "Teeth"],
        [5, 3, "#34d399", "Lips"],
      ] as const) {
        const v = smooth(hl2, length, true);
        plot(
          name,
          v.map((_, i) => v[i - shift] ?? Number.NaN),
          color,
          name,
        );
      }
      break;
    case "fractals": {
      const up = gaps(c.length),
        down = gaps(c.length);
      for (let i = 2; i < c.length - 2; i++) {
        if ([i - 2, i - 1, i + 1, i + 2].every((j) => high[i] > high[j]))
          up[i] = high[i];
        if ([i - 2, i - 1, i + 1, i + 2].every((j) => low[i] < low[j]))
          down[i] = low[i];
      }
      pair(up, down, "Fractal high", "Fractal low");
      break;
    }
    case "aroon": {
      const up = gaps(c.length),
        down = gaps(c.length);
      for (let i = n; i < c.length; i++) {
        let h = i - n,
          l = i - n;
        for (let j = i - n; j <= i; j++) {
          if (high[j] >= high[h]) h = j;
          if (low[j] <= low[l]) l = j;
        }
        up[i] = (100 * (n - (i - h))) / n;
        down[i] = (100 * (n - (i - l))) / n;
      }
      pair(up, down, "Aroon Up", "Aroon Down");
      break;
    }
    case "bop":
      basic(
        c.map((v, i) => (range[i] === 0 ? 0 : (v.close - v.open) / range[i])),
      );
      break;
    case "obv":
      basic(
        cumulative(
          delta.map((v, i) => (i === 0 ? 0 : Math.sign(v) * volume[i])),
        ),
      );
      break;
    case "adl":
      basic(adl);
      break;
    case "efi":
      basic(
        smooth(
          delta.map((v, i) => v * volume[i]),
          n,
        ),
      );
      break;
    case "cmf":
      basic(
        ratio(
          sum(
            c.map((v, i) =>
              range[i] === 0
                ? 0
                : ((2 * v.close - v.high - v.low) / range[i]) * v.volume,
            ),
            n,
          ),
          sum(volume, n),
        ),
      );
      break;
    case "mfi": {
      const d = change(hlc3),
        positive = sum(
          d.map((v, i) =>
            i === 0 ? Number.NaN : v > 0 ? hlc3[i] * volume[i] : 0,
          ),
          n,
        ),
        negative = sum(
          d.map((v, i) =>
            i === 0 ? Number.NaN : v < 0 ? hlc3[i] * volume[i] : 0,
          ),
          n,
        );
      basic(
        positive.map((v, i) =>
          !Number.isFinite(v)
            ? Number.NaN
            : negative[i] === 0
              ? v === 0
                ? 50
                : 100
              : 100 - 100 / (1 + v / negative[i]),
        ),
      );
      break;
    }
    case "pvt":
      basic(
        cumulative(
          delta.map((v, i) => (i === 0 ? 0 : (volume[i] * v) / close[i - 1])),
        ),
      );
      break;
    case "net_volume":
      plot(
        id,
        delta.map((v, i) => (i === 0 ? Number.NaN : Math.sign(v) * volume[i])),
        info.color,
        "Signed close volume",
        true,
      );
      break;
    case "donchian": {
      const h = extrema(high, n),
        l = extrema(low, n, false);
      bands(
        h,
        h.map((v, i) => (v + l[i]) / 2),
        l,
      );
      break;
    }
    case "keltner": {
      const m = smooth(close, n);
      bands(
        m.map((v, i) => v + 2 * atr[i]),
        m,
        m.map((v, i) => v - 2 * atr[i]),
      );
      break;
    }
    case "chande_kroll":
      pair(
        extrema(diff(extrema(high, n), atr), 9),
        extrema(
          extrema(low, n, false).map((v, i) => v + atr[i]),
          9,
          false,
        ),
        "Long stop",
        "Short stop",
      );
      break;
    case "psar": {
      const out = gaps(c.length);
      if (c.length < 2) break;
      let up = close[1] >= close[0],
        sar = up ? low[0] : high[0],
        ep = up ? high[0] : low[0],
        af = 0.02;
      for (let i = 1; i < c.length; i++) {
        sar += af * (ep - sar);
        sar = up
          ? Math.min(sar, low[i - 1], low[Math.max(0, i - 2)])
          : Math.max(sar, high[i - 1], high[Math.max(0, i - 2)]);
        if (up ? low[i] < sar : high[i] > sar) {
          up = !up;
          sar = ep;
          ep = up ? high[i] : low[i];
          af = 0.02;
        } else if (up ? high[i] > ep : low[i] < ep) {
          ep = up ? high[i] : low[i];
          af = Math.min(0.2, af + 0.02);
        }
        out[i] = sar;
      }
      basic(out);
      break;
    }
    case "momentum":
      basic(change(close, n));
      break;
    case "williams_r": {
      const h = extrema(high, n),
        l = extrema(low, n, false);
      basic(
        ratio(
          diff(h, close).map((v) => -100 * v),
          diff(h, l),
          -50,
        ),
      );
      break;
    }
    case "ultimate": {
      const bp = c.map((v, i) =>
          i ? v.close - Math.min(v.low, close[i - 1]) : Number.NaN,
        ),
        ur = c.map((v, i) =>
          i
            ? Math.max(v.high, close[i - 1]) - Math.min(v.low, close[i - 1])
            : Number.NaN,
        ),
        parts = [7, 14, 28].map((k) => ratio(sum(bp, k), sum(ur, k), 0.5));
      basic(
        parts[0].map(
          (v, i) => (100 * (4 * v + 2 * parts[1][i] + parts[2][i])) / 7,
        ),
      );
      break;
    }
    case "stoch_rsi": {
      const r = rsi(close, n),
        lo = extrema(r, n, false),
        hi = extrema(r, n),
        k = mean(
          ratio(
            diff(r, lo).map((v) => v * 100),
            diff(hi, lo),
            50,
          ),
          3,
        );
      pair(k, mean(k, 3), "Stoch RSI K", "Stoch RSI D");
      break;
    }
    case "connors_rsi": {
      let streak = 0;
      const s = delta.map((v, i) =>
          i === 0
            ? 0
            : (streak =
                v === 0
                  ? 0
                  : v > 0
                    ? Math.max(0, streak) + 1
                    : Math.min(0, streak) - 1),
        ),
        a = rsi(close, 3),
        b = rsi(s, 2),
        rates = roc(close, 1),
        rank = gaps(c.length);
      for (let i = 100; i < c.length; i++) {
        let count = 0;
        for (let j = i - 99; j <= i; j++) if (rates[j] < rates[i]) count++;
        rank[i] = count;
      }
      basic(a.map((v, i) => (v + b[i] + rank[i]) / 3));
      break;
    }
    case "vortex": {
      const denominator = sum(
        tr.map((v, i) => (i ? v : Number.NaN)),
        n,
      );
      pair(
        ratio(
          sum(
            high.map((v, i) => (i ? Math.abs(v - low[i - 1]) : Number.NaN)),
            n,
          ),
          denominator,
          0,
        ),
        ratio(
          sum(
            low.map((v, i) => (i ? Math.abs(v - high[i - 1]) : Number.NaN)),
            n,
          ),
          denominator,
          0,
        ),
        "VI+",
        "VI−",
      );
      break;
    }
    case "dmi": {
      const up = change(high),
        down = change(low).map((v) => -v),
        plus = up.map((v, i) =>
          i ? (v > down[i] && v > 0 ? v : 0) : Number.NaN,
        ),
        minus = down.map((v, i) =>
          i ? (v > up[i] && v > 0 ? v : 0) : Number.NaN,
        ),
        a = smooth(
          tr.map((v, i) => (i ? v : Number.NaN)),
          n,
          true,
        );
      pair(
        ratio(
          smooth(plus, n, true).map((v) => 100 * v),
          a,
          0,
        ),
        ratio(
          smooth(minus, n, true).map((v) => 100 * v),
          a,
          0,
        ),
        "+DI",
        "−DI",
      );
      break;
    }
    case "trix":
      basic(roc(smooth(smooth(smooth(close, n), n), n), 1));
      break;
    case "tsi":
    case "smi_ergodic": {
      const small = id === "tsi" ? 13 : 5,
        v = ratio(
          smooth(smooth(delta, n), small).map((v) => 100 * v),
          smooth(smooth(delta.map(Math.abs), n), small),
          0,
        );
      pair(
        v,
        smooth(v, id === "tsi" ? 13 : 5),
        id === "tsi" ? "TSI" : "SMI Ergodic",
        "Signal",
      );
      if (id === "smi_ergodic")
        plot(
          "oscillator",
          diff(v, smooth(v, 5)),
          "#34d399",
          "Ergodic oscillator",
          true,
        );
      break;
    }
    case "rvi": {
      const a = mean(
          kernel(
            c.map((v) => v.close - v.open),
            [1, 2, 2, 1],
          ),
          n,
        ),
        b = mean(kernel(range, [1, 2, 2, 1]), n),
        r = ratio(a, b, 0);
      pair(r, kernel(r, [1, 2, 2, 1]), "RVI", "RVI signal");
      break;
    }
    case "rvi_volatility": {
      const sd = deviation(close, n),
        up = smooth(
          sd.map((v, i) =>
            !Number.isFinite(v) ? Number.NaN : delta[i] > 0 ? v : 0,
          ),
          14,
          true,
        ),
        dn = smooth(
          sd.map((v, i) =>
            !Number.isFinite(v) ? Number.NaN : delta[i] < 0 ? v : 0,
          ),
          14,
          true,
        );
      basic(
        ratio(
          up.map((v) => 100 * v),
          up.map((v, i) => v + dn[i]),
          50,
        ),
      );
      break;
    }
    case "choppiness":
      basic(
        ratio(sum(tr, n), diff(extrema(high, n), extrema(low, n, false))).map(
          (v) => (100 * Math.log10(v)) / Math.log10(n),
        ),
      );
      break;
    case "fisher": {
      const h = extrema(hl2, n),
        l = extrema(hl2, n, false),
        out = gaps(c.length);
      let prev = 0,
        f = 0;
      for (let i = n - 1; i < c.length; i++) {
        const normalized =
          h[i] === l[i] ? 0 : 2 * ((hl2[i] - l[i]) / (h[i] - l[i]) - 0.5);
        prev = Math.max(
          -0.999,
          Math.min(0.999, 0.33 * normalized + 0.67 * prev),
        );
        f = 0.5 * Math.log((1 + prev) / (1 - prev)) + 0.5 * f;
        out[i] = f;
      }
      pair(
        out,
        out.map((_, i) => out[i - 1] ?? Number.NaN),
        "Fisher",
        "Trigger",
      );
      break;
    }
    case "chaikin_volatility":
      basic(roc(smooth(range, n), n));
      break;
    case "chaikin_oscillator":
      basic(diff(smooth(adl, 3), smooth(adl, 10)));
      break;
    case "volume_oscillator":
      basic(
        ratio(smooth(volume, n), smooth(volume, 2 * n)).map(
          (v) => (v - 1) * 100,
        ),
      );
      break;
    case "price_oscillator": {
      const v = ratio(smooth(close, n), smooth(close, 2 * n)).map(
        (v) => (v - 1) * 100,
      );
      pair(v, smooth(v, 9), "PPO", "PPO signal");
      break;
    }
    case "historical_volatility":
    case "zero_trend_volatility": {
      const returns = close.map((v, i) =>
          i ? Math.log(v / close[i - 1]) : Number.NaN,
        ),
        step = barSeconds && barSeconds > 0 ? barSeconds : candleSeconds(c),
        annual = 100 * Math.sqrt((365 * 86400) / step);
      basic(
        (id === "historical_volatility"
          ? deviation(returns, n, true)
          : mean(
              returns.map((v) => v * v),
              n,
            ).map(Math.sqrt)
        ).map((v) => v * annual),
      );
      break;
    }
    case "mass_index": {
      const a = smooth(range, 9);
      basic(sum(ratio(a, smooth(a, 9)), n));
      break;
    }
    case "stddev":
      basic(deviation(close, n));
      break;
    case "stderr":
      basic(deviation(close, n, true).map((v) => v / Math.sqrt(n)));
      break;
    case "median_price":
      basic(hl2);
      break;
    case "typical_price":
      basic(hlc3);
      break;
    case "average_price":
      basic(c.map((v) => (v.open + v.high + v.low + v.close) / 4));
      break;
    case "pivots": {
      const keys = ["P", "R1", "R2", "S1", "S2"],
        vectors = keys.map(() => gaps(c.length));
      const step = barSeconds && barSeconds > 0 ? barSeconds : candleSeconds(c);
      let day = -1,
        h = 0,
        l = 0,
        last = 0,
        lastTime = 0,
        complete = false,
        levels: number[] = [];
      for (let i = 0; i < c.length; i++) {
        const bucket = Math.floor(Number(c[i].time) / 86400);
        if (bucket !== day) {
          if (
            day >= 0 &&
            bucket === day + 1 &&
            complete &&
            lastTime === (day + 1) * 86400 - step
          ) {
            const p = (h + l + last) / 3;
            levels = [p, 2 * p - l, p + h - l, 2 * p - h, p - h + l];
          } else levels = [];
          day = bucket;
          h = high[i];
          l = low[i];
          complete =
            step > 0 && step <= 86400 && Number(c[i].time) === bucket * 86400;
        } else if (Number(c[i].time) - lastTime !== step) {
          complete = false;
        }
        h = Math.max(h, high[i]);
        l = Math.min(l, low[i]);
        last = close[i];
        lastTime = Number(c[i].time);
        levels.forEach((v, j) => (vectors[j][i] = v));
      }
      keys.forEach((key, j) =>
        plot(
          key,
          vectors[j],
          j === 0 ? "#fbbf24" : j < 3 ? "#34d399" : "#fb7185",
          key,
        ),
      );
      break;
    }
    case "klinger": {
      let trend = -1,
        cm = range[0] ?? 0;
      const vf = c.map((v, i) => {
        if (i === 0) return 0;
        const next = hlc3[i] > hlc3[i - 1] ? 1 : -1;
        cm = next === trend ? cm + range[i] : range[i - 1] + range[i];
        trend = next;
        return cm === 0 ? 0 : v.volume * 2 * (range[i] / cm - 1) * trend * 100;
      });
      const k = diff(smooth(vf, 34), smooth(vf, 55));
      pair(k, smooth(k, 13), "Klinger", "Klinger signal");
      break;
    }
    // Profiles are rendered in price coordinates by the shared SVG layer.
    case "volume_profile":
    case "visible_volume_profile":
      break;
  }
  return { id, placement: info.placement, series };
}
