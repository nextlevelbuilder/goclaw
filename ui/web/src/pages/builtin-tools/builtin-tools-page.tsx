import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Package, RefreshCw, Settings, Info } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { useBuiltinTools, type BuiltinToolData } from "./hooks/use-builtin-tools";
import { BuiltinToolSettingsDialog } from "./builtin-tool-settings-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";

/* eslint-disable @typescript-eslint/no-explicit-any */

const CATEGORY_ORDER = [
  "filesystem", "runtime", "web", "memory", "media", "browser",
  "sessions", "messaging", "scheduling", "subagents", "skills", "delegation", "teams",
];

function hasEditableSettings(tool: BuiltinToolData): boolean {
  return tool.settings != null && Object.keys(tool.settings).length > 0;
}

function getConfigHint(tool: BuiltinToolData): string | undefined {
  return (tool.metadata as any)?.config_hint as string | undefined;
}

function isDeprecated(tool: BuiltinToolData): boolean {
  return (tool.metadata as any)?.deprecated === true;
}

export function BuiltinToolsPage() {
  const { t } = useTranslation("tools");
  const { tools, loading, refresh, updateTool } = useBuiltinTools();
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && tools.length === 0);
  const [search, setSearch] = useState("");
  const [settingsTool, setSettingsTool] = useState<BuiltinToolData | null>(null);

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

  const handleSaveSettings = async (name: string, settings: Record<string, unknown>) => {
    await updateTool(name, { settings });
  };

  return (
    <div className="p-4 sm:p-6">
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
      />
    </div>
  );
}

function CategoryGroup({
  category,
  tools,
  onToggle,
  onSettings,
}: {
  category: string;
  tools: BuiltinToolData[];
  onToggle: (tool: BuiltinToolData) => void;
  onSettings: (tool: BuiltinToolData) => void;
}) {
  const { t } = useTranslation("tools");
  return (
    <div className="rounded-lg border">
      <div className="flex items-center gap-2 border-b bg-muted/40 px-4 py-2">
        <span className="text-sm font-medium">{t(`builtin.categories.${category}`, category)}</span>
        <Badge variant="secondary" className="h-5 px-1.5 text-[11px]">
          {tools.length}
        </Badge>
      </div>
      <div className="divide-y">
        {tools.map((tool) => (
          <ToolRow key={tool.name} tool={tool} onToggle={onToggle} onSettings={onSettings} />
        ))}
      </div>
    </div>
  );
}

function ToolRow({
  tool,
  onToggle,
  onSettings,
}: {
  tool: BuiltinToolData;
  onToggle: (tool: BuiltinToolData) => void;
  onSettings: (tool: BuiltinToolData) => void;
}) {
  const { t } = useTranslation("tools");
  const configHint = getConfigHint(tool);
  const editable = hasEditableSettings(tool);
  const deprecated = isDeprecated(tool);

  return (
    <div className={`flex items-center gap-4 px-4 py-2 hover:bg-muted/30 transition-colors${deprecated ? " opacity-60" : ""}`}>
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-1.5">
          <span className="text-sm font-medium leading-tight">{tool.display_name}</span>
          <code className="text-[11px] text-muted-foreground">{tool.name}</code>
          {deprecated && (
            <TooltipProvider delayDuration={200}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge variant="destructive" className="ml-1 h-4 px-1 text-[10px] leading-none cursor-default">
                    {t("builtin.deprecated")}
                  </Badge>
                </TooltipTrigger>
                <TooltipContent side="top">
                  <p className="text-xs">{t("builtin.deprecatedTooltip")}</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          )}
          {!deprecated && tool.requires && tool.requires.length > 0 && (
            <TooltipProvider delayDuration={200}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge variant="outline" className="ml-1 h-4 px-1 text-[10px] leading-none cursor-default">
                    {t("builtin.requires")}
                  </Badge>
                </TooltipTrigger>
                <TooltipContent side="top">
                  <p className="text-xs">{t("builtin.requiresTooltip", { list: tool.requires.join(", ") })}</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          )}
        </div>
        {tool.description && (
          <p className="text-xs text-muted-foreground leading-snug truncate mt-0.5">
            {t(`builtin.descriptions.${tool.name}`, tool.description)}
          </p>
        )}
      </div>

      <div className="flex items-center gap-1.5 shrink-0">
        {editable && !deprecated && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onSettings(tool)}
            className="h-7 gap-1 px-2 text-xs"
          >
            <Settings className="h-3 w-3" />
            {t("builtin.settings")}
          </Button>
        )}
        {!editable && !deprecated && configHint && (
          <TooltipProvider delayDuration={200}>
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="flex items-center gap-1 text-[11px] text-muted-foreground cursor-default">
                  <Info className="h-3 w-3" />
                  {configHint}
                </span>
              </TooltipTrigger>
              <TooltipContent side="top">
                <p className="text-xs">{t("builtin.configuredVia")}</p>
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        )}
        <Switch
          checked={tool.enabled}
          onCheckedChange={() => onToggle(tool)}
          disabled={deprecated}
        />
      </div>
    </div>
  );
}
