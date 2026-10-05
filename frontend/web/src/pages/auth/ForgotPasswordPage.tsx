import { useState } from "react";
import { Link } from "react-router-dom";
import { useAuth } from "../../shared/auth/AuthContext";
import TurnstileField from "../../shared/ui/TurnstileField";

export default function ForgotPasswordPage() {
  const { resetPassword } = useAuth();
  const [email, setEmail] = useState("");
  const [captchaToken, setCaptchaToken] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const siteKey = import.meta.env.VITE_TURNSTILE_SITE_KEY as string | undefined;
  const captchaRequired = !!siteKey;

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setError(null);

    if (captchaRequired && !captchaToken.trim()) {
      setError("captcha_required");
      setSubmitting(false);
      return;
    }

    try {
      await resetPassword(email.trim(), captchaToken.trim());
      setDone(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : "request_failed");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="min-h-screen bg-background text-foreground flex items-center justify-center p-4">
      <div className="w-full max-w-md rounded-2xl border border-border bg-card text-card-foreground p-6">
        <h1 className="text-2xl font-semibold">Восстановление пароля</h1>
        <p className="mt-1 text-sm text-muted-foreground">Укажи email, и мы отправим ссылку для сброса.</p>
        <form onSubmit={onSubmit} className="mt-6 space-y-4">
          <label className="block">
            <span className="text-sm text-muted-foreground">Email</span>
            <input
              className="mt-1 w-full rounded-xl bg-background border border-input px-3 py-2 outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
            />
          </label>

          <TurnstileField token={captchaToken} onToken={setCaptchaToken} />

          <button
            disabled={submitting}
            className="w-full rounded-xl bg-primary text-primary-foreground py-2 font-medium hover:bg-primary-hover focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-60"
            type="submit"
          >
            {submitting ? "Отправляем..." : "Отправить ссылку"}
          </button>
        </form>
        {done && (
          <div className="mt-4 rounded-xl border border-border bg-secondary px-3 py-2 text-sm text-muted-foreground">
            Если такой email существует, ссылка для восстановления отправлена.
          </div>
        )}
        {error && <div className="mt-4 rounded-xl border border-destructive/30 bg-destructive/10 text-destructive px-3 py-2 text-sm">{error}</div>}
        <div className="mt-4 text-sm text-muted-foreground">
          <Link className="underline" to="/login">
            Назад ко входу
          </Link>
        </div>
      </div>
    </div>
  );
}
