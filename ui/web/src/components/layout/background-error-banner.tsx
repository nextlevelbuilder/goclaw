import { useState, useEffect, useCallback, useRef } from "react";
import { useTranslation } from "react-i18next";
import { AlertTriangle, X, Settings2 } from "lucide-react";
import { useHttp } from "@/hooks/use-ws";
import { useWsEvent } from "@/hooks/use-ws-event";
import { useAuthStore } from "@/stores/use-auth-store";
import { Button } from "@/components/ui/button";

interface ProviderErrorPayload {
  reason: string;
  worker: string;
  message: string;
  timestamp: string;
}

interface BackgroundErrorBannerProps {
  settingsOpen: boolean;
  onOpenSettings: () => void;
}

export function BackgroundErrorBanner({ settingsOpen, onOpenSettings }: BackgroundErrorBannerProps) {
  const { t } = useTranslation("system-settings");
  const http = useHttp();
  const role = useAuthStore((s) => s.role);
  const isAdmin = role === "admin" || role === "owner";
  const [error, setError] = useState<ProviderErrorPayload | null>(null);
  const [dismissed, setDismissed] = useState(false);

  // Fetch alert state from the specific system config key.
  const fetchAlert = useCallback(() => {
    if (!isAdmin) return;
    http.get<{ key: string; value: string }>("/v1/system-configs/alert.background.provider_error")
      .then((res) => {
        if (res.value) {
          try { setError(JSON.parse(res.value)); } catch { /* ignore */ }
        } else {
          setError(null);
        }
      })
      .catch(() => { setError(null); }); // 404 = no alert
  }, [http, isAdmin]);

  // Fetch on mount
  useEffect(() => { fetchAlert(); }, [fetchAlert]);

  // Re-fetch when settings modal closes (user may have fixed the issue)
  const prevSettingsOpen = useRef(settingsOpen);
  useEffect(() => {
    if (prevSettingsOpen.current && !settingsOpen) fetchAlert();
    prevSettingsOpen.current = settingsOpen;
  }, [settingsOpen, fetchAlert]);

  // Real-time WS event
  useWsEvent("background.error", useCallback((payload: unknown) => {
    setError(payload as ProviderErrorPayload);
    setDismissed(false);
  }, []));

  if (!isAdmin || !error || dismissed) return null;

  const reasonKey = `alert.reason.${error.reason}`;
  const reasonText = t(reasonKey, { defaultValue: t("alert.reason.unknown") });

  return (
    <div className="flex items-center gap-3 border-b border-destructive/30 bg-destructive/10 px-4 py-2 text-sm text-destructive">
      <AlertTriangle className="h-4 w-4 shrink-0" />
      <span className="flex-1">
        {t("alert.message", { reason: reasonText })}
      </span>
      <Button variant="outline" size="sm" onClick={onOpenSettings} className="shrink-0 gap-1.5 h-7 text-xs">
        <Settings2 className="h-3.5 w-3.5" />
        {t("alert.openSettings")}
      </Button>
      <button
        onClick={() => setDismissed(true)}
        className="shrink-0 rounded p-1 hover:bg-destructive/20"
      >
        <X className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}
