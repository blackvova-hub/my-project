import { useState } from "react";
import TurnstileWidget from "./TurnstileWidget";

const STORAGE_KEY = "captcha_gate_v1";

export default function CaptchaGate({ children }: { children: React.ReactNode }) {
  const siteKey = import.meta.env.VITE_TURNSTILE_SITE_KEY as string | undefined;
  const [ready] = useState(true);
  const [passed, setPassed] = useState(() => {
    if (!siteKey) return true;
    try {
      return localStorage.getItem(STORAGE_KEY) === "1";
    } catch {
      return false;
    }
  });
  const [error, setError] = useState<string | null>(null);

  if (!ready) {
    return (
      <div className="min-h-screen bg-background text-foreground flex items-center justify-center">
        Загрузка...
      </div>
    );
  }

  if (passed) {
    return <>{children}</>;
  }

  return (
    <div className="min-h-screen bg-background text-foreground flex items-center justify-center p-6">
      <div className="w-full max-w-md rounded-2xl border border-border bg-card text-card-foreground p-6 text-center">
        <div className="text-xs uppercase tracking-wide text-muted-foreground">Проверка безопасности</div>
        <h1 className="mt-3 text-2xl font-semibold">Загрузка</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          Подтверди, что ты не робот, чтобы продолжить.
        </p>
        <div className="mt-5 flex justify-center">
          <div className="rounded-xl border border-border bg-secondary p-3">
            <TurnstileWidget
              siteKey={siteKey!}
              onSuccess={() => {
                try {
                  localStorage.setItem(STORAGE_KEY, "1");
                } catch {
                  // ignore
                }
                setError(null);
                setPassed(true);
              }}
              onExpire={() => setError("Капча истекла. Попробуй ещё раз.")}
              onError={() => setError("Не удалось загрузить капчу. Обнови страницу.")}
            />
          </div>
        </div>
        {error ? <div className="mt-3 text-xs text-amber-700 dark:text-amber-300">{error}</div> : null}
      </div>
    </div>
  );
}
