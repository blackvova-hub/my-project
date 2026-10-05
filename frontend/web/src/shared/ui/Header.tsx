import { useEffect, useRef, type MouseEvent } from "react";
import { Link, NavLink, useLocation, useNavigate } from "react-router-dom";
import { Moon, Sun } from "lucide-react";
import { useAuth } from "../auth/AuthContext";
import { hasPlan } from "../auth/plan";
import { useTheme } from "../theme/ThemeContext";
import type { Theme } from "../theme/theme";

const themeOptions = [
  { value: "light", label: "Светлая тема", Icon: Sun },
  { value: "dark", label: "Тёмная тема", Icon: Moon },
  { value: "green", label: "Фирменная зелёная тема", Icon: null },
] as const;

const indicatorPosition: Record<Theme, string> = {
  light: "translate-x-0",
  dark: "translate-x-full",
  green: "translate-x-[200%]",
};

export default function Header({
  staticPosition = false,
  onProfile,
}: {
  staticPosition?: boolean;
  onProfile?: () => void;
}) {
  const { isAuth, user } = useAuth();
  const { theme, setTheme } = useTheme();
  const navigate = useNavigate();
  const location = useLocation();

  const isAccountPage = location.pathname.startsWith("/account");
  const headerRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    const updateOffset = () => {
      const height = headerRef.current?.offsetHeight ?? 0;
      const offset = staticPosition ? 0 : height;
      document.documentElement.style.setProperty(
        "--app-header-offset",
        `${offset}px`,
      );
    };

    updateOffset();
    const observer = new ResizeObserver(updateOffset);
    if (headerRef.current) observer.observe(headerRef.current);
    return () => {
      observer.disconnect();
      document.documentElement.style.removeProperty("--app-header-offset");
    };
  }, [staticPosition]);

  const linkBase = "app-nav-link whitespace-nowrap text-base transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring";
  const linkActive = "text-primary";
  const linkInactive = "text-muted-foreground hover:text-foreground";
  const linkClass = (isActive: boolean) =>
    `${linkBase} ${isActive ? linkActive : linkInactive}`;
  const isAnchorActive = (hash: string) =>
    location.pathname === "/" && (location.hash || "#top") === hash;
  const anchorLinkClass = (hash: string) => linkClass(isAnchorActive(hash));
  const actionClass = "rounded-xl border border-border bg-secondary px-4 py-2 text-sm text-secondary-foreground transition hover:bg-accent hover:text-accent-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring";
  const dropdownClass = "app-nav-dropdown pointer-events-none absolute left-0 top-full mt-2 w-44 rounded-xl border border-border bg-popover p-2 text-popover-foreground opacity-0 group-hover:pointer-events-auto group-hover:opacity-100 group-focus-within:pointer-events-auto group-focus-within:opacity-100";
  const dropdownLinkClass = "block rounded-lg px-3 py-2 text-sm text-popover-foreground hover:bg-accent hover:text-accent-foreground focus-visible:outline-2 focus-visible:outline-ring";
  const mobileMenuLinkClass = dropdownLinkClass;

  const scrollToHash = (hash: string) => {
    const target = document.querySelector(hash);
    if (!target) return;
    const headerOffset = headerRef.current?.offsetHeight ?? 0;
    const targetTop =
      target.getBoundingClientRect().top + window.scrollY - headerOffset - 12;
    window.scrollTo({
      top: targetTop,
      behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "instant" : "smooth",
    });
  };

  const handleAnchorClick =
    (hash: string) => (event: MouseEvent<HTMLAnchorElement>) => {
      event.preventDefault();

      if (location.pathname !== "/") {
        navigate({ pathname: "/", hash });
      } else if (location.hash !== hash) {
        navigate({ hash }, { replace: true });
      }

      scrollToHash(hash);
    };

  useEffect(() => {
    if (!location.hash) return;
    scrollToHash(location.hash);
  }, [location.hash]);

  return (
    <header
      ref={headerRef}
      data-app-header="true"
      data-authenticated={isAuth}
      className={[
        "z-50 border-b",
        "border-border bg-background/95 text-foreground",
        staticPosition ? "relative" : "sticky top-0",
      ].join(" ")}
    >
      <div
        className={
          "app-header-inner flex w-full flex-wrap items-center gap-x-4 gap-y-2 px-4 py-4 sm:px-6"
        }
      >
        <Link
          to="/"
          data-app-logo="true"
          aria-label="Short&Long — главная"
          onClick={() => window.scrollTo({ top: 0, behavior: "instant" })}
          className="app-brand shrink-0 whitespace-nowrap text-lg font-semibold tracking-tight text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          Short&Long
        </Link>

        {!isAuth ? (
          <nav className="hidden items-center gap-6 md:flex">
            <Link
              to="/#top"
              aria-current={isAnchorActive("#top") ? "page" : undefined}
              onClick={handleAnchorClick("#top")}
              className={anchorLinkClass("#top")}
            >
              Главная
            </Link>
            <Link
              to="/#about"
              aria-current={isAnchorActive("#about") ? "page" : undefined}
              onClick={handleAnchorClick("#about")}
              className={anchorLinkClass("#about")}
            >
              О нас
            </Link>
            <Link
              to="/#benefits"
              aria-current={isAnchorActive("#benefits") ? "page" : undefined}
              onClick={handleAnchorClick("#benefits")}
              className={anchorLinkClass("#benefits")}
            >
              Что внутри
            </Link>
            <Link
              to="/#team"
              aria-current={isAnchorActive("#team") ? "page" : undefined}
              onClick={handleAnchorClick("#team")}
              className={anchorLinkClass("#team")}
            >
              Команда
            </Link>
            <Link
              to="/#beta"
              aria-current={isAnchorActive("#beta") ? "page" : undefined}
              onClick={handleAnchorClick("#beta")}
              className={anchorLinkClass("#beta")}
            >
              Бета
            </Link>
          </nav>
        ) : (
          <nav className="hidden items-center gap-4 xl:flex">
            <NavLink
              to="/"
              className={({ isActive }) => linkClass(isActive)}
            >
              Главная
            </NavLink>
            {hasPlan(user?.plan, "pro") ? (
              <div className="relative group">
                <NavLink
                  to="/scanners/1"
                  className={({ isActive }) => linkClass(isActive)}
                >
                  Сканеры
                </NavLink>
                <div className={dropdownClass}>
                  <span className="pointer-events-auto absolute -top-2 left-0 h-2 w-full" />
                  <Link
                    to="/scanners/1"
                    className={dropdownLinkClass}
                  >
                    Сканер 1
                  </Link>
                  <Link
                    to="/scanners/2"
                    className={dropdownLinkClass}
                  >
                    Сканер 2
                  </Link>
                  <Link
                    to="/scanners/3"
                    className={dropdownLinkClass}
                  >
                    Сканер 3
                  </Link>
                </div>
              </div>
            ) : hasPlan(user?.plan, "standard") ? (
              <div className="relative group">
                <NavLink
                  to="/scanners/1"
                  className={({ isActive }) => linkClass(isActive)}
                >
                  Сканеры
                </NavLink>
                <div className={dropdownClass}>
                  <span className="pointer-events-auto absolute -top-2 left-0 h-2 w-full" />
                  <Link
                    to="/scanners/1"
                    className={dropdownLinkClass}
                  >
                    Сканер 1
                  </Link>
                  <Link
                    to="/scanners/2"
                    className={dropdownLinkClass}
                  >
                    Сканер 2
                  </Link>
                </div>
              </div>
            ) : (
              <NavLink
                to="/scanners/1"
                className={({ isActive }) => linkClass(isActive)}
              >
                Сканеры
              </NavLink>
            )}
            <NavLink
              to="/backtest"
              className={({ isActive }) => linkClass(isActive)}
            >
              Тест стратегии
            </NavLink>
            <NavLink
              to="/replay"
              className={({ isActive }) => linkClass(isActive)}
            >
              Бэктест
            </NavLink>
            {hasPlan(user?.plan, "standard") ? (
              <NavLink
                to="/news"
                className={({ isActive }) => linkClass(isActive)}
              >
                Новости
              </NavLink>
            ) : null}
            {hasPlan(user?.plan, "pro") ? (
              <div className="relative group">
                <NavLink
                  to="/community"
                  className={({ isActive }) => linkClass(isActive)}
                >
                  Комьюнити
                </NavLink>
                <div className={dropdownClass}>
                  <span className="pointer-events-auto absolute -top-2 left-0 h-2 w-full" />
                  <Link
                    to="/community/channels"
                    className={dropdownLinkClass}
                  >
                    Каналы
                  </Link>
                  <Link
                    to="/community/feed"
                    className={dropdownLinkClass}
                  >
                    Лента
                  </Link>
                </div>
              </div>
            ) : null}
            {user?.isAdmin ? (
              <NavLink
                to="/admin"
                className={({ isActive }) => linkClass(isActive)}
              >
                Админка
              </NavLink>
            ) : null}
          </nav>
        )}

        {isAuth ? (
          <details className="app-mobile-menu relative ml-auto xl:hidden">
            <summary
              className="cursor-pointer list-none rounded-lg border border-border px-3 py-2 text-sm text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
              aria-label="Открыть навигацию"
            >
              Меню
            </summary>
            <nav
              aria-label="Мобильная навигация"
              className="absolute right-0 top-full z-50 mt-2 w-56 rounded-xl border border-border bg-popover p-2 text-popover-foreground shadow-lg"
              onClick={(event) => {
                if ((event.target as HTMLElement).closest("a"))
                  event.currentTarget
                    .closest("details")
                    ?.removeAttribute("open");
              }}
            >
              <Link
                className={mobileMenuLinkClass}
                to="/"
              >
                Главная
              </Link>
              <Link
                className={mobileMenuLinkClass}
                to="/scanners/1"
              >
                Сканеры
              </Link>
              <Link
                className={mobileMenuLinkClass}
                to="/backtest"
              >
                Тест стратегии
              </Link>
              <Link
                className={mobileMenuLinkClass}
                to="/replay"
              >
                Бэктест
              </Link>
              {hasPlan(user?.plan, "standard") ? (
                <Link
                  className={mobileMenuLinkClass}
                  to="/news"
                >
                  Новости
                </Link>
              ) : null}
              {hasPlan(user?.plan, "pro") ? (
                <Link
                  className={mobileMenuLinkClass}
                  to="/community"
                >
                  Комьюнити
                </Link>
              ) : null}
              {user?.isAdmin ? (
                <Link
                  className={mobileMenuLinkClass}
                  to="/admin"
                >
                  Админка
                </Link>
              ) : null}
            </nav>
          </details>
        ) : null}
        <div className="app-header-actions ml-auto flex shrink-0 items-center gap-2">
          <div
            role="group"
            aria-label="Тема оформления"
            className="app-theme-switch relative grid h-8 w-20 shrink-0 grid-cols-3 rounded-md border border-border bg-background text-muted-foreground"
          >
            <span
              aria-hidden="true"
              data-theme-indicator=""
              className={`pointer-events-none absolute inset-y-0.5 left-0.5 w-[calc((100%-0.25rem)/3)] rounded-sm bg-accent motion-safe:transition-transform motion-safe:duration-300 motion-safe:ease-out ${indicatorPosition[theme]}`}
            />
            {themeOptions.map(({ value, label, Icon }) => (
              <button
                key={value}
                type="button"
                aria-label={label}
                title={label}
                aria-pressed={theme === value}
                onClick={(event) => {
                  const rect = event.currentTarget.getBoundingClientRect();
                  setTheme(value, { x: rect.x + rect.width / 2, y: rect.y + rect.height / 2 });
                }}
                className="relative z-10 flex h-full items-center justify-center rounded-sm transition-colors hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-[-3px] focus-visible:outline-ring aria-pressed:text-foreground"
              >
                {Icon ? <Icon aria-hidden="true" size={14} strokeWidth={1.8} /> : (
                  <span aria-hidden="true" className="font-sans text-sm font-black leading-none tracking-tight">SL</span>
                )}
              </button>
            ))}
          </div>
          {!isAuth ? (
            <div className="app-auth-actions flex items-center gap-2">
              <Link to="/login" className={actionClass}>
                Войти
              </Link>
              <Link
                to="/register"
                className="rounded-xl bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground transition hover:bg-primary-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
              >
                Регистрация
              </Link>
            </div>
          ) : (
            <div className="flex min-w-[120px] items-center justify-end gap-2">
              {onProfile ? (
                <button onClick={onProfile} className={actionClass}>
                  Профиль
                </button>
              ) : !isAccountPage ? (
                <Link to="/account" className={actionClass}>
                  Кабинет
                </Link>
              ) : (
                <span className="invisible px-4 py-2 text-sm">Кабинет</span>
              )}
            </div>
          )}
        </div>
      </div>
    </header>
  );
}
