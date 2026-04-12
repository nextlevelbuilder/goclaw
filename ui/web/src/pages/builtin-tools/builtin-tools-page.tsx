import { useState, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Package, RefreshCw, Settings, AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { useBuiltinTools, type BuiltinToolData } from "./hooks/use-builtin-tools";
import { BuiltinToolSettingsDialog } from "./builtin-tool-settings-dialog";
import { MEDIA_TOOLS } from "./media-provider-params-schema";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useTenants } from "@/hooks/use-tenants";
import { CategoryGroup } from "./builtin-tool-category-group";

const CATEGORY_ORDER = [
  "filesystem", "runtime", "web", "memory", "media", "browser",
  "sessions", "messaging", "scheduling", "subagents", "skills", "delegation", "teams",
];

// Hardcoded master tenant UUID; mirrors backend store.MasterTenantID. When
// the current tenant is NOT the master, UI routes settings writes through
// the tenant-config endpoint. Backend enforces the same rule defensively —
// this is purely a UX guard so tenant admins never see a 403 from the
// global endpoint.
const MASTER_TENANT_ID = "0193a5b0-7000-7000-8000-000000000001";

/** Media tool that is enabled but has no provider chain configured */
function needsProviderConfig(tool: BuiltinToolData): boolean {
  if (!MEDIA_TOOLS.has(tool.name) || !tool.enabled) return false;
  const settings = tool.settings;
  if (!settings) return true;
  const providers = settings.providers as unknown[] | undefined;
  return !providers || providers.length === 0;
}

export function BuiltinToolsPage() {
  const { t } = useTranslation("tools");
  const {
    tools,
    loading,
    refresh,
    updateTool,
    setTenantConfig,
    deleteTenantConfig,
    setTenantSettings,
    clearTenantSettings,
  } = useBuiltinTools();
  const { currentTenantId } = useTenants();
  const hasTenantScope = !!currentTenantId && currentTenantId !== MASTER_TENANT_ID;
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && tools.length === 0);
  const [search, setSearch] = useState("");
  const [settingsTool, setSettingsTool] = useState<BuiltinToolData | null>(null);

  // Media tools enabled but missing provider configuration
  const unconfigured = useMemo(
    () => tools.filter(needsProviderConfig),
    [tools],
  );

  const filtered = tools.filter(
    (t) =>
      t.name.toLowerCase().includes(search.toLowerCase()) ||
      t.display_name.toLowerCase().includes(search.toLowerCase()) ||
      t.description.toLowerCase().includes(search.toLowerCase()),
  );

  const grouped = new Map<string, BuiltinToolData[]>();
  for (const tool of filtered) {
    const cat = tool.category || "general";
    if (!grouped.has(cat)) grouped.set(cat, []);
    grouped.get(cat)!.push(tool);
  }
  const sortedCategories = [...grouped.keys()].sort(
    (a, b) => (CATEGORY_ORDER.indexOf(a) ?? 99) - (CATEGORY_ORDER.indexOf(b) ?? 99),
  );

  const handleToggle = async (tool: BuiltinToolData) => {
    await updateTool(tool.name, { enabled: !tool.enabled });
  };

  // Route settings writes through the correct endpoint based on scope:
  //   - Master scope → PUT /v1/tools/builtin/{name}   (global default)
  //   - Tenant scope → PUT /v1/tools/builtin/{name}/tenant-config (override)
  // Backend enforces master-scope defensively (Phase 0b), so even if this
  // UI check drifted, a tenant admin would hit 403 instead of corrupting
  // the global defaults.
  const handleSaveSettings = async (name: string, settings: Record<string, unknown>) => {
    if (hasTenantScope) {
      await setTenantSettings(name, settings);
    } else {
      await updateTool(name, { settings });
    }
  };

  const handleResetTenantSettings = async (name: string) => {
    await clearTenantSettings(name);
  };

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("builtin.title")}
        description={t("builtin.description")}
        actions={
          <Button
            variant="outline"
            size="sm"
            onClick={refresh}
            disabled={spinning}
            className="gap-1"
          >
            <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} />
            {t("common:refresh", "Refresh")}
          </Button>
        }
      />

      <div className="mt-4 flex items-center gap-3">
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder={t("builtin.searchPlaceholder")}
          className="max-w-sm"
        />
        <span className="text-xs text-muted-foreground">
          {filtered.length !== 1
            ? t("builtin.toolCountPlural", { count: filtered.length })
            : t("builtin.toolCount", { count: filtered.length })}
          {sortedCategories.length > 0 && ` · ${t("builtin.categoryCount", { count: sortedCategories.length })}`}
        </span>
      </div>

      {unconfigured.length > 0 && (
        <div className="mt-4 flex items-start gap-3 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 dark:border-amber-900/50 dark:bg-amber-950/30">
          <AlertTriangle className="h-4 w-4 text-amber-600 dark:text-amber-400 shrink-0 mt-0.5" />
          <div className="flex-1 min-w-0">
            <p className="text-sm text-amber-800 dark:text-amber-200">
              {t("builtin.unconfiguredWarning", { count: unconfigured.length })}
            </p>
            <div className="flex flex-wrap gap-1.5 mt-2">
              {unconfigured.map((tool) => (
                <Button
                  key={tool.name}
                  variant="outline"
                  size="sm"
                  onClick={() => setSettingsTool(tool)}
                  className="h-6 gap-1 px-2 text-xs border-amber-300 dark:border-amber-800"
                >
                  <Settings className="h-3 w-3" />
                  {tool.display_name}
                </Button>
              ))}
            </div>
          </div>
        </div>
      )}

      <div className="mt-4 space-y-3">
        {showSkeleton ? (
          <TableSkeleton rows={8} />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={Package}
            title={search ? t("builtin.noMatchTitle") : t("builtin.emptyTitle")}
            description={
              search ? t("builtin.noMatchDescription") : t("builtin.emptyDescription")
            }
          />
        ) : (
          sortedCategories.map((category) => (
            <CategoryGroup
              key={category}
              category={category}
              tools={grouped.get(category)!}
              onToggle={handleToggle}
              onSettings={setSettingsTool}
              tenantId={currentTenantId ?? null}
              onSetTenantConfig={setTenantConfig}
              onDeleteTenantConfig={deleteTenantConfig}
            />
          ))
        )}
      </div>

      <BuiltinToolSettingsDialog
        tool={settingsTool}
        open={settingsTool !== null}
        onOpenChange={(open) => {
          if (!open) setSettingsTool(null);
        }}
        onSave={handleSaveSettings}
        tenantScope={hasTenantScope}
        onResetToDefault={hasTenantScope ? handleResetTenantSettings : undefined}
      />
    </div>
  );
}

