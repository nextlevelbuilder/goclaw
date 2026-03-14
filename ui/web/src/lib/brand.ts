function slugValid(s: string): boolean {
  return /^[a-z0-9-]+$/.test(s);
}

interface BrandData {
  appName: string;
  appKey: string;
  tagline: string;
  logoUrl: string;
}

// Mutable brand singleton — initial values from env vars (build-time defaults),
// then overridden by server-sent brand config on connect.
export const brand: BrandData = {
  appName: import.meta.env.VITE_BRAND_APP_NAME || "Agent Studio",
  appKey: (() => {
    const k = import.meta.env.VITE_BRAND_APP_KEY || "agent-studio";
    return slugValid(k) ? k : "agent-studio";
  })(),
  tagline: import.meta.env.VITE_BRAND_TAGLINE || "Build your own AI agent",
  logoUrl: import.meta.env.VITE_BRAND_LOGO_URL || "",
};

/**
 * Update brand values from server-sent config (connect response).
 * Only overrides non-empty fields — empty server values keep the env-var defaults.
 */
export function updateBrand(data: Partial<BrandData>): void {
  if (data.appName) brand.appName = data.appName;
  if (data.appKey && slugValid(data.appKey)) brand.appKey = data.appKey;
  if (data.tagline !== undefined) brand.tagline = data.tagline;
  if (data.logoUrl !== undefined) brand.logoUrl = data.logoUrl;
}
