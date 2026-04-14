/** Shared types for the skill upload dialog and its sub-components. */

/** Per-skill lifecycle status */
export type SkillStatus =
  | "validating"
  | "valid"        // new skill, ready to upload
  | "unchanged"    // server returned identical hash — skipped
  | "invalid"
  | "uploading"
  | "success"
  | "warning"      // uploaded but deps_warning present
  | "error";

export interface SkillEntry {
  id: string;
  dir: string;
  status: SkillStatus;
  name?: string;
  slug?: string;
  contentHash?: string;
  error?: string;
}

export interface FileEntry {
  id: string;
  file: File;
  /** One entry per detected skill in this ZIP */
  skills: SkillEntry[];
}
