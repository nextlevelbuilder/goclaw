import { useTranslation } from "react-i18next";

export type LoginMode = "token" | "pairing";

interface LoginTabsProps {
  mode: LoginMode;
  onModeChange: (mode: LoginMode) => void;
}

export function LoginTabs({ mode, onModeChange }: LoginTabsProps) {
  const { t } = useTranslation("login");
  return (
    <div className="flex rounded-md border bg-muted p-1">
      <button
        type="button"
        onClick={() => onModeChange("token")}
        className={`flex-1 rounded-sm px-3 py-1.5 text-sm font-medium transition-colors ${
          mode === "token"
            ? "bg-background text-foreground shadow-sm"
            : "text-muted-foreground hover:text-foreground"
        }`}
      >
        {t("tabs.token")}
      </button>
      <button
        type="button"
        onClick={() => onModeChange("pairing")}
        className={`flex-1 rounded-sm px-3 py-1.5 text-sm font-medium transition-colors ${
          mode === "pairing"
            ? "bg-background text-foreground shadow-sm"
            : "text-muted-foreground hover:text-foreground"
        }`}
      >
        {t("tabs.pairing")}
      </button>
    </div>
  );
}
