import { describe, expect, it } from "vitest";
import {
  BRANDING_ASSET_ACCEPT,
  MAX_BRANDING_ASSET_BYTES,
  buildBrandingPatch,
  normalizeBrandingDraft,
  validateBrandingAssetFile,
  withUploadedBrandingAssetURL,
} from "./branding-section-utils";

describe("branding-section-utils", () => {
  it("builds a stable branding payload and preserves cleared fields", () => {
    const draft = normalizeBrandingDraft({
      app_name: "Acme AI",
      meta_title: "Acme AI Console",
      logo_url: "/branding-assets/logo.png",
      favicon_url: "",
    });

    expect(buildBrandingPatch(draft)).toEqual({
      app_name: "Acme AI",
      meta_title: "Acme AI Console",
      logo_url: "/branding-assets/logo.png",
      favicon_url: "",
    });
  });

  it("applies uploaded asset URLs to the requested media field only", () => {
    const next = withUploadedBrandingAssetURL(
      { logo_url: "/old.png", og_image_url: "/og.png" },
      "logo_url",
      "/branding-assets/new-logo.png",
    );

    expect(next).toEqual({
      logo_url: "/branding-assets/new-logo.png",
      og_image_url: "/og.png",
    });
  });

  it("validates branding asset extension and size before upload", () => {
    expect(BRANDING_ASSET_ACCEPT).toContain(".webp");
    expect(validateBrandingAssetFile({ name: "logo.png", size: MAX_BRANDING_ASSET_BYTES })).toBeNull();
    expect(validateBrandingAssetFile({ name: "logo.gif", size: 10 })).toBe("unsupported_type");
    expect(validateBrandingAssetFile({ name: "logo.svg", size: MAX_BRANDING_ASSET_BYTES + 1 })).toBe("too_large");
  });
});
