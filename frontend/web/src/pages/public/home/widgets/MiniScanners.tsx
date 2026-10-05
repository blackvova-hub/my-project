import { useState } from "react";
import { AnimatePresence, LayoutGroup, motion, useReducedMotion } from "framer-motion";
import type { SignalRow } from "../../scanners/types";
import { MiniSignalsTable } from "./MiniSignalsTable";
import {
  useCloseTrade,
  useCreateTrade,
  useSignals,
  useTradeMeta,
  useTradeStrategies,
  useTrades,
} from "../../scanners/queries";
import { useToast } from "../../../../shared/ui/ToastProvider";
export function MiniScanners(props: {
  userKey: string;
  slot?: "SLOT_1" | "SLOT_2" | "SLOT_3";
}) {
  const reduceMotion = useReducedMotion();
  const [selectedSlot, setSelectedSlot] = useState<"SLOT_1" | "SLOT_2" | "SLOT_3" | "ALL">(
    props.slot ?? "ALL"
  );
  const signalsLimit = 15;
  const activeSlotParam =
    selectedSlot === "ALL" ? undefined : selectedSlot === "SLOT_3" ? "3" : selectedSlot === "SLOT_2" ? "2" : "1";
  const activeScopeLabel = (() => {
    if (selectedSlot === "ALL") return "Все сканеры";
    if (selectedSlot === "SLOT_3") return "Сканер 3";
    if (selectedSlot === "SLOT_2") return "Сканер 2";
    return "Сканер 1";
  })();
  void activeScopeLabel;
  const toast = useToast();
  const signalsQuery = useSignals(props.userKey, signalsLimit, activeSlotParam);
  const tradesQuery = useTrades(props.userKey, "OPEN", 200);
  const strategiesQuery = useTradeStrategies(props.userKey);
  const tradeMetaQuery = useTradeMeta(props.userKey);
  const createTrade = useCreateTrade(props.userKey);
  const closeTrade = useCloseTrade(props.userKey);
  return (
    <section className="rounded-2xl border border-border bg-surface-raised p-5 md:p-6 min-w-0 h-[720px] flex flex-col">
      <div className="min-h-0 flex-1 flex flex-col">
        <div className="mb-2 flex flex-wrap items-center justify-between gap-3">
          <div className="text-base font-semibold text-foreground">Последние сигналы</div>
        </div>
        <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
          <LayoutGroup id="mini-scanner-tabs">
            <div className="relative inline-flex flex-wrap rounded-xl border border-border bg-background p-1">
              {([
                ["SLOT_1", "Сканер 1"],
                ["SLOT_2", "Сканер 2"],
                ["SLOT_3", "Сканер 3"],
                ["ALL", "Все сканеры"],
              ] as const).map(([value, label]) => {
                const active = selectedSlot === value;
                return (
                  <motion.button
                    key={value}
                    type="button"
                    onClick={() => setSelectedSlot(value)}
                    whileHover={reduceMotion ? undefined : { y: -1 }}
                    whileTap={reduceMotion ? undefined : { scale: 0.97 }}
                    className={"relative isolate rounded-lg px-3 py-1.5 text-xs font-semibold focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring transition-colors duration-200 " + (active ? "text-primary" : "text-muted-foreground hover:text-foreground")}
                  >
                    {active ? (
                      <motion.span
                        layoutId="mini-scanner-tab-active"
                        className="absolute inset-0 -z-10 rounded-lg border border-border-strong bg-accent"
                        transition={reduceMotion ? { duration: 0 } : { type: "spring", stiffness: 560, damping: 38, mass: 0.65 }}
                      />
                    ) : null}
                    <span className="relative z-10">{label}</span>
                  </motion.button>
                );
              })}
            </div>
          </LayoutGroup>
        </div>
        <AnimatePresence mode="wait" initial={false}>
          <motion.div
            key={selectedSlot}
            className="min-h-0 flex-1"
            initial={reduceMotion ? false : { opacity: 0, x: 14, filter: "blur(3px)" }}
            animate={{ opacity: 1, x: 0, filter: "blur(0px)" }}
            exit={reduceMotion ? undefined : { opacity: 0, x: -10, filter: "blur(2px)" }}
            transition={{ duration: reduceMotion ? 0 : 0.18, ease: [0.22, 1, 0.36, 1] }}
          >
          <MiniSignalsTable
            rows={(signalsQuery.data ?? []) as SignalRow[]}
            trades={tradesQuery.data ?? []}
            strategies={strategiesQuery.data ?? []}
            tradeMeta={tradeMetaQuery.data}
            onBuy={async (signalId: string) => {
              try {
                const trade = await createTrade.mutateAsync({ signal_id: signalId });
                toast.success("Buy зафиксирован.");
                return trade;
              } catch (err) {
                const message = err instanceof Error ? err.message : "Не удалось создать сделку.";
                toast.error(message);
                throw err;
              }
            }}
            onCloseTrade={async (tradeId, input) => {
              try {
                const trade = await closeTrade.mutateAsync({ id: tradeId, input });
                toast.success("Сделка сохранена.");
                return trade;
              } catch (err) {
                const message = err instanceof Error ? err.message : "Не удалось сохранить сделку.";
                toast.error(message);
                throw err;
              }
            }}
            isTradeBusy={createTrade.isPending || closeTrade.isPending}
          />
          </motion.div>
        </AnimatePresence>
      </div>
    </section>
  );
}
