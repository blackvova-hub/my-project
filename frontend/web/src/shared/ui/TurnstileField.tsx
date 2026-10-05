import { useMemo, useState } from "react";
import TurnstileWidget from "./TurnstileWidget";

export type TurnstileFieldProps = {
  label?: string;
  token: string;
  onToken: (token: string) => void;
  className?: string;
};

export default function TurnstileField({ label = "Captcha", token, onToken, className }: TurnstileFieldProps) {
  const siteKey = import.meta.env.VITE_TURNSTILE_SITE_KEY as string | undefined;
  const enabled = useMemo(() => !!siteKey, [siteKey]);
  const [error, setError] = useState<string | null>(null);

  if (!enabled) return null;

  return (
    <div className={"rounded-xl border border-border bg-secondary p-3 " + (className ?? "")}>
      <div className="text-sm text-muted-foreground">{label}</div>
      <div className="mt-2 flex justify-center">
        <TurnstileWidget
          siteKey={siteKey!}
          onSuccess={(t) => {
            setError(null);
            onToken(t);
          }}
          onExpire={() => {
            onToken("");
            setError("Капча истекла. Попробуй ещё раз.");
          }}
          onError={() => {
            onToken("");
            setError("Не удалось загрузить капчу. Обнови страницу.");
          }}
        />
      </div>
      {error ? <div className="mt-2 text-xs text-amber-700 dark:text-amber-300">{error}</div> : null}

      {!token ? <div className="mt-2 text-xs text-muted-foreground">Требуется подтверждение.</div> : null}
    </div>
  );
}
