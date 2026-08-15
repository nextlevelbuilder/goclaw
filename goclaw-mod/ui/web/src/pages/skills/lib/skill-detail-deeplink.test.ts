import { describe, expect, it } from "vitest";
import { normalizeSkillDetailTab, parseSkillDetailVersionParam } from "./skill-detail-deeplink";

describe("skill detail deeplink", () => {
  it("keeps evolution tab when the skill has an id", () => {
    expect(normalizeSkillDetailTab("evolution", true, true)).toBe("evolution");
  });

  it("falls back from unavailable tabs to content", () => {
    expect(normalizeSkillDetailTab("files", false, true)).toBe("content");
    expect(normalizeSkillDetailTab("evolution", true, false)).toBe("content");
    expect(normalizeSkillDetailTab("unknown", true, true)).toBe("content");
  });

  it("parses positive integer versions only", () => {
    expect(parseSkillDetailVersionParam("4")).toBe(4);
    expect(parseSkillDetailVersionParam("0")).toBeNull();
    expect(parseSkillDetailVersionParam("bad")).toBeNull();
  });
});
