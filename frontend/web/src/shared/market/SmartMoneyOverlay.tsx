import type { SmartMoneyIndicatorId } from "./indicatorTypes";
import type {
  SmartMoneyDirection,
  SmartMoneyLevel,
  SmartMoneyOutput,
  SmartMoneyZone,
} from "./smartMoneyEngine";

type Coordinate = { x: number; y: number };
type LabelBounds = { left: number; right: number; top: number; bottom: number };
type LevelLayout = {
  level: SmartMoneyLevel;
  startX: number;
  endX: number;
  y: number;
  color: string;
  labelX: number;
  showLabel: boolean;
};
type MarkerLayout = {
  marker: SmartMoneyOutput["markers"][number];
  point: Coordinate;
  color: string;
  textY: number;
  triangle: string;
  showLabel: boolean;
};

const LEVEL_LABEL_FONT_SIZE = 8.5;
const LEVEL_LABEL_SIDE_SPACE = 18;
const LABEL_COLLISION_GAP = 3;

const LEVEL_COLORS: Record<SmartMoneyIndicatorId, string> = {
  swing_points: "#cbd5e1",
  bos: "#4ade80",
  choch: "#fbbf24",
  mss: "#c4b5fd",
  order_blocks: "#2dd4bf",
  fvg: "#38bdf8",
  liquidity: "#fde047",
  equal_high_low: "#fb923c",
  liquidity_sweeps: "#f472b6",
};

function zoneColors(zone: SmartMoneyZone) {
  if (zone.indicator === "fvg") {
    return zone.direction === "bullish"
      ? { stroke: "#38bdf8", fill: "rgba(56,189,248,.115)", text: "#bae6fd" }
      : { stroke: "#a78bfa", fill: "rgba(139,92,246,.12)", text: "#ddd6fe" };
  }
  return zone.direction === "bullish"
    ? { stroke: "#2dd4bf", fill: "rgba(20,184,166,.13)", text: "#99f6e4" }
    : { stroke: "#fb7185", fill: "rgba(244,63,94,.12)", text: "#fecdd3" };
}

function levelColor(level: SmartMoneyLevel) {
  if (level.indicator === "liquidity") return level.direction === "bearish" ? "#fde047" : "#22d3ee";
  if (level.indicator === "equal_high_low") return level.direction === "bearish" ? "#fb923c" : "#60a5fa";
  return LEVEL_COLORS[level.indicator];
}

function markerColor(indicator: SmartMoneyIndicatorId, direction: SmartMoneyDirection) {
  if (indicator === "swing_points") return direction === "bearish" ? "#e2e8f0" : "#6ee7b7";
  if (indicator === "liquidity_sweeps") return direction === "bearish" ? "#f472b6" : "#c084fc";
  return LEVEL_COLORS[indicator];
}

function levelDash(level: SmartMoneyLevel) {
  if (level.indicator === "bos") return "6 4";
  if (level.indicator === "liquidity" || level.indicator === "equal_high_low") return "3 4";
  if (level.indicator === "choch") return "none";
  return "2 2";
}

function labelsOverlap(candidate: LabelBounds, occupied: readonly LabelBounds[]) {
  return occupied.some((bounds) => (
    candidate.left < bounds.right + LABEL_COLLISION_GAP
    && candidate.right > bounds.left - LABEL_COLLISION_GAP
    && candidate.top < bounds.bottom + LABEL_COLLISION_GAP
    && candidate.bottom > bounds.top - LABEL_COLLISION_GAP
  ));
}

function visibleLevel(
  level: SmartMoneyLevel,
  active: ReadonlySet<SmartMoneyIndicatorId>,
  levels: readonly SmartMoneyLevel[],
) {
  if (!active.has(level.indicator)) return false;
  if (level.indicator !== "choch" || !active.has("mss")) return true;
  return !levels.some((candidate) => (
    candidate.indicator === "mss"
    && candidate.startTime === level.startTime
    && candidate.endTime === level.endTime
    && Math.abs(candidate.price - level.price) <= Math.abs(level.price) * 1e-10
  ));
}

