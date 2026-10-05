import { useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useAuth } from "../../shared/auth/AuthContext";

export default function ResetPasswordPage() {
  const { confirmResetPassword } = useAuth();
  const [params] = useSearchParams();
  const token = useMemo(() => params.get("token")?.trim() || "", [params]);
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const hasMinLength = password.length >= 8;
  const hasLower = /[a-z]/.test(password);
  const hasUpper = /[A-Z]/.test(password);
  const hasDigit = /\d/.test(password);
  const hasSpecial = /[^A-Za-z0-9]/.test(password);

  function mapError(code: string) {
    switch (code) {
      case "invalid_input":
        return "Слишком слабый пароль. Нужны заглавные и строчные буквы, цифры и спецсимволы.";
      case "reset_link_invalid":
        return "Ссылка недействительна или истекла. Запросите новую.";
      case "request_failed":
        return "Не удалось сменить пароль. Попробуйте еще раз.";
      default:
        return code;
    }
  }

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    if (!token) {
      setError("Ссылка недействительна или истекла. Запросите новую.");
      return;
    }
    if (password !== confirmPassword) {
      setError("Пароли не совпадают.");
      return;
    }
    if (!(hasMinLength && hasLower && hasUpper && hasDigit && hasSpecial)) {
      setError("Слишком слабый пароль. Добавьте заглавные и строчные буквы, цифры и спецсимволы.");
      return;
    }
    setSubmitting(true);
    try {
      await confirmResetPassword(token, password);
      setDone(true);
    } catch (err) {
      const code = err instanceof Error ? err.message : "request_failed";
      setError(mapError(code));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="min-h-screen bg-background text-foreground flex items-center justify-center p-4">
      <div className="w-full max-w-md rounded-2xl border border-border bg-card text-card-foreground p-6">
        <h1 className="text-2xl font-semibold">Новый пароль</h1>
        <form onSubmit={onSubmit} className="mt-6 space-y-4">
          <label className="block">
            <span className="text-sm text-muted-foreground">Новый пароль</span>
            <input className="mt-1 w-full rounded-xl bg-background border border-input px-3 py-2 outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30" type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
            <div className="mt-2 flex flex-wrap gap-2 text-xs text-muted-foreground">
              <span className={`rounded-full border px-2 py-0.5 ${hasMinLength ? "border-primary/30 bg-primary/10 text-primary" : "border-border bg-secondary text-muted-foreground"}`}>
                Минимум 8 символов
              </span>
              <span className={`rounded-full border px-2 py-0.5 ${hasLower ? "border-primary/30 bg-primary/10 text-primary" : "border-border bg-secondary text-muted-foreground"}`}>
                Строчные буквы
              </span>
              <span className={`rounded-full border px-2 py-0.5 ${hasUpper ? "border-primary/30 bg-primary/10 text-primary" : "border-border bg-secondary text-muted-foreground"}`}>
                Заглавные буквы
              </span>
              <span className={`rounded-full border px-2 py-0.5 ${hasDigit ? "border-primary/30 bg-primary/10 text-primary" : "border-border bg-secondary text-muted-foreground"}`}>
                Цифры
              </span>
              <span className={`rounded-full border px-2 py-0.5 ${hasSpecial ? "border-primary/30 bg-primary/10 text-primary" : "border-border bg-secondary text-muted-foreground"}`}>
                Спецсимволы
              </span>
            </div>
          </label>
          <label className="block">
            <span className="text-sm text-muted-foreground">Повтори пароль</span>
            <input className="mt-1 w-full rounded-xl bg-background border border-input px-3 py-2 outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30" type="password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)} required />
          </label>
          <button disabled={submitting || done} className="w-full rounded-xl bg-primary text-primary-foreground py-2 font-medium hover:bg-primary-hover focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-60" type="submit">
            {submitting ? "Сохраняем..." : "Сменить пароль"}
          </button>
        </form>
        {done && <div className="mt-4 rounded-xl border border-border bg-secondary px-3 py-2 text-sm text-muted-foreground">Пароль изменён. <Link className="underline" to="/login">Перейти ко входу</Link></div>}
        {error && <div className="mt-4 rounded-xl border border-destructive/30 bg-destructive/10 text-destructive px-3 py-2 text-sm">{error}</div>}
      </div>
    </div>
  );
}
