import { useCallback, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { flushSync } from "react-dom";
import { ThemeContext } from "./ThemeContext";
import {
  applyTheme,
  getInitialTheme,
  readStoredTheme,
  THEME_STORAGE_KEY,
  type Theme,
} from "./theme";

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setCurrentTheme] = useState<Theme>(getInitialTheme);
  const transitionTimeout = useRef<number | null>(null);
  const activeTransition = useRef<ViewTransition | null>(null);

  useEffect(() => {
    if (document.documentElement.dataset.theme !== theme) {
      applyTheme(theme);
    }
  }, [theme]);

  useEffect(() => {
    const preference = window.matchMedia("(prefers-color-scheme: dark)");
    const syncSystemTheme = () => {
      if (!readStoredTheme()) {
        setCurrentTheme(preference.matches ? "dark" : "light");
      }
    };
    const syncStoredTheme = (event: StorageEvent) => {
      if (event.key === THEME_STORAGE_KEY || event.key === null) {
        setCurrentTheme(getInitialTheme());
      }
    };

    preference.addEventListener("change", syncSystemTheme);
    window.addEventListener("storage", syncStoredTheme);
    return () => {
      preference.removeEventListener("change", syncSystemTheme);
      window.removeEventListener("storage", syncStoredTheme);
    };
  }, []);

  useEffect(() => () => {
    if (transitionTimeout.current !== null) {
      window.clearTimeout(transitionTimeout.current);
    }
    document.documentElement.classList.remove("theme-switching");
    activeTransition.current?.skipTransition();
    document.documentElement.classList.remove("theme-revealing");
  }, []);

  const setTheme = useCallback((nextTheme: Theme, origin?: { x: number; y: number }) => {
    const root = document.documentElement;
    if (root.dataset.theme === nextTheme && !activeTransition.current) return;
    const shouldAnimate = !window.matchMedia("(prefers-reduced-motion: reduce)").matches;

    if (transitionTimeout.current !== null) {
      window.clearTimeout(transitionTimeout.current);
      transitionTimeout.current = null;
    }

    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, nextTheme);
    } catch {
      // The selected theme still applies for this session when storage is unavailable.
    }
    const updateTheme = () => {
      applyTheme(nextTheme);
      flushSync(() => setCurrentTheme(nextTheme));
    };

    // Finish a previous reveal before starting another, including rapid clicks.
    const previous = activeTransition.current;
    previous?.skipTransition();
    root.classList.remove("theme-switching");

    if (shouldAnimate && document.startViewTransition) {
      const x = origin?.x ?? window.innerWidth / 2;
      const y = origin?.y ?? 0;
      const radius = Math.hypot(
        Math.max(x, window.innerWidth - x),
        Math.max(y, window.innerHeight - y),
      );
      root.style.setProperty("--theme-reveal-x", `${x}px`);
      root.style.setProperty("--theme-reveal-y", `${y}px`);
      root.style.setProperty("--theme-reveal-radius", `${radius}px`);
      root.classList.add("theme-revealing");

      const transition = document.startViewTransition(async () => {
        if (previous) await previous.updateCallbackDone.catch(() => {});
        updateTheme();
      });
      activeTransition.current = transition;
      // ready rejects when a transition is skipped or the tab is not visible.
      void transition.ready.catch(() => {});
      void transition.finished.catch(() => {}).finally(() => {
        if (activeTransition.current !== transition) return;
        activeTransition.current = null;
        root.classList.remove("theme-revealing");
      });
      return;
    }

    root.classList.remove("theme-revealing");
    root.classList.add("theme-switching");
    if (shouldAnimate) void window.getComputedStyle(root).color;
    const applyFallback = () => {
      updateTheme();
      transitionTimeout.current = window.setTimeout(() => {
        root.classList.remove("theme-switching");
        transitionTimeout.current = null;
      }, 320);
    };
    if (previous) {
      void previous.updateCallbackDone.catch(() => {}).then(applyFallback);
    } else {
      applyFallback();
    }
  }, []);

  return (
    <ThemeContext.Provider value={{ theme, setTheme }}>
      {children}
    </ThemeContext.Provider>
  );
}
