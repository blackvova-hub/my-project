import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import {
  Children,
  Fragment,
  isValidElement,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type ReactNode,
  type SelectHTMLAttributes,
} from "react";
import { createPortal } from "react-dom";

export type AnimatedSelectOption = {
  value: string;
  label: string;
  disabled?: boolean;
};

type Props = {
  value: string;
  onChange: (value: string) => void;
  options: AnimatedSelectOption[];
  className?: string;
  disabled?: boolean;
  ariaLabel?: string;
};

export function AnimatedSelect({
  value,
  onChange,
  options,
  className = "",
  disabled = false,
  ariaLabel,
}: Props) {
  const reduceMotion = useReducedMotion();
  const buttonRef = useRef<HTMLButtonElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const listboxId = useId();
  const typeahead = useRef({ text: "", at: 0 });
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const [position, setPosition] = useState<{
    left: number;
    top?: number;
    bottom?: number;
    width: number;
    maxHeight: number;
    origin: "top" | "bottom";
  }>({ left: 0, top: 0, width: 0, maxHeight: 320, origin: "top" });
  const selectedIndex = Math.max(
    0,
    options.findIndex((option) => option.value === value),
  );
  const selected = options[selectedIndex] ?? options[0];

  const enabledIndexes = useMemo(
    () =>
      options
        .map((option, index) => (!option.disabled ? index : -1))
        .filter((index) => index >= 0),
    [options],
  );

  const updatePosition = () => {
    const rect = buttonRef.current?.getBoundingClientRect();
    if (!rect) return;
    const gap = 6;
    const viewportPadding = 8;
    const preferredHeight = 320;
    const width = Math.min(
      Math.max(rect.width, 220),
      window.innerWidth - viewportPadding * 2,
    );
    const left = Math.min(
      Math.max(viewportPadding, rect.left),
      window.innerWidth - width - viewportPadding,
    );
    const spaceBelow = window.innerHeight - rect.bottom - gap - viewportPadding;
    const spaceAbove = rect.top - gap - viewportPadding;
    const openUp = spaceBelow < 180 && spaceAbove > spaceBelow;
    const maxHeight = Math.max(
      120,
      Math.min(preferredHeight, openUp ? spaceAbove : spaceBelow),
    );
    setPosition({
      left,
      top: openUp ? undefined : rect.bottom + gap,
      bottom: openUp ? window.innerHeight - rect.top + gap : undefined,
      width,
      maxHeight,
      origin: openUp ? "bottom" : "top",
    });
  };

  useEffect(() => {
    if (!open) return;
    const close = (event: PointerEvent) => {
      const target = event.target as Node;
      if (
        !buttonRef.current?.contains(target) &&
        !listRef.current?.contains(target)
      )
        setOpen(false);
    };
    const closeOnViewportChange = () => setOpen(false);
    const closeOnPageScroll = (event: Event) => {
      const target = event.target;
      if (target instanceof Node && listRef.current?.contains(target)) return;
      setOpen(false);
    };
    document.addEventListener("pointerdown", close);
    window.addEventListener("resize", closeOnViewportChange);
    window.addEventListener("scroll", closeOnPageScroll, true);
    window.addEventListener("wheel", closeOnPageScroll, {
      capture: true,
      passive: true,
    });
    window.addEventListener("touchmove", closeOnPageScroll, {
      capture: true,
      passive: true,
    });
    return () => {
      document.removeEventListener("pointerdown", close);
      window.removeEventListener("resize", closeOnViewportChange);
      window.removeEventListener("scroll", closeOnPageScroll, true);
      window.removeEventListener("wheel", closeOnPageScroll, true);
      window.removeEventListener("touchmove", closeOnPageScroll, true);
    };
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const list = listRef.current;
    const option = list?.querySelector<HTMLElement>(
      '[data-option-index="' + activeIndex + '"]',
    );
    if (!list || !option) return;
    // Scroll the menu itself. scrollIntoView also scrolls the page underneath
    // the portal, which triggers closeOnPageScroll while the menu is opening.
    const top = option.offsetTop;
    const bottom = top + option.offsetHeight;
    if (top < list.scrollTop) list.scrollTop = top;
    else if (bottom > list.scrollTop + list.clientHeight)
      list.scrollTop = bottom - list.clientHeight;
  }, [activeIndex, open]);

  const move = (direction: 1 | -1) => {
    if (!enabledIndexes.length) return;
    const current = enabledIndexes.indexOf(activeIndex);
    const next =
      current < 0
        ? 0
        : (current + direction + enabledIndexes.length) % enabledIndexes.length;
    setActiveIndex(enabledIndexes[next]);
  };

  const selectIndex = (index: number) => {
    const option = options[index];
    if (!option || option.disabled) return;
    onChange(option.value);
    setOpen(false);
    buttonRef.current?.focus();
  };

  const show = () => {
    updatePosition();
    setActiveIndex(selectedIndex);
    setOpen(true);
  };

  return (
    <>
      <button
        ref={buttonRef}
        type="button"
        role="combobox"
        disabled={disabled}
        aria-label={ariaLabel}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={listboxId}
        aria-activedescendant={
          open ? `${listboxId}-option-${activeIndex}` : undefined
        }
        onClick={() => {
          if (disabled) return;
          if (open) setOpen(false);
          else show();
        }}
        onKeyDown={(event) => {
          if (disabled) return;
          if (event.key === "ArrowDown" || event.key === "ArrowUp") {
            event.preventDefault();
            if (!open) {
              show();
            } else {
              move(event.key === "ArrowDown" ? 1 : -1);
            }
          } else if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            if (open) selectIndex(activeIndex);
            else show();
          } else if (event.key === "Escape") {
            if (open) event.stopPropagation();
            setOpen(false);
          } else if (event.key === "Tab") {
            setOpen(false);
          } else if (event.key === "Home" && open && enabledIndexes.length) {
            event.preventDefault();
            setActiveIndex(enabledIndexes[0]);
          } else if (event.key === "End" && open && enabledIndexes.length) {
            event.preventDefault();
            setActiveIndex(enabledIndexes[enabledIndexes.length - 1]);
          } else if (
            event.key.length === 1 &&
            !event.ctrlKey &&
            !event.metaKey &&
            !event.altKey
          ) {
            const now = Date.now();
            const query =
              (now - typeahead.current.at < 650 ? typeahead.current.text : "") +
              event.key.toLocaleLowerCase();
            typeahead.current = { text: query, at: now };
            const match = options.findIndex(
              (option) =>
                !option.disabled &&
                option.label.toLocaleLowerCase().startsWith(query),
            );
            if (match >= 0) {
              if (!open) show();
              setActiveIndex(match);
            }
          }
        }}
        className={[
          "group flex appearance-none items-center justify-between gap-3 bg-none text-left transition-[border-color,background-color,color,box-shadow,transform] duration-200 disabled:cursor-not-allowed disabled:opacity-50",
          className,
        ].join(" ")}
      >
        <span className="min-w-0 truncate">{selected?.label ?? value}</span>
        <motion.svg
          aria-hidden="true"
          viewBox="0 0 20 20"
          className="h-4 w-4 shrink-0 text-current opacity-70"
          animate={{ rotate: open ? 180 : 0 }}
          transition={
            reduceMotion
              ? { duration: 0 }
              : { duration: 0.2, ease: [0.22, 1, 0.36, 1] }
          }
        >
          <path fill="currentColor" d="M5.25 7.5 10 12.25 14.75 7.5Z" />
        </motion.svg>
      </button>

      {typeof document !== "undefined"
        ? createPortal(
            <AnimatePresence>
              {open ? (
                <motion.div
                  ref={listRef}
                  id={listboxId}
                  role="listbox"
                  aria-label={ariaLabel}
                  initial={
                    reduceMotion
                      ? false
                      : {
                          opacity: 0,
                          y: position.origin === "bottom" ? 8 : -8,
                          scaleY: 0.96,
                        }
                  }
                  animate={{ opacity: 1, y: 0, scaleY: 1 }}
                  exit={
                    reduceMotion
                      ? { opacity: 0 }
                      : {
                          opacity: 0,
                          y: position.origin === "bottom" ? 5 : -5,
                          scaleY: 0.97,
                        }
                  }
                  transition={{
                    duration: reduceMotion ? 0 : 0.18,
                    ease: [0.22, 1, 0.36, 1],
                  }}
                  style={{
                    left: position.left,
                    top: position.top,
                    bottom: position.bottom,
                    width: position.width,
                    maxHeight: position.maxHeight,
                    transformOrigin: position.origin,
                  }}
                  className="fixed z-[250] overflow-y-auto overscroll-contain rounded-xl border border-border bg-popover p-1.5 text-popover-foreground shadow-md"
                >
                  {options.map((option, index) => {
                    const selectedOption = option.value === value;
                    const highlighted = index === activeIndex;
                    return (
                      <button
                        key={option.value}
                        type="button"
                        role="option"
                        id={`${listboxId}-option-${index}`}
                        tabIndex={-1}
                        aria-selected={selectedOption}
                        disabled={option.disabled}
                        data-option-index={index}
                        onPointerMove={() =>
                          !option.disabled && setActiveIndex(index)
                        }
                        onClick={() => selectIndex(index)}
                        className={[
                          "flex w-full items-center justify-between rounded-lg px-3 py-2.5 text-left text-sm transition-colors duration-150",
                          selectedOption
                            ? "bg-accent text-accent-foreground"
                            : highlighted
                              ? "bg-secondary text-secondary-foreground"
                              : "text-popover-foreground hover:bg-secondary",
                          option.disabled
                            ? "cursor-not-allowed opacity-40"
                            : "",
                        ].join(" ")}
                      >
                        <span>{option.label}</span>
                        {selectedOption ? (
                          <svg
                            aria-hidden="true"
                            width="16"
                            height="16"
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                            strokeWidth="1.65"
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            className="shrink-0 text-primary"
                          >
                            <path d="m5 12 4 4 10-10" />
                          </svg>
                        ) : null}
                      </button>
                    );
                  })}
                </motion.div>
              ) : null}
            </AnimatePresence>,
            document.body,
          )
        : null}
    </>
  );
}