function zoneCoordinates(
  zone: SmartMoneyZone,
  project: (time: number, price: number) => Coordinate | null,
  width: number,
) {
  const topLeft = project(Number(zone.startTime), zone.top);
  const bottomLeft = project(Number(zone.startTime), zone.bottom);
  if (!topLeft || !bottomLeft) return null;
  const end = zone.endTime ? project(Number(zone.endTime), zone.top) : null;
  const rawX1 = topLeft.x;
  const rawX2 = zone.endTime ? (end?.x ?? width) : width;
  if (Math.max(rawX1, rawX2) < 0 || Math.min(rawX1, rawX2) > width) return null;
  const x1 = Math.max(0, Math.min(width, rawX1));
  const x2 = Math.max(0, Math.min(width, rawX2));
  return {
    x: Math.min(x1, x2),
    y: Math.min(topLeft.y, bottomLeft.y),
    width: Math.max(2, Math.abs(x2 - x1)),
    height: Math.max(2, Math.abs(bottomLeft.y - topLeft.y)),
    showLabel: rawX1 >= 0,
  };
}

function levelLayouts(
  output: SmartMoneyOutput,
  active: ReadonlySet<SmartMoneyIndicatorId>,
  width: number,
  project: (time: number, price: number) => Coordinate | null,
  occupiedLabels: LabelBounds[],
) {
  const layouts: LevelLayout[] = [];

  for (const level of output.levels) {
    if (!visibleLevel(level, active, output.levels)) continue;
    const start = project(Number(level.startTime), level.price);
    if (!start) continue;
    const end = level.endTime ? project(Number(level.endTime), level.price) : null;
    const rawEndX = level.active ? width : (end?.x ?? width);
    if (Math.max(start.x, rawEndX) < 0 || Math.min(start.x, rawEndX) > width) continue;
    const startX = Math.max(0, Math.min(width, start.x));
    const endX = Math.max(0, Math.min(width, rawEndX));
    const lineWidth = Math.abs(endX - startX);
    if (lineWidth < 3) continue;

    const labelWidth = level.label.length * 5.7;
    const labelX = (startX + endX) / 2;
    const labelLeft = labelX - labelWidth / 2;
    const labelRight = labelX + labelWidth / 2;
    const labelY = start.y - 4;
    const labelBounds = {
      left: labelLeft,
      right: labelRight,
      top: labelY - LEVEL_LABEL_FONT_SIZE,
      bottom: labelY + 1,
    };
    const hasHorizontalSpace = lineWidth >= labelWidth + LEVEL_LABEL_SIDE_SPACE * 2;
    const overlapsLabel = labelsOverlap(labelBounds, occupiedLabels);
    const showLabel = hasHorizontalSpace && !overlapsLabel;
    if (showLabel) occupiedLabels.push(labelBounds);
    layouts.push({ level, startX, endX, y: start.y, color: levelColor(level), labelX, showLabel });
  }

  return layouts;
}

function markerLayouts(
  output: SmartMoneyOutput,
  active: ReadonlySet<SmartMoneyIndicatorId>,
  width: number,
  project: (time: number, price: number) => Coordinate | null,
  occupiedLabels: LabelBounds[],
) {
  const layouts: MarkerLayout[] = [];

  for (const marker of output.markers) {
    if (!active.has(marker.indicator)) continue;
    const point = project(Number(marker.time), marker.price);
    if (!point || point.x < 0 || point.x > width) continue;
    const isHigh = marker.direction === "bearish";
    const textY = point.y + (isHigh ? -9 : 15);
    const triangle = isHigh
      ? `${point.x - 3},${point.y - 3} ${point.x + 3},${point.y - 3} ${point.x},${point.y + 1}`
      : `${point.x - 3},${point.y + 3} ${point.x + 3},${point.y + 3} ${point.x},${point.y - 1}`;
    layouts.push({ marker, point, color: markerColor(marker.indicator, marker.direction), textY, triangle, showLabel: false });
  }

  const prioritized = [...layouts].sort((left, right) => {
    const kindPriority = Number(right.marker.kind === "sweep") - Number(left.marker.kind === "sweep");
    return kindPriority || Number(right.marker.time) - Number(left.marker.time);
  });
  for (const layout of prioritized) {
    const fontSize = layout.marker.kind === "sweep" ? 8.5 : 8;
    const labelWidth = layout.marker.label.length * 5.3;
    const labelBounds = {
      left: layout.point.x - labelWidth / 2,
      right: layout.point.x + labelWidth / 2,
      top: layout.textY - fontSize,
      bottom: layout.textY + 2,
    };
    const fitsHorizontally = labelBounds.left >= 4 && labelBounds.right <= width - 12;
    if (!fitsHorizontally || labelsOverlap(labelBounds, occupiedLabels)) continue;
    layout.showLabel = true;
    occupiedLabels.push(labelBounds);
  }

  return layouts;
}

