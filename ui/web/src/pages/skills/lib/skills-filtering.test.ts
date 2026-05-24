import { describe, expect, it } from "vitest";
import type { SkillInfo } from "../hooks/use-skills";
import { deriveSkillStats, displayDependencyName, filterSkills, sortSkills } from "./skills-filtering";

function skill(overrides: Partial<SkillInfo>): SkillInfo {
  return {
    id: overrides.name ?? "id",
    name: overrides.name ?? "Skill",
    description: overrides.description ?? "",
    source: "custom",
    ...overrides,
  };
}

describe("skills filtering", () => {
  const skills = [
    skill({ name: "Core", is_system: true, manager_agents: [] }),
    skill({ name: "Missing", missing_deps: ["pip:google", "npm:zod"], manager_agents: [{ id: "a1" }] }),
    skill({ name: "Disabled", enabled: false, manager_agents: [{ id: "a2" }] }),
    skill({ name: "Archived", status: "archived", manager_agents: [{ id: "a3" }] }),
    skill({ name: "Unmanaged", manager_agents: [] }),
  ];

  it("derives attention stats without counting system skills as unmanaged", () => {
    expect(deriveSkillStats(skills)).toEqual({
      total: 5,
      missingDeps: 1,
      disabled: 1,
      archived: 1,
      unmanaged: 1,
      attention: 4,
    });
  });

  it("filters attention and unmanaged skills", () => {
    expect(filterSkills(skills, { q: "", filter: "attention", agent: null }).map((s) => s.name))
      .toEqual(["Missing", "Disabled", "Archived", "Unmanaged"]);
    expect(filterSkills(skills, { q: "", filter: "unmanaged", agent: null }).map((s) => s.name))
      .toEqual(["Unmanaged"]);
  });

  it("searches skill and agent metadata", () => {
    expect(filterSkills(skills, { q: "a2", filter: "all", agent: null }).map((s) => s.name)).toEqual(["Disabled"]);
  });

  it("sorts dependency-heavy skills first", () => {
    expect(sortSkills(skills, "deps")[0]?.name).toBe("Missing");
  });

  it("strips dependency runtime prefixes only for display", () => {
    expect(displayDependencyName("pip:google")).toBe("google");
    expect(displayDependencyName("core")).toBe("core");
  });
});
