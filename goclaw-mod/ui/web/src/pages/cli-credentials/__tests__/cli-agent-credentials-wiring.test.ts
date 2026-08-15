import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

function source(path: string): string {
  return readFileSync(resolve(process.cwd(), path), "utf8");
}

describe("CLI agent credential UI wiring", () => {
  it("exposes one Agent Access action before advanced user overrides", () => {
    const table = source("src/pages/cli-credentials/cli-credentials-table.tsx");

    expect(table).toContain("onAgentAccess");
    expect(table).toContain("agentAccess.title");
    expect(table).not.toContain("onAgentCreds");
    expect(table.indexOf("onAgentAccess(item")).toBeLessThan(table.indexOf("onUserCreds(item)"));
  });

  it("mounts a single Agent Access dialog from the panel", () => {
    const panel = source("src/pages/cli-credentials/cli-credentials-panel.tsx");

    expect(panel).toContain("cli-agent-access-dialog");
    expect(panel).toContain("CLIAgentAccessDialog");
    expect(panel).toContain("agentAccessTarget");
    expect(panel).not.toContain("agentCredsTarget");
    expect(panel).not.toContain("grantsTarget");
  });
});
