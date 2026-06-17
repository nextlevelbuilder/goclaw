export function parseSkillDetailVersionParam(value: string | null): number | null {
  if (!value) return null;
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed <= 0) return null;
  return parsed;
}

export function normalizeSkillDetailTab(value: string, hasFiles: boolean, hasEvolution: boolean): "content" | "files" | "evolution" {
  if (value === "files" && hasFiles) return "files";
  if (value === "evolution" && hasEvolution) return "evolution";
  return "content";
}

export function shouldLoadSkillDetailFile(
  detailTab: string,
  selectedFilePath: string | null,
  filesCount: number,
  activePath: string | null,
): selectedFilePath is string {
  return detailTab === "files" && !!selectedFilePath && filesCount > 0 && activePath !== selectedFilePath;
}
