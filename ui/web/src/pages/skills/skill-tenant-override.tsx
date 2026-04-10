import { useTranslation } from "react-i18next";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import type { SkillInfo } from "./hooks/use-skills";

interface SkillTenantOverrideProps {
  skill: SkillInfo;
  toggling: boolean;
  onSetTenantConfig: (id: string, enabled: boolean) => Promise<void>;
  onDeleteTenantConfig: (id: string) => Promise<void>;
}

/** Per-tenant enable/disable override control shown when a tenant scope is active. */
export function SkillTenantOverride({
  skill,
  toggling,
  onSetTenantConfig,
  onDeleteTenantConfig,
}: SkillTenantOverrideProps) {
  const { t } = useTranslation("skills");
  const hasOverride = skill.tenant_enabled !== null && skill.tenant_enabled !== undefined;
  const checked = hasOverride ? (skill.tenant_enabled ?? false) : (skill.enabled !== false);
  const label = hasOverride
    ? skill.tenant_enabled ? t("tenant.enabled") : t("tenant.disabled")
    : t("tenant.default");
  const badgeVariant = hasOverride
    ? skill.tenant_enabled ? "default" : "secondary"
    : "outline";

  return (
    <div className="flex items-center gap-1">
      <TooltipProvider delayDuration={200}>
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge
              variant={badgeVariant as "default" | "secondary" | "outline"}
              className="h-5 cursor-default px-1.5 text-2xs leading-none"
            >
              {label}
            </Badge>
          </TooltipTrigger>
          <TooltipContent side="top">
            <p className="text-xs">{t("tenant.overrideHint")}</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
      <Switch
        size="sm"
        checked={checked}
        disabled={toggling}
        onCheckedChange={(val) => onSetTenantConfig(skill.id!, val)}
        aria-label={t("tenant.overrideHint")}
      />
      {hasOverride && (
        <TooltipProvider delayDuration={200}>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                disabled={toggling}
                onClick={() => onDeleteTenantConfig(skill.id!)}
                className="h-5 w-5 p-0 text-muted-foreground hover:text-foreground"
              >
                <X className="h-3 w-3" />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="top">
              <p className="text-xs">{t("tenant.resetDefault")}</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      )}
    </div>
  );
}
