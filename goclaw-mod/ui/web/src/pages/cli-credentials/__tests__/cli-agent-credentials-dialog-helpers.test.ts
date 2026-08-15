import { describe, expect, it } from "vitest";
import { buildAgentCredentialPayload } from "../cli-agent-credentials-dialog-helpers";

describe("agent credential payload builder", () => {
  it("builds a PAT payload for git agent credentials", () => {
    const result = buildAgentCredentialPayload({
      isGit: true,
      type: "pat",
      hostScope: " github.com ",
      token: "ghp_test",
      privateKey: "",
      hasExistingSecret: false,
      envEntries: [],
      isNewEntry: true,
    });

    expect(result).toEqual({
      kind: "typed",
      payload: {
        credential_type: "pat",
        host_scope: "github.com",
        blob: { token: "ghp_test" },
      },
    });
  });

  it("keeps an existing typed secret when edit form has no replacement secret", () => {
    const result = buildAgentCredentialPayload({
      isGit: true,
      type: "ssh_key",
      hostScope: "git.example.com:2222",
      token: "",
      privateKey: "",
      hasExistingSecret: true,
      envEntries: [],
      isNewEntry: false,
    });

    expect(result).toEqual({ kind: "no_change" });
  });

  it("requires env vars for a new env credential", () => {
    const result = buildAgentCredentialPayload({
      isGit: false,
      type: "env",
      hostScope: "",
      token: "",
      privateKey: "",
      hasExistingSecret: false,
      envEntries: [],
      isNewEntry: true,
    });

    expect(result).toEqual({ kind: "error", errorKey: "env_required" });
  });
});