export function SmartMoneyOverlay({
  output,
  active,
  width,
  project,
}: {
  output: SmartMoneyOutput;
  active: ReadonlySet<SmartMoneyIndicatorId>;
  width: number;
  project: (time: number, price: number) => Coordinate | null;
}) {
  const occupiedLabels: LabelBounds[] = [];
  const markers = markerLayouts(output, active, width, project, occupiedLabels);
  const levels = levelLayouts(output, active, width, project, occupiedLabels);
  return (
    <g data-smart-money-overlay="true">
      {output.zones.map((zone) => {
        if (!active.has(zone.indicator)) return null;
        const bounds = zoneCoordinates(zone, project, width);
        if (!bounds) return null;
        const colors = zoneColors(zone);
        const isActive = zone.status === "active";
        const strokeDasharray = zone.status === "invalidated" ? "3 4" : "none";
        const labelWidth = zone.label.length * 5.3;
        const showLabel = bounds.width >= labelWidth + 14 && bounds.height >= 12;
        return (
          <g key={zone.id} opacity={isActive ? 1 : 0.42}>
            <rect
              {...bounds}
              rx="2"
              fill={colors.fill}
              stroke={colors.stroke}
              strokeWidth={isActive ? 1 : 0.8}
              strokeDasharray={strokeDasharray}
            />
            {bounds.showLabel && showLabel ? (
              <text
                x={bounds.x + bounds.width / 2}
                y={bounds.y + bounds.height / 2 + 3}
                textAnchor="middle"
                fill={colors.text}
                fontSize="8.5"
                fontWeight="600"
                paintOrder="stroke"
                stroke="#0d1711"
                strokeWidth="2.5"
              >
                {zone.label}
              </text>
            ) : null}
          </g>
        );
      })}

      {levels.map(({ level, startX, endX, y, color, labelX, showLabel }) => {
        return (
          <g key={level.id} opacity={level.active ? 0.9 : 0.82}>
            <line
              x1={startX}
              y1={y}
              x2={endX}
              y2={y}
              stroke={color}
              strokeWidth={level.indicator === "mss" ? 1.55 : 1}
              strokeDasharray={levelDash(level)}
            />
            {showLabel ? (
              <text
                x={labelX}
                y={y - 4}
                textAnchor="middle"
                fill={color}
                fontSize={LEVEL_LABEL_FONT_SIZE}
                fontWeight="700"
                paintOrder="stroke"
                stroke="#0d1711"
                strokeWidth="3"
                strokeLinejoin="round"
              >
                {level.label}
              </text>
            ) : null}
          </g>
        );
      })}

      {markers.map(({ marker, point, color, textY, triangle, showLabel }) => {
        return (
          <g key={marker.id}>
            <polygon points={triangle} fill={color} opacity=".92" />
            {showLabel ? (
              <text
                x={point.x}
                y={textY}
                textAnchor="middle"
                fill={color}
                fontSize={marker.kind === "sweep" ? "8.5" : "8"}
                fontWeight="700"
                paintOrder="stroke"
                stroke="#0d1711"
                strokeWidth="2.5"
              >
                {marker.label}
              </text>
            ) : null}
          </g>
        );
      })}
    </g>
  );
}
