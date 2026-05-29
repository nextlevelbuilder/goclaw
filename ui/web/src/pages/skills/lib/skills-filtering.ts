import type { SkillInfo } from "../hooks/use-skills";
import type { SkillsFilter, SkillsPageState } from "./skills-page-state";

export interface SkillHealthStats {
  total: number;
  missingDeps: number;
  disabled: number;
  archived: number;
  unmanaged: number;
  attention: number;
}

export function displayDependencyName(dep: string): string {
  return dep.replace(/^(pip|npm):/, "");
}

export function deriveSkillStats(skills: SkillInfo[]): SkillHealthStats {
  return skills.reduce<SkillHealthStats>((stats, skill) => {
    stats.total += 1;
    if (hasMissingDeps(skill)) stats.missingDeps += 1;
    if (isDisabled(skill)) stats.disabled += 1;
    if (isArchived(skill)) stats.archived += 1;
    if (isUnmanaged(skill)) stats.unmanaged += 1;
    if (needsAttention(skill)) stats.attention += 1;
    return stats;
  }, { total: 0, missingDeps: 0, disabled: 0, archived: 0, unmanaged: 0, attention: 0 });
}

export function filterSkills(skills: SkillInfo[], state: Pick<SkillsPageState, "q" | "filter" | "agent">): SkillInfo[] {
  const q = state.q.trim().toLowerCase();
  return skills.filter((skill) => {
    if (q && !matchesQuery(skill, q)) return false;
    if (state.agent && !matchesAgent(skill, state.agent)) return false;
    return matchesFilter(skill, state.filter);
  });
}

export function sortSkills(skills: SkillInfo[], sort: SkillsPageState["sort"]): SkillInfo[] {
  const next = [...skills];
  next.sort((a, b) => {
    if (sort === "deps") {
      const diff = (b.missing_deps?.length ?? 0) - (a.missing_deps?.length ?? 0);
      if (diff !== 0) return diff;
    }
    if (sort === "version") {
      const diff = (b.version ?? 0) - (a.version ?? 0);
      if (diff !== 0) return diff;
    }
    return a.name.localeCompare(b.name);
  });
  return next;
}

export function needsAttention(skill: SkillInfo): boolean {
  return hasMissingDeps(skill) || isDisabled(skill) || isArchived(skill) || isUnmanaged(skill);
}

export function isUnmanaged(skill: SkillInfo): boolean {
  return !skill.is_system && (skill.manager_agents?.length ?? 0) === 0;
}

export function isArchived(skill: SkillInfo): boolean {
  return skill.status === "archived";
}

export function isDisabled(skill: SkillInfo): boolean {
  return skill.enabled === false;
}

export function hasMissingDeps(skill: SkillInfo): boolean {
  return (skill.missing_deps?.length ?? 0) > 0;
}

function matchesFilter(skill: SkillInfo, filter: SkillsFilter): boolean {
  switch (filter) {
    case "attention":
      return needsAttention(skill);
    case "missing-deps":
      return hasMissingDeps(skill);
    case "disabled":
      return isDisabled(skill);
    case "archived":
      return isArchived(skill);
    case "unmanaged":
      return isUnmanaged(skill);
    case "all":
      return true;
  }
}

function matchesQuery(skill: SkillInfo, q: string): boolean {
  const agentLabels = [
    skill.creator_agent?.display_name,
    skill.creator_agent?.agent_key,
    skill.creator_agent?.id,
    ...(skill.manager_agents ?? []).flatMap((agent) => [agent.display_name, agent.agent_key, agent.id]),
  ];
  return [
    skill.name,
    skill.slug,
    skill.description,
    skill.author,
    ...agentLabels,
  ].some((value) => value?.toLowerCase().includes(q));
}

function matchesAgent(skill: SkillInfo, agentRef: string): boolean {
  const candidates = [
    skill.creator_agent,
    ...(skill.manager_agents ?? []),
  ];
  return candidates.some((agent) =>
    agent?.id === agentRef || agent?.agent_key === agentRef || agent?.display_name === agentRef,
  );
}