type NativeSelectProps = Omit<
  SelectHTMLAttributes<HTMLSelectElement>,
  "value" | "onChange"
> & {
  value: string | number;
  onChange: (event: ChangeEvent<HTMLSelectElement>) => void;
};

function nodeText(node: ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (!isValidElement<{ children?: ReactNode }>(node)) return "";
  return Children.toArray(node.props.children).map(nodeText).join("");
}

function optionsFromChildren(children: ReactNode): AnimatedSelectOption[] {
  const result: AnimatedSelectOption[] = [];
  Children.forEach(children, (child) => {
    if (
      !isValidElement<{
        value?: string | number;
        disabled?: boolean;
        children?: ReactNode;
      }>(child)
    )
      return;
    if (child.type === Fragment) {
      result.push(...optionsFromChildren(child.props.children));
      return;
    }
    if (child.type !== "option") return;
    result.push({
      value: String(child.props.value ?? nodeText(child.props.children)),
      label: nodeText(child.props.children),
      disabled: child.props.disabled,
    });
  });
  return result;
}

export function AnimatedNativeSelect({
  value,
  onChange,
  children,
  className,
  disabled,
  ...props
}: NativeSelectProps) {
  const options = useMemo(() => optionsFromChildren(children), [children]);
  return (
    <AnimatedSelect
      value={String(value)}
      onChange={(next) =>
        onChange({ target: { value: next } } as ChangeEvent<HTMLSelectElement>)
      }
      options={options}
      className={className}
      disabled={disabled}
      ariaLabel={props["aria-label"]}
    />
  );
}
