import { CliCredentialsPanel } from "@/pages/cli-credentials/cli-credentials-panel";

// TODO(phase-8): Row-level agent_grants_summary chips will render here
// inside the CliCredentialsPanel table rows once Phase 8 is implemented.

/** CLI Credentials tab body — mounts the shared panel extracted from cli-credentials-page. */
export function CliCredentialsTab() {
  return <CliCredentialsPanel />;
}
