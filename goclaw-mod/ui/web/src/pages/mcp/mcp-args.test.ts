import { describe, expect, it } from "vitest";
import { formatShellArgs, parseShellArgs } from "./mcp-args";

describe("MCP argv formatting", () => {
  it("preserves comma-separated values as one argument", () => {
    const input = "-t preset.default,preset.base.batch,task.v2.task.list";

    expect(parseShellArgs(input)).toEqual([
      "-t",
      "preset.default,preset.base.batch,task.v2.task.list",
    ]);
  });

  it("renders saved args with spaces between argv entries, not commas", () => {
    const args = [
      "/path/to/cli.js",
      "mcp",
      "-t",
      "preset.default,preset.base.batch,task.v2.task.list",
    ];

    expect(formatShellArgs(args)).toBe(
      "/path/to/cli.js mcp -t preset.default,preset.base.batch,task.v2.task.list",
    );
  });

  it("round trips quoted arguments containing spaces", () => {
    const args = ["--flag", "value with spaces", "--json", "{\"ok\":true}", ""];

    expect(parseShellArgs(formatShellArgs(args))).toEqual(args);
  });
});
