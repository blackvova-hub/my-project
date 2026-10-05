import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import type { IChartApi, ISeriesApi, UTCTimestamp } from "lightweight-charts";
import { DRAWING_TOOLS, type DrawingTool } from "../../../shared/market/chartDrawings";
import { ToolIcon } from "../../../shared/market/ChartToolIcon";
import { SmartMoneyOverlay } from "../../../shared/market/SmartMoneyOverlay";
import type { SmartMoneyOutput } from "../../../shared/market/smartMoneyEngine";
import type { SmartMoneyIndicatorId } from "../../../shared/market/indicatorTypes";
import type { Candle } from "./model";
import { Icon } from "./Icons";

export type Tool = DrawingTool;
type Point = { time: number; price: number };
export type Drawing = {
  tool: Exclude<Tool, "cursor">;
  points: Point[];
  color: string;
  width: number;
  text?: string;
  locked?: boolean;
};
export type DrawingAPI = {
  chart: IChartApi;
  series: ISeriesApi<"Candlestick">;
};
const toolColors: Record<Exclude<Tool, "cursor">, string> = {
  horizontal: "#facc15", vertical: "#c084fc", trend: "#60a5fa",
  ray: "#fb923c", rectangle: "#60a5fa", fibonacci: "#fbbf24",
  brush: "#f9a8d4", text: "#6ee7b7", ruler: "#4ade80",
};

function resolvePoint(
  api: DrawingAPI,
  bounds: DOMRect | undefined,
  candles: Candle[],
  magnet: boolean,
  lastTime: number,
  interval: number,
  clientX: number,
  clientY: number,
): Point | null {
  if (!bounds) return null;
  const scale = api.chart.timeScale();
  const x = clientX - bounds.left;
  const y = clientY - bounds.top;
  const rawPrice = api.series.coordinateToPrice(y);
  if (rawPrice === null || y < 0 || y > bounds.height) return null;
  const base = scale.timeToCoordinate(lastTime as UTCTimestamp);
  const chartTime = scale.coordinateToTime(x) as number | null;
  const time = chartTime ?? (base === null ? null : lastTime + Math.round((x - base) / scale.options().barSpacing) * interval);
  if (time === null) return null;
  const nearest = candles.reduce<Candle | null>((best, candle) => Math.abs(candle.time - time) < Math.abs((best?.time ?? Infinity) - time) ? candle : best, null);
  const snappedTime = nearest && Math.abs(nearest.time - time) <= interval / 2 ? nearest.time : time;
  let price: number = Number(rawPrice);
  if (magnet && nearest && nearest.time === snappedTime) {
    let nearestDistance = 10;
    for (const candidate of [nearest.open, nearest.high, nearest.low, nearest.close]) {
      const coordinate = api.series.priceToCoordinate(candidate);
      if (coordinate !== null && Math.abs(coordinate - y) <= nearestDistance) {
        nearestDistance = Math.abs(coordinate - y);
        price = candidate;
      }
    }
  }
  return { time: snappedTime, price };
}

export function DrawingToolbar({
  tool,
  onTool,
  onUndo,
  onClear,
  onDelete,
  onLock,
  onVisibility,
  onMagnet,
  selected,
  visible,
  magnet,
  count,
  disabled,
}: {
  tool: Tool;
  onTool: (tool: Tool) => void;
  onUndo: () => void;
  onClear: () => void;
  onDelete: () => void;
  onLock: () => void;
  onVisibility: () => void;
  onMagnet: () => void;
  selected: Drawing | null;
  visible: boolean;
  magnet: boolean;
  count: number;
  disabled: boolean;
}) {
  return (
    <div className="rp-drawing-tools" role="toolbar" aria-label="Рисование">
      {DRAWING_TOOLS.map(({ value, label }) => (
        <button
          key={value}
          className="rp-icon rp-tool-button"
          title={label}
          aria-label={label}
          aria-pressed={value === tool}
          disabled={disabled}
          onClick={() => onTool(value)}
        >
          <ToolIcon tool={value} />
        </button>
      ))}
      <span className="rp-tool-divider" aria-hidden="true" />
      <button
        className="rp-icon"
        aria-label="Отменить последний объект"
        title="Отменить последний объект"
        disabled={!count}
        onClick={onUndo}
      >
        <Icon name="undo" />
      </button>
      <button className="rp-icon" aria-label="Удалить выбранный объект" title="Удалить выбранный объект" disabled={!selected} onClick={onDelete}>
        <Icon name="trash" />
      </button>
      <button
        className="rp-icon"
        aria-label="Удалить все объекты"
        title="Удалить все объекты"
        disabled={!count}
        onClick={onClear}
      >
        <Icon name="clear" />
      </button>
      <button className="rp-icon" aria-label={selected?.locked ? "Разблокировать объект" : "Заблокировать объект"} title={selected?.locked ? "Разблокировать объект" : "Заблокировать объект"} disabled={!selected} onClick={onLock}>
        <Icon name={selected?.locked ? "unlock" : "lock"} />
      </button>
      <button className="rp-icon" aria-label={visible ? "Скрыть объекты" : "Показать объекты"} title={visible ? "Скрыть объекты" : "Показать объекты"} disabled={!count} onClick={onVisibility}>
        <Icon name={visible ? "eye" : "eyeOff"} />
      </button>
      <button className="rp-icon" aria-label="Магнит к OHLC" title={magnet ? "Магнит к OHLC включён" : "Магнит к OHLC выключен"} aria-pressed={magnet} onClick={onMagnet}>
        <Icon name="magnet" />
      </button>
    </div>
  );
}

