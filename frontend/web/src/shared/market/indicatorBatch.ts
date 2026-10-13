import type { PerpCandle } from "./perpMarketData";
import type { ClassicIndicatorId } from "./indicatorTypes";
import { calculateIndicator, type IndicatorOutput } from "./indicatorEngine";
import type { StudySettings } from "../charts/studySettings";
export type StudyDefinition = StudySettings & {
  key: string;
  id: ClassicIndicatorId;
  period?: number;
  color?: string;
};
type WorkerRequest = {
  token: number;
  candles: PerpCandle[];
  barSeconds?: number;
  definitions: StudyDefinition[];
};
export type WorkerResponse = {
  token: number;
  output?: { key: string; indicator: IndicatorOutput }[];
  error?: string;
};
type Job = {
  request: WorkerRequest;
  signal?: AbortSignal;
  resolve: (output: NonNullable<WorkerResponse["output"]>) => void;
  reject: (error: Error) => void;
  abort: () => void;
};
let worker: Worker | null = null,
  token = 0,
  active: number | null = null;
const jobs = new Map<number, Job>();
function dispatch() {
  if (active !== null || !worker) return;
  const job = jobs.values().next().value as Job | undefined;
  if (!job) return;
  active = job.request.token;
  try {
    worker.postMessage(job.request);
  } catch (error) {
    jobs.delete(active);
    active = null;
    job.signal?.removeEventListener("abort", job.abort);
    job.reject(
      error instanceof Error ? error : new Error("Ошибка фонового расчёта"),
    );
    dispatch();
  }
}
function background(
  request: Omit<WorkerRequest, "token">,
  signal?: AbortSignal,
) {
  if (signal?.aborted)
    return Promise.reject(new DOMException("Расчёт отменён", "AbortError"));
  if (!worker) {
    worker = new Worker(new URL("./indicator.worker.ts", import.meta.url), {
      type: "module",
    });
    worker.onmessage = (event: MessageEvent<WorkerResponse>) => {
      const result = event.data,
        job = jobs.get(result.token);
      jobs.delete(result.token);
      active = null;
      if (job) {
        job.signal?.removeEventListener("abort", job.abort);
        if (result.error) job.reject(new Error(result.error));
        else job.resolve(result.output ?? []);
      }
      dispatch();
    };
    worker.onerror = () => {
      worker?.terminate();
      worker = null;
      active = null;
      for (const job of jobs.values()) {
        job.signal?.removeEventListener("abort", job.abort);
        job.reject(new Error("Ошибка фонового расчёта индикаторов"));
      }
      jobs.clear();
    };
  }
  const id = ++token;
  return new Promise<NonNullable<WorkerResponse["output"]>>(
    (resolve, reject) => {
      const abort = () => {
        // A running calculation may finish and show a valid recent snapshot.
        // Queued superseded snapshots are removed before expensive cloning.
        if (active === id) return;
        jobs.delete(id);
        signal?.removeEventListener("abort", abort);
        reject(new DOMException("Расчёт отменён", "AbortError"));
      };
      jobs.set(id, {
        request: { ...request, token: id },
        signal,
        resolve,
        reject,
        abort,
      });
      signal?.addEventListener("abort", abort, { once: true });
      dispatch();
    },
  );
}
export async function calculateIndicatorBatch(
  candles: PerpCandle[],
  definitions: StudyDefinition[],
  signal?: AbortSignal,
  barSeconds?: number,
) {
  const inWorker =
    typeof Worker !== "undefined" &&
    candles.length >= 2000 &&
    definitions.length > 0;
  const [base, extra] = await Promise.all([
    Promise.all(
      definitions
        .filter(() => !inWorker)
        .map(async (definition) => ({
          definition,
          indicator: await calculateIndicator(
            candles,
            definition.id,
            definition.period,
            barSeconds,
            definition,
          ),
        })),
    ),
    inWorker
      ? background(
          {
            candles,
            barSeconds,
            definitions,
          },
          signal,
        )
      : Promise.resolve([]),
  ]);
  const results = new Map(base.map((d) => [d.definition.key, d.indicator]));
  for (const value of extra) results.set(value.key, value.indicator);
  return definitions.map((definition) => ({
    definition,
    indicator: results.get(definition.key)!,
  }));
}
