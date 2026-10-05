import { lazy, Suspense } from "react";
import { useAuth } from "../../../shared/auth/AuthContext";

const HomePage = lazy(() => import("./HomePage"));
const AuthHomeDashboard = lazy(() => import("../home/AuthHomeDashboard"));

export default function RootHome() {
  const { isAuth, isLoading } = useAuth();

  // Гость -> маркетинговая главная
  // Авторизованный -> терминал/дашборд
  if (isLoading) {
    return (
      <div className="min-h-[70vh] flex items-center justify-center">
        <div className="flex flex-col items-center gap-3 text-muted-foreground">
          <div className="h-10 w-10 animate-spin rounded-full border-2 border-border border-t-primary" />
          <div className="text-sm">Загрузка…</div>
        </div>
      </div>
    );
  }

  return (
    <Suspense
      fallback={
        <div className="min-h-[70vh] flex items-center justify-center text-sm text-muted-foreground">
          Загрузка…
        </div>
      }
    >
      {isAuth ? <AuthHomeDashboard /> : <HomePage />}
    </Suspense>
  );
}
