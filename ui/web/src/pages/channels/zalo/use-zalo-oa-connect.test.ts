import { describe, expect, it } from "vitest";
import { extractCode } from "./use-zalo-oa-connect";

describe("extractCode", () => {
  it("extracts code, OA, and state from the full callback URL", () => {
    expect(extractCode("https://gateway.example/oauth/zalo/callback?code=auth-code&oa_id=12345&state=state-token")).toEqual({
      code: "auth-code",
      oaID: "12345",
      state: "state-token",
    });
  });

  it("rejects raw authorization codes that cannot be bound to state", () => {
    expect(extractCode("auth-code")).toEqual({ code: "", oaID: "", state: "" });
  });
});
