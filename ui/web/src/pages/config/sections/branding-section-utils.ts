export interface BrandingData {
  app_name?: string;
  app_short_name?: string;
  meta_title?: string;
  meta_description?: string;
  meta_keywords?: string;
  logo_url?: string;
  favicon_url?: string;
  apple_touch_icon_url?: string;
  og_title?: string;
  og_description?: string;
  og_image_url?: string;
  theme_color?: string;
}

export type BrandingAssetField = "logo_url" | "favicon_url" | "apple_touch_icon_url" | "og_image_url";

export const BRANDING_ASSET_ACCEPT = ".svg,.png,.jpg,.jpeg,.webp,.ico";
export const MAX_BRANDING_ASSET_BYTES = 2 * 1024 * 1024;

const BRANDING_KEYS: Array<keyof BrandingData> = [
  "app_name",
  "app_short_name",
  "meta_title",
  "meta_description",
  "meta_keywords",
  "logo_url",
  "favicon_url",
  "apple_touch_icon_url",
  "og_title",
  "og_description",
  "og_image_url",
  "theme_color",
];

const ALLOWED_EXTENSIONS = new Set(["svg", "png", "jpg", "jpeg", "webp", "ico"]);

export function normalizeBrandingDraft(data: BrandingData | undefined): BrandingData {
  const out: BrandingData = {};
  for (const key of BRANDING_KEYS) {
    const value = data?.[key];
    if (typeof value === "string") out[key] = value;
  }
  return out;
}

export function buildBrandingPatch(draft: BrandingData): BrandingData {
  return normalizeBrandingDraft(draft);
}

export function withUploadedBrandingAssetURL(draft: BrandingData, field: BrandingAssetField, url: string): BrandingData {
  return { ...draft, [field]: url };
}

export function validateBrandingAssetFile(file: Pick<File, "name" | "size">): string | null {
  if (file.size > MAX_BRANDING_ASSET_BYTES) return "too_large";
  const ext = file.name.split(".").pop()?.toLowerCase() ?? "";
  if (!ALLOWED_EXTENSIONS.has(ext)) return "unsupported_type";
  return null;
}
