// Keep release/beta identity visible while hiding build slugs and git metadata.
// "v3.12.0-beta.13-skills-ux-99f30ecc" -> "v3.12.0.beta.13"
// "v2.5.1-3-g4fd653c1" -> "v2.5.1", "dev" -> "dev"
export function cleanVersion(v: string): string {
  const match = v.match(/^(v?\d+\.\d+\.\d+)(?:-((?:beta|alpha|rc)\.?\d*))?/i);
  if (!match) return v;

  const base = match[1]!;
  const prerelease = match[2];
  if (!prerelease) return base;
  const normalizedPrerelease = prerelease.replace(/^(beta|alpha|rc)(\d+)$/i, "$1.$2");
  return `${base}.${normalizedPrerelease}`;
}
