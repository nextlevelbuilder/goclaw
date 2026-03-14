function slugValid(s: string): boolean {
  return /^[a-z0-9-]+$/.test(s);
}

export const brand = {
  appName: import.meta.env.VITE_BRAND_APP_NAME || "GoClaw",
  appKey: (() => {
    const k = import.meta.env.VITE_BRAND_APP_KEY || "goclaw";
    return slugValid(k) ? k : "goclaw";
  })(),
  tagline: import.meta.env.VITE_BRAND_TAGLINE || "",
  logoUrl: import.meta.env.VITE_BRAND_LOGO_URL || "",
} as const;
