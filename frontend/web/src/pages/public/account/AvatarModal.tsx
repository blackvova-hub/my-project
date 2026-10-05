import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";

type Props = {
  avatarUrl: string;
  initials: string;
  status: string;
  onDismiss: () => void;
  onFile: (file: File | null) => void;
};

export default function AvatarModal({ avatarUrl, initials, status, onDismiss, onFile }: Props) {
  const inputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") onDismiss();
    };
    window.addEventListener("keydown", closeOnEscape);
    return () => {
      document.body.style.overflow = previousOverflow;
      window.removeEventListener("keydown", closeOnEscape);
    };
  }, [onDismiss]);

  if (typeof document === "undefined") return null;

  const acceptFile = (file: File | null) => {
    onFile(file);
    if (file) onDismiss();
  };

  return createPortal(
    <div className="fixed inset-0 z-[130] flex items-end justify-center p-2 sm:items-center sm:p-6">
      <button type="button" aria-label="Закрыть выбор фотографии" className="absolute inset-0 bg-scrim" onClick={onDismiss} />
      <section role="dialog" aria-modal="true" aria-labelledby="avatar-modal-title" className="relative max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-lg overflow-y-auto rounded-[22px] border border-border bg-surface-raised p-5 text-foreground [box-shadow:var(--shadow-overlay)] sm:rounded-3xl sm:p-6">
        <header className="flex items-center justify-between gap-4">
          <div>
            <h2 id="avatar-modal-title" className="text-lg font-semibold tracking-tight sm:text-xl">Фото профиля</h2>
            <p className="mt-1 text-xs text-muted-foreground">JPG или PNG, до 1.5 МБ</p>
          </div>
          <button type="button" onClick={onDismiss} aria-label="Закрыть" className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border border-border bg-card text-muted-foreground transition hover:border-border-strong hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
            <svg aria-hidden="true" viewBox="0 0 20 20" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
              <path d="m5 5 10 10M15 5 5 15" />
            </svg>
          </button>
        </header>

        <div className="mt-5 flex justify-center">
          <div className="h-36 w-36 overflow-hidden rounded-3xl border border-border bg-card sm:h-40 sm:w-40">
            {avatarUrl ? <img src={avatarUrl} alt="Аватар профиля" className="h-full w-full object-cover" /> : <div className="flex h-full w-full items-center justify-center text-3xl font-semibold">{initials}</div>}
          </div>
        </div>

        <div
          className="mt-5 flex min-h-28 cursor-pointer flex-col items-center justify-center rounded-2xl border border-dashed border-border-strong bg-card p-4 text-center transition hover:border-border-strong hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          role="button"
          tabIndex={0}
          onClick={() => inputRef.current?.click()}
          onKeyDown={(event) => {
            if (event.key === "Enter" || event.key === " ") inputRef.current?.click();
          }}
          onDragOver={(event) => event.preventDefault()}
          onDrop={(event) => {
            event.preventDefault();
            acceptFile(event.dataTransfer.files?.[0] ?? null);
          }}
        >
          <svg aria-hidden="true" viewBox="0 0 24 24" className="h-6 w-6 text-primary" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">
            <path d="M12 16V4m0 0L7.5 8.5M12 4l4.5 4.5" />
            <path d="M5 14v4a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-4" />
          </svg>
          <div className="mt-2 text-sm font-medium text-foreground">Выберите файл или перетащите сюда</div>
          <div className="mt-1 text-xs text-muted-foreground">Изображение сохранится вместе с профилем</div>
        </div>

        <input ref={inputRef} type="file" accept="image/*" className="sr-only" onChange={(event) => acceptFile(event.target.files?.[0] ?? null)} />
        {status ? <div className="mt-3 text-center text-xs text-muted-foreground">{status}</div> : null}
      </section>
    </div>,
    document.body
  );
}
