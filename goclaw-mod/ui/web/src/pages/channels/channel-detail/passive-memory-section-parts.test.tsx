import { describe, expect, it } from "vitest";
import {
  memoryItemDebugBadgeClass,
  memoryItemDebugBadges,
  memoryItemDebugBadgeTooltipKey,
} from "./passive-memory-section-parts";
import type { ChannelMemoryExtractionItem } from "@/types/channel";

describe("memoryItemDebugBadges", () => {
  const baseItem: ChannelMemoryExtractionItem = {
    id: "item-1",
    run_id: "run-1",
    item_type: "projects",
    summary: "Project Orion uses the ExampleCo collaboration platform",
    confidence: 0.7,
    status: "pending_review",
    created_at: "2026-07-07T05:34:11Z",
  };

  it("returns topics and entities as compact debug badges", () => {
    const badges = memoryItemDebugBadges({
      ...baseItem,
      topics: ["planning", "design"],
      entities: ["Alex Example", "Project Orion"],
    });

    expect(badges).toEqual([
      { kind: "topic", label: "planning" },
      { kind: "topic", label: "design" },
      { kind: "entity", label: "Alex Example" },
      { kind: "entity", label: "Project Orion" },
    ]);
  });

  it("does not emit empty badge groups", () => {
    expect(memoryItemDebugBadges({ ...baseItem, topics: [], entities: [] })).toEqual([]);
    expect(memoryItemDebugBadges(baseItem)).toEqual([]);
  });

  it("uses distinct styling and tooltip keys for topics and entities", () => {
    expect(memoryItemDebugBadgeClass("topic")).toContain("sky");
    expect(memoryItemDebugBadgeClass("entity")).toContain("violet");
    expect(memoryItemDebugBadgeClass("topic")).not.toEqual(memoryItemDebugBadgeClass("entity"));
    expect(memoryItemDebugBadgeTooltipKey("topic")).toBe("detail.passiveMemory.debugBadge.topic");
    expect(memoryItemDebugBadgeTooltipKey("entity")).toBe("detail.passiveMemory.debugBadge.entity");
  });
});
