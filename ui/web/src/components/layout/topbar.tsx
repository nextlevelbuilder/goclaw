import { Moon, Sun, PanelLeftClose, PanelLeftOpen, Menu, LogOut, Globe, Clock, Building2, ChevronDown, Check, User, KeyRound, Info } from "lucide-react";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { useUiStore } from "@/stores/use-ui-store";
import { useAuthStore } from "@/stores/use-auth-store";
import { useTenants } from "@/hooks/use-tenants";
import { useIsMobile } from "@/hooks/use-media-query";

import { ROUTES, SUPPORTED_LANGUAGES, LANGUAGE_LABELS, TIMEZONE_OPTIONS, LOCAL_STORAGE_KEYS, type Language } from "@/lib/constants";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Popover } from "radix-ui";
import { useState } from "react";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { AboutDialog } from "./about-dialog";

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
  const isMobile = useIsMobile();
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
        <Select value={language} onValueChange={(v) => setLanguage(v as Language)}>
          <SelectTrigger
            title={t("language")}
            className="h-auto w-auto gap-1 border-0 bg-transparent px-2 py-1.5 text-sm text-muted-foreground shadow-none hover:bg-accent hover:text-accent-foreground focus-visible:ring-0 dark:bg-transparent dark:hover:bg-accent **:data-radix-select-icon:hidden"
          >
            <Globe className="h-4 w-4 shrink-0" />
            <span className="hidden sm:inline"><SelectValue /></span>
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
            className="h-auto w-auto gap-1 border-0 bg-transparent px-2 py-1.5 text-sm text-muted-foreground shadow-none hover:bg-accent hover:text-accent-foreground focus-visible:ring-0 dark:bg-transparent dark:hover:bg-accent **:data-radix-select-icon:hidden"
          >
            <Clock className="h-4 w-4 shrink-0" />
            <span className="hidden sm:inline"><SelectValue /></span>
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

        <UserMenu />
      </div>

    </header>
  );
}

function UserMenu() {
  const { t } = useTranslation("topbar");
  const { t: tt } = useTranslation("tenants");
  const logout = useAuthStore((s) => s.logout);
  const userId = useAuthStore((s) => s.userId);
  const { currentTenant, currentTenantName, tenants, isCrossTenant, isMultiTenant, currentTenantId } = useTenants();
  const [open, setOpen] = useState(false);
  const [showLogoutConfirm, setShowLogoutConfirm] = useState(false);
  const [showAbout, setShowAbout] = useState(false);
  const navigate = useNavigate();

  const tenantLabel = currentTenant?.name || currentTenantName || "";

  const handleSwitchTenant = (_tenantId: string, slug: string) => {
    // Cross-tenant admin: narrow scope to specific tenant
    // Non-cross-tenant: use tenant_hint for pairing
    if (isCrossTenant) {
      localStorage.setItem(LOCAL_STORAGE_KEYS.TENANT_ID, slug);
    } else {
      localStorage.setItem(LOCAL_STORAGE_KEYS.TENANT_HINT, slug);
    }
    window.location.reload();
  };

  return (
    <>
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <button
          className="flex cursor-pointer items-center gap-1.5 rounded-md px-2 py-1.5 text-sm text-muted-foreground hover:bg-accent hover:text-accent-foreground"
          title={userId || t("logout")}
        >
          <User className="h-4 w-4 shrink-0" />
          <span className="max-w-32 truncate hidden sm:inline">
            {userId}{tenantLabel ? ` (${tenantLabel})` : ""}
          </span>
          <ChevronDown className="h-3 w-3 shrink-0 opacity-50" />
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          align="end"
          sideOffset={8}
          className="z-50 w-56 rounded-lg border bg-popover p-1 text-popover-foreground shadow-md animate-in fade-in-0 zoom-in-95 pointer-events-auto"
        >
          {/* Tenant name */}
          {tenantLabel && (
            <div className="px-2 py-1.5 text-sm font-medium truncate border-b mb-1">
              {tenantLabel}
            </div>
          )}

          {/* Tenant section */}
          {isMultiTenant && (
            <>
              <div className="px-2 py-1 text-xs font-medium text-muted-foreground">
                {tt("currentTenant")}
              </div>
              {tenants.map((tenant) => (
                <button
                  key={tenant.id}
                  onClick={() => {
                    if (tenant.id !== currentTenantId) {
                      handleSwitchTenant(tenant.id, tenant.slug);
                    }
                    setOpen(false);
                  }}
                  className="flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent"
                >
                  <Building2 className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                  <span className="flex-1 truncate text-left">{tenant.name}</span>
                  {tenant.id === currentTenantId && (
                    <Check className="h-3.5 w-3.5 shrink-0 text-primary" />
                  )}
                </button>
              ))}
              <div className="my-1 border-t" />
            </>
          )}

          {/* Tenants */}
          {isMultiTenant && (
            <button
              onClick={() => { setOpen(false); navigate(ROUTES.TENANTS); }}
              className="flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent"
            >
              <Building2 className="h-3.5 w-3.5 shrink-0" />
              <span>{tt("title")}</span>
            </button>
          )}

          {/* API Keys shortcut */}
          <button
            onClick={() => { setOpen(false); navigate(ROUTES.API_KEYS); }}
            className="flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent"
          >
            <KeyRound className="h-3.5 w-3.5 shrink-0" />
            <span>{t("apiKeys")}</span>
          </button>

          {/* About */}
          <button
            onClick={() => { setOpen(false); setShowAbout(true); }}
            className="flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent"
          >
            <Info className="h-3.5 w-3.5 shrink-0" />
            <span>{t("about.menuItem")}</span>
          </button>

          <div className="my-1 border-t" />

          {/* Logout */}
          <button
            onClick={() => { setOpen(false); setShowLogoutConfirm(true); }}
            className="flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm text-destructive hover:bg-accent"
          >
            <LogOut className="h-3.5 w-3.5 shrink-0" />
            <span>{t("logout")}</span>
          </button>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>

    <ConfirmDialog
      open={showLogoutConfirm}
      onOpenChange={setShowLogoutConfirm}
      title={t("logout")}
      description={t("logoutConfirm")}
      confirmLabel={t("logout")}
      variant="destructive"
      onConfirm={() => { setShowLogoutConfirm(false); logout(); }}
    />

    <AboutDialog open={showAbout} onOpenChange={setShowAbout} />
    </>
  );
}
