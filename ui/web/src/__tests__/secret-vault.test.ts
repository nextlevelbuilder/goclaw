import { describe, expect, it } from "vitest";
import enSecretVault from "@/i18n/locales/en/secret-vault.json";
import viSecretVault from "@/i18n/locales/vi/secret-vault.json";
import zhSecretVault from "@/i18n/locales/zh/secret-vault.json";
import {
  defaultApproval,
  isValidSecretName,
  parseDomains,
} from "@/pages/secret-vault/secret-vault-utils";
import {
  dropRequest,
  enqueueRequest,
  isExpired,
  isForUser,
  parseSecretRequest,
} from "@/components/chat/secret-request-state";

function flatten(obj: Record<string, unknown>, prefix = ""): string[] {
  return Object.entries(obj).flatMap(([k, v]) =>
    v && typeof v === "object" ? flatten(v as Record<string, unknown>, `${prefix}${k}.`) : [`${prefix}${k}`],
  );
}

describe("secret-vault utils", () => {
  it("validates secret names", () => {
    expect(isValidSecretName("github_token")).toBe(true);
    expect(isValidSecretName("a")).toBe(true);
    expect(isValidSecretName("Bad Name")).toBe(false);
    expect(isValidSecretName("-lead")).toBe(false);
    expect(isValidSecretName("")).toBe(false);
    expect(isValidSecretName("x".repeat(65))).toBe(false);
  });

  it("parses domains, trimming, lowercasing and de-duplicating", () => {
    expect(parseDomains(" API.GitHub.com, github.com  github.com\n")).toEqual(["api.github.com", "github.com"]);
    expect(parseDomains("")).toEqual([]);
  });

  it("defaults approval on for logins only", () => {
    expect(defaultApproval("login")).toBe(true);
    expect(defaultApproval("api_key")).toBe(false);
  });
});

describe("secret request state", () => {
  const base = { id: "r1", kind: "login", name: "site", expiresAt: 0 };

  it("rejects malformed payloads", () => {
    expect(parseSecretRequest(null)).toBeNull();
    expect(parseSecretRequest("x")).toBeNull();
    expect(parseSecretRequest({ ...base, id: "" })).toBeNull();
    expect(parseSecretRequest({ ...base, kind: "other" })).toBeNull();
    expect(parseSecretRequest({ ...base, name: 5 })).toBeNull();
  });

  it("keeps only the known metadata fields", () => {
    const req = parseSecretRequest({ ...base, domains: ["a.com", 3], secret: "must-not-survive", extra: 1 });
    expect(req).not.toBeNull();
    expect(req?.domains).toEqual(["a.com"]);
    expect(JSON.stringify(req)).not.toContain("must-not-survive");
  });

  it("filters by user and expiry", () => {
    const req = parseSecretRequest({ ...base, userId: "u1", expiresAt: 1000 })!;
    expect(isForUser(req, "u1")).toBe(true);
    expect(isForUser(req, "u2")).toBe(false);
    expect(isForUser({ ...req, userId: undefined }, "u2")).toBe(true);
    expect(isExpired(req, 999)).toBe(false);
    expect(isExpired(req, 1000)).toBe(true);
    expect(isExpired({ ...req, expiresAt: 0 }, 99999)).toBe(false);
  });

  it("queues without duplicates and drops by id", () => {
    const a = parseSecretRequest(base)!;
    const b = parseSecretRequest({ ...base, id: "r2" })!;
    let q = enqueueRequest([], a);
    q = enqueueRequest(q, a);
    q = enqueueRequest(q, b);
    expect(q.map((r) => r.id)).toEqual(["r1", "r2"]);
    expect(dropRequest(q, "r1").map((r) => r.id)).toEqual(["r2"]);
  });
});

describe("secret-vault locale parity", () => {
  const en = flatten(enSecretVault).sort();
  it("vi has the same keys as en", () => {
    expect(flatten(viSecretVault).sort()).toEqual(en);
  });
  it("zh has the same keys as en", () => {
    expect(flatten(zhSecretVault).sort()).toEqual(en);
  });
});
