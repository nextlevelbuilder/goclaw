import { useEffect } from "react";
import { useUiStore, type Theme } from "@/stores/use-ui-store";

function getBrightnessMode(theme: Theme): "light" | "dark" {
  if (theme === "system") {
    return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }
  return theme;
}

function applyTheme(theme: Theme, colorScheme: string) {
  const root = document.documentElement;
  const brightnessMode = getBrightnessMode(theme);
  // Apply class order: colorScheme brightnessMode (e.g., "neon dark" or "default light")
  root.className = `${colorScheme} ${brightnessMode}`;
}

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const theme = useUiStore((s) => s.theme);
  const colorScheme = useUiStore((s) => s.colorScheme);

  useEffect(() => {
    applyTheme(theme, colorScheme);
  }, [theme, colorScheme]);

  // Listen for system theme changes when in "system" mode
  useEffect(() => {
    if (theme !== "system") return;
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const handler = () => applyTheme(theme, colorScheme);
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, [theme, colorScheme]);

  return <>{children}</>;
}
