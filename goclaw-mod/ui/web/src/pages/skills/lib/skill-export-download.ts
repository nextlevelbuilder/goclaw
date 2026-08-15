export type SkillExportFormat = "zip" | "tar.gz" | "tgz";

export type SkillExportSummary = {
  id?: string;
  slug?: string;
  name?: string;
  version?: number;
};

export function buildSkillExportPath(ids: string[], format: SkillExportFormat): string {
  const cleanIds = Array.from(new Set(ids.map((id) => id.trim()).filter(Boolean)));
  if (cleanIds.length === 0) {
    throw new Error("at least one skill is required");
  }

  const params = new URLSearchParams();
  params.set("format", format);
  for (const id of cleanIds) params.append("id", id);

  return `/v1/skills/export?${params.toString()}`;
}

export function skillExportExtension(format: SkillExportFormat): ".zip" | ".tar.gz" {
  return format === "zip" ? ".zip" : ".tar.gz";
}

export function skillExportDownloadName(
  skills: SkillExportSummary[],
  format: SkillExportFormat,
  now = new Date(),
): string {
  const extension = skillExportExtension(format);
  if (skills.length === 1) {
    const skill = skills[0]!;
    const label = sanitizeFilePart(skill.slug || skill.name || skill.id || "skill");
    const version = skill.version ? `-v${skill.version}` : "";
    return `goclaw-skill-${label}${version}${extension}`;
  }

  return `goclaw-skills-export-${formatUtcTimestamp(now)}${extension}`;
}

function sanitizeFilePart(value: string): string {
  return value.trim().toLowerCase().replace(/[^a-z0-9._-]+/g, "-").replace(/^-+|-+$/g, "") || "skill";
}

function formatUtcTimestamp(date: Date): string {
  const year = date.getUTCFullYear();
  const month = pad(date.getUTCMonth() + 1);
  const day = pad(date.getUTCDate());
  const hour = pad(date.getUTCHours());
  const minute = pad(date.getUTCMinutes());
  return `${year}${month}${day}-${hour}${minute}`;
}

function pad(value: number): string {
  return String(value).padStart(2, "0");
}
