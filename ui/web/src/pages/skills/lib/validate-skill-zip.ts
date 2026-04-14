/** Client-side validation for skill ZIP files before upload.
 * Mirrors server-side checks in internal/http/skills_upload.go
 *
 * Supports both single-skill ZIPs (SKILL.md at root or one top-level dir)
 * and multi-skill ZIPs (one SKILL.md per top-level directory). */
import JSZip from "jszip";

export interface SkillZipValidation {
  valid: boolean;
  name?: string;
  slug?: string;
  description?: string;
  /** i18n key under "upload." namespace */
  error?: string;
  errorDetail?: string;
}

/** Per-skill entry returned by validateMultiSkillZip */
export interface SkillValidationEntry {
  valid: boolean;
  /** Top-level directory name, or "" for root-level SKILL.md */
  dir: string;
  name?: string;
  slug?: string;
  description?: string;
  /** SHA-256 hex digest of the SKILL.md content */
  contentHash?: string;
  /** i18n key under "upload." namespace */
  error?: string;
  errorDetail?: string;
}

export interface MultiSkillZipValidation {
  skills: SkillValidationEntry[];
  /** Top-level ZIP error (corrupt file, not a ZIP, too large) */
  error?: string;
}

// Constants matching server-side (internal/http/skills.go)
const MAX_SKILL_SIZE = 20 * 1024 * 1024; // 20MB
const MAX_SKILLS_PER_ZIP = 50;
const SLUG_REGEX = /^[a-z0-9][a-z0-9-]*[a-z0-9]$/;
const FRONTMATTER_REGEX = /^---\r?\n([\s\S]*?)\r?\n---/;

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/**
 * Validate a skill ZIP file client-side — backward-compatible single-skill path.
 * Delegates to validateMultiSkillZip and returns the first skill's result.
 */
export async function validateSkillZip(file: File): Promise<SkillZipValidation> {
  const multi = await validateMultiSkillZip(file);
  if (multi.error) return { valid: false, error: multi.error };
  const first = multi.skills[0];
  if (!first) return { valid: false, error: "upload.noSkillMd" };
  return {
    valid: first.valid,
    name: first.name,
    slug: first.slug,
    description: first.description,
    error: first.error,
    errorDetail: first.errorDetail,
  };
}

/**
 * Validate a ZIP that may contain one or multiple skills.
 *
 * Detection logic:
 * - If SKILL.md exists at root → single-skill mode (root entry, dir="")
 * - Otherwise → scan each top-level directory for SKILL.md
 *
 * Returns one SkillValidationEntry per detected SKILL.md, each independently
 * validated with a SHA-256 contentHash.
 */
export async function validateMultiSkillZip(file: File): Promise<MultiSkillZipValidation> {
  if (!file.name.toLowerCase().endsWith(".zip")) {
    return { skills: [], error: "upload.onlyZip" };
  }
  if (file.size > MAX_SKILL_SIZE) {
    return { skills: [], error: "upload.tooLarge" };
  }

  let zip: JSZip;
  try {
    zip = await JSZip.loadAsync(file);
  } catch {
    return { skills: [], error: "upload.invalidZip" };
  }

  const entries = await findAllSkillMds(zip);
  if (entries.length === 0) {
    return { skills: [] };
  }
  if (entries.length > MAX_SKILLS_PER_ZIP) {
    return { skills: [], error: "upload.tooManySkills" };
  }

  // Validate each skill entry concurrently
  const skills = await Promise.all(
    entries.map(({ dir, content }) => validateSkillEntry(dir, content)),
  );

  return { skills };
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

interface SkillMdEntry {
  dir: string;
  content: string;
}

/**
 * Find all SKILL.md files in the ZIP.
 *
 * Priority rule: if SKILL.md exists at root level, return only that one entry
 * (single-skill mode). Otherwise collect one per top-level directory.
 */
async function findAllSkillMds(zip: JSZip): Promise<SkillMdEntry[]> {
  // Root-level SKILL.md → single-skill mode
  if (zip.files["SKILL.md"] && !zip.files["SKILL.md"].dir) {
    const content = await zip.files["SKILL.md"].async("string");
    return [{ dir: "", content }];
  }

  // Multi-skill: collect directories that contain a SKILL.md
  const paths = Object.keys(zip.files);
  const topDirs = new Set(
    paths
      .map((p) => p.split("/")[0])
      .filter((d): d is string => Boolean(d)),
  );

  const results: SkillMdEntry[] = [];
  // Process dirs in stable sorted order for deterministic output
  for (const dir of [...topDirs].sort()) {
    const key = dir + "/SKILL.md";
    if (zip.files[key] && !zip.files[key].dir) {
      const content = await zip.files[key].async("string");
      results.push({ dir, content });
    }
  }
  return results;
}

/** Validate a single SKILL.md entry, compute its hash, return SkillValidationEntry */
async function validateSkillEntry(dir: string, content: string): Promise<SkillValidationEntry> {
  if (!content.trim()) {
    return { valid: false, dir, error: "upload.emptySkillMd" };
  }

  const match = content.match(FRONTMATTER_REGEX);
  if (!match?.[1]) {
    return { valid: false, dir, error: "upload.noFrontmatter" };
  }

  const fields = parseFrontmatterFields(match[1]);
  if (!fields.name) {
    return { valid: false, dir, error: "upload.nameRequired" };
  }

  const slug = fields.slug ?? slugify(fields.name);
  if (!SLUG_REGEX.test(slug)) {
    return { valid: false, dir, error: "upload.invalidSlug", errorDetail: slug };
  }

  const contentHash = await hashContent(content);

  return {
    valid: true,
    dir,
    name: fields.name,
    slug,
    description: fields.description,
    contentHash,
  };
}

/** SHA-256 hex digest using Web Crypto API */
async function hashContent(content: string): Promise<string> {
  const data = new TextEncoder().encode(content);
  const buf = await crypto.subtle.digest("SHA-256", data);
  return Array.from(new Uint8Array(buf))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

/** Simple key: value parser matching server's parseSkillFrontmatter() */
function parseFrontmatterFields(raw: string): Record<string, string> {
  const fields: Record<string, string> = {};
  for (const line of raw.split(/\r?\n/)) {
    const idx = line.indexOf(":");
    if (idx > 0) {
      const key = line.slice(0, idx).trim();
      const val = line
        .slice(idx + 1)
        .trim()
        .replace(/^["']|["']$/g, "");
      if (key && val) fields[key] = val;
    }
  }
  return fields;
}

function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}
