import { describe, expect, it } from "vitest";
import { summarizeSkillStatuses } from "./skill-upload-summary";

describe("skill upload summary", () => {
  it("separates upload result states", () => {
    expect(summarizeSkillStatuses([
      { status: "valid" },
      { status: "success" },
      { status: "warning" },
      { status: "unchanged" },
      { status: "invalid" },
      { status: "error" },
      { status: "uploading" },
    ])).toEqual({
      total: 7,
      valid: 1,
      uploaded: 1,
      warnings: 1,
      unchanged: 1,
      failed: 1,
      invalid: 1,
      inProgress: 1,
    });
  });
});
