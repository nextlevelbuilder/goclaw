import { Outlet, useLocation } from "react-router";
import { WifiOff } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Sidebar } from "./sidebar";
import { Topbar } from "./topbar";
import { ErrorBoundary } from "@/components/shared/error-boundary";
import { useUiStore } from "@/stores/use-ui-store";
import { useAuthStore } from "@/stores/use-auth-store";
import { useIsMobile } from "@/hooks/use-media-query";
import { cn } from "@/lib/utils";

export function AppLayout() {
  const { t } = useTranslation("common");
  const location = useLocation();
  const sidebarCollapsed = useUiStore((s) => s.sidebarCollapsed);
  const mobileSidebarOpen = useUiStore((s) => s.mobileSidebarOpen);
  const setMobileSidebarOpen = useUiStore((s) => s.setMobileSidebarOpen);
  const connected = useAuthStore((s) => s.connected);
  const isMobile = useIsMobile();

  return (
    <div className="flex h-dvh overflow-hidden safe-top">
      {isMobile ? (
        <>
          {/* Backdrop */}
          {mobileSidebarOpen && (
            <div
              className="fixed inset-0 z-40 bg-black/50 transition-opacity"
              onClick={() => setMobileSidebarOpen(false)}
            />
          )}
          {/* Slide-out sidebar */}
          <div
            className={cn(
              "fixed inset-y-0 left-0 z-50 safe-top safe-bottom safe-left transition-transform duration-200 ease-in-out",
              mobileSidebarOpen ? "translate-x-0" : "-translate-x-full",
            )}
          >
            <Sidebar collapsed={false} onNavItemClick={() => setMobileSidebarOpen(false)} />
          </div>
        </>
      ) : (
        <Sidebar collapsed={sidebarCollapsed} />
      )}
      <div className="flex flex-1 flex-col overflow-hidden">
        <Topbar />
        {!connected && (
          <div className="flex items-center gap-2 border-b border-destructive/30 bg-destructive/10 px-4 py-2 text-sm text-destructive">
            <WifiOff className="h-4 w-4 shrink-0" />
            <span>{t("disconnectedGateway")}</span>
          </div>
        )}
        <main className="flex-1 overflow-y-auto">
          <ErrorBoundary key={location.pathname}>
            <Outlet />
          </ErrorBoundary>
        </main>
      </div>
    </div>
  );
}
