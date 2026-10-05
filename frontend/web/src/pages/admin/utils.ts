import type { AdminUser } from "./types";

export function formatDate(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "—" : date.toLocaleString("ru-RU", { dateStyle: "medium", timeStyle: "short" });
}

export function planLabel(plan: string) {
  return plan === "pro" ? "Pro" : plan === "standard" ? "Standard" : plan === "free" ? "Free" : plan;
}

export function subscriptionState(user: AdminUser) {
  if (user.subscriptionFrozenAt) return { label: "Заморожена", tone: "frozen" };
  if (user.subscriptionActive) return { label: "Активна", tone: "active" };
  return { label: "Неактивна", tone: "inactive" };
}
