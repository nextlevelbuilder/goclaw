import { Moon, Sun } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useUiStore } from "@/stores/use-ui-store";

interface LoginLayoutProps {
  children: React.ReactNode;
  subtitle?: string;
}

export function LoginLayout({ children, subtitle }: LoginLayoutProps) {
  const { t } = useTranslation("topbar");
  const theme = useUiStore((s) => s.theme);
  const setTheme = useUiStore((s) => s.setTheme);
  const isDark =
    theme === "dark" ||
    (theme === "system" &&
      window.matchMedia("(prefers-color-scheme: dark)").matches);

  return (
    <div className="relative flex min-h-dvh items-center justify-center bg-background px-4">
      <button
        type="button"
        onClick={() => setTheme(isDark ? "light" : "dark")}
        className="absolute top-4 right-4 cursor-pointer rounded-md border bg-card p-2 text-muted-foreground shadow-sm transition-colors hover:bg-accent hover:text-accent-foreground"
        title={t("toggleTheme")}
        aria-label={t("toggleTheme")}
      >
        {isDark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
      </button>
      <div className="w-full max-w-sm space-y-6 rounded-lg border bg-card p-6 shadow-sm sm:p-8">
        <div className="text-center">
          <img src="/goclaw-icon.svg" alt="GoClaw" className="mx-auto mb-3 h-20 w-20" />
          <h1 className="text-3xl font-bold tracking-tight">GoClaw</h1>
          {subtitle && (
            <p className="mt-2 text-sm text-muted-foreground">{subtitle}</p>
          )}
        </div>
        {children}
      </div>
    </div>
  );
}
