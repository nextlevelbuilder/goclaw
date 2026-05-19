import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import type { SkillInfo } from "./hooks/use-skills";
import { displayDependencyName, hasMissingDeps, isArchived, isDisabled } from "./lib/skills-filtering";

interface SkillStatusBadgesProps {
  skill: SkillInfo;
}

export function SkillStatusBadges({ skill }: SkillStatusBadgesProps) {
  const { t } = useTranslation("skills");
  const archived = isArchived(skill);
  const disabled = isDisabled(skill);
  const missing = skill.missing_deps ?? [];

  return (
    <div className="flex max-w-[220px] flex-wrap gap-1">
      <Badge variant={archived ? "warning" : "success"} className="text-2xs">
        {archived ? t("deps.statusArchived") : t("deps.statusActive")}
      </Badge>
      {disabled && <Badge variant="destructive" className="text-2xs">{t("toggle.disabled")}</Badge>}
      {hasMissingDeps(skill) && (
        <Badge
          variant="warning"
          className="text-2xs"
          title={missing.map(displayDependencyName).join(", ")}
        >
          {t("status.missingDepsCount", { count: missing.length })}
        </Badge>
      )}
    </div>
  );
}
