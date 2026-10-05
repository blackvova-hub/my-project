import { useState, useCallback } from "react";
import type { Drawing } from "./Drawings";
const EMPTY: Drawing[] = [];
export function useDrawings(session: number) {
  const [saved, setSaved] = useState<{ session: number; items: Drawing[] }>({
    session,
    items: [],
  });
  const drawings = saved.session === session ? saved.items : EMPTY;
  const add = useCallback(
    (drawing: Drawing) =>
      setSaved((d) => ({
        session,
        items: [...(d.session === session ? d.items.slice(-99) : []), drawing],
      })),
    [session],
  );
  const update = useCallback(
    (index: number, change: (drawing: Drawing) => Drawing) =>
      setSaved((current) => current.session === session ? {
        session,
        items: current.items.map((drawing, position) => position === index ? change(drawing) : drawing),
      } : current),
    [session],
  );
  const remove = useCallback(
    (index: number) => setSaved((current) => current.session === session ? {
      session,
      items: current.items.filter((_, position) => position !== index),
    } : current),
    [session],
  );
  return {
    drawings,
    add,
    update,
    remove,
    undo: () =>
      setSaved((d) => ({
        session,
        items: d.session === session ? d.items.slice(0, -1) : [],
      })),
    clear: () => setSaved({ session, items: [] }),
  };
}
