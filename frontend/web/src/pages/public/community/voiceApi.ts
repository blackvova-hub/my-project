import { http } from "../../../shared/api/http";

export type VoicePair = { id: string; base: string; quote: string };
export type VoiceCatalogue = {
  items: VoicePair[];
  sources: {
    name: string;
    pairs: number;
    available: boolean;
    complete: boolean;
    updatedAt: string;
  }[];
  configured: boolean;
};
export const fetchVoicePairs = (signal: AbortSignal) =>
  http<VoiceCatalogue>("/api/community/voice/pairs", { signal });
export const fetchVoiceRooms = (signal: AbortSignal) =>
  http<{ rooms: Record<string, number> }>("/api/community/voice/rooms", {
    signal,
  });
export const joinVoiceRoom = (pair: string, signal: AbortSignal) =>
  http<{ token: string; url: string; room: string }>(
    "/api/community/voice/join",
    {
      method: "POST",
      body: JSON.stringify({ pair }),
      signal,
    },
  );

export function searchPairs(
  items: VoicePair[],
  search: string,
  active: Record<string, number>,
  occupied: boolean,
) {
  const query = search.toUpperCase().replace(/[\s/_-]/g, "");
  return items.filter(
    (p) =>
      (!occupied || active[p.id] > 0) && `${p.base}${p.quote}`.includes(query),
  );
}
export function voiceError(
  error: unknown,
  fallback = "Не удалось подключиться. Повторите попытку.",
) {
  if (error instanceof Error && error.name === "NotAllowedError")
    return "Нет доступа к микрофону. Разрешите его в настройках сайта и попробуйте снова.";
  if (error instanceof Error && error.name === "NotFoundError")
    return "Микрофон не найден. Подключите устройство и повторите попытку.";
  if (error instanceof Error && error.name === "NotReadableError")
    return "Микрофон занят другим приложением или недоступен.";
  if (error instanceof Error && error.name === "NotSupportedError")
    return "Браузер не поддерживает захват звука. Откройте страницу в актуальной версии Chrome, Edge, Firefox или Safari.";
  if (error instanceof Error && /permission denied/i.test(error.message))
    return "Нет доступа к микрофону. Разрешите его в настройках сайта и попробуйте снова.";
  if (
    error instanceof Error &&
    !/^(HTTP |could not|failed|connection|signal)/i.test(error.message)
  )
    return error.message;
  return fallback;
}
