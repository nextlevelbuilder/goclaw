/**
 * Pure logic helpers for DynamicParamForm.
 * No React, no DOM — safe to import in unit tests without jsdom.
 *
 * Exported functions:
 *   initializeDefaults  — build initial value map from schemas
 *   applyNestedChange   — immutable update of value map by key
 *   getRendererName     — returns ParamType string (for dispatch dispatch map testing)
 *   coerceNumericValue  — NaN → default; used by number/integer/range fields
 */
import type { ParamSchema, ParamType } from "@/api/tts-capabilities";

export type ParamValueMap = Record<string, string | number | boolean>;

/**
 * Builds an initial value object from an ordered list of ParamSchema.
 * Each key maps to schema.default (or "" when default is absent).
 * Keys are stored flat (dot-notation preserved as-is) — the wire format
 * matches what the backend reads from opts.Params.
 */
export function initializeDefaults(schemas: ParamSchema[]): ParamValueMap {
  const out: ParamValueMap = {};
  for (const s of schemas) {
    out[s.key] = s.default !== undefined ? (s.default as string | number | boolean) : "";
  }
  return out;
}

/**
 * Returns a new map with key set to val — immutable (does not mutate src).
 * Key may be a dot-notation string (e.g. "voice_settings.stability"); it is
 * stored flat as-is to match the backend wire format.
 */
export function applyNestedChange(
  src: ParamValueMap,
  key: string,
  val: string | number | boolean,
): ParamValueMap {
  return { ...src, [key]: val };
}

/**
 * Returns the renderer name for a ParamType (identity function).
 * Exists so tests can assert the dispatch table without importing React.
 */
export function getRendererName(type: ParamType): ParamType {
  return type;
}

/**
 * Coerces a potentially-NaN numeric input to a safe value.
 * Used by RangeField / NumberField / IntegerField onChange handlers.
 */
export function coerceNumericValue(
  val: number,
  defaultVal: number | undefined,
): number {
  if (Number.isNaN(val)) return defaultVal ?? 0;
  return val;
}
