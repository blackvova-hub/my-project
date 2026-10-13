import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuth } from "../auth/AuthContext";
import {
  fetchChartConfig,
  saveChartConfig,
  type ChartConfig,
} from "../market/chartConfigApi";
import { normalizeDrawingSets, type Drawing } from "./drawings/model";
import type { IndicatorId } from "../market/indicatorTypes";
import type { PerpInterval } from "../market/perpMarketData";
const EMPTY: Drawing[] = [];
export function useChartWorkspace(key: string, fallbackKey?: string) {
  const [restoreEpoch, setRestoreEpoch] = useState(0);
  useEffect(() => {
    const restore = () => setRestoreEpoch((n) => n + 1);
    window.addEventListener("shortlong:drawings-restored", restore);
    return () =>
      window.removeEventListener("shortlong:drawings-restored", restore);
  }, []);
  const { user } = useAuth(),
    owner = user?.id ?? "guest",
    storage = `shortlong:charts:v2:${owner}`;
  const snapshot = useMemo(() => {
    let sets: Record<string, Drawing[]> = {};
    try {
      sets = normalizeDrawingSets(
        JSON.parse(localStorage.getItem(storage) ?? "{}"),
      );
    } catch {
      /* Storage may be disabled. */
    }
    return { key, sets, restoreEpoch };
  }, [storage, key, restoreEpoch]);
  const local = snapshot.sets;
  const query = useQuery({
    queryKey: ["chart-workspace", owner],
    queryFn: ({ signal }) => fetchChartConfig(signal),
    enabled: !!user,
    staleTime: 60000,
    retry: false,
  });
  const localRef = useRef(local),
    configRef = useRef<ChartConfig | null>(null),
    [status, setStatus] = useState("Сохранение в браузере");
  useEffect(() => {
    configRef.current = query.data ?? null;
    let latest = local;
    try {
      latest = normalizeDrawingSets(
        JSON.parse(localStorage.getItem(storage) ?? "{}"),
      );
    } catch {
      /* Keep in-memory drawings. */
    }
    localRef.current = { ...(query.data?.drawing_sets ?? {}), ...latest };
  }, [query.data, local, storage]);
  const initial = useMemo(() => {
    const canonical = key.startsWith("window-")
      ? key.split(":").slice(1).join(":")
      : key;
    const parts = canonical.split(":");
    const legacy =
      parts.length === 4 && parts[1] === "linear"
        ? `${parts[0]}:${parts[2]}:${parts[3]}`
        : canonical;
    const fallbackParts = fallbackKey?.split(":").slice(1) ?? [];
    const legacyFallback =
      fallbackParts.length === 4 && fallbackParts[1] === "linear"
        ? `${fallbackParts[0]}:${fallbackParts[2]}:${fallbackParts[3]}`
        : "";
    let latest = local;
    try {
      latest = normalizeDrawingSets(
        JSON.parse(localStorage.getItem(storage) ?? "{}"),
      );
    } catch {
      /* Use the initial local snapshot. */
    }
    return (
      latest[key] ??
      (fallbackKey ? latest[fallbackKey] : undefined) ??
      (fallbackKey
        ? latest[fallbackKey.split(":").slice(1).join(":")]
        : undefined) ??
      latest[canonical] ??
      latest[legacy] ??
      latest[legacyFallback] ??
      query.data?.drawing_sets[key] ??
      (fallbackKey ? query.data?.drawing_sets[fallbackKey] : undefined) ??
      (fallbackKey
        ? query.data?.drawing_sets[fallbackKey.split(":").slice(1).join(":")]
        : undefined) ??
      query.data?.drawing_sets[canonical] ??
      query.data?.drawing_sets[legacy] ??
      query.data?.drawing_sets[legacyFallback] ??
      EMPTY
    );
  }, [local, query.data, key, storage, fallbackKey]);
  const update = useCallback(
    (items: Drawing[]) => {
      try {
        const latest = normalizeDrawingSets(
          JSON.parse(localStorage.getItem(storage) ?? "{}"),
        );
        // An initial empty render must not shadow drawings still loading from
        // the account. Actual edits (including deletions of a known set) win.
        if (!items.length && user && !configRef.current && !(key in latest))
          return;
        localRef.current = { ...localRef.current, ...latest, [key]: items };
        localStorage.setItem(storage, JSON.stringify(localRef.current));
        if (
          key in latest &&
          JSON.stringify(latest[key]) !== JSON.stringify(items)
        )
          window.dispatchEvent(
            new CustomEvent("shortlong:drawings-changed", { detail: owner }),
          );
      } catch {
        setStatus("Не удалось сохранить: хранилище браузера недоступно");
      }
    },
    [key, storage, user, owner],
  );
  const save = useCallback(
    async (interval: PerpInterval, indicators: IndicatorId[]) => {
      try {
        if (!user) {
          const message = "Сохранено в браузере · войдите для синхронизации";
          setStatus(message);
          return message;
        }
        const stored = normalizeDrawingSets(
          JSON.parse(localStorage.getItem(storage) ?? "{}"),
        );
        const sets = {
          ...(configRef.current?.drawing_sets ?? {}),
          ...localRef.current,
          ...stored,
        };
        if (
          Object.keys(sets).length > 50 ||
          JSON.stringify(sets).length > 240000
        ) {
          const message =
            "Сохранено в браузере · разметка слишком большая для синхронизации. Доступен экспорт JSON.";
          setStatus(message);
          return message;
        }
        await saveChartConfig({
          version: 1,
          selected_interval: interval,
          active_indicator_ids: indicators,
          drawing_sets: {
            ...(configRef.current?.drawing_sets ?? {}),
            ...localRef.current,
            ...stored,
          },
        });
        setStatus("Сохранено в аккаунте");
        return "Сохранено в аккаунте";
      } catch {
        const message =
          "В браузере сохранено · серверное сохранение недоступно";
        setStatus(message);
        return message;
      }
    },
    [user, storage],
  );
  return { initial, update, save, status, config: query.data };
}
