import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import type { SkillInfo, SkillAgentRef } from "@/types/skill";

interface SkillAgentChipsProps {
  skill: SkillInfo;
}

export function SkillAgentChips({ skill }: SkillAgentChipsProps) {
  const { t } = useTranslation("skills");
  const managers = skill.manager_agents ?? [];
  const visibleManagers = managers.slice(0, 2);
  const overflow = managers.length - visibleManagers.length;
  const unmanaged = !skill.is_system && managers.length === 0;

  return (
    <div className="flex max-w-[260px] flex-wrap gap-1">
      {skill.creator_agent && (
        <Badge variant="outline" className="max-w-full text-2xs" title={t("agents.creator")}>
          {t("agents.creator")}: {agentLabel(skill.creator_agent)}
        </Badge>
      )}
      {visibleManagers.map((agent) => (
        <Badge key={agent.id || agent.agent_key || agent.display_name} variant="secondary" className="max-w-full text-2xs" title={t("agents.managers")}>
          {agentLabel(agent)}
        </Badge>
      ))}
      {overflow > 0 && <Badge variant="outline" className="text-2xs">+{overflow}</Badge>}
      {unmanaged && <Badge variant="warning" className="text-2xs">{t("status.unmanaged")}</Badge>}
      {!skill.creator_agent && managers.length === 0 && skill.author && (
        <Badge variant="outline" className="max-w-full text-2xs">{skill.author}</Badge>
      )}
      {!skill.creator_agent && managers.length === 0 && !skill.author && !unmanaged && (
        <span className="text-muted-foreground">—</span>
      )}
    </div>
  );
}

function agentLabel(agent: SkillAgentRef): string {
  return agent.display_name || agent.agent_key || agent.id || "unknown";
}
