import { describe, expect, it } from "vitest";
import { resolveGraphNodeDisplayColor } from "./graph-node-colors";

describe("resolveGraphNodeDisplayColor", () => {
  it("keeps the adapter-assigned color for knowledge graph entity nodes", () => {
    expect(resolveGraphNodeDisplayColor({
      entityType: "person",
      color: "#E85D24",
    }, false)).toBe("#E85D24");
  });

  it("uses theme-aware vault colors for vault document nodes", () => {
    expect(resolveGraphNodeDisplayColor({
      docType: "document",
      entityType: "document",
      color: "#8b5cf6",
    }, false)).toBe("#0891b2");
  });
});
