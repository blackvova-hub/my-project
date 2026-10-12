export type ChartPoint = { time: number; price: number };
export type ToolGroup =
  | "lines"
  | "fibonacci"
  | "shapes"
  | "text"
  | "patterns"
  | "measure";
export type ToolDefinition = {
  id: string;
  label: string;
  group: ToolGroup;
  points: number;
  shortcut?: string;
  labels?: string[];
};
const tools = (
  group: ToolGroup,
  rows: [string, string, number, string?][],
): ToolDefinition[] =>
  rows.map(([id, label, points, shortcut]) => ({
    id,
    label,
    points,
    group,
    shortcut,
  }));
export const TOOLS: ToolDefinition[] = [
  ...tools("lines", [
    ["trend", "Линия тренда", 2, "T"],
    ["arrow", "Стрелка", 2],
    ["ray", "Луч", 2],
    ["extended", "Удлинённая линия", 2],
    ["angle", "Угол тренда", 2],
    ["horizontal", "Горизонтальная линия", 1, "H"],
    ["horizontal-ray", "Горизонтальный луч", 1, "J"],
    ["vertical", "Вертикальная линия", 1, "V"],
    ["cross", "Перекрещенные линии", 1, "C"],
    ["channel", "Параллельный канал", 3],
    ["regression", "Регрессионный канал", 2],
    ["flat-channel", "Плоский канал", 2],
    ["disjoint-channel", "Расходящийся канал", 4],
  ]),
  ...tools("fibonacci", [
    ["fibonacci", "Коррекция Фибоначчи", 2, "F"],
    ["fib-extension", "Расширение Фибоначчи", 3],
    ["pitchfork", "Вилы Эндрюса", 3],
    ["schiff", "Вилы Шиффа", 3],
    ["modified-schiff", "Модифицированные вилы Шиффа", 3],
    ["inside-pitchfork", "Вилы внутри", 3],
    ["fib-channel", "Канал Фибоначчи", 3],
    ["fib-time", "Временные периоды Фибоначчи", 2],
    ["fib-trend-time", "Периоды Фибоначчи на основе тренда", 3],
    ["gann-box", "Коробка Ганна", 2],
    ["gann-square", "Квадрат Ганна", 2],
    ["gann-fan", "Веер Ганна", 2],
    ["fib-fan", "Веер Фибоначчи", 2],
  ]),
  ...tools("shapes", [
    ["brush", "Кисть", 0],
    ["highlighter", "Маркер", 0],
    ["rectangle", "Прямоугольник", 2],
    ["circle", "Окружность", 2],
    ["ellipse", "Эллипс", 2],
    ["path", "Траектория", -1],
    ["curve", "Кривая Безье", 3],
    ["polyline", "Ломаная линия", -1],
    ["triangle", "Треугольник", 3],
    ["rotated-rectangle", "Вращающийся прямоугольник", 3],
    ["arc", "Дуга", 3],
    ["double-curve", "Двойная кривая", 4],
  ]),
  ...tools("text", [
    ["text", "Текст", 1],
    ["screen-text", "Текст на экране", 1],
    ["note", "Заметка", 1],
    ["screen-note", "Заметка на экране", 1],
    ["callout", "Сноска", 2],
    ["comment", "Комментарий", 1],
    ["price-label", "Метка цены", 1],
    ["price-note", "Ценовая заметка", 1],
    ["arrow-marker", "Стрелка-указатель", 1],
    ["arrow-left", "Стрелка влево", 1],
    ["arrow-right", "Стрелка вправо", 1],
    ["arrow-up", "Стрелка вверх", 1],
    ["arrow-down", "Стрелка вниз", 1],
    ["emoji", "Эмодзи / символ", 1],
  ]),
  ...tools("patterns", [
    ["xabcd", "Шаблон XABCD", 5],
    ["cypher", "Паттерн Cypher", 5],
    ["abcd", "Шаблон ABCD", 4],
    ["triangle-pattern", "Треугольник · паттерн", 4],
    ["three-drives", "Паттерн трёх движений", 7],
    ["head-shoulders", "Голова и плечи", 7],
    ["elliott-impulse", "Импульсная волна Эллиотта (12345)", 6],
    ["elliott-triangle", "Волна Эллиотта (ABCDE)", 6],
    ["elliott-triple", "Тройная комбинация (WXYXZ)", 6],
    ["elliott-correction", "Коррекционная волна (ABC)", 4],
    ["elliott-double", "Двойная комбинация (WXY)", 4],
    ["cycles", "Разделение циклов", 2],
    ["time-cycles", "Временные циклы", 2],
    ["sine", "Синусоида", 2],
  ]),
  ...tools("measure", [
    ["long", "Длинная позиция", 3],
    ["short", "Короткая позиция", 3],
    ["forecast", "Прогноз", 2],
    ["date-range", "Диапазон дат", 2],
    ["price-range", "Диапазон цен", 2],
    ["ruler", "Диапазон цены и времени", 2],
    ["bars-pattern", "Шаблон баров", 3],
    ["price-projection", "Проекция цены", 3],
    ["projection", "Проекция", 3],
    ["volume-profile", "Профиль объёма по свечам", 2],
  ]),
];
export type ToolId = string;
export type Drawing = {
  id: number;
  type: ToolId;
  points: ChartPoint[];
  color?: string;
  width?: number;
  dash?: "solid" | "dashed" | "dotted";
  fill?: boolean;
  text?: string;
  locked?: boolean;
  hidden?: boolean;
  screen?: [number, number];
  levels?: number[];
  quantity?: number;
  sample?: {
    time: number;
    open: number;
    high: number;
    low: number;
    close: number;
    volume: number;
  }[];
};
export const toolDefinition = (id: string) =>
  TOOLS.find((tool) => tool.id === id);
