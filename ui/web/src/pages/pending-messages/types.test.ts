import { describe, expect, it } from "vitest";
import { formatPendingGroupLabel } from "./types";

describe("formatPendingGroupLabel", () => {
  it("qualifies a child group with its resolved parent title", () => {
    expect(formatPendingGroupLabel({
      history_key: "thread-1",
      group_title: "launch-thread",
      parent_history_key: "parent-1",
      parent_group_title: "product-planning",
    })).toBe("launch-thread / product-planning");
  });

  it("falls back to stable IDs when titles are unavailable", () => {
    expect(formatPendingGroupLabel({
      history_key: "thread-1",
      parent_history_key: "parent-1",
    })).toBe("thread-1 / parent-1");
  });
});
