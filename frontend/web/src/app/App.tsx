import React, { Suspense, lazy } from "react";
import { Routes, Route, Navigate, useLocation, Link } from "react-router-dom";
import { PrimaryExchangeGate } from "../shared/exchange/PrimaryExchangeGate";
import { PublicLayout } from "../layouts/PublicLayout";
import { StandaloneLayout } from "../layouts/StandaloneLayout";
import { useAuth } from "../shared/auth/AuthContext";
import { hasPlan, type PlanLevel } from "../shared/auth/plan";
import { ScrollReveal } from "../shared/ui/ScrollReveal";

const DEV_AUTH_BYPASS =
  import.meta.env.DEV &&
  String(import.meta.env.VITE_DEV_AUTH_BYPASS).toLowerCase() === "true";

const RootHome = lazy(() => import("../pages/public/hero/RootHome"));
const LoginPage = lazy(() => import("../pages/auth/LoginPage"));
const RegisterPage = lazy(() => import("../pages/auth/RegisterPage"));
const AnalyticsPage = lazy(() => import("../pages/analytics/AnalyticsPage"));
const DashboardPage = lazy(() => import("../pages/admin/DashboardPage"));
const WalletRegistryPage = lazy(
  () => import("../pages/admin/WalletRegistryPage"),
);
const ScannersPageOne = lazy(
  () => import("../pages/public/scanners/ScannersPageOne"),
);
const ScannersPageTwo = lazy(
  () => import("../pages/public/scanners/ScannersPageTwo"),
);
const ScannersPageThree = lazy(
  () => import("../pages/public/scanners/ScannersPageThree"),
);
const NewsPage = lazy(() => import("../pages/public/news/NewsPage"));
const BacktestPage = lazy(
  () => import("../pages/public/backtest/BacktestPage"),
);
const ReplayPage = lazy(() => import("../pages/public/replay/ReplayPage"));
const CookiesPage = lazy(() => import("../pages/public/legal/CookiesPage"));
const PrivacyPage = lazy(() => import("../pages/public/legal/PrivacyPage"));
const TermsPage = lazy(() => import("../pages/public/legal/TermsPage"));
const ForgotPasswordPage = lazy(
  () => import("../pages/auth/ForgotPasswordPage"),
);
const ResetPasswordPage = lazy(() => import("../pages/auth/ResetPasswordPage"));

function RequireAuth({ children }: { children: React.ReactNode }) {
  const { isAuth, isLoading } = useAuth();
  const location = useLocation();

  if (isLoading && !isAuth) {
    return (
      <div className="min-h-screen bg-background text-foreground flex items-center justify-center">
        Загрузка...
      </div>
    );
  }

  if (DEV_AUTH_BYPASS) {
    return children;
  }

  if (!isAuth) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }

  return children;
}

function RequireAdmin({ children }: { children: React.ReactNode }) {
  const { user, isLoading } = useAuth();
  const location = useLocation();

  if (isLoading && !user) {
    return (
      <div className="min-h-screen bg-background text-foreground flex items-center justify-center">
        Загрузка...
      </div>
    );
  }

  if (!user) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }

  if (DEV_AUTH_BYPASS) {
    return children;
  }

  if (!user.isAdmin) {
    return <Navigate to="/account" replace />;
  }

  return children;
}

function AccessDenied({ minPlan }: { minPlan: PlanLevel }) {
  const label = minPlan === "pro" ? "PRO" : "Standard";
  return (
    <div className="min-h-[70vh] flex items-center justify-center px-4">
      <div className="w-full max-w-lg rounded-3xl border border-border bg-card p-8 text-center text-card-foreground">
        <div className="text-xs uppercase tracking-wide text-muted-foreground">
          Доступ ограничен
        </div>
        <h2 className="mt-3 text-2xl font-extrabold">Нужна подписка {label}</h2>
        <p className="mt-3 text-sm text-muted-foreground">
          Текущая подписка не даёт доступ к этому разделу. Перейди в кабинет,
          чтобы управлять планом.
        </p>
        <div className="mt-5 flex justify-center">
          <Link
            to="/account"
            className="rounded-xl border border-border bg-secondary px-4 py-2 text-sm font-semibold text-secondary-foreground hover:bg-accent hover:text-accent-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            Перейти в кабинет
          </Link>
        </div>
      </div>
    </div>
  );
}

