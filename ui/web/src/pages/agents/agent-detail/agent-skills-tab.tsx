import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { Zap } from "lucide-react";
import { useAgentSkills } from "../hooks/use-agent-skills";

interface AgentSkillsTabProps {
  agentId: string;
}

const visibilityVariant = (v: string) => {
  switch (v) {
    case "public":
      return "success";
    case "internal":
      return "secondary";
    case "private":
      return "outline";
    default:
      return "outline";
  }
};

export function AgentSkillsTab({ agentId }: AgentSkillsTabProps) {
  const { t } = useTranslation("agents");
  const { skills, loading, grantSkill, revokeSkill } = useAgentSkills(agentId);
  const [search, setSearch] = useState("");
  const [toggling, setToggling] = useState<string | null>(null);

  const filtered = skills.filter(
    (s) =>
      s.name.toLowerCase().includes(search.toLowerCase()) ||
      s.slug.toLowerCase().includes(search.toLowerCase()) ||
      s.description.toLowerCase().includes(search.toLowerCase()),
  );

  const handleToggle = async (skillId: string, granted: boolean) => {
    setToggling(skillId);
    try {
      if (granted) {
        await revokeSkill(skillId);
      } else {
        await grantSkill(skillId);
      }
    } finally {
      setToggling(null);
    }
  };

  if (loading && skills.length === 0) {
    return <TableSkeleton />;
  }

  if (!loading && skills.length === 0) {
    return (
      <EmptyState
        icon={Zap}
        title={t("skills.noSkillsAvailable")}
        description={t("skills.noSkillsDesc")}
      />
    );
  }

  return (
    <div className="max-w-4xl space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">
          {t("skills.skillsGranted", {
            granted: skills.filter((s) => s.granted).length,
            total: skills.length,
          })}
        </p>
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder={t("skills.filterSkills")}
          className="w-full sm:w-64"
        />
      </div>

      <div className="divide-y rounded-lg border">
        {filtered.map((skill) => (
          <div key={skill.id} className="flex items-center justify-between gap-4 px-4 py-3">
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className="font-medium">{skill.name}</span>
                <Badge variant={visibilityVariant(skill.visibility)} className="text-[10px]">
                  {skill.visibility}
                </Badge>
                {skill.is_system && (
                  <Badge variant="outline" className="border-blue-500 text-blue-600 text-[10px]">
                    {t("skills.system")}
                  </Badge>
                )}
              </div>
              {skill.description && (
                <p className="mt-0.5 truncate text-sm text-muted-foreground">{skill.description}</p>
              )}
            </div>
            {skill.is_system ? (
              <span className="text-xs text-muted-foreground whitespace-nowrap">{t("skills.alwaysAvailable")}</span>
            ) : (
              <Switch
                checked={skill.granted}
                disabled={toggling === skill.id}
                onCheckedChange={() => handleToggle(skill.id, skill.granted)}
              />
            )}
          </div>
        ))}
        {filtered.length === 0 && (
          <div className="px-4 py-8 text-center text-sm text-muted-foreground">
            {t("skills.noSkillsMatch")}
          </div>
        )}
      </div>
    </div>
  );
}
