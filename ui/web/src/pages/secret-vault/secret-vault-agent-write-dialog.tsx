import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useVaultAgentWrite } from "./hooks/use-secret-vault";
import { isVaultAgentWriteOn } from "./secret-vault-utils";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/** Per-agent opt-in for saving secrets into the vault (other_config.vault_agent_write). */
export function SecretVaultAgentWriteDialog({ open, onOpenChange }: Props) {
  const { t } = useTranslation("secret-vault");
  const { t: tc } = useTranslation("common");
  const { agents, loading } = useAgents();
  const { setAgentWrite } = useVaultAgentWrite();
  const [busyId, setBusyId] = useState<string | null>(null);

  const handleToggle = async (agentId: string, on: boolean) => {
    setBusyId(agentId);
    try {
      await setAgentWrite(agentId, on);
    } catch {
      // The hook already showed an error toast; the switch keeps the stored value.
    } finally {
      setBusyId(null);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("agentWrite.title")}</DialogTitle>
          <DialogDescription>{t("agentWrite.description")}</DialogDescription>
        </DialogHeader>

        <div className="max-h-80 space-y-1 overflow-y-auto overscroll-contain">
          {agents.length === 0 && !loading && (
            <p className="text-sm text-muted-foreground">{t("agentWrite.empty")}</p>
          )}
          {agents.map((a) => {
            const id = `sv-agent-write-${a.id}`;
            return (
              <div key={a.id} className="flex items-center justify-between gap-3 rounded-md px-2 py-2 hover:bg-muted/50">
                <label htmlFor={id} className="min-w-0 cursor-pointer">
                  <div className="truncate text-sm">{a.display_name || a.agent_key}</div>
                  <div className="truncate font-mono text-xs text-muted-foreground">{a.agent_key}</div>
                </label>
                <Switch
                  id={id}
                  aria-label={t("agentWrite.toggle")}
                  title={t("agentWrite.toggle")}
                  checked={isVaultAgentWriteOn(a.other_config)}
                  disabled={busyId !== null}
                  onCheckedChange={(on) => void handleToggle(a.id, on)}
                />
              </div>
            );
          })}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {tc("close")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
