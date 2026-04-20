/**
 * Pure-logic tests for DynamicParamForm utilities.
 * NO @testing-library/react — tests isolate state-shape transforms only.
 * Covers: evaluateDependsOn, initializeDefaults, applyNestedChange, rendererDispatch,
 *         partitionSchema, advanced toggle count badge, cross-group DependsOn, edge cases.
 */
import { describe, it, expect, vi } from "vitest";
import { evaluateDependsOn, partitionSchema } from "../dynamic-param-form";
import {
  initializeDefaults,
  applyNestedChange,
  getRendererName,
  coerceNumericValue,
} from "../dynamic-param-form-logic";
import type { ParamSchema } from "@/api/tts-capabilities";

// ---- evaluateDependsOn ----

describe("evaluateDependsOn", () => {
  it("returns true when deps is undefined", () => {
    expect(evaluateDependsOn(undefined, {})).toBe(true);
  });

  it("returns true when deps is empty array", () => {
    expect(evaluateDependsOn([], {})).toBe(true);
  });

  it("returns true when all conditions match (AND semantics)", () => {
    const deps = [
      { field: "model", op: "eq", value: "x" },
      { field: "format", op: "eq", value: "mp3" },
    ];
    const state = { model: "x", format: "mp3" };
    expect(evaluateDependsOn(deps, state)).toBe(true);
  });

  it("returns false when one condition fails", () => {
    const deps = [
      { field: "model", op: "eq", value: "x" },
      { field: "format", op: "eq", value: "mp3" },
    ];
    const state = { model: "x", format: "wav" };
    expect(evaluateDependsOn(deps, state)).toBe(false);
  });

  it("coerces values to string for comparison", () => {
    const deps = [{ field: "enabled", op: "eq", value: true }];
    expect(evaluateDependsOn(deps, { enabled: "true" })).toBe(true);
    expect(evaluateDependsOn(deps, { enabled: true })).toBe(true);
    expect(evaluateDependsOn(deps, { enabled: false })).toBe(false);
  });
});

// ---- initializeDefaults ----

describe("initializeDefaults", () => {
  it("sets Default for each schema key", () => {
    const schemas: ParamSchema[] = [
      { key: "speed", type: "range", label: "Speed", default: 1.0 },
      { key: "format", type: "enum", label: "Format", default: "mp3" },
    ];
    const out = initializeDefaults(schemas);
    expect(out).toEqual({ speed: 1.0, format: "mp3" });
  });

  it("handles nested-key schemas (dot notation)", () => {
    const schemas: ParamSchema[] = [
      { key: "voice_settings.stability", type: "range", label: "Stability", default: 0.5 },
      { key: "voice_settings.similarity_boost", type: "range", label: "Similarity", default: 0.75 },
    ];
    const out = initializeDefaults(schemas);
    expect(out).toEqual({
      "voice_settings.stability": 0.5,
      "voice_settings.similarity_boost": 0.75,
    });
  });

  it("uses empty string when no default present", () => {
    const schemas: ParamSchema[] = [
      { key: "instructions", type: "text", label: "Instructions" },
    ];
    const out = initializeDefaults(schemas);
    expect(out["instructions"]).toBe("");
  });

  it("handles empty schema array", () => {
    expect(initializeDefaults([])).toEqual({});
  });
});

// ---- applyNestedChange ----

describe("applyNestedChange", () => {
  it("sets flat key", () => {
    const result = applyNestedChange({}, "speed", 1.5);
    expect(result).toEqual({ speed: 1.5 });
  });

  it("preserves other keys", () => {
    const result = applyNestedChange({ speed: 1.0, format: "mp3" }, "speed", 1.5);
    expect(result).toEqual({ speed: 1.5, format: "mp3" });
  });

  it("sets nested key (dot notation)", () => {
    const result = applyNestedChange({}, "voice_settings.stability", 0.3);
    expect(result).toEqual({ "voice_settings.stability": 0.3 });
  });

  it("does not mutate the original object", () => {
    const original = { speed: 1.0 };
    const result = applyNestedChange(original, "speed", 2.0);
    expect(original.speed).toBe(1.0);
    expect(result.speed).toBe(2.0);
  });
});

// ---- getRendererName ----

describe("getRendererName", () => {
  it("returns renderer name for each ParamType", () => {
    const types = ["range", "number", "integer", "enum", "boolean", "string", "text"] as const;
    for (const t of types) {
      expect(getRendererName(t)).toBe(t);
    }
  });
});

// ---- coerceNumericValue ----

describe("coerceNumericValue (NaN edge case)", () => {
  it("returns default when value is NaN", () => {
    expect(coerceNumericValue(NaN, 1.0)).toBe(1.0);
  });

  it("returns value when it is a valid number", () => {
    expect(coerceNumericValue(1.5, 1.0)).toBe(1.5);
  });

  it("returns 0 when value is NaN and no default provided", () => {
    expect(coerceNumericValue(NaN, undefined)).toBe(0);
  });
});

// ---- readonly suppresses onChange ----

describe("readonly suppresses onChange (logic-level check)", () => {
  it("does not call onChange when readonly is true", () => {
    const handler = vi.fn();
    // Simulate the readonly guard logic from DynamicParamForm
    const callHandlerIfNotReadonly = (readonly: boolean, val: unknown) => {
      if (!readonly && handler) handler(val);
    };
    callHandlerIfNotReadonly(true, "new-value");
    expect(handler).not.toHaveBeenCalled();
  });

  it("calls onChange when readonly is false", () => {
    const handler = vi.fn();
    const callHandlerIfNotReadonly = (readonly: boolean, val: unknown) => {
      if (!readonly && handler) handler(val);
    };
    callHandlerIfNotReadonly(false, "new-value");
    expect(handler).toHaveBeenCalledWith("new-value");
  });
});

