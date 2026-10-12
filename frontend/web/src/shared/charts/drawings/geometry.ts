import {
  DEFAULT_LEVELS,
  positionMetrics,
  toolDefinition,
  type ChartPoint,
  type Drawing,
} from "./model.ts";
export type XY = { x: number; y: number };
export type Shape = {
  kind: "path" | "text" | "ellipse";
  path?: string;
  x?: number;
  y?: number;
  rx?: number;
  ry?: number;
  text?: string;
  fill?: boolean;
  color?: string;
  opacity?: number;
};
export type Geometry = { shapes: Shape[]; handles: XY[] };
export function drawingGeometry(
  d: Drawing,
  project: (p: ChartPoint) => XY | null,
  width: number,
  height: number,
): Geometry {
  const p = d.points.map(project);
  if (p.some((v) => !v)) return { shapes: [], handles: [] };
  const pts = p as XY[],
    [a, b = a, c = b, f = c] = pts,
    shapes: Shape[] = [];
  const path = (points: XY[], fill = false, color?: string, opacity?: number) =>
    shapes.push({
      kind: "path",
      path:
        points.map((v, i) => `${i ? "L" : "M"}${v.x} ${v.y}`).join(" ") +
        (fill ? " Z" : ""),
      fill,
      color,
      opacity,
    });
  const line = (a: XY, b: XY, color?: string) => path([a, b], false, color);
  const text = (at: XY, value: string, color?: string) =>
    shapes.push({ kind: "text", x: at.x, y: at.y, text: value, color });
  const rect = (a: XY, b: XY, color?: string) =>
    path(
      [a, { x: b.x, y: a.y }, b, { x: a.x, y: b.y }],
      d.fill !== false,
      color,
    );
  const ellipse = (at: XY, rx: number, ry: number, fill = false) =>
    shapes.push({
      kind: "ellipse",
      x: at.x,
      y: at.y,
      rx: Math.abs(rx),
      ry: Math.abs(ry),
      fill,
    });
  const longLine = (start: XY, end: XY, both: boolean) => {
    const dx = end.x - start.x,
      dy = end.y - start.y,
      size = (Math.max(width, height) * 4) / Math.max(Math.hypot(dx, dy), 1);
    line(both ? { x: start.x - dx * size, y: start.y - dy * size } : start, {
      x: end.x + dx * size,
      y: end.y + dy * size,
    });
  };
  const arrow = (start: XY, end: XY) => {
    line(start, end);
    const angle = Math.atan2(end.y - start.y, end.x - start.x);
    path([
      {
        x: end.x - 10 * Math.cos(angle - 0.45),
        y: end.y - 10 * Math.sin(angle - 0.45),
      },
      end,
      {
        x: end.x - 10 * Math.cos(angle + 0.45),
        y: end.y - 10 * Math.sin(angle + 0.45),
      },
    ]);
  };
  const channel = (a: XY, b: XY, c: XY, levels: number[]) => {
    const dy = c.y - a.y - ((b.y - a.y) * (c.x - a.x)) / (b.x - a.x || 1);
    for (const k of levels) {
      line({ x: a.x, y: a.y + dy * k }, { x: b.x, y: b.y + dy * k });
      text({ x: b.x + 5, y: b.y + dy * k }, String(k));
    }
  };
  const type = d.type,
    def = toolDefinition(type);
  if (def && def.points > 0 && pts.length < def.points) {
    path(pts);
    return { shapes, handles: pts };
  }
  if (type === "horizontal" || type === "horizontal-ray") {
    line({ x: type === "horizontal" ? 0 : a.x, y: a.y }, { x: width, y: a.y });
    text(
      { x: 8, y: a.y - 5 },
      d.points[0].price.toLocaleString("ru-RU", {
        maximumSignificantDigits: 8,
      }),
    );
  } else if (type === "vertical") line({ x: a.x, y: 0 }, { x: a.x, y: height });
  else if (type === "cross") {
    line({ x: 0, y: a.y }, { x: width, y: a.y });
    line({ x: a.x, y: 0 }, { x: a.x, y: height });
  } else if (["trend", "angle", "forecast"].includes(type)) {
    line(a, b);
    if (type === "angle")
      text(
        b,
        `${((Math.atan2(a.y - b.y, b.x - a.x) * 180) / Math.PI).toFixed(1)}° (экран)`,
      );
    if (type === "forecast")
      text(
        b,
        `${((d.points[1].price / d.points[0].price - 1) * 100).toFixed(2)}%`,
      );
  } else if (type === "ray" || type === "extended")
    longLine(a, b, type === "extended");
  else if (type === "arrow") arrow(a, b);
  else if (type === "channel") {
    channel(a, b, c, [0, 0.5, 1]);
  } else if (type === "fib-channel")
    channel(a, b, c, d.levels ?? DEFAULT_LEVELS);
  else if (type === "flat-channel") {
    line({ x: a.x, y: a.y }, { x: b.x, y: a.y });
    line({ x: a.x, y: b.y }, b);
    line({ x: a.x, y: (a.y + b.y) / 2 }, { x: b.x, y: (a.y + b.y) / 2 });
  } else if (type === "disjoint-channel") {
    line(a, b);
    line(c, f);
  } else if (
    ["pitchfork", "schiff", "modified-schiff", "inside-pitchfork"].includes(
      type,
    )
  ) {
    let origin = a;
    const middle = { x: (b.x + c.x) / 2, y: (b.y + c.y) / 2 };
    if (type === "schiff") origin = { x: a.x, y: (a.y + b.y) / 2 };
    if (type === "modified-schiff")
      origin = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
    if (type === "inside-pitchfork")
      origin = { x: (a.x + middle.x) / 2, y: (a.y + middle.y) / 2 };
    line(b, c);
    longLine(origin, middle, false);
    const delta = { x: middle.x - origin.x, y: middle.y - origin.y };
    for (const v of [b, c])
      longLine(v, { x: v.x + delta.x, y: v.y + delta.y }, false);
  } else if (type === "fibonacci" || type === "fib-extension") {
    const base = type === "fib-extension" ? c : a,
      delta = b.y - a.y;
    for (const k of d.levels ??
      (type === "fib-extension"
        ? [0, 0.618, 1, 1.618, 2.618]
        : DEFAULT_LEVELS)) {
      const y = base.y + delta * k;
      line(
        { x: Math.min(a.x, b.x), y },
        { x: Math.max(b.x, c.x, a.x) + 60, y },
      );
      const price =
        type === "fib-extension"
          ? d.points[2].price + (d.points[1].price - d.points[0].price) * k
          : d.points[0].price + (d.points[1].price - d.points[0].price) * k;
      text(
        { x: Math.min(a.x, b.x) + 4, y: y - 4 },
        `${k} · ${price.toLocaleString("ru-RU", { maximumSignificantDigits: 7 })}`,
      );
    }
  } else if (type === "fib-time" || type === "fib-trend-time") {
    const base = type === "fib-time" ? a : c;
    for (const k of [0, 1, 2, 3, 5, 8, 13, 21]) {
      const x = base.x + (b.x - a.x) * k;
      line({ x, y: 0 }, { x, y: height });
      text({ x: x + 4, y: 18 }, String(k));
    }
  } else if (type === "fib-fan" || type === "gann-fan") {
    for (const k of type === "fib-fan"
      ? [0.382, 0.5, 0.618, 0.786, 1]
      : [0.125, 0.25, 0.5, 1, 2, 4, 8]) {
      const end = { x: b.x, y: a.y + (b.y - a.y) * k };
      longLine(a, end, false);
      text(end, String(k));
    }
  } else if (type === "gann-box" || type === "gann-square") {
    const end =
      type === "gann-square"
        ? { x: b.x, y: a.y + Math.sign(b.y - a.y || 1) * Math.abs(b.x - a.x) }
        : b;
    rect(a, end);
    for (const k of [0.25, 0.5, 0.75]) {
      line(
        { x: a.x + (end.x - a.x) * k, y: a.y },
        { x: a.x + (end.x - a.x) * k, y: end.y },
      );
      line(
        { x: a.x, y: a.y + (end.y - a.y) * k },
        { x: end.x, y: a.y + (end.y - a.y) * k },
      );
    }
    line(a, end);
    line({ x: a.x, y: end.y }, { x: end.x, y: a.y });
  } else if (type === "rectangle") rect(a, b);
  else if (type === "circle")
    ellipse(
      a,
      Math.hypot(b.x - a.x, b.y - a.y),
      Math.hypot(b.x - a.x, b.y - a.y),
      d.fill,
    );
  else if (type === "ellipse")
    ellipse(
      { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 },
      (b.x - a.x) / 2,
      (b.y - a.y) / 2,
      d.fill,
    );
  else if (type === "rotated-rectangle") {
    const dx = b.x - a.x,
      dy = b.y - a.y,
      t = ((c.x - a.x) * -dy + (c.y - a.y) * dx) / (dx * dx + dy * dy || 1),
      off = { x: -dy * t, y: dx * t };
    path(
      [
        a,
        b,
        { x: b.x + off.x, y: b.y + off.y },
        { x: a.x + off.x, y: a.y + off.y },
      ],
      d.fill,
    );
  } else if (type === "triangle") path(pts, true);
  else if (type === "arc") {
    const det = 2 * (a.x * (b.y - c.y) + b.x * (c.y - a.y) + c.x * (a.y - b.y));
    if (Math.abs(det) < 1e-8) path([a, b, c]);
    else {
      const aa = a.x * a.x + a.y * a.y,
        bb = b.x * b.x + b.y * b.y,
        cc = c.x * c.x + c.y * c.y,
        center = {
          x: (aa * (b.y - c.y) + bb * (c.y - a.y) + cc * (a.y - b.y)) / det,
          y: (aa * (c.x - b.x) + bb * (a.x - c.x) + cc * (b.x - a.x)) / det,
        },
        radius = Math.hypot(a.x - center.x, a.y - center.y),
        tau = Math.PI * 2,
        angle = (p: XY) => Math.atan2(p.y - center.y, p.x - center.x),
        dab = (angle(b) - angle(a) + tau) % tau,
        dac = (angle(c) - angle(a) + tau) % tau,
        sweep = dab <= dac ? 1 : 0,
        large = (sweep ? dac : tau - dac) > Math.PI ? 1 : 0;
      shapes.push({
        kind: "path",
        path: `M${a.x} ${a.y} A${radius} ${radius} 0 ${large} ${sweep} ${c.x} ${c.y}`,
      });
    }
  } else if (type === "curve")
    shapes.push({
      kind: "path",
      path: `M${a.x} ${a.y} Q${b.x} ${b.y} ${c.x} ${c.y}`,
    });
  else if (type === "double-curve")
    shapes.push({
      kind: "path",
      path: `M${a.x} ${a.y} C${b.x} ${b.y} ${c.x} ${c.y} ${f.x} ${f.y}`,
    });
  else if (type === "long" || type === "short") {
    const m = positionMetrics(d),
      end = Math.max(b.x, c.x, a.x + 30);
    rect(a, { x: end, y: b.y }, "#34d399");
    rect(a, { x: end, y: c.y }, "#fb7185");
    line(a, { x: end, y: a.y });
    text({ x: a.x + 4, y: b.y - 5 }, `Цель: ${m.profit.toFixed(2)}`, "#34d399");
    text({ x: a.x + 4, y: c.y + 15 }, `Риск: ${m.loss.toFixed(2)}`, "#fb7185");
    text(
      { x: a.x + 4, y: a.y - 5 },
      m.valid
        ? `${type === "long" ? "LONG" : "SHORT"} · R/R ${m.ratio?.toFixed(2)}`
        : "Проверьте цель и стоп",
    );
  } else if (["ruler", "date-range", "price-range"].includes(type)) {
    rect(a, b);
    const dt = Math.abs(d.points[1].time - d.points[0].time),
      dp = d.points[1].price - d.points[0].price;
    text(
      { x: Math.min(a.x, b.x) + 4, y: Math.min(a.y, b.y) - 6 },
      `${type === "date-range" ? "" : `${dp.toLocaleString("ru-RU", { maximumSignificantDigits: 7 })} (${((dp / Math.abs(d.points[0].price || 1)) * 100).toFixed(2)}%)`}${type === "price-range" ? "" : ` · ${(dt / 3600).toFixed(2)} ч`}`,
    );
  } else if (type === "cycles" || type === "time-cycles" || type === "sine") {
    const period = Math.max(Math.abs(b.x - a.x), 10);
    if (type === "cycles")
      for (let x = a.x; x < width; x += period) {
        line({ x, y: 0 }, { x, y: height });
      }
    else {
      const data: XY[] = [];
      for (let x = Math.max(0, a.x - period); x <= width; x += 3) {
        const phase = ((x - a.x) / period) * Math.PI * 2;
        data.push({
          x,
          y:
            type === "sine"
              ? a.y + Math.sin(phase) * (b.y - a.y)
              : a.y - Math.abs(Math.sin(phase / 2)) * Math.abs(b.y - a.y),
        });
      }
      path(data);
    }
  } else if (
    type === "bars-pattern" ||
    type === "price-projection" ||
    type === "projection" ||
    type === "regression" ||
    type === "volume-profile"
  ) {
    const sample = d.sample ?? [];
    if (sample.length < 2)
      text(a, "Для инструмента нужны свечи выбранного диапазона");
    else if (type === "regression") {
      const n = sample.length,
        avg = sample.reduce((s, v) => s + v.close, 0) / n,
        mid = (n - 1) / 2;
      const slope =
          sample.reduce((s, v, i) => s + (i - mid) * (v.close - avg), 0) /
          (sample.reduce((s, _, i) => s + (i - mid) ** 2, 0) || 1),
        intercept = avg - slope * mid,
        dev = Math.sqrt(
          sample.reduce(
            (s, v, i) => s + (v.close - intercept - slope * i) ** 2,
            0,
          ) / n,
        );
      for (const k of [-2, 0, 2]) {
        const start = project({
            time: sample[0].time,
            price: intercept + dev * k,
          }),
          end = project({
            time: sample[n - 1].time,
            price: intercept + slope * (n - 1) + dev * k,
          });
        if (start && end) line(start, end);
      }
    } else if (type === "volume-profile") {
      const lo = Math.min(...sample.map((v) => v.low)),
        hi = Math.max(...sample.map((v) => v.high)),
        bins = Array.from({ length: 24 }, () => 0),
        step = (hi - lo || 1) / 24;
      for (const v of sample) {
        const first = Math.max(
            0,
            Math.min(23, Math.floor((v.low - lo) / step)),
          ),
          last = Math.max(
            first,
            Math.min(23, Math.floor((v.high - lo) / step)),
          );
        for (let i = first; i <= last; i++)
          bins[i] += v.volume / (last - first + 1);
      }
      const max = Math.max(...bins, 1);
      bins.forEach((value, i) => {
        const top = project({
            time: d.points[0].time,
            price: lo + step * (i + 1),
          }),
          bot = project({ time: d.points[0].time, price: lo + step * i });
        if (top && bot)
          rect(top, {
            x: top.x + (value / max) * Math.abs(b.x - a.x),
            y: bot.y,
          });
      });
      text(a, "Объём по OHLC · оценка распределения");
    } else {
      const first = sample[0],
        deltaTime = d.points[2].time - first.time,
        deltaPrice = d.points[2].price - first.close;
      const translated = sample.map((v) => ({
        time: v.time + deltaTime,
        price:
          type === "price-projection"
            ? (v.close * d.points[2].price) / first.close
            : v.close + deltaPrice,
      }));
      if (type === "bars-pattern")
        for (const v of sample) {
          const high = project({
              time: v.time + deltaTime,
              price: v.high + deltaPrice,
            }),
            low = project({
              time: v.time + deltaTime,
              price: v.low + deltaPrice,
            }),
            open = project({
              time: v.time + deltaTime,
              price: v.open + deltaPrice,
            }),
            close = project({
              time: v.time + deltaTime,
              price: v.close + deltaPrice,
            });
          if (high && low && open && close) {
            line(high, low);
            rect({ x: open.x - 2, y: open.y }, { x: close.x + 2, y: close.y });
          }
        }
      else path(translated.map(project).filter((v): v is XY => !!v));
    }
  } else if (def?.group === "text") {
    const at = d.screen
      ? { x: d.screen[0] * width, y: d.screen[1] * height }
      : a;
    const arrows: Record<string, string> = {
      "arrow-marker": "↗",
      "arrow-left": "←",
      "arrow-right": "→",
      "arrow-up": "↑",
      "arrow-down": "↓",
    };
    if (type === "callout") line(a, b);
    text(
      type === "callout" ? b : at,
      arrows[type] ??
        (type === "price-label"
          ? d.points[0].price.toLocaleString("ru-RU", {
              maximumSignificantDigits: 8,
            })
          : d.text || "Заметка"),
    );
  } else {
    path(pts, false, undefined, type === "highlighter" ? 0.35 : undefined);
    if (def?.group === "patterns") {
      const labels =
        type === "xabcd" || type === "cypher"
          ? ["X", "A", "B", "C", "D"]
          : type === "abcd"
            ? ["A", "B", "C", "D"]
            : type.startsWith("elliott")
              ? type === "elliott-impulse"
                ? ["0", "1", "2", "3", "4", "5"]
                : type === "elliott-triangle"
                  ? ["0", "A", "B", "C", "D", "E"]
                  : type === "elliott-triple"
                    ? ["0", "W", "X", "Y", "X", "Z"]
                    : type === "elliott-double"
                      ? ["0", "W", "X", "Y"]
                      : ["0", "A", "B", "C"]
              : pts.map((_, i) => String(i + 1));
      pts.forEach((v, i) =>
        text({ x: v.x + 4, y: v.y - 8 }, labels[i] ?? String(i)),
      );
      if (type === "xabcd" || type === "cypher" || type === "abcd") {
        for (let i = 2; i < pts.length; i++) {
          line(pts[i - 2], pts[i]);
          const n =
            Math.abs(d.points[i].price - d.points[i - 1].price) /
            Math.max(
              Math.abs(d.points[i - 1].price - d.points[i - 2].price),
              1e-12,
            );
          text(
            {
              x: (pts[i].x + pts[i - 1].x) / 2,
              y: (pts[i].y + pts[i - 1].y) / 2,
            },
            n.toFixed(3),
          );
        }
      }
    }
  }
  return {
    shapes,
    handles: d.screen
      ? [{ x: d.screen[0] * width, y: d.screen[1] * height }]
      : pts,
  };
}
