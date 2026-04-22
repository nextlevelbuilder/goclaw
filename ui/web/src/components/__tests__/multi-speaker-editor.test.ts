import { describe, it, expect } from "vitest";
import {
  addSpeaker,
  removeSpeaker,
  updateSpeakerVoice,
  MAX_GEMINI_SPEAKERS,
} from "../multi-speaker-editor";
import type { SpeakerVoice } from "../multi-speaker-editor";

const makeSpeakers = (...names: string[]): SpeakerVoice[] =>
  names.map((name, i) => ({ speaker: name, voiceId: `Voice${i}` }));

describe("addSpeaker", () => {
  it("increments count from 1 to 2", () => {
    const result = addSpeaker(makeSpeakers("Joe"), { speaker: "Jane", voiceId: "Puck" });
    expect(result).toHaveLength(2);
    expect(result[1]!.speaker).toBe("Jane");
  });

  it("enforces Gemini limit of 2 speakers", () => {
    const full = makeSpeakers("Joe", "Jane");
    const result = addSpeaker(full, { speaker: "Bob", voiceId: "Kore" });
    expect(result).toHaveLength(MAX_GEMINI_SPEAKERS);
    expect(result).toEqual(full); // unchanged
  });
});

describe("removeSpeaker", () => {
  it("removes speaker at index 1", () => {
    const speakers = makeSpeakers("Joe", "Jane");
    const result = removeSpeaker(speakers, 1);
    expect(result).toHaveLength(1);
    expect(result[0]!.speaker).toBe("Joe");
  });

  it("removes speaker at index 0", () => {
    const speakers = makeSpeakers("Joe", "Jane");
    const result = removeSpeaker(speakers, 0);
    expect(result).toHaveLength(1);
    expect(result[0]!.speaker).toBe("Jane");
  });
});

describe("updateSpeakerVoice", () => {
  it("updates voice for given index without mutating others", () => {
    const speakers = makeSpeakers("Joe", "Jane");
    const result = updateSpeakerVoice(speakers, 0, "NewVoice");
    expect(result[0]!.voiceId).toBe("NewVoice");
    expect(result[1]!.voiceId).toBe(speakers[1]!.voiceId); // unchanged
  });

  it("returns a new array (no mutation)", () => {
    const speakers = makeSpeakers("Joe");
    const result = updateSpeakerVoice(speakers, 0, "X");
    expect(result).not.toBe(speakers);
  });
});

describe("MAX_GEMINI_SPEAKERS constant", () => {
  it("is 2", () => {
    expect(MAX_GEMINI_SPEAKERS).toBe(2);
  });
});

describe("readonly guard", () => {
  it("addSpeaker returns unchanged array when already at limit (readonly equivalent)", () => {
    const full = makeSpeakers("Joe", "Jane");
    expect(addSpeaker(full, { speaker: "Bob", voiceId: "Kore" })).toEqual(full);
  });
});
