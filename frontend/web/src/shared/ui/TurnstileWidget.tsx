import { useEffect, useRef } from "react";

type TurnstileWidgetProps = {
  siteKey: string;
  onSuccess: (token: string) => void;
  onExpire?: () => void;
  onError?: (error?: string) => void;
  className?: string;
};

declare global {
  interface Window {
    turnstile?: {
      render: (container: HTMLElement, params: Record<string, unknown>) => string | undefined;
      reset: (widgetId?: string) => void;
      remove: (widgetId?: string) => void;
    };
  }
}

const SCRIPT_ID = "cf-turnstile-script";
const SCRIPT_SRC = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";

export default function TurnstileWidget({
  siteKey,
  onSuccess,
  onExpire,
  onError,
  className,
}: TurnstileWidgetProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const widgetIdRef = useRef<string | undefined>(undefined);
  const onSuccessRef = useRef(onSuccess);
  const onExpireRef = useRef(onExpire);
  const onErrorRef = useRef(onError);

  useEffect(() => {
    onSuccessRef.current = onSuccess;
    onExpireRef.current = onExpire;
    onErrorRef.current = onError;
  }, [onSuccess, onExpire, onError]);

  useEffect(() => {
    if (!siteKey || !containerRef.current) return;

    const renderWidget = () => {
      if (!window.turnstile || !containerRef.current) return;
      if (widgetIdRef.current) return;
      widgetIdRef.current = window.turnstile.render(containerRef.current, {
        sitekey: siteKey,
        callback: (token: string) => onSuccessRef.current(token),
        "expired-callback": () => onExpireRef.current?.(),
        "error-callback": (err?: string) => onErrorRef.current?.(err),
        "refresh-expired": "auto",
        retry: "auto",
        "response-field": false,
        appearance: "always",
        execution: "render",
        size: "normal",
      });
    };

    if (window.turnstile) {
      renderWidget();
      return;
    }

    let script = document.getElementById(SCRIPT_ID) as HTMLScriptElement | null;
    if (!script) {
      script = document.createElement("script");
      script.id = SCRIPT_ID;
      script.src = SCRIPT_SRC;
      script.async = true;
      script.defer = true;
      document.head.appendChild(script);
    }

    const handleLoad = () => renderWidget();
    script.addEventListener("load", handleLoad);

    return () => {
      if (widgetIdRef.current && window.turnstile) {
        window.turnstile.remove(widgetIdRef.current);
        widgetIdRef.current = undefined;
      }
      script?.removeEventListener("load", handleLoad);
    };
  }, [siteKey]);

  return <div className={className} ref={containerRef} />;
}
