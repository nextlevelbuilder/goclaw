import { useTranslation } from "react-i18next";
import { Zap, Pencil, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { SkillTenantOverride } from "./skill-tenant-override";
import type { SkillInfo } from "./hooks/use-skills";

const visibilityColor: Record<string, string> = {
  public: "default",
  internal: "secondary",
  private: "outline",
};

interface SkillTableRowProps {
  skill: SkillInfo;
  tab: "core" | "custom";
  hasTenantScope: boolean;
  toggling: string | null;
  onView: (name: string) => void;
  onEdit: (skill: SkillInfo) => void;
  onDelete: (skill: SkillInfo) => void;
  onToggle: (skill: SkillInfo, enabled: boolean) => void;
  onCycleVisibility: (skill: SkillInfo) => void;
  onSetTenantConfig: (id: string, enabled: boolean) => Promise<void>;
  onDeleteTenantConfig: (id: string) => Promise<void>;
}

/** Single row in the skills table with inline status, visibility, and action controls. */
export function SkillTableRow({
  skill, tab, hasTenantScope, toggling,
  onView, onEdit, onDelete, onToggle, onCycleVisibility,
  onSetTenantConfig, onDeleteTenantConfig,
}: SkillTableRowProps) {
  const { t } = useTranslation("skills");
  const isArchived = skill.status === "archived";
  const isDisabled = skill.enabled === false;
  const hasMissing = (skill.missing_deps?.length ?? 0) > 0;

  return (
    <tr className={cn("border-b last:border-0 hover:bg-muted/30", (isArchived || isDisabled) && "opacity-60")}>
      <td className="px-4 py-3">
        <div className="flex items-center gap-2 flex-wrap">
          <Zap className="h-4 w-4 text-muted-foreground shrink-0" />
          <button
            type="button"
            className="font-medium text-left hover:underline cursor-pointer"
            onClick={() => onView(skill.name)}
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
        <td className="px-4 py-3 text-sm text-muted-foreground">{skill.author || "—"}</td>
      )}
      <td className="px-4 py-3">
        <div className="flex flex-col gap-1">
          <Badge
            variant="outline"
            className={cn(
              "text-2xs w-fit",
              isArchived
                ? "border-amber-500 text-amber-600 dark:border-amber-600 dark:text-amber-400"
                : "border-emerald-500 text-emerald-600 dark:border-emerald-600 dark:text-emerald-400",
            )}
          >
            {isArchived ? t("deps.statusArchived") : t("deps.statusActive")}
          </Badge>
          {hasMissing && (() => {
            const deps = skill.missing_deps!.map((d) => d.replace(/^(pip|npm):/, ""));
            const shown = deps.slice(0, 3);
            const rest = deps.length - shown.length;
            return (
              <span className="text-2xs text-amber-600 dark:text-amber-400 leading-tight">
                {shown.join(", ")}{rest > 0 && `, +${rest}`}
              </span>
            );
          })()}
        </div>
      </td>
      {tab === "custom" && (
        <td className="px-4 py-3">
          {skill.visibility && (
            skill.id ? (
              <button type="button" onClick={() => onCycleVisibility(skill)} title={t("visibility.clickToCycle")}>
                <Badge
                  variant={visibilityColor[skill.visibility] as "default" | "secondary" | "outline"}
                  className="cursor-pointer hover:opacity-80 transition-opacity"
                >
                  {skill.visibility}
                </Badge>
              </button>
            ) : (
              <Badge variant={visibilityColor[skill.visibility] as "default" | "secondary" | "outline"}>
                {skill.visibility}
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
              </Button>
              {!skill.is_system && (
                <Button
                  variant="ghost" size="sm"
                  onClick={() => onDelete(skill)}
                  className="gap-1 text-destructive hover:text-destructive"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              )}
            </>
          )}
        </div>
      </td>
    </tr>
  );
}
