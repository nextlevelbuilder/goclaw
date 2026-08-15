import { describe, expect, it } from "vitest";
import {
  getNextSkillAccessMode,
  getSkillAccessModeBadgeVariant,
  getSkillAccessModeKey,
} from "./skill-access-mode";

describe("skill access mode helpers", () => {
  it("maps raw visibility values to stable access-mode keys", () => {
    expect(getSkillAccessModeKey("private")).toBe("private");
    expect(getSkillAccessModeKey("internal")).toBe("internal");
    expect(getSkillAccessModeKey("public")).toBe("public");
    expect(getSkillAccessModeKey("team")).toBe("unknown");
  });

  it("cycles known modes and normalizes unknown values to private", () => {
    expect(getNextSkillAccessMode("private")).toBe("internal");
    expect(getNextSkillAccessMode("internal")).toBe("public");
    expect(getNextSkillAccessMode("public")).toBe("private");
    expect(getNextSkillAccessMode("team")).toBe("private");
  });

  it("uses conservative badge variants for unknown values", () => {
    expect(getSkillAccessModeBadgeVariant("public")).toBe("default");
    expect(getSkillAccessModeBadgeVariant("internal")).toBe("secondary");
    expect(getSkillAccessModeBadgeVariant("private")).toBe("outline");
    expect(getSkillAccessModeBadgeVariant("team")).toBe("outline");
  });
});
