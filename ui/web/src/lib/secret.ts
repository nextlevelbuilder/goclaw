/** Returns true if the value is a masked secret (contains "***"). */
export function isSecret(val: unknown): boolean {
  return typeof val === "string" && val.includes("***");
}
