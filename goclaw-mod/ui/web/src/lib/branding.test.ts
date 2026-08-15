import { afterEach, describe, expect, it } from "vitest";
import { getRuntimeBranding } from "./branding";

describe("runtime branding", () => {
  afterEach(() => {
    document.body.innerHTML = "";
    document.head.innerHTML = "";
  });

  it("falls back to built-in GoClaw branding", () => {
    expect(getRuntimeBranding()).toEqual({
      appName: "GoClaw",
      appShortName: "GoClaw",
      logoUrl: "/goclaw-icon.svg",
    });
  });

  it("reads branding injected by the server", () => {
    const script = document.createElement("script");
    script.id = "goclaw-branding";
    script.type = "application/json";
    script.textContent = JSON.stringify({
      app_name: "Acme Agents",
      app_short_name: "Acme",
      logo_url: "/branding-assets/logo.png",
    });
    document.head.appendChild(script);

    expect(getRuntimeBranding()).toEqual({
      appName: "Acme Agents",
      appShortName: "Acme",
      logoUrl: "/branding-assets/logo.png",
    });
  });
});
