import { Link, Outlet, useLocation } from "react-router-dom"
import { lazy, Suspense, useEffect, useMemo, useState } from "react"
import Header from "../shared/ui/Header"
import { Footer } from "../shared/ui/Footer"
import { ToastProvider } from "../shared/ui/ToastProvider"
import { useAuth } from "../shared/auth/AuthContext"
import { hasPlan } from "../shared/auth/plan"

const AiAssistantWidget = lazy(() =>
  import("../shared/ui/AiAssistantWidget").then((module) => ({
    default: module.AiAssistantWidget,
  })),
)

type CookieConsentChoice = "all" | "essential";

const COOKIE_CONSENT_KEY = "cookie_consent";
const LEGACY_COOKIE_ACCEPTED_KEY = "cookies_accepted";
const MOBILE_NOTICE_SHOWN_KEY = "mobile_notice_shown_v1";

function getCookieConsentKey(userNumID?: number | null) {
  if (typeof userNumID === "number" && Number.isFinite(userNumID)) {
    return `${COOKIE_CONSENT_KEY}_u_${userNumID}`;
  }
  return COOKIE_CONSENT_KEY;
}

function readCookieConsent(key: string): CookieConsentChoice | null {
  try {
    const raw = localStorage.getItem(key);
    if (raw === "all" || raw === "essential") return raw;
    return null;
  } catch {
    return null;
  }
}

function writeCookieConsent(key: string, choice: CookieConsentChoice) {
  try {
    localStorage.setItem(key, choice);
  } catch {
    // ignore storage errors (private mode, quota, etc.)
  }
}

function readEffectiveCookieConsent(userKey: string): CookieConsentChoice | null {
  const stored = readCookieConsent(userKey) ?? readCookieConsent(getCookieConsentKey(null));
  if (stored) return stored;
  try {
    return localStorage.getItem(LEGACY_COOKIE_ACCEPTED_KEY) === "1" ? "all" : null;
  } catch {
    return null;
  }
}

function migrateLegacyCookieConsent(userKey: string) {
  if (readCookieConsent(userKey) || readCookieConsent(getCookieConsentKey(null))) return;
  try {
    if (localStorage.getItem(LEGACY_COOKIE_ACCEPTED_KEY) !== "1") return;
  } catch {
    return;
  }
  writeCookieConsent(getCookieConsentKey(null), "all");
  writeCookieConsent(userKey, "all");
}

