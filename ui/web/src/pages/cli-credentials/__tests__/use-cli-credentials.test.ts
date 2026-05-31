import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { normalizeCliPreset, normalizeCliPresets } from "../hooks/use-cli-credentials";

describe("cli credential preset normalization", () => {
  it("normalizes nullable arrays from adapter-managed presets", () => {
    const preset = normalizeCliPreset({
      binary_name: "git",
      description: "Git with credential adapter",
      env_vars: null,
      deny_args: ["(?i)credential-helper"],
      deny_verbose: null,
      timeout: 300,
      tips: "Adapter handles auth automatically",
      adapter_name: "git",
    });

    expect(preset).toEqual({
      binary_name: "git",
      description: "Git with credential adapter",
      env_vars: [],
      deny_args: ["(?i)credential-helper"],
      deny_verbose: [],
      timeout: 300,
      tips: "Adapter handles auth automatically",
      adapter_name: "git",
    });
  });

  it("returns an empty preset map for missing API payloads", () => {
    expect(normalizeCliPresets(null)).toEqual({});
    expect(normalizeCliPresets(undefined)).toEqual({});
  });

  it("keeps agent credential API calls on the agent-credentials endpoint family", () => {
    const source = readFileSync(
      resolve(process.cwd(), "src/pages/cli-credentials/hooks/use-cli-agent-credentials.ts"),
      "utf8",
    );

    expect(source).toContain("/v1/cli-credentials/${binaryId}/agent-credentials");
    expect(source).toContain("/v1/cli-credentials/${binaryId}/agent-credentials/${agentId}");
    expect(source).toContain("useCliAgentCredentials");
  });
});
