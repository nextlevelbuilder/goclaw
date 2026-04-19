/**
 * Flatten/unflatten utilities for channel config objects.
 *
 * Channel config schemas use dot-notation keys (e.g. "features.comment_reply")
 * while the database stores nested JSON ({"features": {"comment_reply": true}}).
 * These helpers bridge the two representations.
 */

/**
 * Flatten a nested object to dot-notation keys.
 * Arrays are NOT flattened — they are kept as-is.
 *
 * @example
 * flattenConfig({ features: { comment_reply: true }, page_id: "123" })
 * // → { "features.comment_reply": true, "page_id": "123" }
 */
export function flattenConfig(
  obj: Record<string, unknown>,
  prefix = "",
): Record<string, unknown> {
  const result: Record<string, unknown> = {};
  for (const key of Object.keys(obj)) {
    const fullKey = prefix ? `${prefix}.${key}` : key;
    const value = obj[key];
    if (
      value !== null &&
      typeof value === "object" &&
      !Array.isArray(value)
    ) {
      Object.assign(result, flattenConfig(value as Record<string, unknown>, fullKey));
    } else {
      result[fullKey] = value;
    }
  }
  return result;
}

/**
 * Unflatten dot-notation keys back to a nested object.
 * Non-dot keys are placed at the root level unchanged.
 *
 * @example
 * unflattenConfig({ "features.comment_reply": true, "page_id": "123" })
 * // → { features: { comment_reply: true }, page_id: "123" }
 */
export function unflattenConfig(
  flat: Record<string, unknown>,
): Record<string, unknown> {
  const result: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(flat)) {
    const parts = key.split(".");
    let current = result;
    for (let i = 0; i < parts.length - 1; i++) {
      const part = parts[i]!;
      const existing = current[part];
      if (
        existing === undefined ||
        existing === null ||
        typeof existing !== "object" ||
        Array.isArray(existing)
      ) {
        current[part] = {};
      }
      current = current[part] as Record<string, unknown>;
    }
    const last = parts[parts.length - 1]!;
    current[last] = value;
  }
  return result;
}
