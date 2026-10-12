import { useCallback, useMemo, useState } from "react";
import { normalizeDrawings, type Drawing } from "./model";
type History = {
  key: string;
  past: Drawing[][];
  current: Drawing[];
  future: Drawing[][];
};
export function useDrawingHistory(key: string, initial: Drawing[] = []) {
  const [history, setHistory] = useState<History>(() => ({
    key,
    past: [],
    current: normalizeDrawings(initial),
    future: [],
  }));
  const current = history.key === key ? history.current : initial;
  const commit = useCallback(
    (items: Drawing[]) =>
      setHistory((h) => ({
        key,
        past: [
          ...(h.key === key ? h.past : []),
          h.key === key ? h.current : initial,
        ].slice(-50),
        current: items,
        future: [],
      })),
    [key, initial],
  );
  const replace = useCallback(
    (items: Drawing[]) =>
      setHistory({
        key,
        past: [],
        current: normalizeDrawings(items),
        future: [],
      }),
    [key],
  );
  const undo = useCallback(
    () =>
      setHistory((h) =>
        h.key === key && h.past.length
          ? {
              ...h,
              past: h.past.slice(0, -1),
              current: h.past.at(-1)!,
              future: [h.current, ...h.future].slice(0, 50),
            }
          : h,
      ),
    [key],
  );
  const redo = useCallback(
    () =>
      setHistory((h) =>
        h.key === key && h.future.length
          ? {
              ...h,
              past: [...h.past, h.current].slice(-50),
              current: h.future[0],
              future: h.future.slice(1),
            }
          : h,
      ),
    [key],
  );
  return useMemo(
    () => ({
      items: current,
      commit,
      replace,
      undo,
      redo,
      canUndo: history.key === key && !!history.past.length,
      canRedo: history.key === key && !!history.future.length,
    }),
    [
      current,
      commit,
      replace,
      undo,
      redo,
      history.key,
      history.past.length,
      history.future.length,
      key,
    ],
  );
}
