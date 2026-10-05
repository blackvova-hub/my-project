/* eslint-disable react-refresh/only-export-components */
import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
} from "react";

export type ToastVariant = "success" | "error" | "info";

const MAX_TOASTS = 3;
const DEFAULT_TTL = 3200;
const OVERFLOW_TTL = 1200;

type ToastItem = {
  id: string;
  message: string;
  variant: ToastVariant;
  leaving?: boolean;
};

type ToastContextValue = {
  success: (message: string) => void;
  error: (message: string) => void;
  info: (message: string) => void;
};

const ToastContext = createContext<ToastContextValue | null>(null);

function uid() {
  return Math.random().toString(16).slice(2) + Date.now().toString(16);
}

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<ToastItem[]>([]);
  const timeouts = useRef<Record<string, number>>({});

  const remove = useCallback((id: string) => {
    setToasts((prev) => prev.map((t) => (t.id === id ? { ...t, leaving: true } : t)));
    window.setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id));
      if (timeouts.current[id]) {
        window.clearTimeout(timeouts.current[id]);
        delete timeouts.current[id];
      }
    }, 250);
  }, []);

  const push = useCallback(
    (variant: ToastVariant, message: string) => {
      const id = uid();
      let ttl = DEFAULT_TTL;
      setToasts((prev) => {
        if (prev.length < MAX_TOASTS) {
          return [...prev, { id, message, variant }];
        }

        const [removed, ...rest] = prev;
        if (removed && timeouts.current[removed.id]) {
          window.clearTimeout(timeouts.current[removed.id]);
          delete timeouts.current[removed.id];
        }
        ttl = OVERFLOW_TTL;
        return [...rest, { id, message, variant }];
      });
      timeouts.current[id] = window.setTimeout(() => remove(id), ttl);
    },
    [remove]
  );

  const value = useMemo(
    () => ({
      success: (message: string) => push("success", message),
      error: (message: string) => push("error", message),
      info: (message: string) => push("info", message),
    }),
    [push]
  );

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div
        className="fixed right-4 z-[60] flex w-[260px] max-w-[90vw] flex-col gap-1.5"
        style={{ top: "calc(var(--app-header-offset, 0px) + 16px)" }}
      >
        {toasts.map((toast) => (
          <div
            key={toast.id}
            className={
              "rounded-lg border px-3 py-2 text-xs shadow-lg transition-all duration-200 animate-[toast-in_220ms_ease-out] " +
              (toast.leaving ? "opacity-0 translate-y-1" : "opacity-100 translate-y-0") +
              (toast.variant === "success"
                ? " border-primary/40 bg-primary text-primary-foreground"
                : toast.variant === "error"
                  ? " border-destructive/40 bg-card text-destructive"
                  : " border-border bg-card text-card-foreground")
            }
          >
            {toast.message}
          </div>
        ))}
      </div>
      <style>{`
        @keyframes toast-in {
          from { opacity: 0; transform: translateY(-4px) scale(0.98); }
          to { opacity: 1; transform: translateY(0) scale(1); }
        }
      `}</style>
    </ToastContext.Provider>
  );
}

export function useToast() {
  const ctx = useContext(ToastContext);
  if (!ctx) {
    throw new Error("useToast must be used within ToastProvider");
  }
  return ctx;
}
