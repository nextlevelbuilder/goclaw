/**
 * Unit tests for tools-preview-section.
 *
 * NOTE: @testing-library/react is not installed in this project — tests
 * cover module contracts rather than DOM rendering (mirrors
 * voice-picker.test.tsx / stt-provider-form.test.tsx pattern).
 */
import { describe, it, expect } from "vitest";
import { ToolsPreviewSection, type ToolDefinition } from "./tools-preview-section";

describe("ToolsPreviewSection", () => {
  it("is defined", () => {
    expect(ToolsPreviewSection).toBeDefined();
  });

  it("accepts a well-formed ToolDefinition list", () => {
    const tools: ToolDefinition[] = [
      {
        type: "function",
        function: {
          name: "read_file",
          description: "Reads a file from disk",
          parameters: { type: "object", properties: { path: { type: "string" } } },
        },
      },
      {
        type: "function",
        function: {
          name: "no_params_tool",
          description: "A tool without parameters",
          parameters: {},
        },
      },
    ];
    expect(tools).toHaveLength(2);
    expect(tools[0]?.function?.name).toBe("read_file");
  });

  it("supports an empty tools array", () => {
    const tools: ToolDefinition[] = [];
    expect(tools).toHaveLength(0);
  });
});
