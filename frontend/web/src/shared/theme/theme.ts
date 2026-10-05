export type Theme = "light" | "dark" | "green";

// Keep this key in sync with the early theme script in index.html.
export const THEME_STORAGE_KEY = "site-theme";

const themeClasses = ["light", "dark", "green"] as const;

function isTheme(value: string | null): value is Theme {
  return value === "light" || value === "dark" || value === "green";
}

export function readStoredTheme(): Theme | null {
  try {
    const value = window.localStorage.getItem(THEME_STORAGE_KEY);
    return isTheme(value) ? value : null;
  } catch {
    return null;
  }
}

export function getInitialTheme(): Theme {
  if (typeof window === "undefined") return "light";
  return readStoredTheme() ??
    (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
}

export function applyTheme(theme: Theme): void {
  const root = document.documentElement;
  root.classList.remove(...themeClasses);
  root.classList.add(theme);
  root.dataset.theme = theme;
}
