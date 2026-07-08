import { describe, expect, it } from "vitest";
import { formatPromptGroupLabel } from "./passive-memory-section";

describe("formatPromptGroupLabel", () => {
  it("renders child channel with parent title when available", () => {
    expect(formatPromptGroupLabel({
      history_key: "thread-1",
      group_title: "launch-thread",
      parent_history_key: "parent-1",
      parent_group_title: "product-planning",
    })).toBe("launch-thread / product-planning");
  });

  it("falls back to ids when cached titles are missing", () => {
    expect(formatPromptGroupLabel({
      history_key: "thread-1",
      parent_history_key: "parent-1",
    })).toBe("thread-1 / parent-1");
  });
});
