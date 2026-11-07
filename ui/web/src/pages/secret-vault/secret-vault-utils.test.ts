import { describe, expect, it } from "vitest";
import {
  VAULT_AGENT_WRITE_KEY,
  isPending,
  isVaultAgentWriteOn,
  withVaultAgentWrite,
} from "./secret-vault-utils";

describe("agent vault-write flag", () => {
  it("is on only for an explicit boolean true", () => {
    expect(isVaultAgentWriteOn({ [VAULT_AGENT_WRITE_KEY]: true })).toBe(true);
    expect(isVaultAgentWriteOn({ [VAULT_AGENT_WRITE_KEY]: "true" })).toBe(false);
    expect(isVaultAgentWriteOn({ [VAULT_AGENT_WRITE_KEY]: false })).toBe(false);
    expect(isVaultAgentWriteOn({})).toBe(false);
    expect(isVaultAgentWriteOn(null)).toBe(false);
    expect(isVaultAgentWriteOn(undefined)).toBe(false);
  });

  it("keeps every other other_config key when toggled", () => {
    const before = { allow_image_generation: false, tts_params: { voice: "a" }, description: "x" };
    const on = withVaultAgentWrite(before, true);
    expect(on).toEqual({ ...before, [VAULT_AGENT_WRITE_KEY]: true });
    expect(withVaultAgentWrite(on, false)).toEqual({ ...before, [VAULT_AGENT_WRITE_KEY]: false });
    expect(before).not.toHaveProperty(VAULT_AGENT_WRITE_KEY);
  });

  it("starts from an empty object when the agent has no other_config", () => {
    expect(withVaultAgentWrite(null, true)).toEqual({ [VAULT_AGENT_WRITE_KEY]: true });
  });
});

describe("pending entries", () => {
  it("detects the pending status", () => {
    expect(isPending({ status: "pending" })).toBe(true);
    expect(isPending({ status: "active" })).toBe(false);
    expect(isPending({})).toBe(false);
  });
});
