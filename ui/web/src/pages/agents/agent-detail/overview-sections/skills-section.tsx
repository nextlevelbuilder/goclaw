import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Zap } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { SearchInput } from "@/components/shared/search-input";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { useAgentSkills } from "../../hooks/use-agent-skills";

interface SkillsSectionProps {
  agentId: string;
}

const visibilityVariant = (v: string) => {
  switch (v) {
    case "public": return "success";
    case "internal": return "secondary";
    default: return "outline";
  }
};

export function SkillsSection({ agentId }: SkillsSectionProps) {
  const { t } = useTranslation("agents");
  const { skills, loading, grantSkill, revokeSkill } = useAgentSkills(agentId);
  const [search, setSearch] = useState("");
  const [toggling, setToggling] = useState<string | null>(null);

  const filtered = skills
    .filter((s) =>
      s.name.toLowerCase().includes(search.toLowerCase()) ||
      s.slug.toLowerCase().includes(search.toLowerCase()) ||
      s.description.toLowerCase().includes(search.toLowerCase()),
    )
    .sort((a, b) => {
      const rank = (s: typeof a) => s.granted ? 2 : s.is_system ? 1 : 0;
      return rank(a) - rank(b);
    });

  const handleToggle = async (skillId: string, granted: boolean) => {
    setToggling(skillId);
    try {
      if (granted) await revokeSkill(skillId);
      else await grantSkill(skillId);
    } finally {
      setToggling(null);
    }
  };

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <Zap className="h-4 w-4 text-amber-500" />
          <h3 className="text-sm font-medium">{t("detail.skills")}</h3>
          {!loading && (
            <span className="text-xs text-muted-foreground">
              ({skills.filter((s) => s.granted).length}/{skills.length})
            </span>
          )}
        </div>
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder={t("skills.filterSkills")}
          className="w-40 sm:w-52"
        />
      </div>

      {loading && skills.length === 0 ? (
        <TableSkeleton />
      ) : skills.length === 0 ? (
        <p className="text-xs text-muted-foreground italic px-1">{t("skills.noSkillsAvailable")}</p>
      ) : (
        <div className="divide-y rounded-lg border max-h-[300px] overflow-y-auto overscroll-contain">
          {filtered.map((skill) => (
            <div key={skill.id} className="flex items-center justify-between gap-3 px-3 py-2.5">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-1.5">
                  <span className="text-sm font-medium truncate">{skill.name}</span>
                  <Badge variant={visibilityVariant(skill.visibility)} className="text-[10px] shrink-0">
                    {skill.visibility}
                  </Badge>
                  {skill.is_system && (
                    <Badge variant="outline" className="border-blue-500 text-blue-600 text-[10px] shrink-0">
                      {t("skills.system")}
                    </Badge>
                  )}
                </div>
                {skill.description && (
                  <p className="mt-0.5 truncate text-xs text-muted-foreground">{skill.description}</p>
                )}
              </div>
              {skill.is_system ? (
                <span className="text-xs text-muted-foreground whitespace-nowrap shrink-0">
                  {t("skills.alwaysAvailable")}
                </span>
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
            <div className="px-3 py-6 text-center text-xs text-muted-foreground">
              {t("skills.noSkillsMatch")}
            </div>
          )}
        </div>
      )}
    </section>
  );
}
