import { useTranslation } from "react-i18next";
import { Trash2, Pencil } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { CLIAgentGrant } from "./hooks/use-cli-credentials";

interface Props {
  grant: CLIAgentGrant;
  agentName: string;
  isActive: boolean;
  disabled: boolean;
  onSelect: () => void;
  onRevoke: () => void;
}

/** Renders a single agent grant card in the grants list. */
export function CliCredentialGrantCard({ grant, agentName, isActive, disabled, onSelect, onRevoke }: Props) {
  const { t } = useTranslation("cli-credentials");
  const { t: tc } = useTranslation("common");

  const hasOverrides = grant.deny_args || grant.deny_verbose ||
    grant.timeout_seconds != null || grant.tips;

  return (
    <div
      className={cn(
        "rounded-md border px-3 py-2.5 cursor-pointer transition-colors",
        isActive ? "border-ring bg-accent/50 ring-1 ring-ring/30" : "bg-muted/30 hover:bg-muted/50",
      )}
      onClick={onSelect}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <span className="text-sm font-medium">{agentName}</span>
            {!grant.enabled && (
              <Badge variant="secondary" className="text-[10px] px-1.5 py-0">{tc("disabled")}</Badge>
            )}
            {isActive && <Pencil className="h-3 w-3 text-muted-foreground" />}
          </div>
          {hasOverrides ? (
            <div className="mt-1 flex flex-wrap gap-1">
              {grant.timeout_seconds != null && (
                <Badge variant="outline" className="font-mono text-[10px] px-1.5 py-0">
                  timeout: {grant.timeout_seconds}s
                </Badge>
              )}
              {grant.deny_args && (
                <Badge variant="outline" className="font-mono text-[10px] px-1.5 py-0">
                  deny: {grant.deny_args.length} rules
                </Badge>
              )}
              {grant.tips && (
                <Badge variant="outline" className="font-mono text-[10px] px-1.5 py-0 max-w-[200px] truncate">
                  tips: {grant.tips}
                </Badge>
              )}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground mt-0.5">{t("grants.usingDefaults")}</p>
          )}
        </div>
        <Button
          variant="ghost"
          size="icon"
          className="h-7 w-7 shrink-0"
          onClick={(e) => { e.stopPropagation(); onRevoke(); }}
          disabled={disabled}
        >
          <Trash2 className="h-3.5 w-3.5 text-destructive" />
        </Button>
      </div>
    </div>
  );
}
