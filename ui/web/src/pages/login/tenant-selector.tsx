import { useLocation, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { ShieldAlert, LogOut } from "lucide-react";
import { useAuthStore } from "@/stores/use-auth-store";
import { LoginLayout } from "./login-layout";
import { ROUTES, LOCAL_STORAGE_KEYS } from "@/lib/constants";

export function TenantSelectorPage() {
  const { t } = useTranslation("login");
  const location = useLocation();
  const navigate = useNavigate();
  const availableTenants = useAuthStore((s) => s.availableTenants);
  const isCrossTenant = useAuthStore((s) => s.isCrossTenant);
  const logout = useAuthStore((s) => s.logout);

  const from = (location.state as { from?: { pathname: string } })?.from?.pathname;

  const handleSelect = (slug: string) => {
    localStorage.setItem(LOCAL_STORAGE_KEYS.TENANT_ID, slug);
    useAuthStore.getState().setTenantSelected(true);
    // Reload to reconnect WS with the new tenant_scope
    window.location.replace(from || ROUTES.OVERVIEW);
  };

  const handleLogout = () => {
    logout();
    navigate(ROUTES.LOGIN, { replace: true });
  };

  // No access state: not cross-tenant and no tenants
  if (!isCrossTenant && availableTenants.length === 0) {
    return (
      <LoginLayout subtitle={t("noAccess")}>
        <div className="space-y-5 text-center">
          <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-full bg-amber-100 dark:bg-amber-950/40">
            <ShieldAlert className="h-7 w-7 text-amber-600 dark:text-amber-400" />
          </div>
          <div className="space-y-2">
            <p className="text-sm text-muted-foreground">{t("noAccessDescription")}</p>
            <p className="text-xs text-muted-foreground/70">{t("noAccessHint")}</p>
          </div>
          <button
            onClick={handleLogout}
            className="inline-flex w-full items-center justify-center gap-2 rounded-md border border-input bg-background px-4 py-2.5 text-base md:text-sm font-medium hover:bg-muted transition-colors"
          >
            <LogOut className="h-4 w-4" />
            {t("logout")}
          </button>
        </div>
      </LoginLayout>
    );
  }

  return (
    <LoginLayout subtitle={t("selectTenantDescription")}>
      <div className="space-y-3">
        <h2 className="text-center text-base font-medium">{t("selectTenant")}</h2>

        {/* Individual tenant cards */}
        {availableTenants.map((tenant) => (
          <button
            key={tenant.id}
            onClick={() => handleSelect(tenant.slug)}
            className="w-full rounded-lg border border-input bg-card p-4 text-left transition-colors hover:bg-muted"
          >
            <div className="flex items-center justify-between gap-2">
              <div className="min-w-0">
                <p className="truncate font-medium">{tenant.name}</p>
                <p className="mt-0.5 truncate text-xs text-muted-foreground">{tenant.slug}</p>
              </div>
              <span className="shrink-0 rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground capitalize">
                {tenant.role}
              </span>
            </div>
          </button>
        ))}
      </div>
    </LoginLayout>
  );
}
