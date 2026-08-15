import { useState, useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Pin, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { useAgentSkills } from "@/pages/agents/hooks/use-agent-skills";
import type { AgentData } from "@/types/agent";

interface Props {
  agent: AgentData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

const MAX_PINNED = 10;

function readPinnedSkills(agent: AgentData): string[] {
  const bag = (agent.other_config ?? {}) as Record<string, unknown>;
  return (bag.pinned_skills as string[]) || [];
}

export function PinnedSkillsSection({ agent, onUpdate }: Props) {
  const { t } = useTranslation("agents");
  const { skills, loading: skillsLoading } = useAgentSkills(agent.id);
  const savedPinned = readPinnedSkills(agent);

  const [pinned, setPinned] = useState<string[]>(savedPinned);
  const [saving, setSaving] = useState(false);

  useEffect(() => { setPinned(readPinnedSkills(agent)); }, [agent.other_config]);

  const dirty = JSON.stringify(pinned) !== JSON.stringify(savedPinned);

  // Available skills not yet pinned (granted skills only)
  const availableSkills = useMemo(() => {
    const pinnedSet = new Set(pinned);
    return skills
      .filter((s) => s.granted && !pinnedSet.has(s.slug))
      .sort((a, b) => a.name.localeCompare(b.name));
  }, [skills, pinned]);

  const addPinned = (slug: string) => {
    if (slug && !pinned.includes(slug) && pinned.length < MAX_PINNED) {
      setPinned([...pinned, slug]);
    }
  };

  const removePinned = (slug: string) => {
    setPinned(pinned.filter((s) => s !== slug));
  };

  // Resolve slug → display name from skills list
  const skillName = (slug: string) => {
    const s = skills.find((sk) => sk.slug === slug);
    return s?.name ?? slug;
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      const bag = { ...((agent.other_config ?? {}) as Record<string, unknown>) };
      if (pinned.length > 0) {
        bag.pinned_skills = pinned;
      } else {
        delete bag.pinned_skills;
      }
      await onUpdate({ other_config: bag });
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="space-y-2.5 rounded-lg border p-3 sm:p-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Pin className="h-4 w-4 text-orange-500 shrink-0" />
          <h3 className="text-sm font-medium">{t("detail.prompt.pinnedLabel", "Pinned Skills")}</h3>
          <span className="text-xs text-muted-foreground">({pinned.length}/{MAX_PINNED})</span>
        </div>
        {dirty && (
          <Button size="xs" onClick={handleSave} disabled={saving}>
            {saving ? t("saving", "Saving...") : t("save", "Save")}
          </Button>
        )}
      </div>

      {/* Current pinned — badge chips with remove */}
      {pinned.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {pinned.map((slug) => (
            <Badge
              key={slug} variant="secondary"
              className="text-xs gap-1 pr-1 cursor-pointer hover:bg-destructive/20"
              onClick={() => removePinned(slug)}
            >
              {skillName(slug)}
              <X className="h-3 w-3" />
            </Badge>
          ))}
        </div>
      )}

      {/* Add skill select */}
      {pinned.length < MAX_PINNED && !skillsLoading && availableSkills.length > 0 && (
        <Select value="" onValueChange={addPinned}>
          <SelectTrigger className="w-[220px] text-base md:text-sm h-8">
            <SelectValue placeholder={t("detail.prompt.pinnedPlaceholder", "Add a skill...")} />
          </SelectTrigger>
          <SelectContent>
            {availableSkills.map((s) => (
              <SelectItem key={s.slug} value={s.slug}>
                {s.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}

      <p className="text-xs-plus text-muted-foreground">
        {t("detail.prompt.pinnedHint", "Pinned skills are always inlined in the system prompt. Others use skill_search.")}
      </p>
    </section>
  );
}
