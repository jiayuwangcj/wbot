import { useCallback, useEffect, useState } from "react";
import { theme as antdTheme, type ThemeConfig } from "antd";
import { getAntdTokens } from "../lib/themeTokens";

export type ThemeMode = "light" | "dark" | "dark-contrast";

const STORAGE_KEY = "wbot-theme";

function systemTheme(): ThemeMode {
  if (typeof window === "undefined" || !window.matchMedia) return "light";
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function storedTheme(): ThemeMode | null {
  try {
    const value = window.localStorage.getItem(STORAGE_KEY);
    return value === "light" || value === "dark" || value === "dark-contrast" ? value : null;
  } catch {
    return null;
  }
}

function applyTheme(mode: ThemeMode): void {
  document.documentElement.dataset.theme = mode;
}

export interface ThemeState {
  theme: ThemeMode;
  setTheme: (mode: ThemeMode) => void;
  toggleTheme: () => void;
  antdConfig: ThemeConfig;
}

export function useTheme(): ThemeState {
  const [theme, setThemeState] = useState<ThemeMode>(() => storedTheme() ?? systemTheme());

  useEffect(() => {
    applyTheme(theme);
    const media = window.matchMedia?.("(prefers-color-scheme: dark)");
    const saved = storedTheme();
    if (!media || saved) return undefined;
    const onChange = (event: MediaQueryListEvent): void => {
      setThemeState(event.matches ? "dark" : "light");
    };
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, [theme]);

  const setTheme = useCallback((mode: ThemeMode): void => {
    try {
      window.localStorage.setItem(STORAGE_KEY, mode);
    } catch {
      // Storage can be disabled by a browser privacy setting.
    }
    setThemeState(mode);
  }, []);

  const toggleTheme = useCallback((): void => {
    const next: Record<ThemeMode, ThemeMode> = {
      light: "dark",
      dark: "dark-contrast",
      "dark-contrast": "light",
    };
    setTheme(next[theme]);
  }, [setTheme, theme]);

  const antdConfig: ThemeConfig = {
    algorithm: theme === "light" ? antdTheme.defaultAlgorithm : antdTheme.darkAlgorithm,
    token: getAntdTokens(),
  };

  return { theme, setTheme, toggleTheme, antdConfig };
}
