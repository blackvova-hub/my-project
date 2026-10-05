import { useCallback, useMemo, useState } from "react";
import type { AiChatRequest } from "./api";
import { aiChat } from "./api";

const MAX_MESSAGES_SENT = 6;
const MARKDOWN_DECORATOR_PATTERN = /\*\*|__|`/g;
const MARKDOWN_HEADING_PATTERN = /^#{1,3}\s+/gm;

function normalizeAssistantText(value: string) {
  return value
    .replace(MARKDOWN_DECORATOR_PATTERN, "")
    .replace(MARKDOWN_HEADING_PATTERN, "")
    .trim();
}

export type AssistantMessage = {
  role: "user" | "assistant";
  content: string;
  raw?: string;
  ts: number;
};

type UseAiAssistantOptions = {
  context?: string;
};

export function useAiAssistant(options?: UseAiAssistantOptions) {
  const [messages, setMessages] = useState<AssistantMessage[]>([]);
  const [isOpen, setIsOpen] = useState(false);
  const [isSending, setIsSending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const send = useCallback(
    async (content: string) => {
      const trimmed = content.trim();
      if (!trimmed || isSending) return;

      const userMsg: AssistantMessage = {
        role: "user",
        content: trimmed,
        raw: trimmed,
        ts: Date.now(),
      };
      const requestMessages = [...messages, userMsg].slice(-MAX_MESSAGES_SENT);
      const controller = new AbortController();
      const timeoutId = window.setTimeout(() => controller.abort(), 25_000);

      setMessages((prev) => [...prev, userMsg]);
      setIsSending(true);
      setError(null);

      try {
        const payload: AiChatRequest = {
          messages: requestMessages.map((message) => ({
            role: message.role,
            content: message.raw ?? message.content,
          })),
          context: options?.context ?? undefined,
        };
        const response = await aiChat(payload, controller.signal);
        const botMsg: AssistantMessage = {
          role: "assistant",
          content: normalizeAssistantText(response.reply),
          ts: Date.now(),
        };
        setMessages((prev) => [...prev, botMsg]);
      } catch (err) {
        const apiError = err as { status?: number; message?: string } | null;
        const message = apiError?.message ?? "";
        if (apiError?.status === 429 || message.includes("ai_quota_exceeded")) {
          setError("Дневной лимит AI исчерпан. Попробуй завтра.");
        } else if (apiError?.status === 503) {
          setError("AI временно недоступен. Попробуй чуть позже.");
        } else if (message.includes("aborted") || message.includes("AbortError")) {
          setError("Ответ от AI идёт слишком долго. Попробуй ещё раз.");
        } else if (message.includes("plan_required")) {
          setError("AI доступен начиная с тарифа Standard.");
        } else if (message.includes("ai_unavailable")) {
          setError("AI временно недоступен. Проверь ключ и сеть сервера.");
        } else if (message.includes("ai_not_configured")) {
          setError("AI не настроен на сервере. Проверь переменные окружения.");
        } else if (message.includes("ai_error")) {
          setError("AI вернул ошибку. Попробуй ещё раз чуть позже.");
        } else {
          setError(apiError?.message ?? "Не удалось получить ответ AI.");
        }
      } finally {
        window.clearTimeout(timeoutId);
        setIsSending(false);
      }
    },
    [messages, isSending, options?.context],
  );

  const reset = useCallback(() => setMessages([]), []);
  const transcript = useMemo(() => messages, [messages]);

  return {
    isOpen,
    setIsOpen,
    isSending,
    messages: transcript,
    error,
    setError,
    send,
    reset,
  };
}
