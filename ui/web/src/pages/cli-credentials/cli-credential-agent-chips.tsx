/**
 * cli-credential-agent-chips.tsx
 * Chip row shown under each binary row in the CLI credentials table.
 *
 * Capabilities:
 * - Shows first 5 chips; overflow becomes "+N more" text (no popover needed)
 * - Backend caps the summary at 20 grants per binary; counts beyond that are
 *   truncated. Use the grants management dialog to see/edit the full set.
 * - Chip: agent name + KeyRound icon when env_set=true
 * - Tooltip with agent_key + grant_id + env_set status
 * - Capability-probe: if agent_grants_summary is absent/undefined, renders nothing
 * - Empty state: "No grants" text + Grant now link
 * - Mobile: flex-wrap, no overflow-x
 */
import { useTranslation } from "react-i18next";
import { KeyRound } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import {
  Tooltip, TooltipContent, TooltipProvider, TooltipTrigger,
} from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";
import type { AgentGrantSummary } from "@/types/cli-credential";

const MAX_VISIBLE = 5;

interface Props {
  /** Capability-probe: undefined = field absent from API (old deploy), skip rendering */
  agentGrantsSummary: AgentGrantSummary[] | undefined;
  onOpenGrants: () => void;
}

/** Row of agent chips for a binary. Renders nothing if field is absent from API response. */
export function CliCredentialAgentChips({ agentGrantsSummary, onOpenGrants }: Props) {
  const { t } = useTranslation("cli-credentials");

  // Capability-probe: if field is absent, skip entirely — no crash on rolling deploy
  if (agentGrantsSummary === undefined) return null;

  if (agentGrantsSummary.length === 0) {
    return (
      <div className="flex items-center gap-2 px-4 py-1.5 text-xs text-muted-foreground border-t border-dashed">
        <span>{t("grants.chips.none")}</span>
        <Button
          variant="link"
          size="sm"
          className="h-auto p-0 text-xs"
          onClick={onOpenGrants}
        >
          {t("grants.addGrant")}
        </Button>
      </div>
    );
  }

  const visible = agentGrantsSummary.slice(0, MAX_VISIBLE);
  const overflow = agentGrantsSummary.length - visible.length;

  return (
    <TooltipProvider>
      <div className="flex flex-wrap items-center gap-1.5 px-4 py-1.5 border-t border-dashed">
        {visible.map((grant) => (
          <Tooltip key={grant.grant_id}>
            <TooltipTrigger asChild>
              <Badge
                variant={grant.enabled ? "secondary" : "outline"}
                className="gap-1 cursor-default min-h-[1.75rem] px-2"
              >
                <span
                  className={`inline-block h-1.5 w-1.5 rounded-full shrink-0 ${
                    grant.enabled ? "bg-emerald-500" : "bg-muted-foreground"
                  }`}
                />
                <span className="truncate max-w-[120px]">{grant.name || grant.agent_key}</span>
                {grant.env_set && <KeyRound className="h-3 w-3 shrink-0 text-muted-foreground" />}
              </Badge>
            </TooltipTrigger>
            <TooltipContent side="bottom" className="text-xs max-w-[220px]">
              <div className="grid gap-0.5">
                <span className="font-mono">{grant.agent_key}</span>
                <span className="text-muted-foreground">grant: {grant.grant_id.slice(0, 8)}…</span>
                {grant.env_set && (
                  <span className="text-muted-foreground">{t("grants.envVars.title")}: custom</span>
                )}
              </div>
            </TooltipContent>
          </Tooltip>
        ))}

        {overflow > 0 && (
          <Badge variant="outline" className="cursor-pointer" onClick={onOpenGrants}>
            {t("grants.chips.countMore", { count: overflow })}
          </Badge>
        )}
      </div>
    </TooltipProvider>
  );
}
