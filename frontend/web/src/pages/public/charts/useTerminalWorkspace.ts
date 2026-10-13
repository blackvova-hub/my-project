import { useCallback, useEffect, useRef, useState } from "react";
import { http, type HttpError } from "../../../shared/api/http";
import { normalizeWorkspace, type TerminalWorkspace } from "./workspace";
import { normalizeDrawingSets } from "../../../shared/charts/drawings/model";

type Cloud = {
  workspace: unknown | null;
  revision: string;
  drawing_sets?: unknown;
};
type Local = {
  workspace: TerminalWorkspace;
  saved?: string;
  revision?: string;
  savedDrawings?: string;
};
function readLocal(owner: string): Local {
  try {
    const value = JSON.parse(
      localStorage.getItem(`shortlong:terminal:v3:${owner}`) ?? "null",
    );
    if (value?.workspace)
      return { ...value, workspace: normalizeWorkspace(value.workspace) };
    const legacy = JSON.parse(
      localStorage.getItem(`shortlong:terminal:v2:${owner}`) ?? "null",
    );
    return { workspace: normalizeWorkspace(legacy) };
  } catch {
    return { workspace: normalizeWorkspace(null) };
  }
}
export function useTerminalWorkspace(owner: string, authenticated: boolean) {
  const [initial] = useState(() => readLocal(owner));
  const [workspace, setWorkspace] = useState(initial.workspace);
  const [status, setStatus] = useState("Сохранено в браузере");
  const [conflict, setConflict] = useState(false);
  const current = useRef(workspace),
    edit = useRef(0),
    revision = useRef(initial.revision ?? ""),
    saved = useRef(initial.saved ?? ""),
    savedDrawings = useRef(initial.savedDrawings),
    saving = useRef(false);
  const remote = useRef<Cloud | null>(null);
  useEffect(() => {
    current.current = workspace;
  }, [workspace]);
  const persist = useCallback(
    (value: TerminalWorkspace) => {
      try {
        localStorage.setItem(
          `shortlong:terminal:v3:${owner}`,
          JSON.stringify({
            workspace: value,
            saved: saved.current,
            revision: revision.current,
            savedDrawings: savedDrawings.current,
          }),
        );
        return true;
      } catch {
        setStatus("Хранилище браузера недоступно · экспортируйте JSON");
        return false;
      }
    },
    [owner],
  );
  const update = useCallback(
    (
      change: TerminalWorkspace | ((w: TerminalWorkspace) => TerminalWorkspace),
    ) => {
      edit.current++;
      setWorkspace((w) => (typeof change === "function" ? change(w) : change));
      setStatus("Есть изменения · сохранено локально");
    },
    [],
  );
  useEffect(() => {
    const changed = (event: Event) => {
      if ((event as CustomEvent<string>).detail !== owner) return;
      edit.current++;
      setStatus("Есть изменения · сохранено локально");
    };
    window.addEventListener("shortlong:drawings-changed", changed);
    return () =>
      window.removeEventListener("shortlong:drawings-changed", changed);
  }, [owner]);
  useEffect(() => {
    persist(workspace);
  }, [workspace, persist]);
  useEffect(() => {
    if (!authenticated) return;
    const abort = new AbortController();
    void http<Cloud>("/chart/terminal", { signal: abort.signal })
      .then((value) => {
        if (abort.signal.aborted) return;
        remote.current = value;
        const local = initial;
        const dirty =
          (local.saved !== undefined &&
            local.saved !== JSON.stringify(local.workspace)) ||
          (local.savedDrawings !== undefined &&
            local.savedDrawings !==
              JSON.stringify(
                normalizeDrawingSets(
                  JSON.parse(
                    localStorage.getItem(`shortlong:charts:v2:${owner}`) ??
                      "{}",
                  ),
                ),
              ));
        if (dirty && local.revision && local.revision !== value.revision) {
          setConflict(true);
          setStatus("В аккаунте другая версия · локальные изменения сохранены");
          return;
        }
        revision.current = value.revision;
        if (!edit.current && !dirty && value.workspace) {
          const next = normalizeWorkspace(value.workspace);
          saved.current = JSON.stringify(next);
          setWorkspace(next);
          persist(next);
          setStatus("Сохранено в аккаунте");
        }
      })
      .catch(() => {
        if (!abort.signal.aborted)
          setStatus("Сервер недоступен · сохранено в браузере");
      });
    return () => abort.abort();
  }, [authenticated, persist, initial, owner]);
  const save = useCallback(async () => {
    if (saving.current) return;
    if (!persist(current.current)) return;
    if (!authenticated) {
      setStatus("Сохранено в браузере · войдите для синхронизации");
      return;
    }
    if (conflict) {
      setStatus(
        "Конфликт версий · сначала загрузите версию аккаунта или экспортируйте локальную",
      );
      return;
    }
    saving.current = true;
    const snapshot = current.current,
      serialized = JSON.stringify(snapshot),
      generation = edit.current;
    setStatus("Сохраняется…");
    try {
      const storedDrawings = normalizeDrawingSets(
        JSON.parse(
          localStorage.getItem(`shortlong:charts:v2:${owner}`) ?? "{}",
        ),
      );
      const drawingSets = {
        ...normalizeDrawingSets(remote.current?.drawing_sets),
        ...storedDrawings,
      };
      const body = JSON.stringify({
        workspace: snapshot,
        revision: revision.current,
        drawing_sets: drawingSets,
      });
      if (new TextEncoder().encode(body).length > 250000) {
        setStatus(
          "Сохранено в браузере · набор превышает лимит аккаунта, экспортируйте JSON",
        );
        return;
      }
      const result = await http<Cloud>("/chart/terminal", {
        method: "PUT",
        body,
      });
      remote.current = result;
      revision.current = result.revision;
      saved.current = serialized;
      savedDrawings.current = JSON.stringify(storedDrawings);
      persist(current.current);
      setStatus(
        generation === edit.current
          ? "Сохранено в аккаунте"
          : "Есть новые изменения · сохранено локально",
      );
    } catch (e) {
      if ((e as HttpError).status === 409) {
        setConflict(true);
        setStatus("Конфликт версий · локальные изменения сохранены");
      } else setStatus("Ошибка сервера · сохранено в браузере");
    } finally {
      saving.current = false;
    }
  }, [authenticated, conflict, persist, owner]);
  const loadRemote = useCallback(async () => {
    const result = await http<Cloud>("/chart/terminal");
    if (!result.workspace) return;
    localStorage.setItem(
      `shortlong:terminal:v3:${owner}:backup`,
      JSON.stringify({
        workspace: current.current,
        drawings: localStorage.getItem(`shortlong:charts:v2:${owner}`),
      }),
    );
    if (result.drawing_sets) {
      localStorage.setItem(
        `shortlong:charts:v2:${owner}`,
        JSON.stringify(normalizeDrawingSets(result.drawing_sets)),
      );
      window.dispatchEvent(new Event("shortlong:drawings-restored"));
    }
    remote.current = result;
    revision.current = result.revision;
    const next = normalizeWorkspace(result.workspace);
    saved.current = JSON.stringify(next);
    savedDrawings.current = JSON.stringify(
      normalizeDrawingSets(result.drawing_sets),
    );
    edit.current++;
    setWorkspace(next);
    persist(next);
    setConflict(false);
    setStatus("Загружено из аккаунта");
  }, [persist, owner]);
  return { workspace, update, save, loadRemote, status, conflict };
}
