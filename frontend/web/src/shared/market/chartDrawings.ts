export type DrawingTool =
  | 'cursor'
  | 'horizontal'
  | 'vertical'
  | 'trend'
  | 'ray'
  | 'rectangle'
  | 'fibonacci'
  | 'brush'
  | 'text'
  | 'ruler';

export type DrawingPoint = { time: number; price: number };

type DrawingBase = {
  id: number;
  locked?: boolean;
};

export type ChartDrawing =
  | (DrawingBase & { type: 'horizontal' | 'vertical'; points: [DrawingPoint] })
  | (DrawingBase & { type: 'trend' | 'ray' | 'rectangle' | 'fibonacci' | 'ruler'; points: [DrawingPoint, DrawingPoint] })
  | (DrawingBase & { type: 'brush'; points: DrawingPoint[] })
  | (DrawingBase & { type: 'text'; points: [DrawingPoint]; text: string });

export type DrawingSets = Record<string, ChartDrawing[]>;

export const DRAWING_TOOLS: Array<{ value: DrawingTool; label: string }> = [
  { value: 'cursor', label: 'Курсор: выбрать и переместить' },
  { value: 'horizontal', label: 'Горизонтальная линия' },
  { value: 'vertical', label: 'Вертикальная линия' },
  { value: 'trend', label: 'Трендовая линия' },
  { value: 'ray', label: 'Луч' },
  { value: 'rectangle', label: 'Прямоугольник' },
  { value: 'fibonacci', label: 'Коррекция Фибоначчи' },
  { value: 'brush', label: 'Кисть' },
  { value: 'text', label: 'Текстовая заметка' },
  { value: 'ruler', label: 'Линейка' },
];

const DRAWING_TYPES = new Set(DRAWING_TOOLS
  .map((tool) => tool.value)
  .filter((tool) => tool !== 'cursor'));

function isDrawingPoint(value: unknown): value is DrawingPoint {
  if (!value || typeof value !== 'object') return false;
  const point = value as Partial<DrawingPoint>;
  return typeof point.time === 'number'
    && Number.isFinite(point.time)
    && point.time > 0
    && typeof point.price === 'number'
    && Number.isFinite(point.price);
}

function normalizeDrawing(value: unknown): ChartDrawing | null {
  if (!value || typeof value !== 'object') return null;
  const drawing = value as Partial<ChartDrawing> & { points?: unknown };
  if (!Number.isSafeInteger(drawing.id) || Number(drawing.id) <= 0) return null;
  if (typeof drawing.type !== 'string' || !DRAWING_TYPES.has(drawing.type as Exclude<DrawingTool, 'cursor'>)) return null;
  if (!Array.isArray(drawing.points) || !drawing.points.every(isDrawingPoint)) return null;

  const base = { id: Number(drawing.id), locked: drawing.locked === true };
  if (drawing.type === 'horizontal' || drawing.type === 'vertical') {
    return drawing.points.length === 1
      ? { ...base, type: drawing.type, points: [drawing.points[0]] }
      : null;
  }
  if (drawing.type === 'text') {
    if (drawing.points.length !== 1 || typeof drawing.text !== 'string') return null;
    return { ...base, type: 'text', points: [drawing.points[0]], text: drawing.text.slice(0, 200) };
  }
  if (drawing.type === 'brush') {
    return drawing.points.length > 0 && drawing.points.length <= 800
      ? { ...base, type: 'brush', points: drawing.points }
      : null;
  }
  return drawing.points.length === 2
    ? { ...base, type: drawing.type, points: [drawing.points[0], drawing.points[1]] }
    : null;
}

export function normalizeDrawingSets(value: unknown): DrawingSets {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {};
  const result: DrawingSets = {};
  for (const [key, rawDrawings] of Object.entries(value)) {
    if (key.length === 0 || key.length > 180 || !Array.isArray(rawDrawings)) continue;
    const drawings = rawDrawings.slice(0, 100).map(normalizeDrawing).filter((item): item is ChartDrawing => item !== null);
    if (drawings.length > 0) result[key] = drawings;
  }
  return result;
}

export function drawingSetsSignature(drawingSets: DrawingSets) {
  return JSON.stringify(Object.keys(drawingSets).sort().map((key) => [key, drawingSets[key]]));
}

export function cloneDrawing(drawing: ChartDrawing): ChartDrawing {
  return { ...drawing, points: drawing.points.map((point) => ({ ...point })) } as ChartDrawing;
}
