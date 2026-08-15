import type { FileEntry, SkillEntry, SkillStatus } from "./skill-upload-types";

export interface SkillUploadSummary {
  total: number;
  valid: number;
  uploaded: number;
  warnings: number;
  unchanged: number;
  failed: number;
  invalid: number;
  inProgress: number;
}

const EMPTY_SUMMARY: SkillUploadSummary = {
  total: 0,
  valid: 0,
  uploaded: 0,
  warnings: 0,
  unchanged: 0,
  failed: 0,
  invalid: 0,
  inProgress: 0,
};

export function summarizeUploadEntries(entries: FileEntry[]): SkillUploadSummary {
  return summarizeSkillStatuses(entries.flatMap((entry) => entry.skills));
}

export function summarizeSkillStatuses(skills: Pick<SkillEntry, "status">[]): SkillUploadSummary {
  return skills.reduce<SkillUploadSummary>((summary, skill) => {
    summary.total += 1;
    applyStatus(summary, skill.status);
    return summary;
  }, { ...EMPTY_SUMMARY });
}

function applyStatus(summary: SkillUploadSummary, status: SkillStatus) {
  switch (status) {
    case "valid":
      summary.valid += 1;
      break;
    case "success":
      summary.uploaded += 1;
      break;
    case "warning":
      summary.warnings += 1;
      break;
    case "unchanged":
      summary.unchanged += 1;
      break;
    case "invalid":
      summary.invalid += 1;
      break;
    case "error":
      summary.failed += 1;
      break;
    case "validating":
    case "uploading":
      summary.inProgress += 1;
      break;
  }
}
