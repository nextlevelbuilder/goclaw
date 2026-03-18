import { Moon, Sun, PanelLeftClose, PanelLeftOpen, Menu, LogOut, Bell, Globe, Clock } from "lucide-react";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { useUiStore } from "@/stores/use-ui-store";
import { useAuthStore } from "@/stores/use-auth-store";
import { useIsMobile } from "@/hooks/use-media-query";
import { usePendingPairingsCount } from "@/hooks/use-pending-pairings-count";
import { ROUTES, SUPPORTED_LANGUAGES, LANGUAGE_LABELS, TIMEZONE_OPTIONS, type Language } from "@/lib/constants";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

export function Topbar() {
  const { t } = useTranslation("topbar");
  const theme = useUiStore((s) => s.theme);
  const setTheme = useUiStore((s) => s.setTheme);
  const language = useUiStore((s) => s.language);
  const setLanguage = useUiStore((s) => s.setLanguage);
  const timezone = useUiStore((s) => s.timezone);
  const setTimezone = useUiStore((s) => s.setTimezone);
  const sidebarCollapsed = useUiStore((s) => s.sidebarCollapsed);
  const toggleSidebar = useUiStore((s) => s.toggleSidebar);
  const setMobileSidebarOpen = useUiStore((s) => s.setMobileSidebarOpen);
  const logout = useAuthStore((s) => s.logout);
  const isMobile = useIsMobile();
  const navigate = useNavigate();
  const { pendingCount } = usePendingPairingsCount({ showToast: true });

  const isDark = theme === "dark" || (theme === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);

  const handleSidebarToggle = isMobile
    ? () => setMobileSidebarOpen(true)
    : toggleSidebar;

  return (
    <header className="flex h-14 items-center justify-between border-b bg-background px-4 landscape-compact">
      <div className="flex items-center gap-2">
        <button
          onClick={handleSidebarToggle}
          className="cursor-pointer rounded-md p-2 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
          title={isMobile ? t("openMenu") : sidebarCollapsed ? t("expandSidebar") : t("collapseSidebar")}
        >
          {isMobile ? (
            <Menu className="h-4 w-4" />
          ) : sidebarCollapsed ? (
            <PanelLeftOpen className="h-4 w-4" />
          ) : (
            <PanelLeftClose className="h-4 w-4" />
          )}
        </button>
      </div>

      <div className="flex items-center gap-2">
        <button
          onClick={() => navigate(ROUTES.NODES)}
          className="relative cursor-pointer rounded-md p-2 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
          title={pendingCount > 0 ? t("pendingPairing", { count: pendingCount }) : t("pairingRequests")}
        >
          <Bell className="h-4 w-4" />
          {pendingCount > 0 && (
            <span className="absolute -top-0.5 -right-0.5 h-2 w-2 rounded-full bg-destructive" />
          )}
        </button>

        <Select value={language} onValueChange={(v) => setLanguage(v as Language)}>
          <SelectTrigger
            title={t("language")}
            className="h-auto w-auto gap-1 border-0 bg-transparent px-2 py-1.5 text-xs text-muted-foreground shadow-none hover:bg-accent hover:text-accent-foreground focus-visible:ring-0 dark:bg-transparent dark:hover:bg-accent **:data-radix-select-icon:hidden"
          >
            <Globe className="h-4 w-4 shrink-0" />
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {SUPPORTED_LANGUAGES.map((lang) => (
              <SelectItem key={lang} value={lang}>{LANGUAGE_LABELS[lang]}</SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={timezone} onValueChange={setTimezone}>
          <SelectTrigger
            title={t("timezone")}
            className="h-auto w-auto gap-1 border-0 bg-transparent px-2 py-1.5 text-xs text-muted-foreground shadow-none hover:bg-accent hover:text-accent-foreground focus-visible:ring-0 dark:bg-transparent dark:hover:bg-accent **:data-radix-select-icon:hidden"
          >
            <Clock className="h-4 w-4 shrink-0" />
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {TIMEZONE_OPTIONS.map((tz) => (
              <SelectItem key={tz.value} value={tz.value}>{tz.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>

        <button
          onClick={() => setTheme(isDark ? "light" : "dark")}
          className="cursor-pointer rounded-md p-2 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
          title={t("toggleTheme")}
        >
          {isDark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
        </button>

        <button
          onClick={logout}
          className="cursor-pointer rounded-md p-2 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
          title={t("logout")}
        >
          <LogOut className="h-4 w-4" />
        </button>
      </div>
    </header>
  );
}
