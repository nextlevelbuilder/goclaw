import { describe, it, expect } from "vitest";
import {
  parseCapabilitiesResponse,
  buildCapabilitiesQueryOptions,
  type ProviderCapabilities,
} from "../tts-capabilities";

// ---- parseCapabilitiesResponse ----

describe("parseCapabilitiesResponse", () => {
  it("parses a valid response into typed array", () => {
    const json = {
      providers: [
        {
          provider: "openai",
          display_name: "OpenAI TTS",
          requires_api_key: true,
          models: ["tts-1", "tts-1-hd"],
          voices: [],
          params: null,
          custom_features: null,
        },
      ],
    };
    const result = parseCapabilitiesResponse(json);
    expect(result).toHaveLength(1);
    expect(result[0]!.provider).toBe("openai");
    expect(result[0]!.display_name).toBe("OpenAI TTS");
    expect(result[0]!.requires_api_key).toBe(true);
    expect(result[0]!.models).toEqual(["tts-1", "tts-1-hd"]);
  });

  it("returns empty array when providers field is empty", () => {
    const result = parseCapabilitiesResponse({ providers: [] });
    expect(result).toEqual([]);
  });

  it("throws on malformed input — missing providers key", () => {
    expect(() => parseCapabilitiesResponse({})).toThrow();
  });

  it("throws on null input", () => {
    expect(() => parseCapabilitiesResponse(null)).toThrow();
  });

  it("throws on non-array providers", () => {
    expect(() => parseCapabilitiesResponse({ providers: "bad" })).toThrow();
  });
});

// ---- buildCapabilitiesQueryOptions ----

describe("buildCapabilitiesQueryOptions", () => {
  it("has cache key [tts, capabilities]", () => {
    const opts = buildCapabilitiesQueryOptions(() => Promise.resolve({ providers: [] as ProviderCapabilities[] }));
    expect(opts.queryKey).toEqual(["tts", "capabilities"]);
  });

  it("has staleTime of 5 minutes (300000 ms)", () => {
    const opts = buildCapabilitiesQueryOptions(() => Promise.resolve({ providers: [] as ProviderCapabilities[] }));
    expect(opts.staleTime).toBe(5 * 60_000);
  });

  it("queryFn resolves to parsed providers on success", async () => {
    const fetcher = () =>
      Promise.resolve({
        providers: [
          {
            provider: "edge",
            display_name: "Edge TTS",
            requires_api_key: false,
            models: [],
            voices: [],
            params: null,
            custom_features: null,
          } satisfies ProviderCapabilities,
        ],
      });
    const opts = buildCapabilitiesQueryOptions(fetcher);
    // queryFn signature accepts a QueryFunctionContext; pass a minimal stub.
    const result = await opts.queryFn({ signal: new AbortController().signal } as never);
    expect(result).toHaveLength(1);
    expect(result[0]!.provider).toBe("edge");
  });

  it("error fallback returns empty providers array on fetch failure", async () => {
    const fetcher = () => Promise.reject(new Error("network error"));
    const opts = buildCapabilitiesQueryOptions(fetcher);
    // The queryFn should NOT throw — it catches errors and returns []
    const result = await opts.queryFn({ signal: new AbortController().signal } as never);
    expect(result).toEqual([]);
  });
});
