/** Client-side validation for managed tool ZIP files before upload. */
import JSZip from "jszip";

export interface ToolZipValidation {
  valid: boolean;
  name?: string;
  slug?: string;
  description?: string;
  /** i18n key under "tools:managed.upload." namespace */
  error?: string;
  errorDetail?: string;
}

const MAX_TOOL_SIZE = 20 * 1024 * 1024; // 20MB
const SLUG_REGEX = /^[a-z0-9][a-z0-9-]*[a-z0-9]$/;
const FRONTMATTER_REGEX = /^---\r?\n([\s\S]*?)\r?\n---/;

export async function validateToolZip(file: File): Promise<ToolZipValidation> {
  if (!file.name.toLowerCase().endsWith(".zip")) {
    return { valid: false, error: "tools:managed.upload.invalidZip" };
  }
  if (file.size > MAX_TOOL_SIZE) {
    return { valid: false, error: "tools:managed.upload.invalidZip" };
  }

  let zip: JSZip;
  try {
    zip = await JSZip.loadAsync(file);
  } catch {
    return { valid: false, error: "tools:managed.upload.invalidZip" };
  }

  const toolMdContent = await findToolMd(zip);
  if (toolMdContent === null) {
    return { valid: false, error: "tools:managed.upload.missingManifest" };
  }
  if (!toolMdContent.trim()) {
    return { valid: false, error: "tools:managed.upload.missingManifest" };
  }

  const match = toolMdContent.match(FRONTMATTER_REGEX);
  if (!match?.[1]) {
    return { valid: false, error: "tools:managed.upload.missingManifest" };
  }
  const fields = parseFrontmatterFields(match[1]);
  if (!fields.name) {
    return { valid: false, error: "tools:managed.upload.missingManifest" };
  }

  const slug = fields.slug || slugify(fields.name);
  if (!SLUG_REGEX.test(slug)) {
    return { valid: false, error: "tools:managed.upload.invalidZip", errorDetail: slug };
  }

  return { valid: true, name: fields.name, slug, description: fields.description };
}

async function findToolMd(zip: JSZip): Promise<string | null> {
  if (zip.files["TOOL.md"] && !zip.files["TOOL.md"].dir) {
    return zip.files["TOOL.md"].async("string");
  }
  const paths = Object.keys(zip.files);
  const topDirs = new Set(paths.map((p) => p.split("/")[0]).filter(Boolean));
  for (const dir of topDirs) {
    const key = dir + "/TOOL.md";
    if (zip.files[key] && !zip.files[key].dir) {
      return zip.files[key].async("string");
    }
  }
  return null;
}

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