export function DrawingCanvas({
  api,
  tool,
  drawings,
  onAdd,
  onDone,
  lastTime,
  interval,
  candles,
  selectedIndex,
  onSelect,
  onUpdate,
  visible,
  magnet,
  smartMoneyOutput,
  smartMoneyActive,
}: {
  api: DrawingAPI;
  tool: Tool;
  drawings: Drawing[];
  onAdd: (d: Drawing) => void;
  onDone: () => void;
  lastTime: number;
  interval: number;
  candles: Candle[];
  selectedIndex: number | null;
  onSelect: (index: number) => void;
  onUpdate: (index: number, change: (drawing: Drawing) => Drawing) => void;
  visible: boolean;
  magnet: boolean;
  smartMoneyOutput: SmartMoneyOutput;
  smartMoneyActive: ReadonlySet<SmartMoneyIndicatorId>;
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null),
    draft = useRef<Drawing | null>(null),
    press = useRef<{ x: number; y: number } | null>(null),
    textCommitted = useRef(false);
  const [textDraft, setTextDraft] = useState<{ point: Point; x: number; y: number; value: string; index?: number } | null>(null);
  const finishText = () => {
    if (!textDraft || textCommitted.current) return;
    textCommitted.current = true;
    const note = textDraft.value.trim();
    if (note && textDraft.index !== undefined) onUpdate(textDraft.index, (drawing) => ({ ...drawing, text: note.slice(0, 200) }));
    else if (note) onAdd({ tool: "text", color: toolColors.text, width: 2, points: [textDraft.point], text: note.slice(0, 200) });
    setTextDraft(null);
    onDone();
  };
  const overlayRef = useRef<SVGSVGElement>(null);
  const drag = useRef<{
    index: number; pointerId: number; origin: Point; original: Drawing; handle: number | null;
  } | null>(null);
  const [projectionVersion, setProjectionVersion] = useState(0);
  useEffect(() => {
    const scale = api.chart.timeScale();
    const refresh = () => setProjectionVersion((version) => version + 1);
    scale.subscribeVisibleLogicalRangeChange(refresh);
    scale.subscribeSizeChange(refresh);
    return () => {
      scale.unsubscribeVisibleLogicalRangeChange(refresh);
      scale.unsubscribeSizeChange(refresh);
    };
  }, [api]);
  const scale = api.chart.timeScale();
  const project = (point: Point) => {
    const knownX = scale.timeToCoordinate(point.time as UTCTimestamp);
    const base = scale.timeToCoordinate(lastTime as UTCTimestamp);
    const x = knownX ?? (base === null ? -10000 : base + (point.time - lastTime) / interval * scale.options().barSpacing);
    const y = api.series.priceToCoordinate(point.price);
    return { x, y: y ?? -10000 };
  };
  const pointFromClient = (clientX: number, clientY: number) => resolvePoint(
    api,
    overlayRef.current?.getBoundingClientRect() ?? canvasRef.current?.getBoundingClientRect(),
    candles, magnet, lastTime, interval, clientX, clientY,
  );
  const dragStart = (event: ReactPointerEvent<SVGElement>, index: number, handle: number | null = null) => {
    if (tool !== "cursor") return;
    event.preventDefault();
    event.stopPropagation();
    onSelect(index);
    const drawing = drawings[index];
    if (drawing.locked) return;
    const origin = pointFromClient(event.clientX, event.clientY);
    if (!origin) return;
    drag.current = { index, pointerId: event.pointerId, origin, original: { ...drawing, points: drawing.points.map((point) => ({ ...point })) }, handle };
    event.currentTarget.setPointerCapture(event.pointerId);
  };
  const dragMove = (event: ReactPointerEvent<SVGElement>) => {
    const active = drag.current;
    if (!active || active.pointerId !== event.pointerId) return;
    event.preventDefault();
    event.stopPropagation();
    const point = pointFromClient(event.clientX, event.clientY);
    if (!point) return;
    const points = active.original.points.map((start, index) => active.handle === index ? point : active.handle === null ? {
      time: Math.max(1, start.time + point.time - active.origin.time),
      price: start.price + point.price - active.origin.price,
    } : start);
    onUpdate(active.index, () => ({ ...active.original, points }));
  };
  const dragEnd = (event: ReactPointerEvent<SVGElement>) => {
    if (drag.current?.pointerId !== event.pointerId) return;
    drag.current = null;
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
  };
  const pointerProps = (index: number, handle: number | null = null) => ({
    onPointerDown: (event: ReactPointerEvent<SVGElement>) => dragStart(event, index, handle),
    onPointerMove: dragMove,
    onPointerUp: dragEnd,
    onPointerCancel: dragEnd,
    style: { pointerEvents: tool === "cursor" ? "all" as const : "none" as const, cursor: drawings[index].locked ? "pointer" : handle === null ? "move" : "grab" },
  });
  void projectionVersion;
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    let frame = 0,
      lastPaint = 0;
    const scale = api.chart.timeScale();
    const toX = (time: number) => {
      const x = scale.timeToCoordinate(time as UTCTimestamp);
      if (x !== null) return x;
      const base = scale.timeToCoordinate(lastTime as UTCTimestamp);
      return base === null
        ? -10000
        : base + ((time - lastTime) / interval) * scale.options().barSpacing;
    };
    const paint = (now: number) => {
      frame = requestAnimationFrame(paint);
      if (now - lastPaint < 32) return;
      lastPaint = now;
      const w = scale.width(),
        h = api.chart.panes()[0]?.getHeight() ?? 0,
        dpr = devicePixelRatio || 1;
      if (
        canvas.width !== Math.round(w * dpr) ||
        canvas.height !== Math.round(h * dpr)
      ) {
        canvas.width = Math.round(w * dpr);
        canvas.height = Math.round(h * dpr);
        canvas.style.width = `${w}px`;
        canvas.style.height = `${h}px`;
      }
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, w, h);
      for (const d of [
        ...(visible ? drawings : []),
        ...(draft.current ? [draft.current] : []),
      ]) {
        const points = d.points.map((p) => ({
          x: toX(p.time),
          y: api.series.priceToCoordinate(p.price) ?? -10000,
        }));
        const a = points[0],
          b = points.at(-1)!;
        if (!a) continue;
        ctx.strokeStyle = d.color;
        ctx.fillStyle = d.color;
        ctx.lineWidth = d.width;
        ctx.lineCap = "round";
        ctx.lineJoin = "round";
        ctx.setLineDash([]);
        const line = (x1: number, y1: number, x2: number, y2: number) => {
          ctx.beginPath();
          ctx.moveTo(x1, y1);
          ctx.lineTo(x2, y2);
          ctx.stroke();
        };
        if (d.tool === "horizontal") {
          ctx.setLineDash([6, 4]);
          line(0, a.y, w, a.y);
          ctx.font = "12px system-ui";
          ctx.fillText(d.points[0].price.toFixed(4), 8, a.y - 7);
        } else if (d.tool === "vertical") {
          ctx.setLineDash([6, 4]);
          line(a.x, 0, a.x, h);
        } else if (d.tool === "text") {
          ctx.font = "13px system-ui";
          const text = d.text ?? "";
          const width = Math.min(240, Math.max(54, ctx.measureText(text).width + 16));
          ctx.fillStyle = "#0f2318";
          ctx.fillRect(a.x, a.y - 24, width, 26);
          ctx.strokeStyle = "#6ee7b780";
          ctx.strokeRect(a.x, a.y - 24, width, 26);
          ctx.fillStyle = "#d1fae5";
          ctx.fillText(text, a.x + 8, a.y - 7);
        } else if (d.tool === "rectangle") {
          ctx.globalAlpha = 0.1;
          ctx.fillRect(a.x, a.y, b.x - a.x, b.y - a.y);
          ctx.globalAlpha = 1;
          ctx.strokeRect(a.x, a.y, b.x - a.x, b.y - a.y);
        } else if (d.tool === "fibonacci") {
          for (const ratio of [0, 0.236, 0.382, 0.5, 0.618, 0.786, 1]) {
            const y = a.y + (b.y - a.y) * ratio;
            ctx.globalAlpha = ratio === 0.618 ? 1 : 0.65;
            line(Math.min(a.x, b.x), y, Math.max(a.x, b.x), y);
            ctx.font = "11px system-ui";
            ctx.fillText(String(ratio), Math.max(a.x, b.x) + 5, y + 4);
          }
          ctx.globalAlpha = 1;
        } else if (d.tool === "brush") {
          ctx.beginPath();
          points.forEach((p, i) =>
            i ? ctx.lineTo(p.x, p.y) : ctx.moveTo(p.x, p.y),
          );
          ctx.stroke();
        } else if (d.tool === "ray") {
          const length = Math.max(1, Math.hypot(b.x - a.x, b.y - a.y));
          const scale = Math.max(w, h) * 2 / length;
          line(a.x, a.y, a.x + (b.x - a.x) * scale, a.y + (b.y - a.y) * scale);
        } else {
          line(a.x, a.y, b.x, b.y);
          if (d.tool === "ruler") {
            ctx.font = "12px system-ui";
            const change = ((d.points.at(-1)!.price / d.points[0].price) - 1) * 100;
            ctx.fillText(`${change >= 0 ? "+" : ""}${change.toFixed(2)}%`, b.x + 7, b.y - 7);
          }
        }
        if (!["brush", "horizontal", "vertical", "text"].includes(d.tool))
          for (const p of [a, b]) {
            ctx.beginPath();
            ctx.arc(p.x, p.y, 3, 0, 2 * Math.PI);
            ctx.fill();
          }
      }
    };
    frame = requestAnimationFrame(paint);
    const point = (e: PointerEvent) => resolvePoint(api, canvas.getBoundingClientRect(), candles, magnet, lastTime, interval, e.clientX, e.clientY);
    const down = (e: PointerEvent) => {
      if (tool === "cursor" || e.button !== 0) return;
      const p = point(e);
      if (!p) return;
      e.preventDefault();
      if (tool === "text") {
        const rect = canvas.getBoundingClientRect();
        textCommitted.current = false;
        setTextDraft({ point: p, x: e.clientX - rect.left, y: e.clientY - rect.top, value: "" });
        return;
      }
      if (draft.current && !["horizontal", "vertical", "brush"].includes(tool)) {
        onAdd({ ...draft.current, points: [draft.current.points[0], p] });
        draft.current = null;
        press.current = null;
        onDone();
        return;
      }
      canvas.setPointerCapture(e.pointerId);
      press.current = { x: e.clientX, y: e.clientY };
      draft.current = { tool, color: toolColors[tool], width: 2, points: [p] };
    };
    const move = (e: PointerEvent) => {
      if (!draft.current) return;
      const p = point(e);
      if (!p) return;
      if (tool === "brush") {
        if (draft.current.points.length < 800) draft.current.points.push(p);
      } else if (!["horizontal", "vertical"].includes(tool)) draft.current.points[1] = p;
    };
    const up = (e: PointerEvent) => {
      const d = draft.current;
      const moved = press.current && Math.hypot(e.clientX - press.current.x, e.clientY - press.current.y) > 5;
      press.current = null;
      if (!d) return;
      if (d.tool === "horizontal" || d.tool === "vertical" || d.tool === "brush" || moved) {
        draft.current = null;
        if (d.tool === "horizontal" || d.tool === "vertical" || d.points.length > 1) onAdd(d);
        onDone();
      } else {
        d.points = [d.points[0]];
      }
    };
    const cancel = () => {
      draft.current = null;
      press.current = null;
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || !draft.current) return;
      cancel();
      onDone();
    };
    canvas.addEventListener("pointerdown", down);
    canvas.addEventListener("pointermove", move);
    canvas.addEventListener("pointerup", up);
    canvas.addEventListener("pointercancel", cancel);
    window.addEventListener("keydown", escape);
    return () => {
      cancelAnimationFrame(frame);
      canvas.removeEventListener("pointerdown", down);
      canvas.removeEventListener("pointermove", move);
      canvas.removeEventListener("pointerup", up);
      canvas.removeEventListener("pointercancel", cancel);
      window.removeEventListener("keydown", escape);
    };
  }, [api, tool, drawings, onAdd, onDone, lastTime, interval, candles, visible, magnet]);
  return (
    <>
      <canvas
        ref={canvasRef}
        className="rp-drawing-canvas"
        data-testid="drawing-canvas"
        aria-label="Поле рисования: нажмите и перетащите"
        style={{ pointerEvents: tool === "cursor" ? "none" : "auto" }}
      />
      <svg ref={overlayRef} className="rp-drawing-overlay" aria-hidden="true">
        <SmartMoneyOverlay output={smartMoneyOutput} active={smartMoneyActive} width={scale.width()} project={(time, value) => {
          const point = project({ time, price: value });
          return point.x === -10000 || point.y === -10000 ? null : point;
        }} />
        {tool === "cursor" && visible ? drawings.map((drawing, index) => {
          const a = project(drawing.points[0]);
          const b = project(drawing.points.at(-1)!);
          const selected = selectedIndex === index;
          const common = pointerProps(index);
          const handles = selected && !drawing.locked ? drawing.points.slice(0, drawing.tool === "brush" ? 0 : 2).map((point, handle) => {
            const p = project(point);
            return <circle key={handle} cx={p.x} cy={p.y} r="5" fill="#18231c" stroke="#a7f3d0" strokeWidth="2" {...pointerProps(index, handle)} />;
          }) : null;
          let target;
          if (drawing.tool === "horizontal") target = <line x1="0" y1={a.y} x2="100%" y2={a.y} stroke={selected ? "#a7f3d0" : "transparent"} strokeWidth="12" opacity={selected ? 0.45 : 1} {...common} />;
          else if (drawing.tool === "vertical") target = <line x1={a.x} y1="0" x2={a.x} y2="100%" stroke={selected ? "#a7f3d0" : "transparent"} strokeWidth="12" opacity={selected ? 0.45 : 1} {...common} />;
          else if (drawing.tool === "text") target = <rect x={a.x} y={a.y - 24} width={Math.max(54, (drawing.text?.length ?? 0) * 8 + 16)} height="28" fill="transparent" stroke={selected ? "#a7f3d0" : "transparent"} {...common} onDoubleClick={(event) => {
            event.stopPropagation();
            textCommitted.current = false;
            setTextDraft({ point: drawing.points[0], x: a.x, y: a.y, value: drawing.text ?? "", index });
          }} />;
          else if (drawing.tool === "brush") target = <path d={drawing.points.map((point, position) => { const p = project(point); return `${position ? "L" : "M"}${p.x} ${p.y}`; }).join(" ")} fill="none" stroke={selected ? "#a7f3d0" : "transparent"} strokeWidth="14" opacity={selected ? 0.4 : 1} {...common} />;
          else if (drawing.tool === "rectangle" || drawing.tool === "fibonacci") target = <rect x={Math.min(a.x, b.x)} y={Math.min(a.y, b.y)} width={Math.max(4, Math.abs(a.x - b.x))} height={Math.max(4, Math.abs(a.y - b.y))} fill="transparent" stroke={selected ? "#a7f3d0" : "transparent"} strokeWidth="12" {...common} />;
          else {
            const rayX = drawing.tool === "ray" ? (b.x >= a.x ? scale.width() : 0) : b.x;
            const rayY = drawing.tool === "ray" && b.x !== a.x ? a.y + (b.y - a.y) * (rayX - a.x) / (b.x - a.x) : b.y;
            target = <line x1={a.x} y1={a.y} x2={rayX} y2={rayY} stroke={selected ? "#a7f3d0" : "transparent"} strokeWidth="14" opacity={selected ? 0.4 : 1} {...common} />;
          }
          return <g key={index} data-drawing-index={index}>{target}{handles}</g>;
        }) : null}
      </svg>
      {textDraft ? <input
        autoFocus
        className="rp-text-editor"
        aria-label="Текст заметки"
        maxLength={200}
        value={textDraft.value}
        onChange={(event) => setTextDraft((current) => current ? { ...current, value: event.target.value } : current)}
        onKeyDown={(event) => {
          if (event.key === "Enter") finishText();
          else if (event.key === "Escape") { textCommitted.current = true; setTextDraft(null); onDone(); }
        }}
        onBlur={finishText}
        style={{ left: Math.max(0, textDraft.x), top: Math.max(0, textDraft.y - 28) }}
      /> : null}
    </>
  );
}
