import { afterEach, describe, expect, it, vi } from "vitest";
import { HttpClient } from "./http-client";

function createClient() {
  return new HttpClient("https://example.com", () => "", () => "");
}

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

describe("HttpClient", () => {
  it("returns undefined for 204 no-content responses", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204, statusText: "No Content" }));
    vi.stubGlobal("fetch", fetchMock as unknown as typeof fetch);

    const client = createClient();
    const result = await client.delete<void>("/v1/vault/documents/123");

    expect(result).toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith(
      "https://example.com/v1/vault/documents/123",
      expect.objectContaining({
        method: "DELETE",
        headers: expect.objectContaining({
          "Content-Type": "application/json",
        }),
      }),
    );
  });

  it("parses JSON responses from successful requests", async () => {
    const payload = { deleted: true };
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(payload), {
        status: 200,
        headers: { "content-type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock as unknown as typeof fetch);

    const client = createClient();
    const result = await client.post<{ deleted: boolean }>("/v1/test", { id: "123" });

    expect(result).toEqual(payload);
  });
});