import { useEffect, useRef, useState } from "react";
import { useNavigate, Link } from "react-router-dom";
import { useAuth } from "../../shared/auth/AuthContext";
import { useToast } from "../../shared/ui/ToastProvider";
import TurnstileField from "../../shared/ui/TurnstileField";

export default function LoginPage() {
  const navigate = useNavigate();
  const { login, resendVerification, verifyTwoFALogin } = useAuth();
  const toast = useToast();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [resending, setResending] = useState(false);
  const [info, setInfo] = useState<string | null>(null);
  const [needsVerification, setNeedsVerification] = useState(false);
  const [twoFARequired, setTwoFARequired] = useState(false);
  const [twoFACode, setTwoFACode] = useState("");
  const [twoFAChallenge, setTwoFAChallenge] = useState("");
  const [captchaToken, setCaptchaToken] = useState("");
  const [captchaNeeded, setCaptchaNeeded] = useState(false);
  const prevEmailRef = useRef("");
  const prevPasswordRef = useRef("");
  const emailValue = email.trim();
  const emailValid = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(emailValue);

  function mapError(code: string) {
    switch (code) {
      case "email_not_verified":
        return "Email не подтверждён. Отправь код ещё раз.";
      case "email_send_failed":
        return "Не удалось отправить код. Проверь SMTP настройки.";
      case "password_gen_failed":
      case "password_update_failed":
      case "user_lookup_failed":
        return "Не удалось сбросить пароль. Попробуй позже.";
      case "invalid_input":
        return "Укажи корректный email.";
      case "invalid_credentials":
        return "Неверный email или пароль.";
      case "twofa_send_failed":
        return "Не удалось отправить код 2FA. Попробуй позже.";
      case "invalid_code":
        return "Неверный код подтверждения.";
      case "code_expired":
        return "Код истёк. Отправь новый.";
      case "twofa_disabled":
        return "2FA выключена для этого аккаунта.";
      case "rate_limited":
        return "Слишком много попыток. Подожди пару минут.";
      case "captcha_required":
        return "Подтверди, что ты не робот.";
      case "captcha_invalid":
        return "Капча не пройдена. Попробуй ещё раз.";
      default:
        return code;
    }
  }

  async function submitLogin() {
    setInfo(null);
    setNeedsVerification(false);
    if (twoFARequired) {
      await onVerifyTwoFA();
      return;
    }
    if (!emailValid) {
      toast.error("Проверь email — формат должен быть вроде name@example.com.");
      return;
    }

    const siteKey = import.meta.env.VITE_TURNSTILE_SITE_KEY as string | undefined;
    const captchaRequired = !!siteKey && captchaNeeded;
    if (captchaRequired && !captchaToken.trim()) {
      toast.error(mapError("captcha_required"));
      return;
    }

    setSubmitting(true);
    try {
      const res = await login(email, password, captchaToken.trim() || undefined);
      if (res.requiresTwoFA) {
        setTwoFARequired(true);
        setTwoFAChallenge(res.challenge || "");
        setInfo("Мы отправили код 2FA на email. Введи его ниже.");
        return;
      }
      navigate("/account");
    } catch (err) {
      const code = err instanceof Error ? err.message : "login_failed";
      toast.error(mapError(code));
      setNeedsVerification(code === "email_not_verified");
      if (code === "captcha_required" || code === "captcha_failed" || code === "captcha_invalid") {
        setCaptchaNeeded(true);
        setCaptchaToken("");
      }
    } finally {
      setSubmitting(false);
    }
  }

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    await submitLogin();
  }

  async function onVerifyTwoFA() {
    setInfo(null);
    if (!twoFACode.trim() || !twoFAChallenge) {
      toast.error("Введи код подтверждения.");
      return;
    }
    setSubmitting(true);
    try {
      await verifyTwoFALogin(email, twoFACode.trim(), twoFAChallenge);
      navigate("/account");
    } catch (err) {
      const code = err instanceof Error ? err.message : "verify_failed";
      toast.error(mapError(code));
    } finally {
      setSubmitting(false);
    }
  }

  useEffect(() => {
    const prevEmail = prevEmailRef.current;
    const prevPassword = prevPasswordRef.current;
    if (twoFARequired && (email !== prevEmail || password !== prevPassword)) {
      setTwoFACode("");
      setTwoFAChallenge("");
      setTwoFARequired(false);
      setInfo(null);
    }
    prevEmailRef.current = email;
    prevPasswordRef.current = password;
  }, [email, password, twoFARequired]);

  async function onResend() {
    if (!email) return;
    setInfo(null);
    setResending(true);
    try {
      await resendVerification(email);
      setInfo("Код отправлен на email. Проверь почту.");
    } catch (err) {
      const code = err instanceof Error ? err.message : "resend_failed";
      toast.error(mapError(code));
    } finally {
      setResending(false);
    }
  }

  return (
    <div
      data-reveal-scope="deep"
      className="min-h-screen bg-background text-foreground flex items-center justify-center p-4"
    >
      <div className="w-full max-w-md rounded-2xl border border-border bg-card text-card-foreground p-6">
        <h1 className="text-2xl font-semibold">Вход</h1>
        <p className="mt-1 text-sm text-muted-foreground">Используй email и пароль.</p>

        <form onSubmit={onSubmit} className="mt-6 space-y-4">
          {!twoFARequired ? (
            <>
              <label className="block">
                <span className="text-sm text-muted-foreground">Email</span>
                <input
                  className="mt-1 w-full rounded-xl bg-background border border-input px-3 py-2 outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30"
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  autoComplete="email"
                  required
                />
                {emailValue.length > 0 && !emailValid ? (
                  <div className="mt-2 text-xs text-amber-700 dark:text-amber-300">
                    Нужен корректный email, например name@example.com.
                  </div>
                ) : null}
              </label>

              <label className="block">
                <span className="text-sm text-muted-foreground">Пароль</span>
                <input
                  className="mt-1 w-full rounded-xl bg-background border border-input px-3 py-2 outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="current-password"
                  required
                />
              </label>
            </>
          ) : (
            <>
              <div className="rounded-xl border border-border bg-secondary px-3 py-2 text-sm text-muted-foreground">
                Введи код двухфакторной аутентификации, который пришёл на email.
              </div>

              <label className="block">
                <span className="text-sm text-muted-foreground">Код 2FA</span>
                <input
                  className="mt-1 w-full rounded-xl bg-background border border-input px-3 py-2 outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30 tracking-[0.3em] text-center"
                  type="text"
                  value={twoFACode}
                  onChange={(e) => setTwoFACode(e.target.value)}
                  inputMode="numeric"
                  maxLength={6}
                />
              </label>

              <div className="text-sm text-muted-foreground">
                Код отправлен на <span className="text-foreground">{emailValue}</span>.
              </div>
            </>
          )}

          {info && (
            <div className="rounded-xl border border-border bg-secondary px-3 py-2 text-sm text-muted-foreground">
              {info}
            </div>
          )}

          {needsVerification && (
            <button
              type="button"
              onClick={onResend}
              disabled={resending}
              className="w-full rounded-xl border border-border bg-secondary text-secondary-foreground py-2 text-sm hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-60"
            >
              {resending ? "Отправляем..." : "Отправить код подтверждения"}
            </button>
          )}

          {twoFARequired && (
            <button
              type="button"
              onClick={async () => {
                setInfo(null);
                try {
                  const res = await login(email, password);
                  if (res.requiresTwoFA) {
                    setInfo("Код отправлен повторно на email.");
                  }
                } catch (err) {
                  const code = err instanceof Error ? err.message : "login_failed";
                  toast.error(mapError(code));
                }
              }}
              className="w-full rounded-xl border border-border bg-secondary text-secondary-foreground py-2 text-sm hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-60"
            >
              Отправить код 2FA ещё раз
            </button>
          )}

          {twoFARequired ? (
            <button
              disabled={submitting}
              className="w-full rounded-xl bg-primary text-primary-foreground py-2 font-medium hover:bg-primary-hover focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-60"
              type="button"
              onClick={onVerifyTwoFA}
            >
              {submitting ? "Проверяем..." : "Подтвердить вход"}
            </button>
          ) : (
            <button
              disabled={submitting}
              className="w-full rounded-xl bg-primary text-primary-foreground py-2 font-medium hover:bg-primary-hover focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-60"
              type="submit"
            >
              {submitting ? "Входим..." : "Войти"}
            </button>
          )}

          {captchaNeeded ? <TurnstileField token={captchaToken} onToken={setCaptchaToken} /> : null}
        </form>

        <div className="mt-4 flex items-center justify-between text-sm text-muted-foreground">
          <button
            type="button"
            className="underline"
            onClick={() => navigate("/forgot-password")}
          >
            Забыли пароль?
          </button>
          <div>
            Нет аккаунта?{" "}
            <Link className="text-foreground underline" to="/register">
              Регистрация
            </Link>
          </div>
        </div>
      </div>
    </div>
  );
}
