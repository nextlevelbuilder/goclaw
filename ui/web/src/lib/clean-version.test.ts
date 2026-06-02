import { describe, expect, it } from "vitest";
import { cleanVersion } from "./clean-version";

describe("cleanVersion", () => {
  it("keeps beta version identity and drops build slug", () => {
    expect(cleanVersion("v3.12.0-beta.13-skills-ux-99f30ecc")).toBe("v3.12.0.beta.13");
  });

  it("keeps rc version identity", () => {
    expect(cleanVersion("v3.12.0-rc.2-linux-amd64")).toBe("v3.12.0.rc.2");
  });

  it("strips git distance metadata from stable versions", () => {
    expect(cleanVersion("v2.5.1-3-g4fd653c1")).toBe("v2.5.1");
  });

  it("leaves non-semver versions unchanged", () => {
    expect(cleanVersion("dev")).toBe("dev");
  });
});
