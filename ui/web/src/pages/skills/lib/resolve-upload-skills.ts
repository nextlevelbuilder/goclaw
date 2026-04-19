import { validateMultiSkillZip, type MultiSkillZipValidation } from "./validate-skill-zip";
import type { SkillEntry, SkillStatus } from "./skill-upload-types";

type SkillEntrySeed = Omit<SkillEntry, "id">;

type ValidateSkillArchive = (file: File) => Promise<MultiSkillZipValidation>;

// Browser-side ZIP parsing is best-effort only. Some valid ZIP variants are
// accepted by the backend but rejected by JSZip, so fall back to a direct
// single-file upload instead of blocking the user locally.
export async function resolveUploadSkills(
  file: File,
  validateArchive: ValidateSkillArchive = validateMultiSkillZip,
): Promise<SkillEntrySeed[]> {
  try {
    const validation = await validateArchive(file);
    if (validation.error === "upload.invalidZip") {
      return [fallbackUploadSkill()];
    }
    if (validation.error) {
      return [invalidSkill(validation.error)];
    }
    if (validation.skills.length === 0) {
      return [invalidSkill("upload.noSkillMd")];
    }
    return validation.skills.map((skill) => ({
      dir: skill.dir,
      status: skill.valid ? ("valid" as SkillStatus) : ("invalid" as SkillStatus),
      name: skill.name,
      slug: skill.slug,
      contentHash: skill.contentHash,
      error: skill.error,
    }));
  } catch {
    return [fallbackUploadSkill()];
  }
}

function fallbackUploadSkill(): SkillEntrySeed {
  return { dir: "", status: "valid" };
}

function invalidSkill(error: string): SkillEntrySeed {
  return { dir: "", status: "invalid", error };
}
