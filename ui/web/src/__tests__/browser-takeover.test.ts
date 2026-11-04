import { describe, expect, it } from "vitest";
import enTakeover from "@/i18n/locales/en/browser-takeover.json";
import viTakeover from "@/i18n/locales/vi/browser-takeover.json";
import zhTakeover from "@/i18n/locales/zh/browser-takeover.json";
import {
  dropTakeover,
  enqueueTakeover,
  formatCountdown,
  isTakeoverExpired,
  isTakeoverForUser,
  parseTakeoverRequest,
  secondsLeft,
} from "@/components/chat/takeover-request-state";

describe("takeover request state", () => {
  const valid = { id: "r1", targetId: "t1", host: "login.example.com", reason: "captcha", userId: "u1", expiresAt: 5000 };

  it("parses a valid payload and rejects malformed ones", () => {
    expect(parseTakeoverRequest(valid)).toMatchObject({ id: "r1", host: "login.example.com", userId: "u1" });
    expect(parseTakeoverRequest(null)).toBeNull();
    expect(parseTakeoverRequest({ ...valid, id: "" })).toBeNull();
    expect(parseTakeoverRequest({ ...valid, host: 3 })).toBeNull();
    expect(parseTakeoverRequest({ host: "x" })).toBeNull();
  });

  it("only shows requests for the signed-in user", () => {
    const req = parseTakeoverRequest(valid)!;
    expect(isTakeoverForUser(req, "u1")).toBe(true);
    expect(isTakeoverForUser(req, "u2")).toBe(false);
    expect(isTakeoverForUser({ ...req, userId: undefined }, "u2")).toBe(true);
  });

  it("detects expiry and counts down", () => {
    const req = parseTakeoverRequest(valid)!;
    expect(isTakeoverExpired(req, 4999)).toBe(false);
    expect(isTakeoverExpired(req, 5000)).toBe(true);
    expect(isTakeoverExpired({ ...req, expiresAt: 0 }, 9e12)).toBe(false);
    expect(secondsLeft(req, 2000)).toBe(3);
    expect(secondsLeft(req, 9000)).toBe(0);
    expect(formatCountdown(125)).toBe("2:05");
  });

  it("queues without duplicates and drops by id", () => {
    const req = parseTakeoverRequest(valid)!;
    const q = enqueueTakeover(enqueueTakeover([], req), req);
    expect(q).toHaveLength(1);
    expect(dropTakeover(q, "r1")).toHaveLength(0);
    expect(dropTakeover(q, "other")).toHaveLength(1);
  });
});

describe("browser-takeover locales", () => {
  it("have the same keys in en, vi and zh", () => {
    const keys = Object.keys(enTakeover).sort();
    expect(Object.keys(viTakeover).sort()).toEqual(keys);
    expect(Object.keys(zhTakeover).sort()).toEqual(keys);
  });
});
