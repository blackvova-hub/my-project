import { normalizePlan, type PlanLevel } from "../../../shared/auth/plan.ts";
import type { Condition } from "./model.ts";

export type BacktestLimits = {
  plan: PlanLevel;
  maxSymbols: number;
  maxConditions: number;
  maxGroups: number;
  maxDepth: number;
  maxGroupChildren: number;
};

const limits: Record<PlanLevel, BacktestLimits> = {
  free: { plan: "free", maxSymbols: 1, maxConditions: 3, maxGroups: 2, maxDepth: 0, maxGroupChildren: 3 },
  standard: { plan: "standard", maxSymbols: 5, maxConditions: 12, maxGroups: 6, maxDepth: 1, maxGroupChildren: 8 },
  pro: { plan: "pro", maxSymbols: 10, maxConditions: 32, maxGroups: 32, maxDepth: 2, maxGroupChildren: 8 },
};

export function backtestLimits(plan?: string | null): BacktestLimits {
  return limits[normalizePlan(plan)];
}

export type ConditionUsage = { conditions: number; groups: number; depth: number; nodes: number };

export function conditionUsage(...roots: (Condition | undefined)[]): ConditionUsage {
  const usage: ConditionUsage = { conditions: 0, groups: 0, depth: 0, nodes: 0 };
  const visit = (node: Condition, depth: number) => {
    usage.nodes += 1;
    if (node.kind === "and" || node.kind === "or") {
      usage.depth = Math.max(usage.depth, depth);
      usage.groups += 1;
      node.children.forEach((child) => visit(child, depth + 1));
    } else usage.conditions += 1;
  };
  roots.filter((root): root is Condition => !!root).forEach((root) => visit(root, 0));
  return usage;
}

export function planLabel(plan: PlanLevel): string {
  return plan === "free" ? "Free" : plan === "standard" ? "Standard" : "Pro";
}

export function canAdd(usage: ConditionUsage, limits: BacktestLimits, group = false): boolean {
  return usage.conditions < limits.maxConditions &&
    (!group || usage.groups < limits.maxGroups) && usage.nodes + (group ? 2 : 1) <= 32;
}

export function upgradeHint(limits: BacktestLimits): string {
  return limits.plan === "free" ? "Больше пар, условий и вложенные группы — в Standard и Pro."
    : limits.plan === "standard" ? "До 10 пар и расширенный конструктор — в Pro."
    : "Технический лимит: 32 элемента суммарно, до 8 элементов в одной группе.";
}

export function limitNotice(limits: BacktestLimits, feature: string): string {
  return limits.plan === "free"
    ? `${feature} доступны в Standard и Pro.`
    : limits.plan === "standard"
      ? `${feature} доступны в Pro.`
      : "Достигнут предел конструктора. Уменьшите количество условий или групп, чтобы добавить новые.";
}

export function planLimitError(r: { symbols: string[]; strategy: { entry: Condition; exit?: Condition } }, limits: BacktestLimits): string | null {
  const prefix = `Тариф ${planLabel(limits.plan)}`;
  if (r.symbols.length > limits.maxSymbols) return `${prefix}: максимум торговых пар — ${limits.maxSymbols}. ${upgradeHint(limits)}`;
  const usage = conditionUsage(r.strategy.entry, r.strategy.exit);
  if (usage.conditions > limits.maxConditions) return `${prefix}: максимум условий входа и выхода вместе — ${limits.maxConditions}. ${upgradeHint(limits)}`;
  if (usage.groups > limits.maxGroups) return `${prefix}: максимум групп, включая вход и выход, — ${limits.maxGroups}. ${upgradeHint(limits)}`;
  if (usage.depth > limits.maxDepth) return `${prefix}: ${limits.maxDepth === 0 ? "вложенные группы доступны в Standard и Pro" : `допустимо уровней групп — ${limits.maxDepth + 1}`}.`;
  if (usage.nodes > 32) return "Максимум 32 элемента: условия и группы считаются вместе.";
  return null;
}
