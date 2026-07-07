export interface RuntimeBranding {
  appName: string;
  appShortName: string;
  logoUrl: string;
}

const FALLBACK_BRANDING: RuntimeBranding = {
  appName: "GoClaw",
  appShortName: "GoClaw",
  logoUrl: "/goclaw-icon.svg",
};

interface RuntimeBrandingPayload {
  app_name?: unknown;
  app_short_name?: unknown;
  logo_url?: unknown;
}

function stringValue(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() !== "" ? value.trim() : undefined;
}

export function getRuntimeBranding(): RuntimeBranding {
  if (typeof document === "undefined") return FALLBACK_BRANDING;
  const node = document.getElementById("goclaw-branding");
  if (!node?.textContent) return FALLBACK_BRANDING;
  try {
    const payload = JSON.parse(node.textContent) as RuntimeBrandingPayload;
    const appName = stringValue(payload.app_name) ?? FALLBACK_BRANDING.appName;
    return {
      appName,
      appShortName: stringValue(payload.app_short_name) ?? appName,
      logoUrl: stringValue(payload.logo_url) ?? FALLBACK_BRANDING.logoUrl,
    };
  } catch {
    return FALLBACK_BRANDING;
  }
}
