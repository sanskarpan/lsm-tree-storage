import { useCallback, useEffect, useState } from "react";

export type Theme = "light" | "dark";
export type Density = "comfortable" | "compact";

const THEME_KEY = "lsm.theme";
const DENSITY_KEY = "lsm.density";

function readStoredTheme(): Theme {
  if (typeof window === "undefined") return "dark";
  const stored = window.localStorage.getItem(THEME_KEY);
  if (stored === "light" || stored === "dark") return stored;
  return window.matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light";
}

function readStoredDensity(): Density {
  if (typeof window === "undefined") return "comfortable";
  const stored = window.localStorage.getItem(DENSITY_KEY);
  return stored === "compact" || stored === "comfortable"
    ? stored
    : "comfortable";
}

function applyTheme(theme: Theme) {
  if (typeof document === "undefined") return;
  document.documentElement.classList.toggle("dark", theme === "dark");
  document.documentElement.dataset.theme = theme;
}

function applyDensity(density: Density) {
  if (typeof document === "undefined") return;
  document.documentElement.classList.toggle("compact", density === "compact");
  document.documentElement.dataset.density = density;
}

export interface ThemeController {
  theme: Theme;
  density: Density;
  setTheme: (next: Theme) => void;
  setDensity: (next: Density) => void;
  toggleTheme: () => void;
  toggleDensity: () => void;
}

export function useTheme(): ThemeController {
  const [theme, setThemeState] = useState<Theme>(readStoredTheme);
  const [density, setDensityState] = useState<Density>(readStoredDensity);

  useEffect(() => {
    applyTheme(theme);
    window.localStorage.setItem(THEME_KEY, theme);
  }, [theme]);

  useEffect(() => {
    applyDensity(density);
    window.localStorage.setItem(DENSITY_KEY, density);
  }, [density]);

  const setTheme = useCallback((next: Theme) => setThemeState(next), []);
  const setDensity = useCallback(
    (next: Density) => setDensityState(next),
    [],
  );
  const toggleTheme = useCallback(
    () => setThemeState((current) => (current === "dark" ? "light" : "dark")),
    [],
  );
  const toggleDensity = useCallback(
    () =>
      setDensityState((current) =>
        current === "comfortable" ? "compact" : "comfortable",
      ),
    [],
  );

  return { theme, density, setTheme, setDensity, toggleTheme, toggleDensity };
}
