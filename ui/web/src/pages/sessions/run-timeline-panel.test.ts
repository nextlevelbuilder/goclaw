import { describe, expect, it } from "vitest";
import { getRunTimelineDisplay } from "./run-timeline-panel";

describe("getRunTimelineDisplay", () => {
  it("marks failed items as destructive", () => {
    const display = getRunTimelineDisplay({ item_type: "tool.result", status: "failed" });
    expect(display.labelKey).toBe("detail.timeline.failed");
    expect(display.dotClass).toContain("destructive");
  });

  it("distinguishes tool call and assistant message items", () => {
    expect(getRunTimelineDisplay({ item_type: "tool.call", status: "running" }).labelKey).toBe(
      "detail.timeline.toolCall",
    );
    expect(getRunTimelineDisplay({ item_type: "assistant.message", status: "completed" }).labelKey).toBe(
      "detail.timeline.assistant",
    );
  });
});
