import { useTranslation } from "react-i18next";
import { useAuthStore } from "@/stores/use-auth-store";
import { cn } from "@/lib/utils";

export function ConnectionStatus({ collapsed }: { collapsed?: boolean }) {
  const { t } = useTranslation("common");
  const connected = useAuthStore((s) => s.connected);

  return (
    <div className="flex items-center gap-2 text-xs text-muted-foreground overflow-hidden">
      <span
        className={cn(
          "h-2 w-2 shrink-0 rounded-full",
          connected ? "bg-green-500" : "bg-red-500",
        )}
      />
      {!collapsed && (
        <span className="truncate">{connected ? t("connected") : t("disconnected")}</span>
      )}
    </div>
  );
}
