import { z } from "zod";
import { backtestLimits, planLimitError } from "./limits.ts";
import {
  describeRule,
  makeRule,
  ruleError,
  hasWindow,
  indicators,
  type IndicatorRule,
} from "./rules.ts";
export { makeRule } from "./rules.ts";
export type { IndicatorRule } from "./rules.ts";

export const operandKinds = [
  "close",
  "rsi",
  "ema",
  "sma",
  "macd",
  "macd_signal",
  "macd_histogram",
  "constant",
] as const;
export type OperandKind = (typeof operandKinds)[number];
export type Operand = { kind: OperandKind; period?: number; value?: number };
export type Comparison = {
  kind: "compare";
  operator: "gt" | "lt" | "gte" | "lte" | "crosses_above" | "crosses_below";
  left: Operand;
  right: Operand;
};
export const smcEvents = [
  "bos",
  "choch",
  "mss",
  "liquidity_sweep",
  "fvg",
  "order_block",
] as const;
export type SmartMoneyCondition = {
  kind: "smc";
  event: (typeof smcEvents)[number];
  direction: "bullish" | "bearish";
  withinBars: number;
};
export type Condition =
  | IndicatorRule
  | Comparison
  | SmartMoneyCondition
  | { kind: "and" | "or"; children: Condition[] };
export const operatorNames: Record<Comparison["operator"], string> = {
  lt: "< Меньше",
  lte: "≤ Меньше / равно",
  gt: "> Больше",
  gte: "≥ Больше / равно",
  crosses_above: "Пересекает вверх",
  crosses_below: "Пересекает вниз",
};
export function describeOperand(o: Operand): string {
  return o.kind === "constant"
    ? Number.isFinite(o.value)
      ? String(o.value)
      : "…"
    : `${indicatorNames[o.kind]}${o.period !== undefined ? ` (${Number.isFinite(o.period) ? o.period : "…"})` : ""}`;
}
export function describeCondition(c: Condition): string {
  if (c.kind === "indicator") return describeRule(c);
  if (c.kind === "smc") return "Условие прежнего формата";
  if (c.kind === "compare")
    return `${describeOperand(c.left)} ${c.operator.startsWith("crosses") ? operatorNames[c.operator].toLowerCase() : operatorNames[c.operator].split(" ")[0]} ${describeOperand(c.right)}`;
  return c.children
    .map(describeCondition)
    .join(c.kind === "and" ? " И " : " ИЛИ ");
}
// Keep older JSON / drafts meaningful when the numeric threshold was on the left.
export function normalizeRequest(r: BacktestRequest): BacktestRequest {
  const reverse: Record<Comparison["operator"], Comparison["operator"]> = {
    lt: "gt",
    gt: "lt",
    lte: "gte",
    gte: "lte",
    crosses_above: "crosses_below",
    crosses_below: "crosses_above",
  };
  const visit = (c: Condition): Condition => {
    if (c.kind === "indicator" || c.kind === "smc") return c;
    if (c.kind === "and" || c.kind === "or")
      return { ...c, children: c.children.map(visit) };
    if (c.kind !== "compare") return c;
    const node =
      c.left.kind === "constant"
        ? { ...c, left: c.right, right: c.left, operator: reverse[c.operator] }
        : c;
    if (
      node.right.kind === "constant" &&
      ["rsi", "macd", "macd_histogram"].includes(node.left.kind)
    ) {
      const rule = makeRule(
        node.left.kind === "rsi" ? "rsi" : "macd",
        node.left.kind === "macd"
          ? "line"
          : node.left.kind === "macd_histogram"
            ? "histogram"
            : "value",
      );
      return {
        ...rule,
        operator: node.operator,
        threshold: node.right.value ?? 0,
        ...(rule.period ? { period: node.left.period } : {}),
      };
    }
    return node;
  };
  const root = (c: Condition): Condition =>
    c.kind === "and" || c.kind === "or"
      ? visit(c)
      : { kind: "and", children: [visit(c)] };
  return {
    ...r,
    timeframe:
      r.timeframe === "5m" || r.timeframe === "15m" ? "1h" : r.timeframe,
    strategy: {
      ...r.strategy,
      version: 2,
      entry: root(r.strategy.entry),
      exit: r.strategy.exit ? root(r.strategy.exit) : undefined,
    },
  };
}

const operandSchema: z.ZodType<Operand> = z
  .object({
    kind: z.enum(operandKinds),
    period: z.number().int().min(2).max(400).optional(),
    value: z.number().min(-1e9).max(1e9).optional(),
  })
  .strict()
  .superRefine((o, ctx) => {
    if (["rsi", "ema", "sma"].includes(o.kind) && o.period === undefined)
      ctx.addIssue({ code: "custom", message: "Укажите период индикатора" });
    if (o.kind === "constant" && o.value === undefined)
      ctx.addIssue({ code: "custom", message: "Укажите число для сравнения" });
    if (!["rsi", "ema", "sma"].includes(o.kind) && o.period !== undefined)
      ctx.addIssue({ code: "custom", message: "Лишний период индикатора" });
    if (o.kind !== "constant" && o.value !== undefined)
      ctx.addIssue({ code: "custom", message: "Лишнее значение индикатора" });
  });
