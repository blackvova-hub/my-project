import { createContext, useContext } from "react";
import type { Theme } from "./theme";

type ThemeContextValue = {
  theme: Theme;
  setTheme: (theme: Theme, origin?: { x: number; y: number }) => void;
};

export const ThemeContext = createContext<ThemeContextValue | null>(null);

export function useTheme(): ThemeContextValue {
  const context = useContext(ThemeContext);
  if (!context) throw new Error("useTheme must be used within ThemeProvider");
  return context;
}