export function PublicLayout() {
  const { isAuth, user } = useAuth();
  const location = useLocation();
  const isAccountPage = location.pathname.startsWith("/account");
  const isReplayPage = location.pathname.startsWith("/replay");
  const isBacktestPage = location.pathname.startsWith("/backtest") || location.pathname.startsWith("/replay");
  const [showMobileNotice, setShowMobileNotice] = useState(false);
  const userConsentKey = getCookieConsentKey(user?.num_id ?? null);
  const persistedCookieConsent = useMemo(
    () => readEffectiveCookieConsent(userConsentKey),
    [userConsentKey],
  );
  const [confirmedConsent, setConfirmedConsent] = useState<{
    key: string;
    choice: CookieConsentChoice;
  } | null>(null);
  const cookieConsent =
    confirmedConsent?.key === userConsentKey ? confirmedConsent.choice : persistedCookieConsent;

  useEffect(() => {
    migrateLegacyCookieConsent(userConsentKey);
  }, [userConsentKey]);

  const confirmCookieConsent = (choice: CookieConsentChoice) => {
    writeCookieConsent(userConsentKey, choice);
    if (isAuth && typeof user?.num_id === "number") {
      writeCookieConsent(getCookieConsentKey(null), choice);
    }
    setConfirmedConsent({ key: userConsentKey, choice });
  };

  useEffect(() => {
    if (isAccountPage || isBacktestPage) return;
    try {
      if (
        localStorage.getItem(MOBILE_NOTICE_SHOWN_KEY) === "1" ||
        localStorage.getItem("mobile_notice_dismissed") === "1"
      ) {
        return;
      }
    } catch {
      // If storage is unavailable, the notice can still be shown for this mount.
    }

    const check = () => {
      const isMobile = window.innerWidth < 768;
      if (!isMobile) return;
      try {
        localStorage.setItem(MOBILE_NOTICE_SHOWN_KEY, "1");
      } catch {
        // ignore storage errors (private mode, quota, etc.)
      }
      setShowMobileNotice(true);
      window.removeEventListener("resize", check);
    };
    window.addEventListener("resize", check);
    check();
    return () => window.removeEventListener("resize", check);
  }, [isAccountPage, isBacktestPage]);

  return (
    <ToastProvider>
      <div
        className={
          "relative min-h-screen bg-background text-foreground " +
          (isReplayPage ? "replay-layout " : "") +
          "overflow-x-clip "
        }
      >
        <div className="relative z-10">
          <Header
            staticPosition={isBacktestPage}
          />
          <main data-reveal-scope={isBacktestPage ? undefined : "deep"}>
            <Outlet />
          </main>
          {isAuth && !isBacktestPage && hasPlan(user?.plan, "standard") ? (
            <Suspense fallback={null}>
              <AiAssistantWidget />
            </Suspense>
          ) : null}
          {!isReplayPage ? <Footer /> : null}
        </div>

        {showMobileNotice && !isAccountPage && !isBacktestPage ? (
          <div className="fixed inset-0 z-[60] flex items-center justify-center bg-scrim px-6 py-10">
            <div className="w-full max-w-sm rounded-2xl border border-border bg-popover p-5 text-center text-popover-foreground shadow-lg">
              <div className="text-lg font-semibold text-popover-foreground">
                Внимание
              </div>
              <p className="mt-3 text-sm text-muted-foreground">
                Этот сайт пока не адаптирован под телефоны. Открывайте на планшете или компьютере для лучшего опыта.
              </p>
              <button
                type="button"
                onClick={() => {
                  setShowMobileNotice(false);
                }}
                className="mt-5 w-full rounded-xl bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground transition hover:bg-primary-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
              >
                Понятно
              </button>
            </div>
          </div>
        ) : null}

        {!cookieConsent ? (
          <div className="fixed bottom-4 left-4 right-4 z-[55] md:left-6 md:right-6">
            <div className="mx-auto w-full max-w-4xl rounded-2xl border border-border bg-popover p-3 text-popover-foreground shadow-lg md:p-4">
              <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
                <div className="flex items-center gap-3">
                  <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-muted text-lg leading-none">
                    🍪
                  </div>
                  <div className="text-[15px] text-popover-foreground">
                    Этот сайт использует файлы cookie. Читайте подробнее в{" "}
                    <Link
                      to="/cookies"
                      className="text-primary underline underline-offset-2 hover:text-primary-hover focus-visible:outline-2 focus-visible:outline-ring"
                    >
                      нашей политике cookie
                    </Link>
                    {" "}и{" "}
                    <Link
                      to="/privacy"
                      className="text-primary underline underline-offset-2 hover:text-primary-hover focus-visible:outline-2 focus-visible:outline-ring"
                    >
                      политике конфиденциальности
                    </Link>
                    .
                  </div>
                </div>

                <div className="flex flex-wrap gap-2 md:justify-end">
                  <button
                    type="button"
                    onClick={() => confirmCookieConsent("essential")}
                    className="rounded-xl border border-border bg-secondary px-5 py-2 text-sm font-semibold text-secondary-foreground transition hover:bg-accent hover:text-accent-foreground focus-visible:outline-2 focus-visible:outline-ring"
                  >
                    Не разрешать
                  </button>
                  <button
                    type="button"
                    onClick={() => confirmCookieConsent("all")}
                    className="rounded-xl bg-primary px-5 py-2 text-sm font-semibold text-primary-foreground transition hover:bg-primary-hover focus-visible:outline-2 focus-visible:outline-ring"
                  >
                    Принять все
                  </button>
                </div>
              </div>
            </div>
          </div>
        ) : null}
      </div>
    </ToastProvider>
  );
}
