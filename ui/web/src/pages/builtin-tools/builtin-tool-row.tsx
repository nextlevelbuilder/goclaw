import { useTranslation } from "react-i18next";
import { RefreshCw, Settings, Info } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import type { BuiltinToolData } from "./hooks/use-builtin-tools";

/* eslint-disable @typescript-eslint/no-explicit-any */

// Tools with dedicated settings forms always show the Settings button,
// even when settings is empty (the form lets the user create settings).
const TOOLS_WITH_DEDICATED_FORM = new Set([
  "web_search", "web_fetch", "tts", "knowledge_graph_search",
  "create_image", "create_audio", "create_video",
]);

export function hasEditableSettings(tool: BuiltinToolData): boolean {
  if (TOOLS_WITH_DEDICATED_FORM.has(tool.name)) return true;
  return tool.settings != null && Object.keys(tool.settings).length > 0;
}

export function getConfigHint(tool: BuiltinToolData): string | undefined {
  return (tool.metadata as any)?.config_hint as string | undefined;
}

export function isDeprecated(tool: BuiltinToolData): boolean {
  return (tool.metadata as any)?.deprecated === true;
}

interface TenantOverrideControlProps {
  tool: BuiltinToolData;
  hasOverride: boolean;
  onSetTenantConfig: (name: string, enabled: boolean) => Promise<void>;
  onDeleteTenantConfig: (name: string) => Promise<void>;
}

export function TenantOverrideControl({
  tool,
  hasOverride,
  onSetTenantConfig,
  onDeleteTenantConfig,
}: TenantOverrideControlProps) {
  const { t } = useTranslation("tools");

  const tenantEnabled = tool.tenant_enabled;
  const overrideLabel = hasOverride
    ? tenantEnabled
      ? t("builtin.tenantEnabled")
      : t("builtin.tenantDisabled")
    : t("builtin.tenantDefault");

  const badgeVariant = hasOverride
    ? tenantEnabled
      ? "default"
      : "secondary"
    : "outline";

  return (
    <div className="flex items-center gap-1 border-r pr-2 mr-0.5">
      <TooltipProvider delayDuration={200}>
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge
              variant={badgeVariant}
              className="h-5 cursor-default px-1.5 text-2xs leading-none"
            >
              {overrideLabel}
            </Badge>
          </TooltipTrigger>
          <TooltipContent side="top">
            <p className="text-xs">{t("builtin.tenantOverride")}</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
      <Switch
        checked={hasOverride ? (tenantEnabled ?? false) : tool.enabled}
        onCheckedChange={(checked) => onSetTenantConfig(tool.name, checked)}
        className="scale-75 origin-right"
        aria-label={t("builtin.tenantOverride")}
      />
      {hasOverride && (
        <TooltipProvider delayDuration={200}>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => onDeleteTenantConfig(tool.name)}
                className="h-5 w-5 p-0 text-muted-foreground hover:text-foreground"
                aria-label={t("builtin.resetToDefault")}
              >
                <RefreshCw className="h-3 w-3" />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="top">
              <p className="text-xs">{t("builtin.resetToDefault")}</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      )}
    </div>
  );
}

interface ToolRowProps {
  tool: BuiltinToolData;
  onToggle: (tool: BuiltinToolData) => void;
  onSettings: (tool: BuiltinToolData) => void;
  tenantId: string | null;
  onSetTenantConfig: (name: string, enabled: boolean) => Promise<void>;
  onDeleteTenantConfig: (name: string) => Promise<void>;
}

export function ToolRow({
  tool,
  onToggle,
  onSettings,
  tenantId,
  onSetTenantConfig,
  onDeleteTenantConfig,
}: ToolRowProps) {
  const { t } = useTranslation("tools");
  const configHint = getConfigHint(tool);
  const editable = hasEditableSettings(tool);
  const deprecated = isDeprecated(tool);
  const hasTenantScope = !!tenantId && tenantId !== "0193a5b0-7000-7000-8000-000000000001";
  const hasOverride = tool.tenant_enabled !== null && tool.tenant_enabled !== undefined;

  return (
    <div className={`flex items-center gap-4 px-4 py-2 hover:bg-muted/30 transition-colors${deprecated ? " opacity-60" : ""}`}>
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-1.5">
          <span className="text-sm font-medium leading-tight">{tool.display_name}</span>
          <code className="text-xs-plus text-muted-foreground">{tool.name}</code>
          {deprecated && (
            <TooltipProvider delayDuration={200}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge variant="destructive" className="ml-1 h-4 px-1 text-2xs leading-none cursor-default">
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
                  <Badge variant="outline" className="ml-1 h-4 px-1 text-2xs leading-none cursor-default">
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
                <span className="flex items-center gap-1 text-xs-plus text-muted-foreground cursor-default">
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
        {hasTenantScope && !deprecated ? (
          <TenantOverrideControl
            tool={tool}
            hasOverride={hasOverride}
            onSetTenantConfig={onSetTenantConfig}
            onDeleteTenantConfig={onDeleteTenantConfig}
          />
        ) : (
          <Switch
            checked={tool.enabled}
            onCheckedChange={() => onToggle(tool)}
            disabled={deprecated}
          />
        )}
      </div>
    </div>
  );
}
