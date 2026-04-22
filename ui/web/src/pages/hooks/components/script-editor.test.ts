import { describe, expect, it } from "vitest";
import { parseLineCol } from "./script-editor";

// parseLineCol extracts {line, col} from goja compile-error strings. Format
// comes from goja parser/error.go: "%s: Line %d:%d %s". Keep this test
// permissive about the prefix but strict about the "Line N:M" shape because
// that's what the dispatcher surfaces verbatim to the UI.
describe("parseLineCol", () => {
  it("parses goja format (anonymous): Line N:M message", () => {
    const got = parseLineCol("(anonymous): Line 5:12 Unexpected token");
    expect(got).toEqual({ line: 5, col: 12 });
  });

  it("parses multi-digit line + col", () => {
    const got = parseLineCol("Line 123:456 Something went wrong");
    expect(got).toEqual({ line: 123, col: 456 });
  });

  it("returns null for non-matching shape", () => {
    expect(parseLineCol("some unstructured error")).toBeNull();
    expect(parseLineCol("line 5 col 10 — wrong separator")).toBeNull();
  });

  it("returns null for empty / undefined input", () => {
    expect(parseLineCol(undefined)).toBeNull();
    expect(parseLineCol("")).toBeNull();
  });

  it("picks the first Line occurrence (multi-error case)", () => {
    const got = parseLineCol("Line 2:3 first err; Line 9:9 later");
    expect(got).toEqual({ line: 2, col: 3 });
  });
});
