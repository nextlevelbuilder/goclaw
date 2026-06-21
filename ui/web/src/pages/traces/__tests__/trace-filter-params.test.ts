import { describe, expect, it } from "vitest";
import { buildTraceRequestParams, getActiveTraceFilterChips } from "../trace-filter-params";

describe("trace filter params", () => {
  it("serializes advanced filters without empty values", () => {
    const params = buildTraceRequestParams({
      query: "abc-123",
      agentId: "agent-1",
      channel: "telegram",
      status: "error",
      from: "2026-06-10T01:02:03.000Z",
      to: "2026-06-11T04:05:06.000Z",
      minInputTokens: "10",
      maxInputTokens: "20",
      minOutputTokens: "30",
      maxOutputTokens: "40",
      minToolCalls: "1",
      maxToolCalls: "3",
      toolName: "web_search",
      hasToolCalls: "true",
      limit: 25,
      offset: 50,
    });

    expect(params).toEqual({
      q: "abc-123",
      agent_id: "agent-1",
      channel: "telegram",
      status: "error",
      from: "2026-06-10T01:02:03.000Z",
      to: "2026-06-11T04:05:06.000Z",
      min_input_tokens: "10",
      max_input_tokens: "20",
      min_output_tokens: "30",
      max_output_tokens: "40",
      min_tool_calls: "1",
      max_tool_calls: "3",
      tool_name: "web_search",
      has_tool_calls: "true",
      limit: "25",
      offset: "50",
    });
  });

  it("builds chips for every non-pagination filter", () => {
    const chips = getActiveTraceFilterChips({
      query: "abc",
      status: "running",
      hasToolCalls: "false",
      limit: 50,
      offset: 0,
    });
    expect(chips.map((chip) => chip.key)).toEqual(["query", "status", "hasToolCalls"]);
  });
});
