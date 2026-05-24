export type SkillAccessMode = "private" | "internal" | "public";

export const SKILL_ACCESS_MODE_ORDER: SkillAccessMode[] = ["private", "internal", "public"];

export type SkillAccessModeKey = SkillAccessMode | "unknown";

export function getSkillAccessModeKey(visibility?: string): SkillAccessModeKey {
  return isSkillAccessMode(visibility) ? visibility : "unknown";
}

export function getNextSkillAccessMode(visibility?: string): SkillAccessMode {
  if (!isSkillAccessMode(visibility)) return "private";
  const idx = SKILL_ACCESS_MODE_ORDER.indexOf(visibility);
  return SKILL_ACCESS_MODE_ORDER[(idx + 1) % SKILL_ACCESS_MODE_ORDER.length] ?? "private";
}

export function getSkillAccessModeBadgeVariant(visibility?: string): "default" | "secondary" | "outline" {
  switch (getSkillAccessModeKey(visibility)) {
    case "public":
      return "default";
    case "internal":
      return "secondary";
    case "private":
    case "unknown":
      return "outline";
  }
}

function isSkillAccessMode(value?: string): value is SkillAccessMode {
  return value === "private" || value === "internal" || value === "public";
}
