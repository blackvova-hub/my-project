import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Star, X } from "lucide-react";

import { INDICATOR_CATALOG, type IndicatorId } from "./indicatorTypes";

function IndicatorIcon() {
  return (
    <svg viewBox="0 0 20 20" aria-hidden="true" className="h-[17px] w-[17px]">
      <path
        d="M3 14.5h2.4l2.4-8 3.1 7 2.2-4.3H17"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.45"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M3 17h14"
        stroke="currentColor"
        strokeWidth="1"
        strokeLinecap="round"
        opacity=".45"
      />
    </svg>
  );
}

function SearchIcon() {
  return (
    <svg viewBox="0 0 20 20" aria-hidden="true" className="h-4 w-4">
      <circle
        cx="8.5"
        cy="8.5"
        r="5"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.45"
      />
      <path
        d="m12.2 12.2 4.1 4.1"
        stroke="currentColor"
        strokeWidth="1.45"
        strokeLinecap="round"
      />
    </svg>
  );
}

export function IndicatorMenu({
  active,
  onToggle,
  onClear,
  volumeActive,
  extraCount = 0,
  onVolumeToggle,
  external = [],
  activeExternal = [],
  onExternalToggle,
  onCompare,
}: {
  active: readonly IndicatorId[];
  onToggle: (id: IndicatorId) => void;
  onClear: () => void;
  volumeActive?: boolean;
  extraCount?: number;
  onVolumeToggle?: () => void;
  external?: readonly {
    id: string;
    label: string;
    description: string;
    color: string;
    unavailable?: string;
  }[];
  activeExternal?: readonly string[];
  onExternalToggle?: (id: string) => void;
  onCompare?: () => void;
}) {
  const rootRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const [isOpen, setIsOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [favoritesOnly, setFavoritesOnly] = useState(false);
  const [favorites, setFavorites] = useState<string[]>(() => {
    try {
      const value: unknown = JSON.parse(
        localStorage.getItem("chart_indicator_favorites_v1") ?? "[]",
      );
      return Array.isArray(value)
        ? value
            .filter(
              (id): id is string => typeof id === "string" && id.length < 64,
            )
            .slice(0, 200)
        : [];
    } catch {
      return [];
    }
  });
  const favoriteSet = useMemo(() => new Set(favorites), [favorites]);
  const favorite = (id: string) =>
    setFavorites((prev) => {
      const next = prev.includes(id)
        ? prev.filter((v) => v !== id)
        : [...prev, id];
      try {
        localStorage.setItem(
          "chart_indicator_favorites_v1",
          JSON.stringify(next),
        );
      } catch {
        /* Session-only preference when storage is disabled. */
      }
      return next;
    });
  const [position, setPosition] = useState({ left: 12, top: 12 });
  const activeSet = useMemo(() => new Set(active), [active]);
  const normalizedQuery = query.trim().toLocaleLowerCase("ru-RU");
  const matching = normalizedQuery
    ? INDICATOR_CATALOG.filter((item) =>
        [item.label, item.shortLabel, item.description]
          .join(" ")
          .toLocaleLowerCase("ru-RU")
          .includes(normalizedQuery),
      )
    : INDICATOR_CATALOG;
  const filtered = matching.filter(
    (item) => !favoritesOnly || favoriteSet.has(item.id),
  );
  const externalFiltered = external.filter(
    (item) =>
      (!favoritesOnly || favoriteSet.has(item.id)) &&
      (!normalizedQuery ||
        [item.label, item.description, item.unavailable]
          .join(" ")
          .toLocaleLowerCase("ru-RU")
          .includes(normalizedQuery)),
  );

  const updatePosition = useCallback(() => {
    const bounds = buttonRef.current?.getBoundingClientRect();
    if (!bounds) return;
    const menuHeight = Math.min(660, window.innerHeight - 24);
    const below = window.innerHeight - bounds.bottom >= menuHeight + 8;
    setPosition({
      left: Math.max(12, Math.min(bounds.left, window.innerWidth - 352)),
      top: below
        ? bounds.bottom + 8
        : Math.max(
            12,
            Math.min(
              bounds.top - menuHeight - 8,
              window.innerHeight - menuHeight - 12,
            ),
          ),
    });
  }, []);

  useEffect(() => {
    if (!isOpen) return;
    const onPointerDown = (event: PointerEvent) => {
      const target = event.target as Node;
      if (
        !rootRef.current?.contains(target) &&
        !menuRef.current?.contains(target)
      )
        setIsOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setIsOpen(false);
        buttonRef.current?.focus();
      }
      if (event.key === "Tab") {
        const targets = menuRef.current?.querySelectorAll<HTMLElement>(
          'button:not(:disabled),input:not(:disabled),[tabindex="0"]',
        );
        const first = targets?.[0],
          last = targets?.[targets.length - 1];
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last?.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first?.focus();
        }
      }
    };
    window.addEventListener("pointerdown", onPointerDown);
    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("resize", updatePosition);
    window.addEventListener("scroll", updatePosition, true);
    return () => {
      window.removeEventListener("pointerdown", onPointerDown);
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("resize", updatePosition);
      window.removeEventListener("scroll", updatePosition, true);
    };
  }, [isOpen, updatePosition]);

  return (
    <div ref={rootRef} className="relative shrink-0">
      <button
        ref={buttonRef}
        type="button"
        onClick={() => {
          if (!isOpen) updatePosition();
          setIsOpen((value) => !value);
        }}
        aria-haspopup="dialog"
        aria-expanded={isOpen}
        className={`flex items-center gap-2 rounded-lg border px-3 py-2 text-[11px] font-semibold transition-colors ${
          isOpen || active.length > 0 || volumeActive || extraCount > 0
            ? "border-border-strong bg-accent text-primary"
            : "border-border bg-card text-muted-foreground hover:bg-card hover:text-foreground"
        }`}
      >
        <IndicatorIcon />
        Индикаторы
        {active.length +
          (volumeActive ? 1 : 0) +
          extraCount +
          activeExternal.length >
        0 ? (
          <span className="grid h-4 min-w-4 place-items-center rounded-full bg-primary px-1 text-[9px] font-bold text-primary-foreground">
            {active.length +
              (volumeActive ? 1 : 0) +
              extraCount +
              activeExternal.length}
          </span>
        ) : null}
        <svg
          viewBox="0 0 12 12"
          aria-hidden="true"
          className={`h-3 w-3 transition-transform ${isOpen ? "rotate-180" : ""}`}
        >
          <path
            d="m3 4.5 3 3 3-3"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.3"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
      </button>

      {isOpen && typeof document !== "undefined"
        ? createPortal(
            <div
              ref={menuRef}
              role="dialog"
              aria-label="Каталог индикаторов"
              style={{ left: position.left, top: position.top }}
              className="fixed z-[300] flex max-h-[calc(100dvh-24px)] w-[340px] max-w-[calc(100vw-24px)] flex-col overflow-hidden rounded-xl border border-border bg-popover shadow-lg"
            >
              <div className="border-b border-border p-3">
                <div className="mb-2 flex items-center justify-between text-[12px] font-semibold">
                  <span>Индикаторы</span>
                  <button
                    type="button"
                    aria-label="Закрыть каталог"
                    onClick={() => {
                      setIsOpen(false);
                      buttonRef.current?.focus();
                    }}
                    className="rounded p-1 hover:bg-accent"
                  >
                    <X size={16} />
                  </button>
                </div>
                <div className="flex items-center gap-2 rounded-lg border border-border bg-background px-2.5 text-muted-foreground focus-within:border-border-strong focus-within:text-muted-foreground">
                  <SearchIcon />
                  <input
                    value={query}
                    onChange={(event) => setQuery(event.target.value)}
                    placeholder="Поиск индикатора"
                    aria-label="Поиск индикатора"
                    autoFocus
                    className="h-9 min-w-0 flex-1 bg-transparent text-[11px] text-foreground outline-none placeholder:text-muted-foreground"
                  />
                </div>
              </div>

              <div className="flex items-center gap-2 border-b border-border px-3 py-2 text-[10px]">
                <button
                  type="button"
                  aria-pressed={!favoritesOnly}
                  onClick={() => setFavoritesOnly(false)}
                  className="rounded px-2 py-1 hover:bg-accent"
                >
                  Все
                </button>
                <button
                  type="button"
                  aria-pressed={favoritesOnly}
                  onClick={() => setFavoritesOnly(true)}
                  className="rounded px-2 py-1 hover:bg-accent"
                >
                  Избранные
                </button>
                <span className="ml-auto text-muted-foreground">
                  {filtered.length + externalFiltered.length} индикаторов
                </span>
              </div>
              <div className="site-scrollbar min-h-0 max-h-[min(520px,calc(100vh-160px))] flex-1 overflow-y-auto p-2">
                {onVolumeToggle &&
                (!normalizedQuery ||
                  "объём volume объем".includes(normalizedQuery)) ? (
                  <div className="mb-2">
                    <div className="px-2 pb-1 pt-1 text-[9px] font-semibold uppercase tracking-[0.13em] text-muted-foreground">
                      На графике
                    </div>
                    <button
                      type="button"
                      role="switch"
                      aria-checked={!!volumeActive}
                      onClick={onVolumeToggle}
                      className="flex w-full items-center gap-3 rounded-lg px-2.5 py-2 text-left transition-colors hover:bg-card"
                    >
                      <span className="h-2 w-2 shrink-0 rounded-full bg-primary" />
                      <span className="min-w-0 flex-1">
                        <span className="block text-[11px] font-semibold text-foreground">
                          Объём
                        </span>
                        <span className="block text-[9px] text-muted-foreground">
                          Гистограмма объёма торгов
                        </span>
                      </span>
                      <span
                        className={`relative h-[18px] w-8 shrink-0 rounded-full border ${volumeActive ? "border-border-strong bg-accent" : "border-border bg-card"}`}
                      >
                        <span
                          className={`absolute top-[2px] h-3 w-3 rounded-full ${volumeActive ? "left-[16px] bg-primary" : "left-[2px] bg-muted-foreground"}`}
                        />
                      </span>
                    </button>
                  </div>
                ) : null}
                {(
                  [
                    { id: "overlay", label: "На графике" },
                    { id: "smart-money", label: "Smart Money Concepts" },
                    { id: "pane", label: "Отдельная панель" },
                  ] as const
                ).map((section) => {
                  const items = filtered.filter((item) =>
                    section.id === "smart-money"
                      ? item.group === "smart-money"
                      : item.group !== "smart-money" &&
                        item.placement === section.id,
                  );
                  if (items.length === 0) return null;
                  return (
                    <div key={section.id} className="mb-2 last:mb-0">
                      <div className="px-2 pb-1 pt-1 text-[9px] font-semibold uppercase tracking-[0.13em] text-muted-foreground">
                        {section.label}
                      </div>
                      {items.map((item) => {
                        const enabled = activeSet.has(item.id);
                        return (
                          <div key={item.id} className="flex items-center">
                            <button
                              type="button"
                              role="switch"
                              aria-checked={enabled}
                              onClick={() => onToggle(item.id)}
                              title={item.description}
                              className="flex min-w-0 flex-1 items-center gap-3 rounded-lg px-2.5 py-2 text-left transition-colors hover:bg-card"
                            >
                              <span
                                className="h-2 w-2 shrink-0 rounded-full"
                                style={{ backgroundColor: item.color }}
                              />
                              <span className="min-w-0 flex-1">
                                <span className="block truncate text-[11px] font-semibold text-foreground">
                                  {item.label}
                                </span>
                                <span className="block truncate text-[9px] text-muted-foreground">
                                  {item.description}
                                </span>
                              </span>
                              <span
                                className={`relative h-[18px] w-8 shrink-0 rounded-full border transition-colors ${enabled ? "border-border-strong bg-accent" : "border-border bg-card"}`}
                              >
                                <span
                                  className={`absolute top-[2px] h-3 w-3 rounded-full transition-all ${enabled ? "left-[16px] bg-primary" : "left-[2px] bg-muted-foreground"}`}
                                />
                              </span>
                            </button>
                            <button
                              type="button"
                              aria-label={`${favoriteSet.has(item.id) ? "Убрать" : "Добавить"} ${item.label} ${favoriteSet.has(item.id) ? "из избранного" : "в избранное"}`}
                              aria-pressed={favoriteSet.has(item.id)}
                              onClick={() => favorite(item.id)}
                              className="shrink-0 rounded p-2 text-muted-foreground hover:text-primary"
                            >
                              <Star
                                size={14}
                                fill={
                                  favoriteSet.has(item.id)
                                    ? "currentColor"
                                    : "none"
                                }
                              />
                            </button>
                          </div>
                        );
                      })}
                    </div>
                  );
                })}
                {!!externalFiltered.length && (
                  <div className="mb-2">
                    <div className="px-2 py-1 text-[9px] font-semibold uppercase tracking-wider text-muted-foreground">
                      Рыночные данные · открытые API
                    </div>
                    {externalFiltered.map((item) => {
                      const enabled = activeExternal.includes(item.id);
                      return (
                        <div key={item.id} className="flex items-center">
                          <button
                            type="button"
                            role="switch"
                            aria-checked={enabled}
                            disabled={!!item.unavailable && !enabled}
                            title={item.unavailable || item.description}
                            onClick={() => onExternalToggle?.(item.id)}
                            className="flex min-w-0 flex-1 items-center gap-3 rounded-lg px-2.5 py-2 text-left hover:bg-card disabled:opacity-45"
                          >
                            <span
                              className="h-2 w-2 shrink-0 rounded-full"
                              style={{ backgroundColor: item.color }}
                            />
                            <span className="min-w-0 flex-1">
                              <span className="block text-[11px] font-semibold">
                                {item.label}
                              </span>
                              <span className="block text-[9px] text-muted-foreground">
                                {item.unavailable || item.description}
                              </span>
                            </span>
                            <span aria-hidden="true" className="text-primary">
                              {enabled ? "✓" : "+"}
                            </span>
                          </button>
                          <button
                            type="button"
                            aria-label={`${favoriteSet.has(item.id) ? "Убрать" : "Добавить"} ${item.label} ${favoriteSet.has(item.id) ? "из избранного" : "в избранное"}`}
                            aria-pressed={favoriteSet.has(item.id)}
                            onClick={() => favorite(item.id)}
                            className="shrink-0 rounded p-2 text-muted-foreground hover:text-primary"
                          >
                            <Star
                              size={14}
                              fill={
                                favoriteSet.has(item.id)
                                  ? "currentColor"
                                  : "none"
                              }
                            />
                          </button>
                        </div>
                      );
                    })}
                  </div>
                )}
                {onCompare &&
                  (!normalizedQuery ||
                    /ratio|spread|спред|отношение/.test(normalizedQuery)) && (
                    <button
                      type="button"
                      onClick={() => {
                        setIsOpen(false);
                        buttonRef.current?.focus();
                        onCompare();
                      }}
                      className="w-full rounded-lg px-3 py-2 text-left text-[11px] hover:bg-card"
                    >
                      Ratio / Spread · выбрать второй инструмент
                    </button>
                  )}
                {filtered.length === 0 &&
                externalFiltered.length === 0 &&
                !(
                  onCompare &&
                  /ratio|spread|спред|отношение/.test(normalizedQuery)
                ) ? (
                  <div className="px-3 py-8 text-center text-[11px] text-muted-foreground">
                    Ничего не найдено
                  </div>
                ) : null}
              </div>

              <div className="flex justify-end border-t border-border px-3 py-2.5 text-[9px] text-muted-foreground">
                <button
                  type="button"
                  onClick={() => {
                    onClear();
                    if (volumeActive) onVolumeToggle?.();
                  }}
                  disabled={
                    active.length === 0 &&
                    !volumeActive &&
                    !extraCount &&
                    !activeExternal.length
                  }
                  className="rounded-md px-2 py-1 font-semibold text-muted-foreground transition-colors hover:bg-card hover:text-foreground disabled:pointer-events-none disabled:opacity-30"
                >
                  Выключить все
                </button>
              </div>
            </div>,
            document.body,
          )
        : null}
    </div>
  );
}
