import { describe, expect, it } from "vitest";
import {
  parseSkillDetailVersionParam,
  shouldLoadSkillDetailFile,
} from "./skill-detail-deeplink";

describe("skill detail deeplink helpers", () => {
  it("parses valid version params", () => {
    expect(parseSkillDetailVersionParam("1")).toBe(1);
    expect(parseSkillDetailVersionParam("42")).toBe(42);
  });

  it("rejects malformed version params", () => {
    expect(parseSkillDetailVersionParam(null)).toBeNull();
    expect(parseSkillDetailVersionParam("")).toBeNull();
    expect(parseSkillDetailVersionParam("abc")).toBeNull();
    expect(parseSkillDetailVersionParam("1.5")).toBeNull();
    expect(parseSkillDetailVersionParam("0")).toBeNull();
    expect(parseSkillDetailVersionParam("-1")).toBeNull();
  });

  it("loads a deeplinked file only from the files tab when a file list exists", () => {
    expect(shouldLoadSkillDetailFile("files", "scripts/run.py", 3, null)).toBe(true);
    expect(shouldLoadSkillDetailFile("content", "scripts/run.py", 3, null)).toBe(false);
    expect(shouldLoadSkillDetailFile("files", null, 3, null)).toBe(false);
    expect(shouldLoadSkillDetailFile("files", "scripts/run.py", 0, null)).toBe(false);
    expect(shouldLoadSkillDetailFile("files", "scripts/run.py", 3, "scripts/run.py")).toBe(false);
  });
});
