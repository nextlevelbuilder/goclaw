import { useTranslation } from "react-i18next";
import { useAuthStore } from "@/stores/use-auth-store";
import { cn } from "@/lib/utils";
import { cleanVersion } from "@/lib/clean-version";

const ROLE_STYLES: Record<string, string> = {
  admin: "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300",
  owner: "bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-300",
  operator: "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300",
  viewer: "bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300",
};

export function ConnectionStatus({ collapsed }: { collapsed?: boolean }) {
  const { t } = useTranslation("common");
  const connected = useAuthStore((s) => s.connected);
  const serverVersion = useAuthStore((s) => s.serverInfo?.version);
  const tenantName = useAuthStore((s) => s.tenantName);
  const role = useAuthStore((s) => s.role);

  return (
    <div className="space-y-1.5">
      {/* Tenant + role (expanded only) */}
      {!collapsed && tenantName && (
        <div className="flex items-center justify-between gap-1.5 text-xs overflow-hidden">
          <span className="truncate font-medium text-foreground/80">{tenantName}</span>
          {role && (
            <span className={cn("shrink-0 rounded-full px-1.5 py-0.5 text-2xs font-medium", ROLE_STYLES[role] ?? ROLE_STYLES.viewer)}>
              {role}
            </span>
          )}
        </div>
      )}

      {/* Connection status */}
      <div className="flex items-center gap-2 text-xs text-muted-foreground overflow-hidden">
        <span
          className={cn(
            "h-2 w-2 shrink-0 rounded-full",
            connected ? "bg-green-500" : "bg-red-500",
          )}
        />
        {!collapsed && (
          <span className="truncate">
            {connected ? t("connected") : t("disconnected")}
            {connected && serverVersion && (
              <span className="ml-1 opacity-60">· {cleanVersion(serverVersion)}</span>
            )}
          </span>
        )}
      </div>
    </div>
  );
}
