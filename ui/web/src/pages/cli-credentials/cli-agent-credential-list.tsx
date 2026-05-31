import { useTranslation } from "react-i18next";
import { Loader2, Pencil, Trash2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { CLIAgentCredential } from "./hooks/use-cli-agent-credentials";

interface Props {
  entries: CLIAgentCredential[];
  agentNameMap: Map<string, string>;
  deleting: string | null;
  onEdit: (entry: CLIAgentCredential) => void;
  onDelete: (entry: CLIAgentCredential) => void;
}

export function CliAgentCredentialList({ entries, agentNameMap, deleting, onEdit, onDelete }: Props) {
  const { t } = useTranslation("cli-credentials");

  const credentialLabel = (entry: CLIAgentCredential) => {
    if (entry.credential_type === "pat") return t("userCredentials.credentialTypePAT");
    if (entry.credential_type === "ssh_key") return t("userCredentials.credentialTypeSSH");
    return entry.has_secret ? "env" : t("userCredentials.noSecret");
  };

  return (
    <div className="grid gap-2">
      {entries.map((entry) => (
        <div key={entry.id} className="flex items-center justify-between rounded-md border px-3 py-2">
          <div className="min-w-0">
            <div className="flex items-center gap-2 min-w-0">
              <span className="truncate text-sm font-medium">
                {entry.name || agentNameMap.get(entry.agent_id) || entry.agent_key || entry.agent_id}
              </span>
              <Badge variant="secondary" className="shrink-0 text-xs">{credentialLabel(entry)}</Badge>
            </div>
            {entry.host_scope ? (
              <p className="truncate font-mono text-xs text-muted-foreground">{entry.host_scope}</p>
            ) : null}
          </div>
          <div className="flex shrink-0 items-center gap-1">
            <Button variant="ghost" size="icon" className="h-8 w-8" onClick={() => onEdit(entry)} title={t("agentCredentials.edit")}>
              <Pencil className="h-3.5 w-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8 text-destructive hover:text-destructive"
              onClick={() => onDelete(entry)}
              disabled={deleting === entry.agent_id}
              title={t("agentCredentials.delete")}
            >
              {deleting === entry.agent_id ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Trash2 className="h-3.5 w-3.5" />
              )}
            </Button>
          </div>
        </div>
      ))}
    </div>
  );
}
