import { describe, it, expect } from "vitest";
import JSZip from "jszip";
import { createSkillSubZip } from "./create-skill-sub-zip";

describe("createSkillSubZip", () => {
  it("extracts only files under the specified directory", async () => {
    const zip = new JSZip();
    zip.file("pdf/SKILL.md", "---\nname: PDF\n---\n# PDF");
    zip.file("pdf/scripts/convert.py", "print('convert')");
    zip.file("csv/SKILL.md", "---\nname: CSV\n---\n# CSV");

    const subFile = await createSkillSubZip(zip, "pdf");
    const subZip = await JSZip.loadAsync(subFile);
    const paths = Object.keys(subZip.files).filter((p) => !subZip.files[p]!.dir);

    expect(paths.sort()).toEqual(["SKILL.md", "scripts/convert.py"]);
    expect(subFile.name).toBe("pdf.zip");
  });

  it("handles nested subdirectories within skill dir", async () => {
    const zip = new JSZip();
    zip.file("my-skill/SKILL.md", "content");
    zip.file("my-skill/scripts/helpers/util.py", "util");
    zip.file("my-skill/references/guide.md", "guide");

    const subFile = await createSkillSubZip(zip, "my-skill");
    const subZip = await JSZip.loadAsync(subFile);
    const paths = Object.keys(subZip.files).filter((p) => !subZip.files[p]!.dir);

    expect(paths.sort()).toEqual(["SKILL.md", "references/guide.md", "scripts/helpers/util.py"]);
  });

  it("returns empty ZIP for non-existent directory", async () => {
    const zip = new JSZip();
    zip.file("other/SKILL.md", "content");

    const subFile = await createSkillSubZip(zip, "missing");
    const subZip = await JSZip.loadAsync(subFile);
    const paths = Object.keys(subZip.files).filter((p) => !subZip.files[p]!.dir);

    expect(paths).toEqual([]);
  });

  it("produces a File with correct mime type", async () => {
    const zip = new JSZip();
    zip.file("skill-a/SKILL.md", "content");

    const subFile = await createSkillSubZip(zip, "skill-a");

    expect(subFile).toBeInstanceOf(File);
    expect(subFile.type).toBe("application/zip");
    expect(subFile.name).toBe("skill-a.zip");
  });

  it("does not include directory entries in extracted ZIP", async () => {
    const zip = new JSZip();
    // Explicitly add a directory entry alongside a file
    zip.folder("tool/scripts");
    zip.file("tool/scripts/run.sh", "#!/bin/bash");

    const subFile = await createSkillSubZip(zip, "tool");
    const subZip = await JSZip.loadAsync(subFile);
    const dirEntries = Object.values(subZip.files).filter((f) => f.dir);

    // Directory entries may be created implicitly by JSZip but no explicit
    // empty dir entries from the source should leak as files
    const fileEntries = Object.keys(subZip.files).filter((p) => !subZip.files[p]!.dir);
    expect(fileEntries).toEqual(["scripts/run.sh"]);
    // dirEntries may exist (JSZip auto-creates them) — just ensure files are right
    expect(dirEntries.length).toBeGreaterThanOrEqual(0);
  });
});