const conditionSchema: z.ZodType<Condition> = z.lazy(() =>
  z.union([
    z
      .object({
        kind: z.literal("indicator"),
        indicator: z.string(),
        measure: z.string(),
        operator: z.enum([
          "gt",
          "lt",
          "gte",
          "lte",
          "crosses_above",
          "crosses_below",
        ]),
        threshold: z.number(),
        direction: z.enum(["up", "down", "both"]).optional(),
        windowHours: z.number().optional(),
        period: z.number().optional(),
      })
      .strict()
      .superRefine((r, ctx) => {
        const error = ruleError(r);
        if (error) ctx.addIssue({ code: "custom", message: error });
      }),
    z
      .object({
        kind: z.literal("smc"),
        event: z.enum(smcEvents),
        direction: z.enum(["bullish", "bearish"]),
        withinBars: z.number().int().min(1).max(100),
      })
      .strict(),
    z
      .object({
        kind: z.literal("compare"),
        operator: z.enum([
          "gt",
          "lt",
          "gte",
          "lte",
          "crosses_above",
          "crosses_below",
        ]),
        left: operandSchema,
        right: operandSchema,
      })
      .strict()
      .refine(
        (c) => c.left.kind !== "constant" || c.right.kind !== "constant",
        "Сравнивайте хотя бы один индикатор",
      ),
    z
      .object({
        kind: z.enum(["and", "or"]),
        children: z.array(conditionSchema).min(1).max(8),
      })
      .strict(),
  ]),
);
export const requestSchema = z
  .object({
    exchange: z.literal("bybit"),
    market: z.enum(["spot", "linear"]),
    symbols: z
      .array(z.string().regex(/^[A-Z0-9]{2,24}USDT$/))
      .min(1)
      .max(10)
      .refine(
        (s) => new Set(s).size === s.length,
        "Монеты не должны повторяться",
      ),
    timeframe: z.enum(["5m", "15m", "1h", "4h"]),
    from: z.string().datetime(),
    to: z.string().datetime(),
    strategy: z
      .object({
        version: z.union([z.literal(1), z.literal(2)]),
        direction: z.enum(["long", "short"]),
        entry: conditionSchema,
        exit: conditionSchema.optional(),
        takeProfitPct: z.number().min(0).max(1000),
        stopLossPct: z.number().min(0.01).max(99),
        positionSizePct: z.number().min(0.1).max(100),
        feePct: z.number().min(0).max(5),
        slippagePct: z.number().min(0).max(5),
        initialCapital: z.number().min(100).max(100_000_000),
      })
      .strict(),
  })
  .strict()
  .superRefine((r, ctx) => {
    if (r.market === "spot" && r.strategy.direction === "short")
      ctx.addIssue({
        code: "custom",
        message: "Short доступен только на Futures",
      });
    if (r.strategy.direction === "short" && r.strategy.takeProfitPct >= 100)
      ctx.addIssue({
        code: "custom",
        message: "Тейк-профит Short должен быть меньше 100%",
      });
    let count = 0;
    const visit = (c: Condition, depth: number) => {
      count++;
      if (depth > 3)
        ctx.addIssue({
          code: "custom",
          message: "Не более 3 уровней вложенности",
        });
      if (c.kind === "and" || c.kind === "or")
        c.children.forEach((child) => visit(child, depth + 1));
    };
    visit(r.strategy.entry, 0);
    if (r.strategy.exit) visit(r.strategy.exit, 0);
    if (count > 32)
      ctx.addIssue({ code: "custom", message: "Не более 32 условий" });
  });
