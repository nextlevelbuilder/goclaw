import { describe, expect, it } from "vitest";
import { formatApiCost } from "./format";

describe("formatApiCost", () => {
  it("formats API costs with two decimal places", () => {
    expect(formatApiCost(1.024)).toBe("$1.02");
    expect(formatApiCost(5.934)).toBe("$5.93");
    expect(formatApiCost(304.451178)).toBe("$304.45");
  });

  it("formats empty or zero API costs as zero dollars", () => {
    expect(formatApiCost(undefined)).toBe("$0.00");
    expect(formatApiCost(null)).toBe("$0.00");
    expect(formatApiCost(0)).toBe("$0.00");
  });
});
