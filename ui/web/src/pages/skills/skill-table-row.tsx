import { useTranslation } from "react-i18next";
import { Zap, Pencil, Trash2, Users } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { SkillTenantOverride } from "./skill-tenant-override";
import { SkillAgentChips } from "./skill-agent-chips";
import { SkillStatusBadges } from "./skill-status-badges";
import { getSkillAccessModeBadgeVariant, getSkillAccessModeKey } from "./lib/skill-access-mode";
import type { SkillInfo } from "./hooks/use-skills";

interface SkillTableRowProps {
  skill: SkillInfo;
  tab: "core" | "custom";
  hasTenantScope: boolean;
  toggling: string | null;
  selected: boolean;
  onToggleSelect: (skill: SkillInfo) => void;
  onView: (skill: SkillInfo) => void;
  onEdit: (skill: SkillInfo) => void;
  onManageGrants: (skill: SkillInfo) => void;
  onDelete: (skill: SkillInfo) => void;
  onToggle: (skill: SkillInfo, enabled: boolean) => void;
  onCycleAccessMode: (skill: SkillInfo) => void;
  onSetTenantConfig: (id: string, enabled: boolean) => Promise<void>;
  onDeleteTenantConfig: (id: string) => Promise<void>;
}

/** Single row in the skills table with inline status, access mode, and action controls. */
export function SkillTableRow({
  skill, tab, hasTenantScope, toggling, selected, onToggleSelect,
  onView, onEdit, onManageGrants, onDelete, onToggle, onCycleAccessMode,
  onSetTenantConfig, onDeleteTenantConfig,
}: SkillTableRowProps) {
  const { t } = useTranslation("skills");
  const isArchived = skill.status === "archived";
  const isDisabled = skill.enabled === false;
  const accessModeKey = getSkillAccessModeKey(skill.visibility);
  const accessModeLabel = accessModeKey === "unknown"
    ? t("accessMode.unknown", { value: skill.visibility || t("unknownOwner") })
    : t(`accessMode.${accessModeKey}`);
  const accessModeVariant = getSkillAccessModeBadgeVariant(skill.visibility);

  return (
    <tr className={cn("border-b last:border-0 hover:bg-muted/30", selected && "bg-primary/5", (isArchived || isDisabled) && "opacity-60")}>
      <td className="px-4 py-3">
        {skill.id && (
          <input
            type="checkbox"
            checked={selected}
            onChange={() => onToggleSelect(skill)}
            aria-label={t("bulk.selectSkill", { name: skill.name })}
            className="h-4 w-4 cursor-pointer accent-primary"
          />
        )}
      </td>
      <td className="px-4 py-3">
        <div className="flex items-center gap-2 flex-wrap">
          <Zap className="h-4 w-4 text-muted-foreground shrink-0" />
          <button
            type="button"
            className="font-medium text-left hover:underline cursor-pointer"
            onClick={() => onView(skill)}
          >
            {skill.name}
          </button>
          {skill.is_system && (
            <Badge variant="outline" className="border-blue-500 text-blue-600 text-2xs">
              {t("system")}
            </Badge>
          )}
          {skill.version && <span className="text-xs text-muted-foreground">v{skill.version}</span>}
        </div>
      </td>
      <td className="max-w-xs truncate px-4 py-3 text-muted-foreground">
        {skill.description || t("noDescription")}
      </td>
      {tab === "custom" && (
        <td className="px-4 py-3 text-sm text-muted-foreground">
          <SkillAgentChips skill={skill} />
        </td>
      )}
      <td className="px-4 py-3">
        <SkillStatusBadges skill={skill} />
      </td>
      {tab === "custom" && (
        <td className="px-4 py-3">
          {skill.visibility && (
            skill.id ? (
              <button type="button" onClick={() => onCycleAccessMode(skill)} title={t("accessMode.clickToCycle")}>
                <Badge
                  variant={accessModeVariant}
                  className="cursor-pointer hover:opacity-80 transition-opacity"
                >
                  {accessModeLabel}
                </Badge>
              </button>
            ) : (
              <Badge variant={accessModeVariant}>
                {accessModeLabel}
              </Badge>
            )
          )}
        </td>
      )}
      <td className="px-4 py-3 text-right">
        <div className="flex items-center justify-end gap-2">
          {skill.id && (
            <>
              {hasTenantScope ? (
                <SkillTenantOverride
                  skill={skill}
                  toggling={toggling === skill.id}
                  onSetTenantConfig={onSetTenantConfig}
                  onDeleteTenantConfig={onDeleteTenantConfig}
                />
              ) : (
                <Switch
                  size="sm"
                  checked={skill.enabled !== false}
                  disabled={toggling === skill.id}
                  onCheckedChange={(checked) => onToggle(skill, checked)}
                  title={skill.enabled !== false ? t("toggle.disable") : t("toggle.enable")}
                />
              )}
              <Button variant="ghost" size="sm" onClick={() => onEdit(skill)} className="gap-1">
                <Pencil className="h-3.5 w-3.5" />
                <span className="sr-only">{t("edit.title")}</span>
              </Button>
              {!skill.is_system && (
                <Button variant="ghost" size="sm" onClick={() => onManageGrants(skill)} className="gap-1" title={t("grants.manage")}>
                  <Users className="h-3.5 w-3.5" />
                  <span className="sr-only">{t("grants.manage")}</span>
                </Button>
              )}
              {!skill.is_system && (
                <Button
                  variant="ghost" size="sm"
                  onClick={() => onDelete(skill)}
                  className="gap-1 text-destructive hover:text-destructive"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  <span className="sr-only">{t("delete.confirmLabel")}</span>
                </Button>
              )}
            </>
          )}
        </div>
      </td>
    </tr>
  );
}
