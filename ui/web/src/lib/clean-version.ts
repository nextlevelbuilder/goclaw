// Strip git build metadata from version string.
// "v2.5.1-3-g4fd653c1" → "v2.5.1", "dev" → "dev"
export function cleanVersion(v: string): string {
  const match = v.match(/^(v?\d+\.\d+\.\d+)/);
  return match?.[1] ?? v;
}
