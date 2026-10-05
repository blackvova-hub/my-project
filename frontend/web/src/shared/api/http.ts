// src/shared/api/http.ts
const API_BASE = import.meta.env.VITE_API_BASE ?? "/api";

export type HttpError = Error & {
  status?: number;
  body?: unknown;
};

export async function http<T>(
  path: string,
  init: RequestInit = {}
): Promise<T> {
  const base = API_BASE.replace(/\/+$/, "");
  const baseNoApi = base.replace(/\/api$/, "");
  const url = path.startsWith("/api/") ? `${baseNoApi}${path}` : `${base}${path}`;

  const res = await fetch(url, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(init.headers ?? {}),
    },
    credentials: "include", // КРИТИЧНО для cookie auth (sid)
  });

  if (!res.ok) {
    let body: unknown = null;
    try {
      body = (await res.json()) as unknown;
    } catch {
      // ignore
    }
    const payload = typeof body === "object" && body !== null ? (body as Record<string, unknown>) : null;
    const err: HttpError = new Error(
      typeof payload?.error === "string" ? payload.error : `HTTP ${res.status}`
    );
    err.status = res.status;
    err.body = body;
    throw err;
  }

  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}
