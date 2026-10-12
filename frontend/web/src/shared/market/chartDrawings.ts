// Compatibility exports for saved chart configurations.
import { TOOLS, normalizeDrawingSets } from "../charts/drawings/model";
export { normalizeDrawingSets };
export type {
  Drawing as ChartDrawing,
  ChartPoint as DrawingPoint,
} from "../charts/drawings/model";
export type DrawingTool = string;
export type DrawingSets = Record<
  string,
  import("../charts/drawings/model").Drawing[]
>;
export const DRAWING_TOOLS = [
  { value: "cursor", label: "Курсор: выбрать и переместить" },
  ...TOOLS.map((t) => ({ value: t.id, label: t.label })),
];
export const drawingSetsSignature = (sets: DrawingSets) =>
  JSON.stringify(
    Object.keys(sets)
      .sort()
      .map((key) => [key, sets[key]]),
  );
export const cloneDrawing = (
  drawing: import("../charts/drawings/model").Drawing,
) => ({ ...drawing, points: drawing.points.map((p) => ({ ...p })) });