export type BacktestRequest = z.infer<typeof requestSchema>;
export type Strategy = BacktestRequest["strategy"];
export const indicatorNames: Record<OperandKind, string> = {
  close: "Цена",
  rsi: "RSI",
  ema: "EMA",
  sma: "SMA",
  macd: "MACD (12, 26)",
  macd_signal: "MACD signal (9)",
  macd_histogram: "MACD histogram",
  constant: "Значение",
};
export function makeOperand(kind: OperandKind): Operand {
  return kind === "constant"
    ? { kind, value: 30 }
    : ["rsi", "ema", "sma"].includes(kind)
      ? { kind, period: kind === "rsi" ? 14 : 200 }
      : { kind };
}
export function makeComparison(): Comparison {
  return {
    kind: "compare",
    operator: "lt",
    left: { kind: "rsi", period: 14 },
    right: { kind: "constant", value: 30 },
  };
}
export function makeExit(): Condition {
  return {
    kind: "and",
    children: [{ ...makeRule("rsi"), operator: "gte", threshold: 70 }],
  };
}
export const dateOnly = (iso: string) => iso.slice(0, 10);
export function utcDate(date: string, offset = 0) {
  const d = new Date(`${date}T00:00:00.000Z`);
  d.setUTCDate(d.getUTCDate() + offset);
  return d.toISOString();
}
export function defaultRequest(): BacktestRequest {
  const to = new Date();
  to.setUTCHours(0, 0, 0, 0);
  const from = new Date(to);
  from.setUTCDate(from.getUTCDate() - 90);
  return {
    exchange: "bybit",
    market: "linear",
    symbols: ["BTCUSDT"],
    timeframe: "1h",
    from: from.toISOString(),
    to: to.toISOString(),
    strategy: {
      version: 2,
      direction: "long",
      entry: {
        kind: "and",
        children: [makeRule()],
      },
      takeProfitPct: 5,
      stopLossPct: 2,
      positionSizePct: 20,
      feePct: 0.1,
      slippagePct: 0.05,
      initialCapital: 10_000,
    },
  };
}
export function validateRequest(r: BacktestRequest, plan?: string | null): string | null {
  if (r.symbols.length === 0) return "Добавьте хотя бы одну торговую пару.";
  if (r.timeframe !== "1h" && r.timeframe !== "4h")
    return "Для теста стратегии выберите 1 час или 4 часа";
  const parsed = requestSchema.safeParse(r);
  if (!parsed.success)
    return parsed.error.issues[0]?.message ?? "Проверьте настройки стратегии";
  let invalid: string | null = null;
  const check = (c: Condition) => {
    if (c.kind === "and" || c.kind === "or") {
      c.children.forEach(check);
      return;
    }
    if (c.kind !== "indicator") {
      invalid =
        "Замените условие прежнего формата: выберите индикатор и его параметры";
      return;
    }
    if (indicators[c.indicator]?.futures && r.market !== "linear")
      invalid = "Открытый интерес доступен только на Futures";
    const stepHours = r.timeframe === "4h" ? 4 : 1;
    if (hasWindow(c) && c.windowHours! % stepHours)
      invalid = `Окно условия должно быть кратно ${stepHours} ч`;
  };
  check(r.strategy.entry);
  if (r.strategy.exit) check(r.strategy.exit);
  if (invalid) return invalid;
  const planError = planLimitError(r, backtestLimits(plan));
  if (planError) return planError;
  const from = new Date(r.from),
    to = new Date(r.to),
    now = new Date(),
    earliest = new Date(now);
  earliest.setUTCHours(0, 0, 0, 0);
  earliest.setUTCFullYear(earliest.getUTCFullYear() - 1);
  const step = {
    "5m": 300_000,
    "15m": 900_000,
    "1h": 3_600_000,
    "4h": 14_400_000,
  }[r.timeframe];
  if (
    +from < +earliest ||
    +to > Math.floor(+now / step) * step ||
    +to - +from < 2 * step ||
    +from % step ||
    +to % step
  )
    return "Выберите завершённый период в пределах последнего года (UTC)";
  return null;
}
export function readDraft(user: string): BacktestRequest {
  try {
    const raw = localStorage.getItem(`backtest:draft:v1:${user}`);
    if (raw && raw.length < 32768) {
      const parsed = requestSchema.safeParse(JSON.parse(raw));
      if (parsed.success) return normalizeRequest(parsed.data);
    }
  } catch {
    /* private storage or old draft */
  }
  return defaultRequest();
}
export function saveDraft(user: string, value: BacktestRequest) {
  try {
    localStorage.setItem(`backtest:draft:v1:${user}`, JSON.stringify(value));
  } catch {
    /* the form remains usable without persistence */
  }
}
export function downloadBlob(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export type Trade = {
  symbol: string;
  direction: "long" | "short";
  entryTime: number;
  exitTime: number;
  entryPrice: number;
  exitPrice: number;
  quantity: number;
  fees: number;
  pnl: number;
  pnlPct: number;
  reason: string;
};
export type BacktestResult = {
  engineVersion: string;
  signals?: { symbol: string; evaluatedBars: number; matchedBars: number }[];
  metrics: {
    profitPct: number;
    maxDrawdownPct: number;
    winRate: number;
    tradeCount: number;
    profitFactor: number | null;
    sharpe: number | null;
    finalCapital: number;
    totalFees: number;
  };
  equity: { time: number; value: number }[];
};
export type Job = {
  id: string;
  request: BacktestRequest;
  status: "queued" | "running" | "completed" | "failed" | "cancelled";
  phase: string;
  progress: number;
  error?: string;
  result?: BacktestResult;
  createdAt: string;
  finishedAt?: string;
};
export const isActive = (job?: Job) =>
  job?.status === "queued" || job?.status === "running";
export const statuses: Record<Job["status"], string> = {
  queued: "В очереди",
  running: "Выполняется",
  completed: "Завершён",
  failed: "Ошибка",
  cancelled: "Отменён",
};
