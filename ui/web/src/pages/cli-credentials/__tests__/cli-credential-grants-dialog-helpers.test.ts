import { describe, expect, it } from "vitest";
import {
  buildEnvVarsPayload,
  EMPTY_ENV_STATE,
  envStateFromGrant,
} from "../cli-credential-grants-dialog-helpers";
import type { CLIAgentGrant } from "../hooks/use-cli-credentials";

describe("cli credential grant env helpers", () => {
  it("omits env_vars when existing masked values are not revealed", () => {
    const payload = buildEnvVarsPayload(
      { overrideEnabled: true, entries: [{ key: "TOKEN", value: "", kind: "sensitive", masked: true }] },
      true,
    );
    expect(payload).toBeUndefined();
  });

  it("serializes only visible env entries", () => {
    const payload = buildEnvVarsPayload(
      {
        overrideEnabled: true,
        entries: [
          { key: " CLI_ENV ", value: "agent-value", kind: "value", masked: false },
          { key: "", value: "ignored", kind: "sensitive", masked: false },
          { key: "MASKED", value: "", kind: "sensitive", masked: true },
        ],
      },
      false,
    );
    expect(payload).toEqual({ CLI_ENV: { kind: "value", value: "agent-value" } });
  });

  it("clears existing env override when override is disabled", () => {
    expect(buildEnvVarsPayload(EMPTY_ENV_STATE, true)).toBeNull();
    expect(buildEnvVarsPayload(EMPTY_ENV_STATE, false)).toBeUndefined();
  });

  it("derives masked state from grant env metadata without values", () => {
    const state = envStateFromGrant({
      env_set: true,
      env_keys: ["API_KEY", "TOKEN"],
    } as unknown as CLIAgentGrant);

    expect(state).toEqual({
      overrideEnabled: true,
      entries: [
        { key: "API_KEY", value: "", kind: "sensitive", masked: true },
        { key: "TOKEN", value: "", kind: "sensitive", masked: true },
      ],
    });
  });

  it("derives visible value entries from sanitized grant env metadata", () => {
    const state = envStateFromGrant({
      env_set: true,
      env: {
        PUBLIC_BASE_URL: { kind: "value", value: "https://goclaw.sh", masked: false },
        TOKEN: { kind: "sensitive", value: null, masked: true },
      },
    } as unknown as CLIAgentGrant);

    expect(state).toEqual({
      overrideEnabled: true,
      entries: [
        { key: "PUBLIC_BASE_URL", value: "https://goclaw.sh", kind: "value", masked: false },
        { key: "TOKEN", value: "", kind: "sensitive", masked: true },
      ],
    });
  });
});
