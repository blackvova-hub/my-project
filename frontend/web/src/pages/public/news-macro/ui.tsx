import { useEffect, useRef, type ReactNode } from "react";
import { ExternalLink, RefreshCw, X } from "lucide-react";
import { safeExternalURL } from "./model";
import type { Importance } from "./api";
import { IMPORTANCE } from "./format";
export function External({
  url,
  children,
}: {
  url: string;
  children: ReactNode;
}) {
  const safe = safeExternalURL(url);
  return safe ? (
    <a
      className="nm-external"
      href={safe}
      target="_blank"
      rel="noopener noreferrer"
    >
      {children}
      <ExternalLink size={12} aria-hidden="true" />
    </a>
  ) : (
    <span>{children}</span>
  );
}
export function Panel({
  title,
  action,
  children,
  className = "",
}: {
  title: ReactNode;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={`nm-panel ${className}`}>
      <div className="nm-panel-head">
        <h2>{title}</h2>
        {action}
      </div>
      {children}
    </section>
  );
}
export function Empty({
  title,
  children,
  retry,
}: {
  title: string;
  children?: ReactNode;
  retry?: () => void;
}) {
  return (
    <div className="nm-empty">
      <strong>{title}</strong>
      {children ? <p>{children}</p> : null}
      {retry ? (
        <button className="nm-button" onClick={retry}>
          <RefreshCw size={14} /> Повторить
        </button>
      ) : null}
    </div>
  );
}
export function Skeleton({ rows = 3 }: { rows?: number }) {
  return (
    <div className="nm-skeleton" role="status" aria-label="Загрузка данных">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i}>
          <span />
          <span />
        </div>
      ))}
    </div>
  );
}
export function ImportanceMark({ value }: { value: Importance }) {
  return (
    <span
      className={`nm-importance nm-importance-${value}`}
      title={`Важность: ${IMPORTANCE[value]}`}
      aria-label={`Важность: ${IMPORTANCE[value]}`}
    >
      <i />
      <i />
      <i />
    </span>
  );
}
export function Drawer({
  title,
  children,
  onClose,
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    ref.current?.showModal();
    return () => {
      previous?.focus();
    };
  }, []);
  return (
    <dialog
      ref={ref}
      className="nm-drawer"
      onCancel={onClose}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
      aria-labelledby="nm-drawer-title"
    >
      <div className="nm-drawer-content">
        <header>
          <h2 id="nm-drawer-title">{title}</h2>
          <button
            className="nm-icon-button"
            aria-label="Закрыть панель"
            onClick={onClose}
          >
            <X size={20} />
          </button>
        </header>
        {children}
      </div>
    </dialog>
  );
}
