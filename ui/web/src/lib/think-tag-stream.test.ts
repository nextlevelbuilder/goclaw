import { describe, expect, it } from "vitest";
import { appendFilteredThinkingChunk, createThinkTagStreamFilterState } from "./think-tag-stream";

describe("think tag stream filter", () => {
  it("strips late thinking tags from streamed answer text", () => {
    let state = createThinkTagStreamFilterState();

    state = appendFilteredThinkingChunk(state, "Visible prefix ");
    state = appendFilteredThinkingChunk(state, "<thinking>hidden reasoning</thinking> visible answer");

    expect(state.text).toBe("Visible prefix  visible answer");
    expect(state.thinking).toBe("hidden reasoning");
    expect(state.thinkingDelta).toBe("hidden reasoning");
  });

  it("strips thinking tags split across stream chunks", () => {
    let state = createThinkTagStreamFilterState();

    state = appendFilteredThinkingChunk(state, "Visible ");
    state = appendFilteredThinkingChunk(state, "<");
    state = appendFilteredThinkingChunk(state, "thinking>hidden reasoning");
    state = appendFilteredThinkingChunk(state, "</thinking> answer");

    expect(state.text).toBe("Visible  answer");
    expect(state.thinking).toBe("hidden reasoning");
  });

  it("streams thinking text while the closing tag is still pending", () => {
    let state = createThinkTagStreamFilterState();

    state = appendFilteredThinkingChunk(state, "Visible <thinking>hidden ");
    expect(state.text).toBe("Visible ");
    expect(state.thinking).toBe("hidden ");
    expect(state.thinkingDelta).toBe("hidden ");

    state = appendFilteredThinkingChunk(state, "reasoning</think");
    expect(state.text).toBe("Visible ");
    expect(state.thinking).toBe("hidden reasoning");
    expect(state.thinkingDelta).toBe("reasoning");

    state = appendFilteredThinkingChunk(state, "ing> answer");
    expect(state.text).toBe("Visible  answer");
    expect(state.thinking).toBe("hidden reasoning");
  });

  it("holds a partial think tag name until the tag resolves", () => {
    let state = createThinkTagStreamFilterState();

    state = appendFilteredThinkingChunk(state, "Visible ");
    state = appendFilteredThinkingChunk(state, "<think");

    expect(state.text).toBe("Visible ");

    state = appendFilteredThinkingChunk(state, "ing>hidden reasoning</thinking> answer");

    expect(state.text).toBe("Visible  answer");
    expect(state.thinking).toBe("hidden reasoning");
  });
});
