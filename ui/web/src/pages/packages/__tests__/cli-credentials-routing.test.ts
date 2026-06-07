import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

function source(path: string): string {
  return readFileSync(resolve(process.cwd(), path), "utf8");
}

describe("CLI Credentials package routing", () => {
  it("keeps CLI Credentials inside Packages and out of the left sidebar", () => {
    const sidebar = source("src/components/layout/sidebar.tsx");
    const packagesPage = source("src/pages/packages/packages-page.tsx");

    expect(sidebar).not.toContain("ROUTES.CLI_CREDENTIALS");
    expect(sidebar).not.toContain("nav.cliCredentials");
    expect(packagesPage).toContain('"cli-credentials"');
    expect(packagesPage).toContain("CliCredentialsTab");
  });

  it("keeps the legacy /cli-credentials route as a redirect to the Packages tab", () => {
    const routes = source("src/routes.tsx");

    expect(routes).toContain("ROUTES.CLI_CREDENTIALS");
    expect(routes).toContain("/packages?tab=cli-credentials");
  });
});
