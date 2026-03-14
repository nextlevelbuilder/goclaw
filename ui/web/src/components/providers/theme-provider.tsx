import { useEffect } from "react";
import { useUiStore, type Theme } from "@/stores/use-ui-store";

function applyTheme(theme: Theme) {
  const root = document.documentElement;
  root.classList.remove("light", "dark");

  if (theme === "system") {
    const systemDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    root.classList.add(systemDark ? "dark" : "light");
  } else {
    root.classList.add(theme);
  }
}

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const theme = useUiStore((s) => s.theme);
  const lockedTheme = useUiStore((s) => s.lockedTheme);

  const effectiveTheme = lockedTheme ?? theme;

  useEffect(() => {
    applyTheme(effectiveTheme);
  }, [effectiveTheme]);

  // Listen for system theme changes only when unlocked and in "system" mode
  useEffect(() => {
    if (lockedTheme !== null || theme !== "system") return;
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const handler = () => applyTheme("system");
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, [theme, lockedTheme]);

  return <>{children}</>;
}
