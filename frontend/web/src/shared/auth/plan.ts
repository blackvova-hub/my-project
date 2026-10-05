export type PlanLevel = "free" | "standard" | "pro";

export function normalizePlan(plan?: string | null): PlanLevel {
  const raw = String(plan ?? "").trim().toLowerCase();
  if (raw === "pro") return "pro";
  if (raw === "standard" || raw === "standart") return "standard";
  return "free";
}

export function planRank(plan?: string | null): number {
  const value = normalizePlan(plan);
  if (value === "pro") return 2;
  if (value === "standard") return 1;
  return 0;
}

export function hasPlan(plan: string | null | undefined, min: PlanLevel): boolean {
  return planRank(plan) >= planRank(min);
}