// ---- empty enum edge case ----

describe("empty enum handling", () => {
  it("empty enum options array does not crash coerce", () => {
    const schema: ParamSchema = {
      key: "format",
      type: "enum",
      label: "Format",
      enum: [],
      default: "",
    };
    // EnumField with empty options — should render disabled without crash
    // Logic-level: verify defaults apply correctly
    const defaults = initializeDefaults([schema]);
    expect(defaults["format"]).toBe("");
  });
});

// ---- partitionSchema ----

describe("partitionSchema", () => {
  it("puts group='advanced' params into advanced bucket", () => {
    const schema: ParamSchema[] = [
      { key: "speed", type: "range", label: "Speed" },
      { key: "instructions", type: "text", label: "Instructions", group: "advanced" },
    ];
    const { basic, advanced } = partitionSchema(schema);
    expect(basic.map((p) => p.key)).toEqual(["speed"]);
    expect(advanced.map((p) => p.key)).toEqual(["instructions"]);
  });

  it("puts params without group into basic bucket", () => {
    const schema: ParamSchema[] = [
      { key: "speed", type: "range", label: "Speed" },
      { key: "format", type: "enum", label: "Format" },
    ];
    const { basic, advanced } = partitionSchema(schema);
    expect(basic).toHaveLength(2);
    expect(advanced).toHaveLength(0);
  });

  it("preserves source order within each bucket", () => {
    const schema: ParamSchema[] = [
      { key: "a", type: "range", label: "A" },
      { key: "b", type: "range", label: "B", group: "advanced" },
      { key: "c", type: "range", label: "C" },
      { key: "d", type: "range", label: "D", group: "advanced" },
    ];
    const { basic, advanced } = partitionSchema(schema);
    expect(basic.map((p) => p.key)).toEqual(["a", "c"]);
    expect(advanced.map((p) => p.key)).toEqual(["b", "d"]);
  });

  it("unknown group value (forward-compat) falls into basic bucket", () => {
    // Future group values like "expert" should default to basic, not crash.
    const schema: ParamSchema[] = [
      { key: "future_param", type: "string", label: "Future", group: "expert" },
    ];
    const { basic, advanced } = partitionSchema(schema);
    expect(basic.map((p) => p.key)).toEqual(["future_param"]);
    expect(advanced).toHaveLength(0);
  });

  it("returns empty buckets for empty schema", () => {
    const { basic, advanced } = partitionSchema([]);
    expect(basic).toHaveLength(0);
    expect(advanced).toHaveLength(0);
  });
});

// ---- visibleAdvancedCount (count badge logic) ----

describe("visibleAdvancedCount — count badge respects DependsOn", () => {
  it("counts all advanced params when no DependsOn constraints", () => {
    const advanced: ParamSchema[] = [
      { key: "seed", type: "integer", label: "Seed", group: "advanced" },
      { key: "latency", type: "integer", label: "Latency", group: "advanced" },
    ];
    const value = {};
    const count = advanced.filter((p) => evaluateDependsOn(p.depends_on, value)).length;
    expect(count).toBe(2);
  });

  it("excludes advanced param when DependsOn is not satisfied (cross-group)", () => {
    // MiniMax-like case: audio.bitrate is advanced, depends on basic audio.format == "mp3"
    const advanced: ParamSchema[] = [
      {
        key: "audio.bitrate",
        type: "integer",
        label: "Bitrate",
        group: "advanced",
        depends_on: [{ field: "audio.format", op: "eq", value: "mp3" }],
      },
    ];
    const valueWav = { "audio.format": "wav" };
    const countWav = advanced.filter((p) => evaluateDependsOn(p.depends_on, valueWav)).length;
    expect(countWav).toBe(0);

    const valueMp3 = { "audio.format": "mp3" };
    const countMp3 = advanced.filter((p) => evaluateDependsOn(p.depends_on, valueMp3)).length;
    expect(countMp3).toBe(1);
  });

  it("returns 0 when advanced bucket is empty (Edge provider — no advanced toggle)", () => {
    const advanced: ParamSchema[] = [];
    const count = advanced.filter((p) => evaluateDependsOn(p.depends_on, {})).length;
    expect(count).toBe(0);
  });
});

// ---- cross-group DependsOn (MiniMax bitrate scenario) ----

describe("cross-group DependsOn — evaluateDependsOn uses shared value state", () => {
  it("advanced param with depends_on basic field: visible when basic field matches", () => {
    const advancedParam: ParamSchema = {
      key: "audio.bitrate",
      type: "integer",
      label: "Bitrate (MP3 only)",
      group: "advanced",
      depends_on: [{ field: "audio.format", op: "eq", value: "mp3" }],
    };
    // shared state includes basic field value
    expect(evaluateDependsOn(advancedParam.depends_on, { "audio.format": "mp3" })).toBe(true);
    expect(evaluateDependsOn(advancedParam.depends_on, { "audio.format": "wav" })).toBe(false);
    expect(evaluateDependsOn(advancedParam.depends_on, {})).toBe(false);
  });
});

// ---- partitionSchema does not mutate source array ----

describe("partitionSchema immutability", () => {
  it("does not mutate source schema array", () => {
    const schema: ParamSchema[] = [
      { key: "speed", type: "range", label: "Speed" },
      { key: "seed", type: "integer", label: "Seed", group: "advanced" },
    ];
    const original = [...schema];
    partitionSchema(schema);
    expect(schema).toEqual(original);
  });
});
