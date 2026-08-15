import { describe, expect, it } from "vitest";
import {
  buildSkillExportPath,
  skillExportDownloadName,
  skillExportExtension,
  type SkillExportFormat,
} from "./skill-export-download";

describe("skill export download helpers", () => {
  it("builds a selected export URL with repeated id params", () => {
    const path = buildSkillExportPath(["skill-a", "skill-b"], "zip");
    const url = new URL(path, "https://goclaw.test");

    expect(url.pathname).toBe("/v1/skills/export");
    expect(url.searchParams.get("format")).toBe("zip");
    expect(url.searchParams.getAll("id")).toEqual(["skill-a", "skill-b"]);
  });

  it("rejects empty selections", () => {
    expect(() => buildSkillExportPath([], "zip")).toThrow("at least one skill");
  });

  it.each<SkillExportFormat>(["zip", "tar.gz", "tgz"])("maps extension for %s", (format) => {
    expect(skillExportExtension(format)).toMatch(/^\.(zip|tar\.gz)$/);
  });

  it("names single skill archives by slug and version", () => {
    const name = skillExportDownloadName([{ slug: "my-skill", version: 3 }], "zip");

    expect(name).toBe("goclaw-skill-my-skill-v3.zip");
  });

  it("names multi skill archives with selected extension", () => {
    const name = skillExportDownloadName([
      { slug: "one", version: 1 },
      { slug: "two", version: 2 },
    ], "tar.gz", new Date("2026-05-29T09:30:00Z"));

    expect(name).toBe("goclaw-skills-export-20260529-0930.tar.gz");
  });
});
