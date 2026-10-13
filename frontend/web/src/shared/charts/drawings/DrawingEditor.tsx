import { logicalIndexAtTime } from "../seriesData";
import { useDialogFocus } from "../useDialogFocus";
import {
  useCallback,
  memo,
  useEffect,
  useRef,
  useState,
  type PointerEvent,
} from "react";
import {
  MousePointer2,
  TrendingUp,
  SlidersHorizontal,
  Shapes,
  Type,
  GitBranch,
  Ruler,
  Magnet,
  LockKeyhole,
  Eye,
  EyeOff,
  Trash2,
  Undo2,
  Redo2,
  List,
  X,
  Copy,
  Settings2,
  Download,
  Upload,
  ChevronRight,
  Eraser,
} from "lucide-react";
import {
  PriceScaleMode,
  type IChartApi,
  type ISeriesApi,
} from "lightweight-charts";
import { timestamp, type Candle } from "../engine";
import { drawingGeometry, type XY } from "./geometry";
import {
  MAX_DRAWINGS,
  TOOLS,
  normalizeDrawings,
  toolDefinition,
  type ChartPoint,
  type Drawing,
  type ToolGroup,
} from "./model";
import type { useDrawingHistory } from "./useDrawingHistory";
import "../charts.css";
export type DrawingRuntime = {
  chart: IChartApi;
  series:
    | ISeriesApi<"Candlestick">
    | ISeriesApi<"Line">
    | ISeriesApi<"Bar">
    | ISeriesApi<"Area">;
  disposed?: boolean;
};
type Props = {
  api: DrawingRuntime;
  candles: Candle[];
  interval: number;
  history: ReturnType<typeof useDrawingHistory>;
  disabled?: boolean;
  onSave?: () => void;
  cloudStatus?: string;
  onProject?: (time: number, price: number) => XY | null;
  onScaleChange?: (change: {
    autoScale?: boolean;
    log?: boolean;
    percent?: boolean;
    follow?: boolean;
  }) => void;
};
const groups: { id: ToolGroup; label: string; Icon: typeof TrendingUp }[] = [
  { id: "lines", label: "Линии и каналы", Icon: TrendingUp },
  { id: "fibonacci", label: "Фибоначчи и Ганн", Icon: SlidersHorizontal },
  { id: "shapes", label: "Фигуры и кисти", Icon: Shapes },
  { id: "text", label: "Текст и символы", Icon: Type },
  { id: "patterns", label: "Паттерны и волны", Icon: GitBranch },
  { id: "measure", label: "Позиции и измерения", Icon: Ruler },
];
const clone = (d: Drawing): Drawing => ({
  ...d,
  id: Date.now() * 1000 + Math.floor(Math.random() * 999),
  points: d.points.map((p) => ({ ...p })),
  screen: d.screen ? [...d.screen] : undefined,
});
function DrawingEditorView({
  api,
  candles,
  interval,
  history,
  disabled,
  onSave,
  cloudStatus,
  onScaleChange,
}: Props) {
  const [tool, setTool] = useState("cursor"),
    [group, setGroup] = useState<ToolGroup | null>(null),
    [selected, setSelected] = useState<number | null>(null),
    [visible, setVisible] = useState(true),
    [magnet, setMagnet] = useState(false),
    [stay, setStay] = useState(false),
    [objects, setObjects] = useState(false),
    [settings, setSettings] = useState(false),
    [draft, setDraft] = useState<ChartPoint[]>([]),
    [pointer, setPointer] = useState<ChartPoint | null>(null),
    [context, setContext] = useState<XY | null>(null),
    [editorMessage, setEditorMessage] = useState(""),
    [size, setSize] = useState({ width: 0, height: 0 });
  const [previewItems, setPreviewItems] = useState<Drawing[] | null>(null);
  const [groupPosition, setGroupPosition] = useState({ left: 44, top: 80 });
  const [, invalidate] = useState(0),
    overlay = useRef<SVGSVGElement>(null),
    file = useRef<HTMLInputElement>(null),
    frame = useRef<number | null>(null);
  const drag = useRef<{
      id: number;
      handle: number | null;
      origin: ChartPoint;
      original: Drawing;
      items: Drawing[];
      pointerId: number;
      screen?: XY;
    } | null>(null),
    brush = useRef<ChartPoint[]>([]),
    preview = useRef<Drawing[] | null>(null);
  const selection = history.items.find((d) => d.id === selected),
    def = toolDefinition(tool);
  const redraw = useCallback(() => {
    if (frame.current === null)
      frame.current = requestAnimationFrame(() => {
        frame.current = null;
        if (api.disposed) return;
        const width = api.chart.timeScale().width(),
          height = api.chart.panes()[0]?.getHeight() ?? 0;
        setSize((s) =>
          s.width === width && s.height === height ? s : { width, height },
        );
        setPreviewItems(preview.current);
        invalidate((n) => n + 1);
      });
  }, [api]);
  useEffect(() => {
    const host = overlay.current?.parentElement;
    if (!host) return;
    const resize = () => {
      const pane = api.chart.panes()[0];
      setSize({
        width: api.chart.timeScale().width(),
        height: pane?.getHeight() ?? host.clientHeight - 28,
      });
      redraw();
    };
    const ro = new ResizeObserver(resize);
    ro.observe(host);
    resize();
    api.chart.timeScale().subscribeVisibleLogicalRangeChange(redraw);
    const pane = api.chart.panes()[0]?.getHTMLElement();
    if (pane) ro.observe(pane);
    pane?.addEventListener("wheel", redraw, { passive: true });
    const pointerMove = (event: globalThis.PointerEvent) => {
      if (event.buttons) redraw();
    };
    host.addEventListener("pointermove", pointerMove, { passive: true });
    return () => {
      ro.disconnect();
      api.chart.timeScale().unsubscribeVisibleLogicalRangeChange(redraw);
      pane?.removeEventListener("wheel", redraw);
      host.removeEventListener("pointermove", pointerMove);
      if (frame.current !== null) cancelAnimationFrame(frame.current);
    };
  }, [api, redraw]);
  useEffect(redraw, [candles, redraw]);
  const project = useCallback(
    (p: ChartPoint): XY | null => {
      const scale = api.chart.timeScale();
      let x = scale.timeToCoordinate(timestamp(p.time));
      if (x === null && candles.length) {
        const index = logicalIndexAtTime(candles, p.time, interval);
        x = index === null ? null : scale.logicalToCoordinate(index as never);
      }
      const y = api.series.priceToCoordinate(p.price);
      return x !== null && y !== null ? { x, y } : null;
    },
    [api, candles, interval],
  );
  const pointAt = useCallback(
    (x: number, y: number): ChartPoint | null => {
      const logical = api.chart.timeScale().coordinateToLogical(x),
        price = api.series.coordinateToPrice(y);
      if (logical === null || price === null || !candles.length) return null;
      const index = Math.round(Number(logical)),
        nearest = candles[Math.max(0, Math.min(candles.length - 1, index))];
      let time =
          nearest.time +
          (index - Math.max(0, Math.min(candles.length - 1, index))) * interval,
        value = Number(price);
      if (!magnet) {
        const logicalIndex = Number(logical),
          left = Math.floor(logicalIndex);
        time =
          left >= 0 && left < candles.length - 1
            ? candles[left].time +
              (logicalIndex - left) *
                (candles[left + 1].time - candles[left].time)
            : nearest.time +
              (logicalIndex -
                Math.max(0, Math.min(candles.length - 1, index))) *
                interval;
        time = Math.round(time);
      }
      if (magnet && index >= 0 && index < candles.length) {
        const levels = [nearest.open, nearest.high, nearest.low, nearest.close];
        const snap = levels.reduce((a, b) =>
          Math.abs((api.series.priceToCoordinate(a) ?? 0) - y) <
          Math.abs((api.series.priceToCoordinate(b) ?? 0) - y)
            ? a
            : b,
        );
        if (Math.abs((api.series.priceToCoordinate(snap) ?? 0) - y) < 18)
          value = snap;
      }
      time = Math.max(1, time);
      return { time, price: value };
    },
    [api, candles, interval, magnet],
  );
  const xy = (event: { clientX: number; clientY: number }) => {
    const bounds = overlay.current!.getBoundingClientRect();
    return { x: event.clientX - bounds.left, y: event.clientY - bounds.top };
  };
  useEffect(() => {
    const host = overlay.current?.parentElement;
    if (!host) return;
    const handler = (e: MouseEvent) => {
      if (
        (e.target as HTMLElement).closest(
          "button,input,select,.ct-drawing-rail",
        )
      )
        return;
      e.preventDefault();
      const bounds = overlay.current!.getBoundingClientRect();
      setContext({ x: e.clientX - bounds.left, y: e.clientY - bounds.top });
    };
    host.addEventListener("contextmenu", handler);
    return () => host.removeEventListener("contextmenu", handler);
  }, []);
  const remove = useCallback(() => {
    if (selection?.locked) return;
    history.commit(history.items.filter((d) => d.id !== selected));
    setSelected(null);
    setContext(null);
  }, [history, selection, selected]);
  const patch = useCallback(
    (change: Partial<Drawing>) => {
      if (selected !== null)
        history.commit(
          history.items.map((d) =>
            d.id === selected ? { ...d, ...change } : d,
          ),
        );
    },
    [history, selected],
  );
  const choose = useCallback((id: string) => {
    setTool(id);
    setGroup(null);
    setDraft([]);
    setPointer(null);
    setSelected(null);
    setContext(null);
  }, []);
  useEffect(() => {
    api.chart.applyOptions({
      handleScroll: {
        pressedMouseMove: tool === "cursor" || disabled,
        mouseWheel: true,
        horzTouchDrag: tool === "cursor" || disabled,
        vertTouchDrag: false,
      },
    });
    return () => {
      if (!api.disposed)
        api.chart.applyOptions({
          handleScroll: { pressedMouseMove: true, horzTouchDrag: true },
        });
    };
  }, [api, tool, disabled]);
  const finish = (points: ChartPoint[]) => {
    if (history.items.length >= MAX_DRAWINGS) {
      setEditorMessage("Достигнут лимит 100 объектов");
      return;
    }
    const drawing: Drawing = {
      id: Date.now() * 1000 + Math.floor(Math.random() * 999),
      type: tool,
      points,
      color: "#61d99b",
      width: tool === "highlighter" ? 8 : 2,
      fill: true,
      text:
        def?.group === "text" &&
        ![
          "price-label",
          "arrow-marker",
          "arrow-left",
          "arrow-right",
          "arrow-up",
          "arrow-down",
        ].includes(tool)
          ? tool === "emoji"
            ? "★"
            : "Заметка"
          : undefined,
    };
    if (tool.startsWith("screen-")) {
      const p = project(points[0]);
      if (p)
        drawing.screen = [
          Math.max(0, Math.min(1, p.x / size.width)),
          Math.max(0, Math.min(1, p.y / size.height)),
        ];
    }
    if (
      [
        "regression",
        "volume-profile",
        "bars-pattern",
        "projection",
        "price-projection",
      ].includes(tool)
    ) {
      const lo = Math.min(points[0].time, points[1].time),
        hi = Math.max(points[0].time, points[1].time);
      const sample = candles.filter((c) => c.time >= lo && c.time <= hi);
      if (sample.length < 2 || sample.length > 500) {
        setEditorMessage("Выберите диапазон от 2 до 500 загруженных свечей");
        setDraft([]);
        setPointer(null);
        return;
      }
      drawing.sample = sample;
    }
    setEditorMessage("");
    history.commit([...history.items, drawing]);
    setSelected(drawing.id);
    setDraft([]);
    setPointer(null);
    brush.current = [];
    if (!stay) setTool("cursor");
    if (def?.group === "text") setSettings(true);
  };
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (
        !overlay.current
          ?.closest("[data-chart-surface]")
          ?.contains(document.activeElement)
      )
        return;
      if (
        (e.target as HTMLElement)?.closest(
          "input,textarea,select,[contenteditable=true]",
        )
      )
        return;
      if (e.key === "Escape") {
        choose("cursor");
        setSettings(false);
        setContext(null);
        setObjects(false);
      } else if (e.key === "Delete" || e.key === "Backspace") {
        e.preventDefault();
        remove();
      } else if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "z") {
        e.preventDefault();
        if (e.shiftKey) history.redo();
        else history.undo();
      } else if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "y") {
        e.preventDefault();
        history.redo();
      } else if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") {
        e.preventDefault();
        onSave?.();
      } else if (e.altKey) {
        const next = TOOLS.find((t) => t.shortcut === e.key.toUpperCase());
        if (next) {
          e.preventDefault();
          choose(next.id);
        }
      } else if (e.key === "Enter" && draft.length >= 2 && def?.points === -1) {
        e.preventDefault();
        finish(draft);
      }
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  });
  const down = (e: PointerEvent<SVGSVGElement>) => {
    if (disabled || e.button !== 0 || tool === "cursor") return;
    e.preventDefault();
    const v = xy(e),
      p = pointAt(v.x, v.y);
    if (!p || v.y > size.height || v.x > size.width) return;
    if (def?.points === 0) {
      brush.current = [p];
      setDraft([p]);
      e.currentTarget.setPointerCapture(e.pointerId);
    } else {
      const next = [...draft, p];
      if (def && def.points > 0 && next.length === def.points) finish(next);
      else setDraft(next);
    }
  };
  const move = (e: PointerEvent<SVGSVGElement>) => {
    const v = xy(e),
      p = pointAt(v.x, v.y);
    if (!p) return;
    if (drag.current) {
      const d = drag.current;
      if (d.original.screen) {
        const screen = d.screen!;
        const anchor = d.original.screen;
        preview.current = d.items.map((item) =>
          item.id === d.id
            ? {
                ...d.original,
                screen: [
                  Math.max(
                    0,
                    Math.min(1, anchor[0] + (v.x - screen.x) / size.width),
                  ),
                  Math.max(
                    0,
                    Math.min(1, anchor[1] + (v.y - screen.y) / size.height),
                  ),
                ],
              }
            : item,
        );
      } else {
        const dt = p.time - d.origin.time,
          dp = p.price - d.origin.price;
        preview.current = d.items.map((item) =>
          item.id === d.id
            ? {
                ...d.original,
                points: d.original.points.map((point, i) =>
                  d.handle === null
                    ? {
                        time: Math.max(1, point.time + dt),
                        price: point.price + dp,
                      }
                    : i === d.handle
                      ? p
                      : point,
                ),
              }
            : item,
        );
      }
      redraw();
    } else if (brush.current.length) {
      const last = brush.current.at(-1)!;
      if (last.time !== p.time || Math.abs(last.price - p.price) > 1e-10) {
        if (brush.current.length < 800) brush.current.push(p);
        setDraft([...brush.current]);
      }
    } else if (tool !== "cursor") setPointer(p);
  };
  const up = (e: PointerEvent<SVGSVGElement>) => {
    if (drag.current) {
      if (preview.current) history.commit(preview.current);
      drag.current = null;
      preview.current = null;
      api.chart.applyOptions({
        handleScroll: {
          pressedMouseMove: tool === "cursor",
          horzTouchDrag: tool === "cursor",
        },
      });
      redraw();
    } else if (brush.current.length) finish([...brush.current]);
    if (e.currentTarget.hasPointerCapture(e.pointerId))
      e.currentTarget.releasePointerCapture(e.pointerId);
  };
  const startDrag = (
    e: PointerEvent<SVGElement>,
    d: Drawing,
    handle: number | null,
  ) => {
    if (disabled || e.button !== 0) return;
    if (tool === "eraser") {
      e.preventDefault();
      e.stopPropagation();
      if (!d.locked)
        history.commit(history.items.filter((item) => item.id !== d.id));
      return;
    }
    if (tool !== "cursor") return;
    e.preventDefault();
    e.stopPropagation();
    setSelected(d.id);
    setContext(null);
    if (d.locked) return;
    const v = xy(e),
      p = pointAt(v.x, v.y);
    if (!p) return;
    drag.current = {
      id: d.id,
      handle,
      origin: p,
      original: d,
      items: history.items,
      pointerId: e.pointerId,
      screen: v,
    };
    overlay.current?.setPointerCapture(e.pointerId);
    api.chart.applyOptions({
      handleScroll: { pressedMouseMove: false, horzTouchDrag: false },
    });
  };
  const all = previewItems ?? history.items,
    ghost: Drawing | null = draft.length
      ? {
          id: -1,
          type: tool,
          points: [
            ...draft,
            ...(pointer && def?.points !== 0 ? [pointer] : []),
          ],
          color: "#61d99b",
          width: 2,
        }
      : null;
  const exportJSON = () => {
    const blob = new Blob(
        [JSON.stringify({ version: 2, drawings: history.items }, null, 2)],
        { type: "application/json" },
      ),
      url = URL.createObjectURL(blob),
      a = document.createElement("a");
    a.href = url;
    a.download = "shortlong-drawings.json";
    a.click();
    URL.revokeObjectURL(url);
  };
  return (
    <>
      <aside className="ct-drawing-rail" aria-label="Инструменты рисования">
        <button
          title="Курсор · Esc"
          aria-label="Курсор"
          aria-pressed={tool === "cursor"}
          onClick={() => choose("cursor")}
        >
          <MousePointer2 size={18} />
        </button>
        <button
          title="Ластик · нажмите на объект"
          aria-label="Ластик"
          aria-pressed={tool === "eraser"}
          disabled={disabled}
          onClick={() => choose("eraser")}
        >
          <Eraser size={18} />
        </button>
        {groups.map(({ id, label, Icon }) => (
          <div className="ct-tool-group" key={id}>
            <button
              title={label}
              aria-label={label}
              aria-expanded={group === id}
              aria-pressed={def?.group === id}
              disabled={disabled}
              onClick={(event) => {
                const bounds = event.currentTarget.getBoundingClientRect();
                setGroupPosition({
                  left: Math.min(
                    window.innerWidth - 300,
                    Math.max(4, bounds.right + 4),
                  ),
                  top: Math.max(
                    4,
                    Math.min(bounds.top, window.innerHeight - 320),
                  ),
                });
                setGroup(group === id ? null : id);
              }}
            >
              <Icon size={18} />
              <ChevronRight size={9} className="ct-tool-chevron" />
            </button>
            {group === id && (
              <div
                className="ct-tool-menu"
                role="menu"
                style={{
                  position: "fixed",
                  left: groupPosition.left,
                  top: groupPosition.top,
                }}
              >
                <header>
                  {label}
                  <button
                    aria-label="Закрыть инструменты"
                    onClick={() => setGroup(null)}
                  >
                    <X size={14} />
                  </button>
                </header>
                {TOOLS.filter((t) => t.group === id).map((t) => (
                  <button
                    role="menuitem"
                    key={t.id}
                    onClick={() => choose(t.id)}
                  >
                    <span>{t.label}</span>
                    <kbd>
                      {t.shortcut
                        ? `Alt+${t.shortcut}`
                        : t.points > 1
                          ? `${t.points} точки`
                          : t.points === -1
                            ? "Enter"
                            : ""}
                    </kbd>
                  </button>
                ))}
              </div>
            )}
          </div>
        ))}
        <hr />
        <button
          title="Магнит к OHLC"
          aria-label="Магнит к OHLC"
          aria-pressed={magnet}
          onClick={() => setMagnet(!magnet)}
        >
          <Magnet size={18} />
        </button>
        <button
          title="Продолжать рисование"
          aria-label="Продолжать рисование"
          aria-pressed={stay}
          onClick={() => setStay(!stay)}
        >
          <TrendingUp size={18} />
        </button>
        <button
          title="Видимость разметки"
          aria-label="Видимость разметки"
          aria-pressed={!visible}
          onClick={() => setVisible(!visible)}
        >
          {visible ? <Eye size={18} /> : <EyeOff size={18} />}
        </button>
        <button
          title="Список объектов"
          aria-label="Список объектов"
          aria-pressed={objects}
          onClick={() => setObjects(!objects)}
        >
          <List size={18} />
        </button>
        <hr />
        <button
          title="Отменить · Ctrl+Z"
          aria-label="Отменить"
          disabled={!history.canUndo}
          onClick={history.undo}
        >
          <Undo2 size={18} />
        </button>
        <button
          title="Повторить · Ctrl+Shift+Z"
          aria-label="Повторить"
          disabled={!history.canRedo}
          onClick={history.redo}
        >
          <Redo2 size={18} />
        </button>
        <button
          title="Удалить выбранный объект · Delete"
          aria-label="Удалить выбранный объект"
          disabled={!selection || selection.locked}
          onClick={remove}
        >
          <Trash2 size={18} />
        </button>
      </aside>
      <svg
        ref={overlay}
        className="ct-drawing-overlay"
        width="100%"
        height={size.height}
        style={{
          pointerEvents:
            tool === "cursor" || tool === "eraser" || disabled
              ? "none"
              : "auto",
          touchAction: tool === "cursor" ? "auto" : "none",
        }}
        onPointerDown={down}
        onPointerMove={move}
        onPointerUp={up}
        onPointerCancel={(e) => {
          api.chart.applyOptions({
            handleScroll: {
              pressedMouseMove: tool === "cursor",
              horzTouchDrag: tool === "cursor",
            },
          });
          drag.current = null;
          preview.current = null;
          brush.current = [];
          setDraft([]);
          up(e);
        }}
        onDoubleClick={() => {
          if (def?.points === -1 && draft.length >= 2) finish(draft);
        }}
        onContextMenu={(e) => {
          e.preventDefault();
          setContext(xy(e));
        }}
        aria-label="Разметка графика"
      >
        {[
          ...(visible ? all.filter((d) => !d.hidden) : []),
          ...(ghost ? [ghost] : []),
        ].map((d) => {
          const geo = drawingGeometry(d, project, size.width, size.height),
            active = d.id === selected;
          return (
            <g
              key={d.id}
              data-drawing-id={d.id}
              data-drawing-type={d.type}
              stroke={d.color ?? "#61d99b"}
              strokeWidth={d.width ?? 2}
              strokeDasharray={
                d.dash === "dashed"
                  ? "7 4"
                  : d.dash === "dotted"
                    ? "2 4"
                    : undefined
              }
            >
              {geo.shapes.map((s, i) => {
                const props = {
                  onPointerDown: (e: PointerEvent<SVGElement>) =>
                    startDrag(e, d, null),
                  onDoubleClick: () => {
                    setSelected(d.id);
                    setSettings(true);
                  },
                  onContextMenu: (
                    e: import("react").MouseEvent<SVGElement>,
                  ) => {
                    e.preventDefault();
                    setSelected(d.id);
                    setContext(xy(e));
                  },
                  style: {
                    pointerEvents: (d.id < 0 ||
                    (tool !== "cursor" && tool !== "eraser")
                      ? "none"
                      : "all") as "none" | "all",
                    cursor: d.locked ? "pointer" : "move",
                  },
                };
                return s.kind === "text" ? (
                  <text
                    key={i}
                    {...props}
                    x={s.x}
                    y={s.y}
                    fill={s.color ?? d.color ?? "#61d99b"}
                    stroke="none"
                    fontSize={12}
                    style={{
                      ...props.style,
                      paintOrder: "stroke",
                      filter: "drop-shadow(0 1px 1px var(--card))",
                    }}
                  >
                    {s.text}
                  </text>
                ) : s.kind === "ellipse" ? (
                  <g key={i}>
                    <ellipse
                      cx={s.x}
                      cy={s.y}
                      rx={s.rx}
                      ry={s.ry}
                      fill={s.fill ? (d.color ?? "#61d99b") : "none"}
                      fillOpacity={0.1}
                    />
                    <ellipse
                      {...props}
                      cx={s.x}
                      cy={s.y}
                      rx={s.rx}
                      ry={s.ry}
                      fill="transparent"
                      stroke="transparent"
                      strokeWidth={12}
                    />
                  </g>
                ) : (
                  <g key={i}>
                    <path
                      d={s.path}
                      fill={s.fill ? (s.color ?? d.color ?? "#61d99b") : "none"}
                      fillOpacity={0.12}
                      stroke={s.color}
                      opacity={s.opacity}
                    />
                    <path
                      {...props}
                      d={s.path}
                      fill={s.fill ? "transparent" : "none"}
                      stroke="transparent"
                      strokeWidth={12}
                    />
                  </g>
                );
              })}
              {active &&
                !d.locked &&
                geo.handles.map((p, i) => (
                  <circle
                    key={i}
                    cx={p.x}
                    cy={p.y}
                    r={5}
                    fill="var(--card)"
                    stroke="var(--primary)"
                    strokeWidth={2}
                    style={{ pointerEvents: "all", cursor: "grab" }}
                    onPointerDown={(e) => startDrag(e, d, i)}
                  />
                ))}
            </g>
          );
        })}
      </svg>
      {editorMessage && (
        <div className="ct-drawing-hint" role="alert">
          {editorMessage}
        </div>
      )}
      {tool !== "cursor" &&
        tool !== "eraser" &&
        !disabled &&
        !editorMessage && (
          <div className="ct-drawing-hint">
            {def?.label} ·{" "}
            {def?.points === 0
              ? "Проведите по графику"
              : def?.points === -1
                ? "Добавьте точки, Enter — закончить"
                : `${draft.length}/${def?.points} точек`}{" "}
            · Esc — отмена
            {history.items.length >= MAX_DRAWINGS
              ? " · Достигнут лимит 100 объектов"
              : ""}
          </div>
        )}
      {selection && (
        <div className="ct-object-toolbar">
          <input
            aria-label="Цвет объекта"
            type="color"
            value={selection.color ?? "#61d99b"}
            disabled={selection.locked}
            onChange={(e) => patch({ color: e.target.value })}
          />
          <select
            aria-label="Толщина объекта"
            value={selection.width ?? 2}
            disabled={selection.locked}
            onChange={(e) => patch({ width: Number(e.target.value) })}
          >
            {[1, 2, 3, 4, 6, 8].map((n) => (
              <option key={n} value={n}>
                {n}px
              </option>
            ))}
          </select>
          <button
            aria-label="Настройки объекта"
            onClick={() => setSettings(true)}
          >
            <Settings2 size={15} />
          </button>
          <button
            aria-label="Блокировка объекта"
            aria-pressed={selection.locked === true}
            onClick={() => patch({ locked: !selection.locked })}
          >
            <LockKeyhole size={15} />
          </button>
          <button
            aria-label="Копировать объект"
            disabled={history.items.length >= MAX_DRAWINGS}
            onClick={() => {
              const copy = clone(selection);
              history.commit([...history.items, copy]);
              setSelected(copy.id);
            }}
          >
            <Copy size={15} />
          </button>
          <button
            aria-label="Удалить объект"
            disabled={selection.locked}
            onClick={remove}
          >
            <Trash2 size={15} />
          </button>
          <button
            aria-label="Снять выделение"
            onClick={() => setSelected(null)}
          >
            <X size={15} />
          </button>
        </div>
      )}
      {objects && (
        <aside className="ct-object-list">
          <header>
            Объекты · {history.items.length}
            <button
              aria-label="Закрыть список"
              onClick={() => setObjects(false)}
            >
              <X size={16} />
            </button>
          </header>
          <p>{cloudStatus ?? "Разметка сохраняется в браузере"}</p>
          <div className="ct-object-actions">
            <button onClick={exportJSON}>
              <Download size={14} /> JSON
            </button>
            <button onClick={() => file.current?.click()}>
              <Upload size={14} /> Импорт
            </button>
            <button
              disabled={!history.items.some((d) => !d.locked)}
              onClick={() =>
                history.commit(history.items.filter((d) => d.locked))
              }
            >
              <Trash2 size={14} /> Очистить
            </button>
          </div>
          {history.items.map((d) => (
            <div className="ct-object-row" key={d.id}>
              <button
                onClick={() => {
                  setSelected(d.id);
                  setSettings(true);
                }}
              >
                {toolDefinition(d.type)?.label}
              </button>
              <button
                aria-label="Скрыть объект"
                aria-pressed={d.hidden}
                onClick={() =>
                  history.commit(
                    history.items.map((x) =>
                      x.id === d.id ? { ...x, hidden: !x.hidden } : x,
                    ),
                  )
                }
              >
                {d.hidden ? <EyeOff size={14} /> : <Eye size={14} />}
              </button>
              <button
                aria-label="Заблокировать объект"
                aria-pressed={d.locked}
                onClick={() =>
                  history.commit(
                    history.items.map((x) =>
                      x.id === d.id ? { ...x, locked: !x.locked } : x,
                    ),
                  )
                }
              >
                <LockKeyhole size={14} />
              </button>
            </div>
          ))}
        </aside>
      )}
      <input
        ref={file}
        type="file"
        accept="application/json,.json"
        hidden
        onChange={async (e) => {
          const f = e.target.files?.[0];
          if (!f) return;
          if (f.size > 5 * 1024 * 1024) {
            setEditorMessage("Файл превышает 5 МБ");
            return;
          }
          try {
            const raw = JSON.parse(await f.text());
            const items = normalizeDrawings(raw.drawings);
            if (items.length) history.commit(items.map(clone));
            else setEditorMessage("Файл не содержит допустимых объектов");
          } catch {
            setEditorMessage(
              "Не удалось импортировать разметку: неверный JSON",
            );
          }
          e.target.value = "";
        }}
      />
      {context && (
        <div
          className="ct-context"
          style={{
            left: Math.max(4, Math.min(context.x, size.width - 180)),
            top: Math.max(4, Math.min(context.y, size.height - 180)),
          }}
        >
          <button
            onClick={() => {
              setSettings(true);
              setContext(null);
            }}
            disabled={!selection}
          >
            Настройки объекта
          </button>
          <button disabled={!selection || selection.locked} onClick={remove}>
            Удалить объект
          </button>
          <button
            onClick={() => {
              api.chart.timeScale().fitContent();
              api.chart.priceScale("right").applyOptions({ autoScale: true });
              setContext(null);
            }}
          >
            Сбросить масштаб
          </button>
          <button
            onClick={() => {
              api.chart.timeScale().scrollToRealTime();
              onScaleChange?.({ follow: true });
              setContext(null);
            }}
          >
            К последней свече
          </button>
          <button
            onClick={() => {
              api.chart.priceScale("right").applyOptions({ autoScale: true });
              onScaleChange?.({ autoScale: true });
              setContext(null);
            }}
          >
            Автоматический масштаб цены
          </button>
          <button
            onClick={() => {
              const log =
                api.chart.priceScale("right").options().mode !==
                PriceScaleMode.Logarithmic;
              api.chart.priceScale("right").applyOptions({
                mode: log ? PriceScaleMode.Logarithmic : PriceScaleMode.Normal,
              });
              onScaleChange?.({ log, percent: false });
              setContext(null);
            }}
          >
            Логарифмическая шкала
          </button>
          <button
            onClick={() => {
              const percent =
                api.chart.priceScale("right").options().mode !==
                PriceScaleMode.Percentage;
              api.chart.priceScale("right").applyOptions({
                mode: percent
                  ? PriceScaleMode.Percentage
                  : PriceScaleMode.Normal,
              });
              onScaleChange?.({ percent, log: false });
              setContext(null);
            }}
          >
            Процентная шкала
          </button>
          <button onClick={() => setContext(null)}>Закрыть</button>
        </div>
      )}
      {settings && selection && (
        <DrawingSettings
          key={selection.id}
          drawing={selection}
          onClose={() => setSettings(false)}
          onApply={patch}
        />
      )}
    </>
  );
}
export const DrawingEditor = memo(DrawingEditorView);
function DrawingSettings({
  drawing,
  onClose,
  onApply,
}: {
  drawing: Drawing;
  onClose: () => void;
  onApply: (p: Partial<Drawing>) => void;
}) {
  const [value, setValue] = useState(drawing),
    [error, setError] = useState("");
  const first = useRef<HTMLInputElement>(null);
  useDialogFocus(true, onClose);
  useEffect(() => {
    first.current?.focus();
  }, []);
  return (
    <div className="ct-modal-backdrop" onClick={onClose}>
      <form
        className="ct-dialog"
        role="dialog"
        aria-modal="true"
        aria-label="Настройки объекта"
        onClick={(e) => e.stopPropagation()}
        onSubmit={(e) => {
          e.preventDefault();
          if (
            !value.points.every(
              (p) =>
                Number.isFinite(p.price) &&
                Number.isFinite(p.time) &&
                p.time > 0,
            )
          ) {
            setError("Укажите корректные координаты");
            return;
          }
          onApply(value);
          onClose();
        }}
      >
        <header>
          {toolDefinition(drawing.type)?.label}
          <button
            type="button"
            aria-label="Закрыть настройки"
            onClick={onClose}
          >
            <X size={18} />
          </button>
        </header>
        {toolDefinition(drawing.type)?.group === "text" && (
          <label>
            Текст / символ
            <input
              ref={first}
              maxLength={200}
              disabled={drawing.locked}
              value={value.text ?? ""}
              onChange={(e) => setValue({ ...value, text: e.target.value })}
            />
          </label>
        )}
        <div className="ct-form-row">
          <label>
            Цвет
            <input
              type="color"
              disabled={drawing.locked}
              value={value.color ?? "#61d99b"}
              onChange={(e) => setValue({ ...value, color: e.target.value })}
            />
          </label>
          <label>
            Линия
            <select
              disabled={drawing.locked}
              value={value.dash ?? "solid"}
              onChange={(e) =>
                setValue({ ...value, dash: e.target.value as Drawing["dash"] })
              }
            >
              <option value="solid">Сплошная</option>
              <option value="dashed">Штриховая</option>
              <option value="dotted">Пунктир</option>
            </select>
          </label>
          <label>
            <input
              type="checkbox"
              disabled={drawing.locked}
              checked={value.fill !== false}
              onChange={(e) => setValue({ ...value, fill: e.target.checked })}
            />{" "}
            Заливка
          </label>
        </div>
        {["long", "short"].includes(drawing.type) && (
          <label>
            Количество актива
            <input
              type="number"
              min="0.00000001"
              step="any"
              required
              value={value.quantity ?? 1}
              disabled={drawing.locked}
              onChange={(e) =>
                setValue({ ...value, quantity: Number(e.target.value) })
              }
            />
          </label>
        )}
        {drawing.type.includes("fib") && (
          <label>
            Уровни (через запятую)
            <input
              disabled={drawing.locked}
              defaultValue={(
                drawing.levels ?? [0, 0.236, 0.382, 0.5, 0.618, 0.786, 1]
              ).join(", ")}
              onBlur={(e) => {
                const levels = e.target.value.split(",").map(Number);
                if (
                  levels.length <= 20 &&
                  levels.every((v) => Number.isFinite(v) && Math.abs(v) <= 100)
                )
                  setValue({ ...value, levels });
                else setError("До 20 конечных уровней от −100 до 100");
              }}
            />
          </label>
        )}
        {value.screen ? (
          <div className="ct-form-row">
            {["X, % экрана", "Y, % экрана"].map((label, i) => (
              <label key={label}>
                {label}
                <input
                  type="number"
                  min={0}
                  max={100}
                  step="any"
                  disabled={drawing.locked}
                  value={value.screen![i] * 100}
                  onChange={(e) => {
                    const n = Number(e.target.value);
                    if (Number.isFinite(n) && n >= 0 && n <= 100) {
                      const screen: [number, number] = [...value.screen!];
                      screen[i] = n / 100;
                      setValue({ ...value, screen });
                    }
                  }}
                />
              </label>
            ))}
          </div>
        ) : (
          <details>
            <summary>Координаты · UTC</summary>
            <div className="ct-coordinates">
              {value.points.map((p, i) => (
                <div className="ct-form-row" key={i}>
                  <label>
                    Точка {i + 1}
                    <input
                      type="datetime-local"
                      step={1}
                      disabled={drawing.locked}
                      value={new Date(p.time * 1000).toISOString().slice(0, 19)}
                      onChange={(e) => {
                        const time = Date.parse(e.target.value + "Z") / 1000;
                        if (Number.isFinite(time))
                          setValue({
                            ...value,
                            points: value.points.map((p, j) =>
                              j === i ? { ...p, time } : p,
                            ),
                          });
                      }}
                    />
                  </label>
                  <label>
                    Цена
                    <input
                      type="number"
                      step="any"
                      required
                      disabled={drawing.locked}
                      value={p.price}
                      onChange={(e) =>
                        setValue({
                          ...value,
                          points: value.points.map((p, j) =>
                            j === i
                              ? { ...p, price: Number(e.target.value) }
                              : p,
                          ),
                        })
                      }
                    />
                  </label>
                </div>
              ))}
            </div>
          </details>
        )}

        {error && <p role="alert">{error}</p>}
        <footer>
          <button type="button" onClick={onClose}>
            Отмена
          </button>
          <button
            className="ct-primary"
            type="submit"
            disabled={drawing.locked}
          >
            Применить
          </button>
        </footer>
      </form>
    </div>
  );
}
