export type ApiUser = {
  id: string;
  email: string;
  num_id: number;
  public_id: number;
  displayName: string;
  plan: string;
  subscriptionExpiresAt?: string | null;
  subscriptionFrozenAt?: string | null;
  subscriptionFrozenDaysRemaining?: number;
  subscriptionDaysRemaining?: number;
  subscriptionActive?: boolean;
  isAdmin: boolean;
  twoFAEnabled: boolean;
  avatarUrl: string;
  primaryExchange?: "bybit" | "binance" | null;
  emailVerified: boolean;
  lastSeenAt?: string | null;
  createdAt: string;
};

export type ApiSession = {
  id: string;
  userAgent: string;
  ip: string;
  createdAt: string;
  expiresAt: string;
  isActive: boolean;
  isCurrent: boolean;
};

export type TelegramStatus = {
  linked: boolean;
  enabled: boolean;
  username: string;
};

export type TelegramLink = {
  token: string;
  botUsername: string;
  expiresAt: string;
  deepLink: string;
};

const API_BASE = import.meta.env.VITE_API_BASE_URL || "/api";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers || {}),
    },
  });

  // 204/empty handling
  const text = await res.text();
  let data: unknown = null;
  if (text) {
    try {
      data = JSON.parse(text) as unknown;
    } catch {
      if (!res.ok) {
        throw new Error(text);
      }
      throw new Error("invalid_json_response");
    }
  }

  if (!res.ok) {
    const payload = typeof data === "object" && data !== null ? (data as Record<string, unknown>) : null;
    const errCode = typeof payload?.error === "string" ? payload.error : "request_failed";
    throw new Error(errCode);
  }

  return data as T;
}

export function apiRegister(email: string, password: string, captchaToken?: string) {
  return request<{ requiresVerification: boolean }>("/auth/register", {
    method: "POST",
    body: JSON.stringify({ email, password, captchaToken }),
  });
}

export function apiLogin(email: string, password: string, captchaToken?: string) {
  return request<{ user?: ApiUser; requiresTwoFA?: boolean; challenge?: string }>("/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password, captchaToken }),
  });
}

export function apiVerifyTwoFALogin(email: string, code: string, challenge: string) {
  return request<{ user: ApiUser }>("/auth/twofa/verify-login", {
    method: "POST",
    body: JSON.stringify({ email, code, challenge }),
  });
}

export function apiVerifyEmail(email: string, code: string) {
  return request<{ user: ApiUser }>("/auth/verify-email", {
    method: "POST",
    body: JSON.stringify({ email, code }),
  });
}

export function apiResendVerification(email: string) {
  return request<{ ok: true }>("/auth/resend-verification", {
    method: "POST",
    body: JSON.stringify({ email }),
  });
}

export function apiResetPassword(email: string, captchaToken?: string) {
  return request<{ ok: true; message: string }>("/auth/password-reset/request", {
    method: "POST",
    body: JSON.stringify({ email, captchaToken }),
  });
}

export function apiConfirmResetPassword(token: string, newPassword: string) {
  return request<{ ok: true }>("/auth/password-reset/confirm", {
    method: "POST",
    body: JSON.stringify({ token, newPassword }),
  });
}

export function apiPing() {
  return request<{ ok: true }>("/auth/ping", { method: "POST" });
}

export function apiLogout() {
  return request<{ ok: true }>("/auth/logout", { method: "POST" });
}

export function apiMe() {
  return request<{ user: ApiUser }>("/auth/me", { method: "GET" });
}

export function apiUpdateProfile(input: {
  displayName?: string;
  avatarUrl?: string;
  primaryExchange?: "bybit" | "binance";
}) {
  return request<{ user: ApiUser }>("/auth/profile", {
    method: "PATCH",
    body: JSON.stringify(input),
  });
}

export function apiUpdateTwoFA(enabled: boolean, password?: string, code?: string) {
  return request<{ user: ApiUser }>("/auth/twofa", {
    method: "PATCH",
    body: JSON.stringify({ enabled, password, code }),
  });
}

export function apiRequestTwoFADisable() {
  return request<{ ok: true }>("/auth/twofa/request-disable", { method: "POST" });
}

export function apiRequestTwoFAEnable() {
  return request<{ ok: true }>("/auth/twofa/request-enable", {
    method: "POST",
  });
}

export function apiConfirmTwoFAEnable(code: string) {
  return request<{ user: ApiUser }>("/auth/twofa/confirm-enable", {
    method: "POST",
    body: JSON.stringify({ code }),
  });
}

export function apiSessions() {
  return request<{ sessions: ApiSession[] }>("/auth/sessions", {
    method: "GET",
  });
}

export function apiWatchlist() {
  return request<{ hot: string[]; cold: string[] }>("/auth/watchlist", { method: "GET" });
}

export function apiUpdateWatchlist(hot: string[], cold: string[]) {
  return request<{ hot: string[]; cold: string[] }>("/auth/watchlist", {
    method: "PUT",
    body: JSON.stringify({ hot, cold }),
  });
}

export function apiTelegramStatus() {
  return request<TelegramStatus>("/auth/telegram/status", { method: "GET" });
}

export function apiTelegramLink() {
  return request<TelegramLink>("/auth/telegram/link", { method: "POST" });
}

export function apiTelegramToggle(enabled: boolean) {
  return request<{ enabled: boolean }>("/auth/telegram/toggle", {
    method: "POST",
    body: JSON.stringify({ enabled }),
  });
}

export function apiTelegramUnlink() {
  return request<{ ok: true }>("/auth/telegram/unlink", { method: "POST" });
}
