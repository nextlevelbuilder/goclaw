import { describe, expect, it, vi } from "vitest";

import { resolveUploadSkills } from "./resolve-upload-skills";
import type { MultiSkillZipValidation } from "./validate-skill-zip";

function makeZipFile(name = "skill.zip"): File {
  return new File([new Uint8Array([0x50, 0x4b, 0x03, 0x04])], name, {
    type: "application/zip",
  });
}

describe("resolveUploadSkills", () => {
  it("falls back to direct upload when browser ZIP parsing reports invalidZip", async () => {
    const validateArchive = vi.fn<(_: File) => Promise<MultiSkillZipValidation>>()
      .mockResolvedValue({ skills: [], error: "upload.invalidZip" });

    const result = await resolveUploadSkills(makeZipFile(), validateArchive);

    expect(result).toEqual([{ dir: "", status: "valid" }]);
  });

  it("falls back to direct upload when browser ZIP parsing throws", async () => {
    const validateArchive = vi.fn<(_: File) => Promise<MultiSkillZipValidation>>()
      .mockRejectedValue(new Error("unsupported zip variant"));

    const result = await resolveUploadSkills(makeZipFile(), validateArchive);

    expect(result).toEqual([{ dir: "", status: "valid" }]);
  });

  it("keeps real validation errors blocking", async () => {
    const validateArchive = vi.fn<(_: File) => Promise<MultiSkillZipValidation>>()
      .mockResolvedValue({ skills: [], error: "upload.onlyZip" });

    const result = await resolveUploadSkills(makeZipFile("skill.txt"), validateArchive);

    expect(result).toEqual([{ dir: "", status: "invalid", error: "upload.onlyZip" }]);
  });
});