function FeatureBlocked() {
  return (
    <div className="min-h-[70vh] flex items-center justify-center px-4">
      <div className="w-full max-w-lg rounded-3xl border border-border bg-card p-8 text-center text-card-foreground">
        <div className="text-xs uppercase tracking-wide text-muted-foreground">
          Раздел временно закрыт
        </div>
        <h2 className="mt-3 text-2xl font-extrabold">Страница в разработке</h2>
        <p className="mt-3 text-sm text-muted-foreground">
          Мы уже работаем над этим разделом. Он скоро станет доступен.
        </p>
        <div className="mt-5 flex justify-center">
          <Link
            to="/"
            className="rounded-xl border border-border bg-secondary px-4 py-2 text-sm font-semibold text-secondary-foreground hover:bg-accent hover:text-accent-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            На главную
          </Link>
        </div>
      </div>
    </div>
  );
}

function RequirePlan({
  minPlan,
  children,
}: {
  minPlan: PlanLevel;
  children: React.ReactNode;
}) {
  const { user, isLoading, isAuth } = useAuth();
  const location = useLocation();

  if (isLoading && !isAuth) {
    return (
      <div className="min-h-screen bg-background text-foreground flex items-center justify-center">
        Загрузка...
      </div>
    );
  }

  if (DEV_AUTH_BYPASS) {
    return children;
  }

  if (!isAuth) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }

  if (!hasPlan(user?.plan, minPlan)) {
    return <AccessDenied minPlan={minPlan} />;
  }

  return children;
}

export default function App() {
  const location = useLocation();
  return (
    <>
      <ScrollReveal />
      <Suspense
        fallback={
          <div className="min-h-screen bg-background text-foreground flex items-center justify-center">
            Загрузка...
          </div>
        }
      >
        <Routes>
          <Route
            path="/account"
            element={
              <RequireAuth>
                <AnalyticsPage />
              </RequireAuth>
            }
          />
          <Route
            path="/account/:section"
            element={
              <RequireAuth>
                <AnalyticsPage />
              </RequireAuth>
            }
          />
          <Route element={<PublicLayout />}>
            <Route
              path="/replay"
              element={
                <RequireAuth>
                  <ReplayPage />
                </RequireAuth>
              }
            />
            <Route
              path="/backtest"
              element={
                <RequireAuth>
                  <BacktestPage />
                </RequireAuth>
              }
            />
            <Route path="/" element={<RootHome />} />
            <Route path="/cookies" element={<CookiesPage />} />
            <Route path="/privacy" element={<PrivacyPage />} />
            <Route path="/terms" element={<TermsPage />} />
            <Route
              path="/scanners"
              element={
                <RequireAuth>
                  <Navigate to="/scanners/1" replace />
                </RequireAuth>
              }
            />
            <Route
              path="/scanners/1"
              element={
                <RequireAuth>
                  <ScannersPageOne />
                </RequireAuth>
              }
            />
            <Route
              path="/scanners/2"
              element={
                <RequirePlan minPlan="standard">
                  <ScannersPageTwo />
                </RequirePlan>
              }
            />
            <Route
              path="/scanners/3"
              element={
                <RequirePlan minPlan="pro">
                  <ScannersPageThree />
                </RequirePlan>
              }
            />
            <Route
              path="/exchange"
              element={
                <RequirePlan minPlan="pro">
                  <FeatureBlocked />
                </RequirePlan>
              }
            />
            <Route
              path="/community"
              element={
                <RequirePlan minPlan="pro">
                  <FeatureBlocked />
                </RequirePlan>
              }
            />
            <Route
              path="/community/channels"
              element={
                <RequirePlan minPlan="pro">
                  <FeatureBlocked />
                </RequirePlan>
              }
            />
            <Route
              path="/community/feed"
              element={
                <RequirePlan minPlan="pro">
                  <FeatureBlocked />
                </RequirePlan>
              }
            />
            <Route path="/news" element={<NewsPage />} />
            <Route path="/news/:id" element={<NewsPage />} />
          </Route>

          <Route element={<StandaloneLayout />}>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/register" element={<RegisterPage />} />
          <Route path="/forgot-password" element={<ForgotPasswordPage />} />
          <Route path="/reset-password" element={<ResetPasswordPage />} />
          <Route
            path="/admin/wallet-registry"
            element={
              <RequireAdmin>
                <WalletRegistryPage />
              </RequireAdmin>
            }
          />
          <Route
            path="/admin"
            element={
              <RequireAdmin>
                <DashboardPage />
              </RequireAdmin>
            }
          />
          </Route>
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </Suspense>
      {!location.pathname.startsWith("/account") ? (
        <PrimaryExchangeGate />
      ) : null}
    </>
  );
}
