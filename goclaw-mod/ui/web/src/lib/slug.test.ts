import { describe, expect, it } from "vitest";

import { isValidSlug, slugify } from "./slug";

describe("slugify", () => {
  it("does not leave a trailing dash and passes isValidSlug", () => {
    for (const input of ["hello world!", "My Agent ", "trailing---", "café résumé!", "  spaced  "]) {
      const s = slugify(input);
      expect(s.endsWith("-")).toBe(false);
      expect(isValidSlug(s)).toBe(true);
    }
    expect(slugify("hello world!")).toBe("hello-world");
  });

  it("still handles already-valid slugs and diacritics", () => {
    expect(slugify("valid-slug")).toBe("valid-slug");
    expect(slugify("abc")).toBe("abc");
    expect(slugify("Đà Nẵng")).toBe("da-nang");
  });
});
