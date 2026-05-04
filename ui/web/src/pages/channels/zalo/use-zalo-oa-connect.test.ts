import { describe, it, expect } from "vitest";
import { extractCode } from "./use-zalo-oa-connect";

describe("extractCode", () => {
  const stashedState = "db8fa679f0d522a652c70b5f935348c1f01f7a82d576a5596d89c32960364fcb";

  it("returns raw code when input is not a URL", () => {
    const got = extractCode("iYPhiMZy16swCN-NG", stashedState);
    expect(got.code).toBe("iYPhiMZy16swCN-NG");
    expect(got.oaID).toBe("");
    expect(got.mismatchedState).toBe(false);
  });

  it("trims whitespace on raw code input", () => {
    const got = extractCode("   iYPhiMZy   ", stashedState);
    expect(got.code).toBe("iYPhiMZy");
  });

  it("extracts code AND oa_id from a real-shape Zalo callback URL", () => {
    const url = `https://example.com/zalo-callback?oa_id=4245484535895825355&code=iYPhiMZy16swCN-NGUqQVi4lOfXFoX&state=${stashedState}`;
    const got = extractCode(url, stashedState);
    expect(got.code).toBe("iYPhiMZy16swCN-NGUqQVi4lOfXFoX");
    expect(got.oaID).toBe("4245484535895825355");
    expect(got.mismatchedState).toBe(false);
  });

  it("flags mismatched state when callback state != stashed", () => {
    const url = `https://example.com/zalo-callback?code=abc&state=wrong-state`;
    const got = extractCode(url, stashedState);
    expect(got.code).toBe("abc");
    expect(got.mismatchedState).toBe(true);
  });

  it("does NOT flag mismatch when URL has no state param", () => {
    const url = `https://example.com/zalo-callback?code=abc`;
    const got = extractCode(url, stashedState);
    expect(got.code).toBe("abc");
    expect(got.mismatchedState).toBe(false);
  });

  it("returns empty code when URL has no code param", () => {
    const url = `https://example.com/zalo-callback?oa_id=123`;
    const got = extractCode(url, stashedState);
    expect(got.code).toBe("");
    expect(got.oaID).toBe("123");
  });

  it("handles http:// in addition to https://", () => {
    const url = `http://localhost:5173/zalo-callback?code=local-code&state=${stashedState}`;
    const got = extractCode(url, stashedState);
    expect(got.code).toBe("local-code");
  });

  it("handles non-URL strings gracefully", () => {
    const got = extractCode("not a url at all", stashedState);
    expect(got.code).toBe("not a url at all");
    expect(got.mismatchedState).toBe(false);
  });
});
