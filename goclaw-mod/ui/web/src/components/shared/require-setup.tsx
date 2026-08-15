import { useState, useEffect } from "react";
import { Navigate } from "react-router";
import { WifiOff, Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useBootstrapStatus } from "@/pages/setup/hooks/use-bootstrap-status";
import { useAuthStore } from "@/stores/use-auth-store";
import { ROUTES } from "@/lib/constants";

const CONNECTION_TIMEOUT_MS = 3000;

function SetupLoader() {
  return (
    <div className="flex h-dvh items-center justify-center">
      <div className="h-6 w-6 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
    </div>
  );
}

function DisconnectedOverlay() {
  const { t } = useTranslation("common");
  return (
    <div className="flex h-dvh items-center justify-center bg-background">
      <div className="flex flex-col items-center gap-4 rounded-xl border bg-card p-8 shadow-lg text-center max-w-sm">
        <WifiOff className="h-10 w-10 text-muted-foreground" />
        <h2 className="text-lg font-semibold">{t("serverUnreachable")}</h2>
        <p className="text-sm text-muted-foreground">
          {t("serverUnreachableDesc")}
        </p>
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <Loader2 className="h-3 w-3 animate-spin" />
          <span>{t("reconnecting")}</span>
        </div>
      </div>
    </div>
  );
}

export function RequireSetup({ children }: { children: React.ReactNode }) {
  const { needsSetup, loading } = useBootstrapStatus();
  const connected = useAuthStore((s) => s.connected);
  const [timedOut, setTimedOut] = useState(false);

  useEffect(() => {
    if (connected) {
      setTimedOut(false);
      return;
    }
    const timer = setTimeout(() => setTimedOut(true), CONNECTION_TIMEOUT_MS);
    return () => clearTimeout(timer);
  }, [connected]);

  if (!connected && timedOut) return <DisconnectedOverlay />;
  if (loading) return <SetupLoader />;
  if (needsSetup) return <Navigate to={ROUTES.SETUP} replace />;

  return <>{children}</>;
}
