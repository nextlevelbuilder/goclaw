const NAME_RE = /^[a-z0-9][a-z0-9_-]{0,63}$/;

export function isValidSecretName(name: string): boolean {
  return NAME_RE.test(name);
}

/** Splits a comma/space separated domain list, trimming and dropping blanks and duplicates. */
export function parseDomains(raw: string): string[] {
  const seen = new Set<string>();
  for (const part of raw.split(/[\s,]+/)) {
    const d = part.trim().toLowerCase();
    if (d) seen.add(d);
  }
  return [...seen];
}

/** Logins default to asking before each use, API keys do not. */
export function defaultApproval(kind: "api_key" | "login"): boolean {
  return kind === "login";
}
