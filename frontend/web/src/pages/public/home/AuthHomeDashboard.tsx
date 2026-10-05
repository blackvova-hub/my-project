import { lazy, Suspense, useEffect, useRef, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";

import { MiniScanners } from "./widgets/MiniScanners";
import { MarketTickerMarquee } from "./widgets/MarketTickerMarquee";
import { WatchlistWidget } from "./widgets/WatchlistWidget";
import { useAuth } from "../../../shared/auth/AuthContext";
import { hasPlan } from "../../../shared/auth/plan";

const MiniNewsWidget = lazy(() =>
  import("./widgets/MiniNewsWidget").then((module) => ({ default: module.MiniNewsWidget })),
);
const MarketInsightsWidget = lazy(() =>
  import("./widgets/MarketInsightsWidget").then((module) => ({
    default: module.MarketInsightsWidget,
  })),
);

function DeferredWidget({ children, minHeight }: { children: ReactNode; minHeight: number }) {
  const anchorRef = useRef<HTMLDivElement>(null);
  const [shouldRender, setShouldRender] = useState(false);

  useEffect(() => {
    const anchor = anchorRef.current;
    if (!anchor || shouldRender) return;
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          setShouldRender(true);
          observer.disconnect();
        }
      },
      { rootMargin: "400px 0px" },
    );
    observer.observe(anchor);
    return () => observer.disconnect();
  }, [shouldRender]);

  return (
    <div
      ref={anchorRef}
      style={{ minHeight, contentVisibility: "auto", containIntrinsicSize: `${minHeight}px` }}
    >
      {shouldRender ? (
        <Suspense
          fallback={<div className="h-full rounded-2xl border border-border bg-card" />}
        >
          {children}
        </Suspense>
      ) : null}
    </div>
  );
}

export default function AuthHomeDashboard() {
  const { user } = useAuth();
  const userKey = user?.id ?? "anon";
  const canStandard = hasPlan(user?.plan, "standard");

  if (!canStandard) {
    return (
      <div className="mx-auto w-full max-w-none px-4 sm:px-6 lg:px-8 py-10 md:py-14">
        <div className="rounded-3xl border border-border bg-card p-8 md:p-10">
          <h1 className="mt-3 text-3xl md:text-4xl font-extrabold tracking-tight">
            Доступ закрыт для Free
          </h1>
          <p className="mt-3 max-w-2xl text-sm text-muted-foreground">
            Чтобы пользоваться терминалом, сканерами и новостями, активируй подписку Standard или Pro.
          </p>
          <div className="mt-6">
            <Link
              to="/account"
              className="rounded-xl border border-border bg-secondary px-4 py-2 text-sm font-semibold hover:bg-secondary transition"
            >
              Перейти в кабинет
            </Link>
          </div>
        </div>
      </div>
    );
  }
  return (
    <div className="mx-auto w-full max-w-none px-4 sm:px-6 lg:px-8 pt-2 pb-10 md:pt-3 md:pb-14">
      <MarketTickerMarquee />
      <div className="mt-6 grid gap-6 lg:grid-cols-[minmax(0,1fr)_390px]">
        <div className="min-w-0">
          <MiniScanners userKey={userKey} />
        </div>
        <div className="min-w-0">
          <WatchlistWidget />
        </div>
        <div className="min-w-0 lg:col-span-2">
          <DeferredWidget minHeight={520}>
            <MiniNewsWidget />
          </DeferredWidget>
        </div>
        <div className="min-w-0 lg:col-span-2">
          <DeferredWidget minHeight={640}>
            <MarketInsightsWidget />
          </DeferredWidget>
        </div>
      </div>
    </div>
  );
}
