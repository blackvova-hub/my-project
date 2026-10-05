import { useEffect, useState } from "react";
import { useNavigate, Link } from "react-router-dom";
import { useAuth } from "../../shared/auth/AuthContext";
import TurnstileField from "../../shared/ui/TurnstileField";

export default function RegisterPage() {
  const navigate = useNavigate();
  const { register, verifyEmail } = useAuth();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [captchaToken, setCaptchaToken] = useState("");
  const [captchaGeneration, setCaptchaGeneration] = useState(0);
  const [submitting, setSubmitting] = useState(false);
  const [verifying, setVerifying] = useState(false);
  const [codeSent, setCodeSent] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [info, setInfo] = useState<string | null>(null);
  const [acceptTerms, setAcceptTerms] = useState(false);
  const [acceptPrivacy, setAcceptPrivacy] = useState(false);
  const [acceptCookies, setAcceptCookies] = useState(false);

  const siteKey = import.meta.env.VITE_TURNSTILE_SITE_KEY as string | undefined;
  const captchaRequired = !!siteKey;

  function mapError(code: string) {
    switch (code) {
      case "email_taken":
        return "Email уже зарегистрирован.";
      case "invalid_input":
      case "invalid_json":
        return "Пароль должен быть минимум 8 символов и содержать строчные, заглавные буквы и цифры.";
      case "email_send_failed":
        return "Не удалось отправить код. Проверь настройки SMTP и попробуй ещё раз.";
      case "invalid_code":
        return "Неверный код подтверждения.";
      case "code_expired":
        return "Код истёк. Отправь новый.";
      case "rate_limited":
        return "Слишком много попыток. Подожди пару минут.";
      case "captcha_required":
        return "Подтверди, что ты не робот.";
      case "captcha_failed":
      case "captcha_invalid":
        return "Капча не пройдена. Попробуй ещё раз.";
      default:
        return code;
    }
  }

  const emailValue = email.trim();
  const emailValid = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(emailValue);
  const passwordRules = [
    { id: "length", label: "Минимум 8 символов", ok: password.length >= 8 },
    { id: "lower", label: "Строчные буквы", ok: /[a-z]/.test(password) },
    { id: "upper", label: "Заглавные буквы", ok: /[A-Z]/.test(password) },
    { id: "digit", label: "Цифры", ok: /\d/.test(password) },
  ];
  const missingRules = passwordRules.filter((rule) => !rule.ok);

  function canSendCode() {
    return emailValid && missingRules.length === 0 && acceptTerms && acceptPrivacy && acceptCookies;
  }

  function canVerify() {
    return code.trim().length > 0;
  }

  useEffect(() => {
    setCodeSent(false);
    setInfo(null);
    setError(null);
    setCode("");
  }, [email, password]);

  async function onSendCode() {
    setError(null);
    setInfo(null);
    if (!canSendCode()) {
      if (!emailValid) {
        setError("Проверь email — формат должен быть вроде name@example.com.");
      } else if (!acceptTerms || !acceptPrivacy || !acceptCookies) {
        setError("Подтверди согласие с Условиями, Политикой конфиденциальности и Политикой cookie.");
      } else {
        setError("Пароль слишком слабый. Добавь отсутствующие требования ниже.");
      }
      return;
    }

    if (captchaRequired && !captchaToken.trim()) {
      setError(mapError("captcha_required"));
      return;
    }

    setSubmitting(true);
    try {
      const res = await register(email, password, captchaToken.trim());
      if (res.requiresVerification) {
        setCodeSent(true);
        setCaptchaToken("");
        setInfo("Мы отправили код на email. Введи его ниже.");
        return;
      }
      navigate("/account");
    } catch (err) {
      const code = err instanceof Error ? err.message : "register_failed";
      setError(mapError(code));
      // Turnstile tokens are single-use. Replace the completed widget after
      // any failed request so a stale token can never be submitted again.
      setCaptchaToken("");
      setCaptchaGeneration((value) => value + 1);
    } finally {
      setSubmitting(false);
    }
  }

  async function onVerify(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setInfo(null);
    if (!codeSent) {
      setError("Сначала отправь код на email.");
      return;
    }
    if (!canVerify()) {
      setError("Введи код подтверждения.");
      return;
    }
    setVerifying(true);
    try {
      await verifyEmail(email, code);
      navigate("/account");
    } catch (err) {
      const code = err instanceof Error ? err.message : "verify_failed";
      setError(mapError(code));
    } finally {
      setVerifying(false);
    }
  }

  return (
    <div
      data-reveal-scope="deep"
      className="min-h-screen bg-background text-foreground flex items-center justify-center p-4"
    >
      <div className="w-full max-w-md rounded-2xl border border-border bg-card text-card-foreground p-6">
        <h1 className="text-2xl font-semibold">Регистрация</h1>
        <p className="mt-1 text-sm text-muted-foreground">Создай аккаунт и подтверди email.</p>

        <div className="mt-6 space-y-4">
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
            <span className="text-sm text-muted-foreground">Пароль (минимум 8 символов)</span>
            <input
              className="mt-1 w-full rounded-xl bg-background border border-input px-3 py-2 outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password"
              minLength={8}
              required
            />
            <div className="mt-2 flex flex-wrap gap-2 text-xs text-muted-foreground">
              {passwordRules.map((rule) => (
                <span
                  key={rule.id}
                  className={`rounded-full border px-2 py-0.5 ${
                    rule.ok
                      ? "border-primary/30 bg-primary/10 text-primary"
                      : "border-border bg-secondary text-muted-foreground"
                  }`}
                >
                  {rule.label}
                </span>
              ))}
            </div>
            {missingRules.length > 0 && password.length > 0 ? (
              <div className="mt-2 text-xs text-amber-700 dark:text-amber-300">
                Добавь: {missingRules.map((rule) => rule.label.toLowerCase()).join(", ")}
              </div>
            ) : null}
          </label>

          {!codeSent ? (
            <TurnstileField
              key={captchaGeneration}
              token={captchaToken}
              onToken={setCaptchaToken}
            />
          ) : null}

          <div className="rounded-xl border border-border bg-secondary p-3 text-sm text-card-foreground">
            <div className="mb-2 text-xs uppercase tracking-wide text-muted-foreground">Соглашения</div>

            <label className="mt-2 flex items-start gap-2">
              <input
                type="checkbox"
                checked={acceptTerms}
                onChange={(e) => setAcceptTerms(e.target.checked)}
                className="mt-0.5 h-4 w-4 rounded border-input bg-background accent-primary focus-visible:outline-2 focus-visible:outline-ring"
              />
              <span>
                Я принимаю{" "}
                <Link to="/terms" target="_blank" rel="noreferrer" className="underline text-foreground">
                  Пользовательское соглашение
                </Link>
                .
              </span>
            </label>

            <label className="mt-2 flex items-start gap-2">
              <input
                type="checkbox"
                checked={acceptPrivacy}
                onChange={(e) => setAcceptPrivacy(e.target.checked)}
                className="mt-0.5 h-4 w-4 rounded border-input bg-background accent-primary focus-visible:outline-2 focus-visible:outline-ring"
              />
              <span>
                Я ознакомлен(а) с{" "}
                <Link to="/privacy" target="_blank" rel="noreferrer" className="underline text-foreground">
                  Политикой конфиденциальности
                </Link>
                .
              </span>
            </label>

            <label className="mt-2 flex items-start gap-2">
              <input
                type="checkbox"
                checked={acceptCookies}
                onChange={(e) => setAcceptCookies(e.target.checked)}
                className="mt-0.5 h-4 w-4 rounded border-input bg-background accent-primary focus-visible:outline-2 focus-visible:outline-ring"
              />
              <span>
                Я ознакомлен(а) с{" "}
                <Link to="/cookies" target="_blank" rel="noreferrer" className="underline text-foreground">
                  Политикой cookie
                </Link>
                .
              </span>
            </label>
          </div>

          {!codeSent ? (
            <button
              disabled={submitting}
              className="w-full rounded-xl border border-border bg-secondary py-2 text-sm text-secondary-foreground hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-60"
              type="button"
              onClick={onSendCode}
            >
              {submitting ? "Отправляем..." : "Отправить код"}
            </button>
          ) : null}

          <form onSubmit={onVerify} className="space-y-4">
            <div className="text-sm text-muted-foreground">
              Введи код подтверждения из письма.
            </div>

            <label className="block">
              <span className="text-sm text-muted-foreground">Код подтверждения</span>
              <input
                className="mt-1 w-full rounded-xl bg-background border border-input px-3 py-2 outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30 tracking-[0.3em] text-center"
                type="text"
                value={code}
                onChange={(e) => setCode(e.target.value)}
                inputMode="numeric"
                maxLength={6}
                required
              />
            </label>

            {codeSent ? (
              <div className="text-sm text-muted-foreground">
                Код отправлен на <span className="text-foreground">{email}</span>.
              </div>
            ) : (
              <div className="text-sm text-muted-foreground">Сначала нажми «Отправить код».</div>
            )}

            <button
              disabled={verifying}
              className="w-full rounded-xl bg-primary text-primary-foreground py-2 font-medium hover:bg-primary-hover focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-60"
              type="submit"
            >
              {verifying ? "Проверяем..." : "Подтвердить"}
            </button>

          </form>

          {info && (
            <div className="rounded-xl border border-border bg-secondary px-3 py-2 text-sm text-muted-foreground">
              {info}
            </div>
          )}

          {error && (
            <div className="rounded-xl border border-destructive/30 bg-destructive/10 text-destructive px-3 py-2 text-sm">
              {error}
            </div>
          )}
        </div>

        <div className="mt-4 text-sm text-muted-foreground">
          Уже есть аккаунт?{" "}
          <Link className="text-foreground underline" to="/login">
            Вход
          </Link>
        </div>
      </div>
    </div>
  );
}
