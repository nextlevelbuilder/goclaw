import { useState, useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useCliCredentialGrants } from "./hooks/use-cli-credentials";
import { CliCredentialGrantCard } from "./cli-credential-grant-card";
import { CliCredentialGrantForm } from "./cli-credential-grant-form";
import type { SecureCLIBinary, CLIAgentGrant } from "./hooks/use-cli-credentials";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  binary: SecureCLIBinary;
}

/** Dialog for managing per-agent grants on a CLI credential. */
export function CliCredentialGrantsDialog({ open, onOpenChange, binary }: Props) {
  const { t } = useTranslation("cli-credentials");
  const { t: tc } = useTranslation("common");
  const { agents } = useAgents();
  const { grants, loading, createGrant, updateGrant, deleteGrant } = useCliCredentialGrants(binary.id);

  const [agentId, setAgentId] = useState("");
  const [denyArgs, setDenyArgs] = useState("");
  const [denyVerbose, setDenyVerbose] = useState("");
  const [timeout, setTimeout] = useState("");
  const [tips, setTips] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [editingGrant, setEditingGrant] = useState<CLIAgentGrant | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const agentNameMap = useMemo(() => {
    const map = new Map<string, string>();
    for (const a of agents) map.set(a.id, a.display_name || a.agent_key);
    return map;
  }, [agents]);

  useEffect(() => {
    if (open) clearForm();
  }, [open]);

  const clearForm = () => {
    setAgentId("");
    setDenyArgs("");
    setDenyVerbose("");
    setTimeout("");
    setTips("");
    setEnabled(true);
    setEditingGrant(null);
    setError("");
  };

  const selectGrant = (grant: CLIAgentGrant) => {
    setAgentId(grant.agent_id);
    setDenyArgs(grant.deny_args?.join(", ") ?? "");
    setDenyVerbose(grant.deny_verbose?.join(", ") ?? "");
    setTimeout(grant.timeout_seconds != null ? String(grant.timeout_seconds) : "");
    setTips(grant.tips ?? "");
    setEnabled(grant.enabled);
    setEditingGrant(grant);
    setError("");
  };

  const splitComma = (v: string): string[] | null => {
    const items = v.split(",").map((s) => s.trim()).filter(Boolean);
    return items.length > 0 ? items : null;
  };

  const handleSubmit = async () => {
    if (!agentId) { setError(t("grants.agentRequired")); return; }
    setSaving(true);
    setError("");
    try {
      const input = {
        agent_id: agentId,
        deny_args: splitComma(denyArgs),
        deny_verbose: splitComma(denyVerbose),
        timeout_seconds: timeout ? parseInt(timeout, 10) : null,
        tips: tips.trim() || null,
        enabled,
      };
      if (editingGrant) {
        await updateGrant(editingGrant.id, input);
      } else {
        await createGrant(input);
      }
      clearForm();
    } catch (err) {
      setError(err instanceof Error ? err.message : tc("error"));
    } finally {
      setSaving(false);
    }
  };

  const handleRevoke = async (grant: CLIAgentGrant) => {
    setSaving(true);
    try {
      await deleteGrant(grant.id);
      if (editingGrant?.id === grant.id) clearForm();
    } catch (err) {
      setError(err instanceof Error ? err.message : tc("error"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] flex flex-col sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t("grants.title", { name: binary.binary_name })}</DialogTitle>
        </DialogHeader>

        <div className="space-y-4 -mx-4 px-4 sm:-mx-6 sm:px-6 overflow-y-auto min-h-0">
          {grants.length > 0 && (
            <div className="space-y-2">
              <Label>{t("grants.currentGrants")}</Label>
              <div className="grid gap-2">
                {grants.map((grant) => (
                  <CliCredentialGrantCard
                    key={grant.id}
                    grant={grant}
                    agentName={agentNameMap.get(grant.agent_id) || grant.agent_id}
                    isActive={editingGrant?.id === grant.id}
                    disabled={saving}
                    onSelect={() => selectGrant(grant)}
                    onRevoke={() => handleRevoke(grant)}
                  />
                ))}
              </div>
            </div>
          )}

          <CliCredentialGrantForm
            binary={binary}
            agents={agents}
            agentId={agentId}
            setAgentId={setAgentId}
            denyArgs={denyArgs}
            setDenyArgs={setDenyArgs}
            denyVerbose={denyVerbose}
            setDenyVerbose={setDenyVerbose}
            timeout={timeout}
            setTimeout={setTimeout}
            tips={tips}
            setTips={setTips}
            enabled={enabled}
            setEnabled={setEnabled}
            isEditing={editingGrant !== null}
            saving={saving}
            onSubmit={handleSubmit}
            onCancel={clearForm}
          />

          {loading && <p className="text-xs text-muted-foreground">{tc("loading")}</p>}
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>
      </DialogContent>
    </Dialog>
  );
}
