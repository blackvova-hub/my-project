import { useId, type ReactNode } from "react";
import { LayoutGroup, motion, useReducedMotion } from "framer-motion";

export function Panel({
  children,
  ...props
}: {
  children: ReactNode;
  className?: string;
  id?: string;
  "aria-live"?: "polite" | "assertive" | "off";
  "aria-label"?: string;
}) {
  const reduceMotion = useReducedMotion();
  return (
    <motion.section
      {...props}
      initial={reduceMotion ? false : { opacity: 0, y: 14 }}
      whileInView={{ opacity: 1, y: 0 }}
      viewport={{ once: true, amount: 0.08 }}
      transition={{
        duration: reduceMotion ? 0 : 0.35,
        ease: [0.22, 1, 0.36, 1],
      }}
    >
      {children}
    </motion.section>
  );
}

export function Icon({
  name,
  size = 18,
}: {
  name:
    | "plus"
    | "close"
    | "chart"
    | "play"
    | "history"
    | "download"
    | "upload"
    | "left"
    | "right";
  size?: number;
}) {
  if (name === "play")
    return (
      <svg
        width={size}
        height={size}
        viewBox="0 0 24 24"
        fill="currentColor"
        aria-hidden="true"
      >
        <path d="M7 4.7a1 1 0 0 1 1.5-.86l12 7.3a1 1 0 0 1 0 1.72l-12 7.3A1 1 0 0 1 7 19.3Z" />
      </svg>
    );
  const paths = {
    plus: "M12 5v14M5 12h14",
    close: "m6 6 12 12M6 18 18 6",
    chart: "M4 13h4v8H4zM10 8h4v13h-4zM16 3h4v18h-4z",
    history: "M3 10a9 9 0 1 1 1 7M3 4v6h6M12 7v5l3 2",
    download: "M12 3v12m-5-5 5 5 5-5M4 17v4h16v-4",
    upload: "M12 15V3m-5 5 5-5 5 5M4 17v4h16v-4",
    left: "m15 5-7 7 7 7",
    right: "m9 5 7 7-7 7",
  };
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d={paths[name]} />
    </svg>
  );
}
export function SectionTitle({ children }: { children: ReactNode }) {
  return <h2 className="bt-section-title">{children}</h2>;
}
export function NumberField({
  label,
  value,
  onChange,
  min = 0,
  max,
  step = "any",
  suffix,
  hint,
}: {
  label: string;
  value: number;
  onChange: (value: number) => void;
  min?: number;
  max?: number;
  step?: string | number;
  suffix?: string;
  hint?: string;
}) {
  const id = useId();
  return (
    <div className="bt-field">
      <label htmlFor={id}>{label}</label>
      <div className="bt-input-wrap">
        <input
          id={id}
          aria-describedby={hint ? `${id}-hint` : undefined}
          required
          type="number"
          inputMode="decimal"
          value={Number.isFinite(value) ? value : ""}
          onChange={(e) => onChange(e.currentTarget.valueAsNumber)}
          min={min}
          max={max}
          step={step}
        />
        {suffix ? <span>{suffix}</span> : null}
      </div>
      {hint ? (
        <span className="bt-caption" id={`${id}-hint`}>
          {hint}
        </span>
      ) : null}
    </div>
  );
}
export function Segments<T extends string>({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: T;
  options: { value: T; label: string; disabled?: boolean }[];
  onChange: (value: T) => void;
}) {
  const layoutId = useId();
  const reduceMotion = useReducedMotion();
  return (
    <LayoutGroup id={layoutId}>
      <div className="bt-segments" role="group" aria-label={label}>
        {options.map((option) => (
          <motion.button
            type="button"
            key={option.value}
            aria-pressed={value === option.value}
            disabled={option.disabled}
            onClick={() => onChange(option.value)}
            whileTap={reduceMotion ? undefined : { scale: 0.98 }}
          >
            {value === option.value ? (
              <motion.span
                className="bt-segment-active"
                layoutId="active"
                aria-hidden="true"
                transition={
                  reduceMotion
                    ? { duration: 0 }
                    : { type: "spring", stiffness: 520, damping: 38, mass: 0.7 }
                }
              />
            ) : null}
            <span>{option.label}</span>
          </motion.button>
        ))}
      </div>
    </LayoutGroup>
  );
}
