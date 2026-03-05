import { useState } from "react";
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
        title="No skills available"
        description="Upload skills in the Skills page to grant them to agents."
      />
    );
  }

  return (
    <div className="max-w-4xl space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">
          {skills.filter((s) => s.granted).length} of {skills.length} skills granted
        </p>
        <SearchInput value={search} onChange={setSearch} placeholder="Filter skills..." className="w-full sm:w-64" />
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
              </div>
              {skill.description && (
                <p className="mt-0.5 truncate text-sm text-muted-foreground">{skill.description}</p>
              )}
            </div>
            <Switch
              checked={skill.granted}
              disabled={toggling === skill.id}
              onCheckedChange={() => handleToggle(skill.id, skill.granted)}
            />
          </div>
        ))}
        {filtered.length === 0 && (
          <div className="px-4 py-8 text-center text-sm text-muted-foreground">
            No skills match your search.
          </div>
        )}
      </div>
    </div>
  );
}
