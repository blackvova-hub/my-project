import { useState } from "react";
import { http } from "../../shared/api/http";
import type { Insight } from "./types";
import { Icon } from "./ui";
import { displayLabel, errorMessage } from "./locale";

export function Explanation({
  insight,
  demo,
}: {
  insight: Insight;
  demo: boolean;
}) {
  const [reply, setReply] = useState<{ text: string; source: string } | null>(
      null,
    ),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function explain() {
    setBusy(true);
    setError("");
    try {
      const result = await http<{ text: string; source: string }>(
        "/analytics/explain",
        {
          method: "POST",
          body: JSON.stringify({ type: insight.type, ids: insight.trades }),
        },
      );
      setReply(result);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  if (demo)
    return (
      <p className="an-footnote">
        {" "}
        Пояснения ИИ доступны для подключённых счетов. В демо используются
        пояснения по правилам анализа.{" "}
      </p>
    );
  return (
    <div className="an-ai-explanation">
      {reply ? (
        <>
          <h3>{displayLabel(reply.source)}</h3>
          <p>{reply.text}</p>
        </>
      ) : (
        <>
          <button
            className="an-button"
            disabled={busy || !insight.trades.length}
            onClick={() => void explain()}
          >
            <Icon name="insights" />
            {busy ? "Готовим пояснение…" : "Получить пояснение ИИ"}
          </button>
          <p className="an-footnote">
            {" "}
            Провайдеру ИИ отправляются только рассчитанные сводные показатели.
            API-ключи и отдельные исполнения остаются на сервере.{" "}
          </p>
        </>
      )}
      {error ? (
        <p className="an-error" role="alert">
          {error}
        </p>
      ) : null}
    </div>
  );
}