export const DEFAULT_LEVELS = [0, 0.236, 0.382, 0.5, 0.618, 0.786, 1];
export const MAX_DRAWINGS = 100;
export function normalizeDrawing(value: unknown): Drawing | null {
  if (!value || typeof value !== "object") return null;
  const d = value as Drawing,
    def = toolDefinition(d.type);
  if (
    !def ||
    !Number.isSafeInteger(d.id) ||
    d.id <= 0 ||
    !Array.isArray(d.points) ||
    !d.points.length ||
    d.points.length > 800
  )
    return null;
  if (
    (def.points > 0 && d.points.length !== def.points) ||
    (def.points < 0 && d.points.length < 2)
  )
    return null;
  if (
    !d.points.every(
      (p) =>
        Number.isFinite(p.time) &&
        p.time > 0 &&
        p.time <= 8640000000000 &&
        Number.isFinite(p.price),
    )
  )
    return null;
  return {
    id: d.id,
    type: d.type,
    points: d.points.map((p) => ({ time: p.time, price: p.price })),
    color: /^#[\da-f]{6}$/i.test(d.color ?? "") ? d.color : "#61d99b",
    width: Math.max(1, Math.min(8, Math.round(Number(d.width) || 2))),
    dash: ["solid", "dashed", "dotted"].includes(d.dash ?? "")
      ? d.dash
      : "solid",
    fill: d.fill !== false,
    locked: d.locked === true,
    hidden: d.hidden === true,
    text: typeof d.text === "string" ? d.text.slice(0, 200) : undefined,
    screen:
      Array.isArray(d.screen) &&
      d.screen.length === 2 &&
      d.screen.every((n) => Number.isFinite(n) && n >= 0 && n <= 1)
        ? d.screen
        : undefined,
    levels:
      Array.isArray(d.levels) &&
      d.levels.length <= 20 &&
      d.levels.every((n) => Number.isFinite(n) && Math.abs(n) <= 100)
        ? d.levels
        : undefined,
    quantity:
      Number.isFinite(d.quantity) && Number(d.quantity) > 0 ? d.quantity : 1,
    sample: Array.isArray(d.sample)
      ? d.sample
          .slice(0, 500)
          .filter((c) =>
            [c.time, c.open, c.high, c.low, c.close, c.volume].every(
              Number.isFinite,
            ),
          )
      : undefined,
  };
}
export function normalizeDrawings(value: unknown): Drawing[] {
  return Array.isArray(value)
    ? value
        .slice(0, MAX_DRAWINGS)
        .map(normalizeDrawing)
        .filter((d): d is Drawing => d !== null)
    : [];
}
export function normalizeDrawingSets(
  value: unknown,
): Record<string, Drawing[]> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  return Object.fromEntries(
    Object.entries(value)
      .slice(0, 500)
      .filter(([key]) => key.length > 0 && key.length <= 180)
      .map(([key, items]) => [key, normalizeDrawings(items)]),
  );
}
export function positionMetrics(d: Drawing) {
  const [entry, target, stop] = d.points.map((p) => p.price),
    side = d.type === "short" ? -1 : 1;
  const reward = (target - entry) * side,
    risk = (entry - stop) * side;
  return {
    reward,
    risk,
    valid: reward > 0 && risk > 0,
    ratio: risk > 0 ? reward / risk : null,
    profit: reward * (d.quantity ?? 1),
    loss: risk * (d.quantity ?? 1),
  };
}
