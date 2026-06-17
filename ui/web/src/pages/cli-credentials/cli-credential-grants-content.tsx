import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Label } from "@/components/ui/label";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useCliCredentialGrants } from "./hooks/use-cli-credentials";
import { CliCredentialGrantCard } from "./cli-credential-grant-card";
import { CliCredentialGrantForm } from "./cli-credential-grant-form";
import {
  EMPTY_ENV_STATE, buildEnvVarsPayload, envStateFromGrant,
} from "./cli-credential-grants-dialog-helpers";
import type { GrantEnvState } from "./cli-credential-grant-env-section";
import type { SecureCLIBinary, CLIAgentGrant } from "./hooks/use-cli-credentials";

interface Props {
  binary: SecureCLIBinary;
}

export function CliCredentialGrantsContent({ binary }: Props) {
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
  const [rejectedKeys, setRejectedKeys] = useState<string[]>([]);
  const [envState, setEnvState] = useState<GrantEnvState>(EMPTY_ENV_STATE);
  const [originalEnvSet, setOriginalEnvSet] = useState(false);

  const agentNameMap = useMemo(() => {
    const map = new Map<string, string>();
    for (const a of agents) map.set(a.id, a.display_name || a.agent_key);
    return map;
  }, [agents]);

  const clearForm = () => {
    setAgentId(""); setDenyArgs(""); setDenyVerbose(""); setTimeout(""); setTips("");
    setEnabled(true); setEditingGrant(null); setError(""); setRejectedKeys([]);
    setEnvState(EMPTY_ENV_STATE); setOriginalEnvSet(false);
  };

  useEffect(() => { clearForm(); }, [binary.id]);

  const selectGrant = (grant: CLIAgentGrant) => {
    setAgentId(grant.agent_id);
    setDenyArgs(grant.deny_args?.join(", ") ?? "");
    setDenyVerbose(grant.deny_verbose?.join(", ") ?? "");
    setTimeout(grant.timeout_seconds != null ? String(grant.timeout_seconds) : "");
    setTips(grant.tips ?? "");
    setEnabled(grant.enabled);
    setEditingGrant(grant);
    setError(""); setRejectedKeys([]);
    setOriginalEnvSet(grant.env_set === true);
    setEnvState(envStateFromGrant(grant));
  };

  const splitComma = (v: string): string[] | null => {
    const items = v.split(",").map((s) => s.trim()).filter(Boolean);
    return items.length > 0 ? items : null;
  };

  const handleSubmit = async () => {
    if (!agentId) { setError(t("grants.agentRequired")); return; }
    setSaving(true); setError(""); setRejectedKeys([]);
    try {
      const envVarsPayload = buildEnvVarsPayload(envState, originalEnvSet);
      const input = {
        agent_id: agentId,
        deny_args: splitComma(denyArgs),
        deny_verbose: splitComma(denyVerbose),
        timeout_seconds: timeout ? parseInt(timeout, 10) : null,
        tips: tips.trim() || null,
        enabled,
        ...(envVarsPayload !== undefined ? { env_vars: envVarsPayload } : {}),
      };
      if (editingGrant) await updateGrant(editingGrant.id, input);
      else await createGrant(input);
      clearForm();
    } catch (err) {
      const msg = err instanceof Error ? err.message : tc("error");
      const details = (err as { details?: { rejected_keys?: string[] } }).details;
      if (details?.rejected_keys) setRejectedKeys(details.rejected_keys);
      setError(msg);
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
        agentId={agentId} setAgentId={setAgentId}
        denyArgs={denyArgs} setDenyArgs={setDenyArgs}
        denyVerbose={denyVerbose} setDenyVerbose={setDenyVerbose}
        timeout={timeout} setTimeout={setTimeout}
        tips={tips} setTips={setTips}
        enabled={enabled} setEnabled={setEnabled}
        envState={envState} setEnvState={setEnvState}
        editingGrantId={editingGrant?.id ?? null}
        initialEnvSet={editingGrant?.env_set === true}
        initialEnvKeys={editingGrant?.env_keys ?? []}
        rejectedKeys={rejectedKeys}
        isEditing={editingGrant !== null}
        saving={saving}
        onSubmit={handleSubmit}
        onCancel={clearForm}
      />
      {loading && <p className="text-xs text-muted-foreground">{tc("loading")}</p>}
      {error && <p className="text-sm text-destructive">{error}</p>}
    </div>
  );
}
