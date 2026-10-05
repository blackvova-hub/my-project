import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import { createPortal } from "react-dom";
import { money, number, pct } from "./model";
import { displayLabel } from "./locale";
import { AnimatedSelect } from "../../shared/ui/AnimatedSelect";

export function Icon({ name, size = 18 }: { name: string; size?: number }) {
  const paths: Record<string, ReactNode> = {
    overview: (
      <>
        <path d="m3 10 9-7 9 7v10H3Z" />
        <path d="M9 20v-7h6v7" />
      </>
    ),
    performance: (
      <>
        <path d="M4 20V12m5 8V5m6 15v-9m5 9V3" />
      </>
    ),
    trades: (
      <>
        <path d="M8 6h13M8 12h13M8 18h13" />
        <path d="M3 6h.01M3 12h.01M3 18h.01" />
      </>
    ),
    edge: (
      <>
        <path d="m3 17 6-9 5 5 7-10M17 3h4v4" />
      </>
    ),
    costs: (
      <>
        <ellipse cx="10" cy="5" rx="6" ry="3" />
        <path d="M4 5v5c0 4 12 4 12 0V5M4 10v5c0 2 3 3 6 3" />
        <path d="M13 14c0-2 8-2 8 0v5c0 3-8 3-8 0Zm0 0c0 3 8 3 8 0" />
      </>
    ),
    risk: <path d="m12 3 9 4v6c0 4-5 7-9 9-4-2-9-5-9-9V7Z" />,
    insights: (
      <>
        <path d="M8 16c0-3-3-3-3-7a7 7 0 0 1 14 0c0 4-3 4-3 7M8 17h8m-7 4h6M12 6v6m-3-3h6" />
      </>
    ),
    connections: (
      <>
        <path
          d="m10 14 4-4m-6 7-2 2a4 4 0 0 1-6-6l5-5a4 4 0 0 1 6 0m2-1 2-2a4 4 0 0 1 6 6l-5 5a4 4 0 0 1-6 0"
          transform="translate(1 0)"
        />
      </>
    ),
    chevron: <path d="m9 5 7 7-7 7" />,
    down: <path d="m6 9 6 6 6-6" />,
    export: (
      <>
        <path d="M12 3v12m-5-5 5 5 5-5M4 14v6h16v-6" />
      </>
    ),
    close: <path d="m5 5 14 14M5 19 19 5" />,
    plus: <path d="M12 5v14M5 12h14" />,
    sync: (
      <>
        <path d="M20 10a8 8 0 0 0-14-5L3 8m0-5v5h5M4 14a8 8 0 0 0 14 5l3-3m0 5v-5h-5" />
      </>
    ),
    check: <path d="m5 12 4 4 10-10" />,
    arrow: <path d="M4 12h16m-6-6 6 6-6 6" />,
    filter: <path d="M3 4h18l-7 8v7l-4 2v-9Z" />,
    logout: (
      <>
        <path d="M9 4H4v16h5m5-13 5 5-5 5m-5-5h10" />
      </>
    ),
    calendar: (
      <>
        <rect x="3" y="5" width="18" height="16" rx="2" />
        <path d="M7 3v4m10-4v4M3 11h18" />
      </>
    ),
    info: (
      <>
        <circle cx="12" cy="12" r="9" />
        <path d="M12 11v6M12 7h.01" />
      </>
    ),
    menu: <path d="M4 6h16M4 12h16M4 18h16" />,
    back: <path d="m14 5-7 7 7 7" />,
  };
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.65"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {paths[name] ?? paths.info}
    </svg>
  );
}
export function Panel({
  title,
  action,
  children,
  className = "",
}: {
  title?: string;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={`an-panel ${className}`}>
      {title ? (
        <div className="an-panel-heading">
          <h2>{title}</h2>
          {action}
        </div>
      ) : null}
      {children}
    </section>
  );
}
export function Value({
  value,
  format = "money",
  signed = true,
}: {
  value: number | null | undefined;
  format?: "money" | "percent" | "number";
  signed?: boolean;
}) {
  return (
    <span
      className={
        value == null
          ? "an-muted"
          : value > 0
            ? "an-positive"
            : value < 0
              ? "an-negative"
              : ""
      }
    >
      {format === "money"
        ? money(value, signed)
        : format === "percent"
          ? pct(value)
          : number(value)}
    </span>
  );
}
export function Metrics({
  items,
}: {
  items: { label: string; value: ReactNode; note?: ReactNode }[];
}) {
  return (
    <div className="an-metrics">
      {items.map((item) => (
        <div className="an-metric" key={item.label}>
          <span>{item.label}</span>
          <strong>{item.value}</strong>
          {item.note ? <small>{item.note}</small> : null}
        </div>
      ))}
    </div>
  );
}
export function Segments<T extends string>({
  items,
  value,
  onChange,
  label,
}: {
  items: readonly T[];
  value: T;
  onChange: (v: T) => void;
  label: string;
}) {
  return (
    <div
      className="an-segments"
      role="group"
      aria-label={label}
      data-count={items.length}
    >
      {items.map((item) => (
        <button
          key={item}
          type="button"
          aria-pressed={value === item}
          className={value === item ? "active" : ""}
          onClick={() => onChange(item)}
        >
          {displayLabel(item)}
        </button>
      ))}
    </div>
  );
}
export function Select({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: { value: string; label: string }[];
}) {
  return (
    <div className="an-select">
      <AnimatedSelect
        ariaLabel={label}
        className="an-select-trigger"
        value={value}
        onChange={onChange}
        options={options.map((o) => ({ ...o, label: displayLabel(o.label) }))}
      />
    </div>
  );
}
export function Empty({
  title,
  children,
  action,
}: {
  title: string;
  children?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="an-empty">
      <div className="an-empty-icon">
        <Icon name="performance" size={28} />
      </div>
      <h2>{title}</h2>
      {children ? <p>{children}</p> : null}
      {action ? <div className="an-empty-actions">{action}</div> : null}
    </div>
  );
}
export function Modal({
  title,
  children,
  onClose,
  wide = false,
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
  wide?: boolean;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const closeRef = useRef(onClose);
  useEffect(() => {
    closeRef.current = onClose;
  }, [onClose]);
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    const old = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const focusables = () =>
      Array.from(
        ref.current?.querySelectorAll<HTMLElement>(
          'button:not(:disabled),input,select,textarea,a[href],[tabindex="0"]',
        ) ?? [],
      ).filter((e) => !e.hidden);
    focusables()[0]?.focus();
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") closeRef.current();
      if (e.key === "Tab") {
        const all = focusables(),
          first = all[0],
          last = all.at(-1);
        if (e.shiftKey && document.activeElement === first) {
          e.preventDefault();
          last?.focus();
        } else if (!e.shiftKey && document.activeElement === last) {
          e.preventDefault();
          first?.focus();
        }
      }
    };
    document.addEventListener("keydown", key);
    return () => {
      document.body.style.overflow = old;
      document.removeEventListener("keydown", key);
      previous?.focus();
    };
  }, []);
  return createPortal(
    <div
      className="an-root an-overlay"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        ref={ref}
        className={`an-dialog ${wide ? "an-dialog-wide" : ""}`}
        role="dialog"
        aria-modal="true"
        aria-label={title}
      >
        <div className="an-panel-heading">
          <h2>{title}</h2>
          <button
            className="an-icon-button"
            aria-label="Закрыть окно"
            onClick={onClose}
          >
            <Icon name="close" />
          </button>
        </div>
        {children}
      </div>
    </div>,
    document.body,
  );
}
export function Rows({
  items,
}: {
  items: { label: string; value: ReactNode; onClick?: () => void }[];
}) {
  return (
    <div className="an-rows">
      {items.map((item, i) =>
        item.onClick ? (
          <button
            className="an-row"
            onClick={item.onClick}
            key={`${item.label}-${i}`}
          >
            <span>{displayLabel(item.label)}</span>
            <strong>{item.value}</strong>
            <Icon name="chevron" size={14} />
          </button>
        ) : (
          <div className="an-row" key={`${item.label}-${i}`}>
            <span>{displayLabel(item.label)}</span>
            <strong>{item.value}</strong>
          </div>
        ),
      )}
    </div>
  );
}
