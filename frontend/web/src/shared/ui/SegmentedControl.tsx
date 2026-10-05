import { useId } from "react";
import { LayoutGroup, motion, useReducedMotion } from "framer-motion";

// Extracted from ScannerConfig so the scanner and arbitrage controls stay identical.
export function SegmentedControl({
  value,
  onChange,
  options,
  label,
  wrap = false,
}: {
  value: string;
  onChange: (v: string) => void;
  options: Array<{ value: string; label: string }>;
  label?: string;
  wrap?: boolean;
}) {
  const layoutId = useId();
  const reduceMotion = useReducedMotion();
  return (
    <LayoutGroup id={layoutId}>
      <div
        role="group"
        aria-label={label}
        className={`inline-flex rounded-xl border border-border bg-background p-1 ${wrap ? "flex-wrap" : ""}`}
      >
        {options.map((o) => {
          const active = o.value === value;
          return (
            <motion.button
              key={o.value}
              type="button"
              aria-pressed={active}
              onClick={() => onChange(o.value)}
              whileTap={reduceMotion ? undefined : { scale: 0.98 }}
              className={`relative isolate rounded-lg px-3 py-1.5 text-sm font-semibold transition-colors duration-200 focus-visible:outline-2 focus-visible:outline-ring ${active ? "text-primary-foreground" : "text-foreground hover:bg-secondary"}`}
            >
              {active && (
                <motion.span
                  aria-hidden="true"
                  layoutId="segment-active"
                  className="absolute inset-0 -z-10 rounded-lg bg-primary"
                  transition={
                    reduceMotion
                      ? { duration: 0 }
                      : {
                          type: "spring",
                          stiffness: 520,
                          damping: 38,
                          mass: 0.7,
                        }
                  }
                />
              )}
              <span className="relative z-10">{o.label}</span>
            </motion.button>
          );
        })}
      </div>
    </LayoutGroup>
  );
}
