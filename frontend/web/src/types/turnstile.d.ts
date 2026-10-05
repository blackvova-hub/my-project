declare module "@marsidev/react-turnstile" {
  import * as React from "react";

  export type TurnstileProps = {
    siteKey: string;
    onSuccess?: (token: string) => void;
    onExpire?: () => void;
    onError?: () => void;
    options?: Record<string, unknown>;
    className?: string;
    style?: React.CSSProperties;
  };

  export const Turnstile: React.FC<TurnstileProps>;
}
