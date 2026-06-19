import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Loader2, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { DialogFooter } from "@/components/ui/dialog";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { toast } from "@/stores/use-toast-store";
import { type ManualEnvEntry } from "./cli-credential-env-vars-section";
import { CliAgentCredentialForm } from "./cli-agent-credential-form";
import { CliAgentCredentialList } from "./cli-agent-credential-list";
import {
  buildAgentCredentialPayload,
  entriesFromEnv,
} from "./cli-agent-credentials-dialog-helpers";
import { type GitCredentialType } from "./cli-credential-git-fields";
import { useCliAgentCredentials, type CLIAgentCredential } from "./hooks/use-cli-agent-credentials";
import type { SecureCLIBinary } from "./hooks/use-cli-credentials";

interface Props {
  binary: SecureCLIBinary;
  onClose: () => void;
}

type ViewState = "list" | "form";

export function CLIAgentCredentialsContent({ binary, onClose }: Props) {
  const { t } = useTranslation("cli-credentials");
  const { t: tc } = useTranslation("common");
  const { agents } = useAgents();
  const { agentCredentials, loading, getCredential, setCredential, deleteCredential } = useCliAgentCredentials(binary.id);
  const isGit = binary.adapter_name === "git";

  const [view, setView] = useState<ViewState>("list");
  const [editEntry, setEditEntry] = useState<CLIAgentCredential | null>(null);
  const [agentId, setAgentId] = useState("");
  const [envEntries, setEnvEntries] = useState<ManualEnvEntry[]>([]);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [gitType, setGitType] = useState<GitCredentialType>("pat");
  const [gitHostScope, setGitHostScope] = useState("github.com");
  const [gitToken, setGitToken] = useState("");
  const [gitPrivateKey, setGitPrivateKey] = useState("");
  const [gitErrorKey, setGitErrorKey] = useState<string | undefined>();
  const [gitHasExistingSecret, setGitHasExistingSecret] = useState(false);

  const agentNameMap = useMemo(() => {
    const map = new Map<string, string>();
    for (const a of agents) map.set(a.id, a.display_name || a.agent_key);
    return map;
  }, [agents]);

  const clearForm = () => {
    setEditEntry(null);
    setAgentId("");
    setEnvEntries([]);
    setGitType(isGit ? "pat" : "env");
    setGitHostScope(isGit ? "github.com" : "");
    setGitToken("");
    setGitPrivateKey("");
    setGitErrorKey(undefined);
    setGitHasExistingSecret(false);
  };

  useEffect(() => {
    setView("list");
    clearForm();
  }, [binary.id]);

  const openAdd = () => {
    clearForm();
    setView("form");
  };

  const openEdit = async (entry: CLIAgentCredential) => {
    setEditEntry(entry);
    setAgentId(entry.agent_id);
    setGitErrorKey(undefined);
    setView("form");
    try {
      const res = await getCredential(entry.agent_id);
      setEnvEntries(entriesFromEnv(res.env));
      if (isGit) {
        const nextType = (res.credential_type ?? "env") as GitCredentialType;
        setGitType(nextType === "pat" || nextType === "ssh_key" ? nextType : "env");
        setGitHostScope(res.host_scope ?? "github.com");
        setGitHasExistingSecret(!!res.has_secret);
        setGitToken("");
        setGitPrivateKey("");
      }
    } catch {
      setEnvEntries([]);
    }
  };

  const handleSave = async () => {
    if (!agentId) return;
    setGitErrorKey(undefined);
    const result = buildAgentCredentialPayload({
      isGit,
      type: gitType,
      hostScope: gitHostScope,
      token: gitToken,
      privateKey: gitPrivateKey,
      hasExistingSecret: gitHasExistingSecret,
      envEntries,
      isNewEntry: editEntry === null,
    });
    if (result.kind === "error") {
      if (result.errorKey === "env_required") toast.error(t("agentCredentials.envRequired"));
      else setGitErrorKey(result.errorKey);
      return;
    }
    if (result.kind === "no_change") {
      toast.success(t("agentCredentials.saved"));
      setView("list");
      return;
    }
    setSaving(true);
    try {
      await setCredential(agentId, result.payload);
      clearForm();
      setView("list");
    } catch (err) {
      const code = (err as { code?: string })?.code;
      if (code?.startsWith("git.cred_")) setGitErrorKey(code);
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (entry: CLIAgentCredential) => {
    setDeleting(entry.agent_id);
    try {
      await deleteCredential(entry.agent_id);
    } finally {
      setDeleting(null);
    }
  };

  return (
    <>
      <div className="space-y-4 -mx-4 px-4 sm:-mx-6 sm:px-6 overflow-y-auto min-h-0">
        {view === "list" ? (
          <>
            <p className="rounded-md border bg-muted/40 px-3 py-2 text-xs text-muted-foreground">{t("agentCredentials.securityHint")}</p>
            {loading ? <p className="text-xs text-muted-foreground">{tc("loading")}</p> : null}
            {agentCredentials.length === 0 ? <p className="py-6 text-center text-sm text-muted-foreground">{t("agentCredentials.empty")}</p> : null}
            <CliAgentCredentialList
              entries={agentCredentials}
              agentNameMap={agentNameMap}
              deleting={deleting}
              onEdit={openEdit}
              onDelete={handleDelete}
            />
          </>
        ) : (
          <CliAgentCredentialForm
            binary={binary}
            agents={agents}
            agentId={agentId}
            setAgentId={setAgentId}
            editing={editEntry !== null}
            envEntries={envEntries}
            setEnvEntries={setEnvEntries}
            gitType={gitType}
            setGitType={setGitType}
            gitHostScope={gitHostScope}
            setGitHostScope={setGitHostScope}
            gitToken={gitToken}
            setGitToken={setGitToken}
            gitPrivateKey={gitPrivateKey}
            setGitPrivateKey={setGitPrivateKey}
            gitErrorKey={gitErrorKey}
            gitHasExistingSecret={gitHasExistingSecret}
          />
        )}
      </div>
      <DialogFooter>
        {view === "list" ? (
          <>
            <Button variant="outline" onClick={onClose}>{tc("close")}</Button>
            <Button onClick={openAdd} className="gap-1"><Plus className="h-3.5 w-3.5" />{t("agentCredentials.add")}</Button>
          </>
        ) : (
          <>
            <Button variant="outline" onClick={() => setView("list")} disabled={saving}>{t("agentCredentials.back")}</Button>
            <Button onClick={handleSave} disabled={saving || !agentId}>{saving ? <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" /> : null}{t("agentCredentials.save")}</Button>
          </>
        )}
      </DialogFooter>
    </>
  );
}
