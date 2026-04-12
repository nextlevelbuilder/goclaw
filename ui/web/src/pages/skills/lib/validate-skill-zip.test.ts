import { describe, it, expect } from "vitest";
import JSZip from "jszip";
// Import the functions we'll create/modify
import { validateSkillZip, validateMultiSkillZip } from "./validate-skill-zip";

// Helper: create a ZIP file from an object mapping paths to content
async function createTestZip(files: Record<string, string>): Promise<File> {
  const zip = new JSZip();
  for (const [path, content] of Object.entries(files)) {
    zip.file(path, content);
  }
  const blob = await zip.generateAsync({ type: "blob" });
  return new File([blob], "test.zip", { type: "application/zip" });
}

const validFrontmatter = `---
name: Test Skill
slug: test-skill
description: A test skill
---
# Test Skill Content`;

const validFrontmatter2 = `---
name: Another Skill
slug: another-skill
description: Another test
---
# Another Skill`;

const noNameFrontmatter = `---
description: Missing name
---
# No Name`;

describe("validateSkillZip (backward compat)", () => {
  it("validates single-skill ZIP at root", async () => {
    const file = await createTestZip({ "SKILL.md": validFrontmatter });
    const result = await validateSkillZip(file);
    expect(result.valid).toBe(true);
    expect(result.name).toBe("Test Skill");
    expect(result.slug).toBe("test-skill");
  });

  it("validates single-skill ZIP in subdirectory", async () => {
    const file = await createTestZip({ "my-skill/SKILL.md": validFrontmatter });
    const result = await validateSkillZip(file);
    expect(result.valid).toBe(true);
    expect(result.name).toBe("Test Skill");
  });

  it("rejects ZIP without SKILL.md", async () => {
    const file = await createTestZip({ "readme.txt": "hello" });
    const result = await validateSkillZip(file);
    expect(result.valid).toBe(false);
    expect(result.error).toBe("upload.noSkillMd");
  });

  it("rejects empty SKILL.md", async () => {
    const file = await createTestZip({ "SKILL.md": "   " });
    const result = await validateSkillZip(file);
    expect(result.valid).toBe(false);
    expect(result.error).toBe("upload.emptySkillMd");
  });

  it("rejects missing frontmatter name", async () => {
    const file = await createTestZip({ "SKILL.md": noNameFrontmatter });
    const result = await validateSkillZip(file);
    expect(result.valid).toBe(false);
    expect(result.error).toBe("upload.nameRequired");
  });

  it("auto-slugifies name when no slug provided", async () => {
    const fm = `---\nname: My Cool Tool\ndescription: test\n---\n# Content`;
    const file = await createTestZip({ "SKILL.md": fm });
    const result = await validateSkillZip(file);
    expect(result.valid).toBe(true);
    expect(result.slug).toBe("my-cool-tool");
  });
});

describe("validateMultiSkillZip", () => {
  it("detects multiple skills in ZIP", async () => {
    const file = await createTestZip({
      "pdf/SKILL.md": validFrontmatter,
      "csv/SKILL.md": validFrontmatter2,
    });
    const result = await validateMultiSkillZip(file);
    expect(result.skills).toHaveLength(2);
    expect(result.skills.map(s => s.slug).sort()).toEqual(["another-skill", "test-skill"]);
  });

  it("root SKILL.md returns single entry (no subdir scan)", async () => {
    const file = await createTestZip({
      "SKILL.md": validFrontmatter,
      "subdir/SKILL.md": validFrontmatter2,
    });
    const result = await validateMultiSkillZip(file);
    expect(result.skills).toHaveLength(1);
    expect(result.skills[0]!.slug).toBe("test-skill");
  });

  it("computes per-skill SHA-256 contentHash", async () => {
    const file = await createTestZip({ "skill/SKILL.md": validFrontmatter });
    const result = await validateMultiSkillZip(file);
    expect(result.skills[0]!.contentHash).toBeDefined();
    expect(result.skills[0]!.contentHash).toMatch(/^[a-f0-9]{64}$/);
  });

  it("ignores directories without SKILL.md", async () => {
    const file = await createTestZip({
      "pdf/SKILL.md": validFrontmatter,
      "assets/image.png": "binary",
    });
    const result = await validateMultiSkillZip(file);
    expect(result.skills).toHaveLength(1);
    expect(result.skills[0]!.dir).toBe("pdf");
  });

  it("reports per-skill validation errors", async () => {
    const file = await createTestZip({
      "good/SKILL.md": validFrontmatter,
      "bad/SKILL.md": noNameFrontmatter,
    });
    const result = await validateMultiSkillZip(file);
    expect(result.skills).toHaveLength(2);
    const good = result.skills.find(s => s.dir === "good");
    const bad = result.skills.find(s => s.dir === "bad");
    expect(good?.valid).toBe(true);
    expect(bad?.valid).toBe(false);
    expect(bad?.error).toBe("upload.nameRequired");
  });

  it("backward compat: single-skill ZIP returns skills array of 1", async () => {
    const file = await createTestZip({ "SKILL.md": validFrontmatter });
    const result = await validateMultiSkillZip(file);
    expect(result.skills).toHaveLength(1);
    expect(result.skills[0]!.valid).toBe(true);
    expect(result.skills[0]!.name).toBe("Test Skill");
  });
});
