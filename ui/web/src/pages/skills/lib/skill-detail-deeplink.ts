export function parseSkillDetailVersionParam(value: string | null): number | null {
  if (!value) return null;
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed <= 0) return null;
  return parsed;
}

export function shouldLoadSkillDetailFile(
  detailTab: string,
  selectedFilePath: string | null,
  filesCount: number,
  activePath: string | null,
): selectedFilePath is string {
  return detailTab === "files" && !!selectedFilePath && filesCount > 0 && activePath !== selectedFilePath;
}
