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
export function useChartWorkspace(key: string) {
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
    return { key, sets };
  }, [storage, key]);
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
    localRef.current = { ...(query.data?.drawing_sets ?? {}), ...local };
  }, [query.data, local]);
  const initial = useMemo(() => {
    const parts = key.split(":");
    const legacy =
      parts.length === 4 && parts[1] === "linear"
        ? `${parts[0]}:${parts[2]}:${parts[3]}`
        : key;
    return (
      local[key] ??
      local[legacy] ??
      query.data?.drawing_sets[key] ??
      query.data?.drawing_sets[legacy] ??
      EMPTY
    );
  }, [local, query.data, key]);
  const update = useCallback(
    (items: Drawing[]) => {
      try {
        const latest = normalizeDrawingSets(
          JSON.parse(localStorage.getItem(storage) ?? "{}"),
        );
        localRef.current = { ...localRef.current, ...latest, [key]: items };
        localStorage.setItem(storage, JSON.stringify(localRef.current));
      } catch {
        setStatus("Не удалось сохранить: хранилище браузера недоступно");
      }
    },
    [key, storage],
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
