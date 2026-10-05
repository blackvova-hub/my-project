// src/pages/public/scanners/ScannersPage.tsx
import { Navigate } from "react-router-dom";
import ScannerConfig from "./ScannerConfig";
import SignalsTable from "./SignalsTable";
import { useCloseTrade, useCreateTrade, useSignals, useTrades, useTradeStrategies, useTradeMeta } from "./queries";
import { useAuth } from "../../../shared/auth/AuthContext";
import { hasPlan } from "../../../shared/auth/plan";
import { useToast } from "../../../shared/ui/ToastProvider";
import type { ApiCloseTradeInput } from "./types";

function slotToParam(slot: "SLOT_1" | "SLOT_2" | "SLOT_3") {
  if (slot === "SLOT_3") return "3";
  if (slot === "SLOT_2") return "2";
  return "1";
}

export default function ScannersPage({ slot }: { slot: "SLOT_1" | "SLOT_2" | "SLOT_3" }) {
  const { user } = useAuth();
  const toast = useToast();
  const userKey = user?.id ?? "anon";
  const isStandard = hasPlan(user?.plan ?? null, "standard");
  const isPro = hasPlan(user?.plan ?? null, "pro");
  const signalsQuery = useSignals(userKey, 50, slotToParam(slot));
  const tradesQuery = useTrades(userKey, "OPEN", 200);
  const strategiesQuery = useTradeStrategies(userKey);
  const tradeMetaQuery = useTradeMeta(userKey);
  const createTrade = useCreateTrade(userKey);
  const closeTrade = useCloseTrade(userKey);

  if (slot === "SLOT_2" && !isStandard) {
    return <Navigate to="/scanners/1" replace />;
  }

  if (slot === "SLOT_3" && !isPro) {
    return <Navigate to="/scanners/1" replace />;
  }

  return (
    <div className="relative mx-auto min-h-screen max-w-7xl px-4 py-8 md:px-6">
      {/* Header */}
      <div className="relative mb-6">

        <h1 className="mt-3 text-3xl font-semibold tracking-tight text-foreground md:text-4xl">
          Крипто-сканер
        </h1>
      </div>

      <div className="relative flex flex-col gap-6">
        <ScannerConfig slot={slot} />
        <SignalsTable
          signals={signalsQuery.data ?? []}
          isLoading={signalsQuery.isLoading}
          error={signalsQuery.error}
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
          onCloseTrade={async (tradeId: string, input: ApiCloseTradeInput) => {
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
      </div>
    </div>
  );
}
