import { useRef, useState } from "react";
import { isStoredArticle, readReadingState, type ReadingState } from "./model";
import type { TerminalNewsItem } from "./api";

type LocalState = ReadingState & { articles?: TerminalNewsItem[] };
export function useReading(userId: string) {
  const key = `short-long:news-macro:v1:${userId || "guest"}`;
  const [state, setState] = useState<LocalState>(() => {
    try {
      const raw = localStorage.getItem(key),
        parsed = readReadingState(raw);
      const extra: unknown = raw ? JSON.parse(raw) : null;
      const articles =
        typeof extra === "object" &&
        extra &&
        "articles" in extra &&
        Array.isArray(extra.articles)
          ? extra.articles.filter(isStoredArticle).slice(-100)
          : [];
      return { ...parsed, articles };
    } catch {
      return readReadingState(null);
    }
  });
  const current = useRef(state);
  const [error, setError] = useState("");
  function update(transform: (previous: LocalState) => LocalState) {
    const next = transform(current.current);
    current.current = next;
    setState(next);
    try {
      localStorage.setItem(key, JSON.stringify(next));
    } catch {
      setError(
        "Браузер запретил локальное хранение. Избранное и отметки сохранятся только до закрытия страницы.",
      );
    }
  }
  function toggleSave(item: TerminalNewsItem) {
    update((previous) => {
      const saved = previous.saved.includes(item.id);
      return {
        ...previous,
        saved: saved
          ? previous.saved.filter((id) => id !== item.id)
          : [...previous.saved, item.id].slice(-100),
        articles: saved
          ? previous.articles?.filter((article) => article.id !== item.id)
          : [
              ...(previous.articles ?? []).filter(
                (article) => article.id !== item.id,
              ),
              item,
            ].slice(-100),
      };
    });
  }
  function markRead(id: string) {
    update((previous) => ({
      ...previous,
      read: [...new Set([...previous.read, id])].slice(-1000),
    }));
  }
  return { state, toggleSave, markRead, error };
}
