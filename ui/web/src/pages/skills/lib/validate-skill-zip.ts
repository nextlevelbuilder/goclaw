/** Client-side validation for skill ZIP files before upload.
 * Mirrors server-side checks in internal/http/skills_upload.go */
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

// Constants matching server-side (internal/http/skills.go)
const MAX_SKILL_SIZE = 20 * 1024 * 1024; // 20MB
const SLUG_REGEX = /^[a-z0-9][a-z0-9-]*[a-z0-9]$/;
const FRONTMATTER_REGEX = /^---\r?\n([\s\S]*?)\r?\n---/;

/** Validate a skill ZIP file client-side. JSZip is lazy-loaded. */
export async function validateSkillZip(file: File): Promise<SkillZipValidation> {
  if (!file.name.toLowerCase().endsWith(".zip")) {
    return { valid: false, error: "upload.onlyZip" };
  }
  if (file.size > MAX_SKILL_SIZE) {
    return { valid: false, error: "upload.tooLarge" };
  }

  let zip: JSZip;
  try {
    zip = await JSZip.loadAsync(file);
  } catch {
    return { valid: false, error: "upload.invalidZip" };
  }

  // Find SKILL.md at root or inside single top-level directory
  const skillMdContent = await findSkillMd(zip);
  if (skillMdContent === null) {
    return { valid: false, error: "upload.noSkillMd" };
  }
  if (!skillMdContent.trim()) {
    return { valid: false, error: "upload.emptySkillMd" };
  }

  // Parse frontmatter
  const match = skillMdContent.match(FRONTMATTER_REGEX);
  if (!match?.[1]) {
    return { valid: false, error: "upload.noFrontmatter" };
  }
  const fields = parseFrontmatterFields(match[1]);
  if (!fields.name) {
    return { valid: false, error: "upload.nameRequired" };
  }

  const slug = fields.slug || slugify(fields.name);
  if (!SLUG_REGEX.test(slug)) {
    return { valid: false, error: "upload.invalidSlug", errorDetail: slug };
  }

  return { valid: true, name: fields.name, slug, description: fields.description };
}

/** Find SKILL.md content — root level or inside a single top-level directory */
async function findSkillMd(zip: JSZip): Promise<string | null> {
  // Try root
  if (zip.files["SKILL.md"] && !zip.files["SKILL.md"].dir) {
    return zip.files["SKILL.md"].async("string");
  }
  // Try single top-level dir (e.g. "my-skill/SKILL.md")
  const paths = Object.keys(zip.files);
  const topDirs = new Set(paths.map((p) => p.split("/")[0]).filter(Boolean));
  for (const dir of topDirs) {
    const key = dir + "/SKILL.md";
    if (zip.files[key] && !zip.files[key].dir) {
      return zip.files[key].async("string");
    }
  }
  return null;
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
