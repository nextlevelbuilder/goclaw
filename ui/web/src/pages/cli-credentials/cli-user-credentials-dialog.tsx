import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { KeyRound, Loader2, Plus, Trash2, Pencil } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { UserPickerCombobox } from "@/components/shared/user-picker-combobox";
import { toast } from "@/stores/use-toast-store";
import { useHttp } from "@/hooks/use-ws";
import i18next from "i18next";
import { CliCredentialEnvVarsSection, type ManualEnvEntry } from "./cli-credential-env-vars-section";
import { CliCredentialGitFields, type GitCredentialType } from "./cli-credential-git-fields";
import type { SecureCLIBinary } from "./hooks/use-cli-credentials";
import type { CLIEnvEntryResponse, CLIEnvPayload } from "@/types/cli-credential";

interface UserCredEntry {
  id: string;
  binary_id: string;
  user_id: string;
  has_env: boolean;
  /** Env variable names (no values) for display */
  env_keys?: string[];
  /** Phase 5: typed-credential adapter routing. */
  credential_type?: string | null;
  host_scope?: string | null;
  created_at: string;
  updated_at: string;
}

interface CLIUserCredentialsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  binary: SecureCLIBinary;
}

type ViewState = "list" | "form";

function entriesFromEnv(env: Record<string, CLIEnvEntryResponse> | null | undefined): ManualEnvEntry[] {
  if (!env || Object.keys(env).length === 0) return [];
  return Object.entries(env).map(([key, entry]) => ({
    key,
    value: entry.value ?? "",
    kind: entry.kind ?? "sensitive",
  }));
}

function envPayloadFromEntries(entries: ManualEnvEntry[]): CLIEnvPayload {
  const env: CLIEnvPayload = {};
  for (const entry of entries) {
    const key = entry.key.trim();
    if (key) env[key] = { kind: entry.kind, value: entry.value };
  }
  return env;
}

export function CLIUserCredentialsDialog({ open, onOpenChange, binary }: CLIUserCredentialsDialogProps) {
  const { t } = useTranslation("cli-credentials");
  const http = useHttp();

  const [view, setView] = useState<ViewState>("list");
  const [entries, setEntries] = useState<UserCredEntry[]>([]);
  const [loadingList, setLoadingList] = useState(false);

  // Form state
  const [editEntry, setEditEntry] = useState<UserCredEntry | null>(null);
  const [userId, setUserId] = useState("");
  // Separate search text from selected value (onChange fires on every keystroke)
  const [userSearchText, setUserSearchText] = useState("");
  const [envEntries, setEnvEntries] = useState<ManualEnvEntry[]>([]);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeletingId] = useState<string | null>(null);

  // Phase 5 — git typed credential form state. Always declared, only
  // exercised when binary.adapter_name === "git".
  const isGit = binary.adapter_name === "git";
  const [gitType, setGitType] = useState<GitCredentialType>("pat");
  const [gitHostScope, setGitHostScope] = useState("");
  const [gitToken, setGitToken] = useState("");
  const [gitPrivateKey, setGitPrivateKey] = useState("");
  const [gitErrorKey, setGitErrorKey] = useState<string | undefined>(undefined);
  const [gitHasExistingSecret, setGitHasExistingSecret] = useState(false);

  // User picker for the form

  const loadList = useCallback(async () => {
    setLoadingList(true);
    try {
      const res = await http.get<{ user_credentials: UserCredEntry[] }>(
        `/v1/cli-credentials/${binary.id}/user-credentials`,
      );
      setEntries(res.user_credentials ?? []);
    } catch {
      // silently ignore
    } finally {
      setLoadingList(false);
    }
  }, [http, binary.id]);

  useEffect(() => {
    if (!open) return;
    setView("list");
    setEditEntry(null);
    setUserId("");
    setUserSearchText("");
    setEnvEntries([]);
    setGitType(isGit ? "pat" : "env");
    setGitHostScope("");
    setGitToken("");
    setGitPrivateKey("");
    setGitErrorKey(undefined);
    setGitHasExistingSecret(false);
    loadList();
  }, [open, loadList, isGit]);

  const openAdd = () => {
    setEditEntry(null);
    setUserId("");
    setUserSearchText("");
    setEnvEntries([]);
    setGitType(isGit ? "pat" : "env");
    setGitHostScope("");
    setGitToken("");
    setGitPrivateKey("");
    setGitErrorKey(undefined);
    setGitHasExistingSecret(false);
    setView("form");
  };

  const openEdit = async (entry: UserCredEntry) => {
    setEditEntry(entry);
    setUserId(entry.user_id);
    setUserSearchText(entry.user_id);
    setEnvEntries([]);
    setGitErrorKey(undefined);
    setView("form");
    // Load existing data for edit. The GET response includes credential_type +
    // host_scope but NEVER the secret blob — so we show "secret set" masked
    // placeholder and require re-entry to change the secret.
    try {
      const res = await http.get<{
        user_id: string;
        env: Record<string, CLIEnvEntryResponse> | null;
        credential_type?: string | null;
        host_scope?: string | null;
        has_secret?: boolean;
      }>(`/v1/cli-credentials/${binary.id}/user-credentials/${entry.user_id}`);
      setEnvEntries(entriesFromEnv(res.env));
      if (isGit) {
        const t = (res.credential_type ?? "env") as GitCredentialType;
        setGitType(t === "pat" || t === "ssh_key" ? t : "env");
        setGitHostScope(res.host_scope ?? "");
        setGitHasExistingSecret(!!res.has_secret);
        setGitToken("");
        setGitPrivateKey("");
      }
    } catch {
      // leave fields empty — user can re-enter
    }
  };

  /** Build the PUT payload for the git typed-credential path.
   * Returns null when the caller should fall through to the legacy env flow
   * (gitType==="env" or non-git binary). Returns a string error message when
   * client-side validation fails BEFORE the network round-trip — we still want
   * inline UI on host_scope required to be instant. */
  const buildGitTypedPayload = (): { credential_type: string; host_scope: string; blob: Record<string, string> } | null | string => {
    if (!isGit || gitType === "env") return null;
    const scope = gitHostScope.trim();
    if (!scope) {
      setGitErrorKey("git.cred_host_scope_required");
      return "host_scope_required";
    }
    if (gitType === "pat") {
      const tok = gitToken;
      // On edit, allow empty token → caller should not submit (keeps existing secret).
      // We block here because typed PUT replaces the blob entirely.
      if (!tok) {
        if (gitHasExistingSecret) return "no_change";
        setGitErrorKey("git.cred_blob_missing_token");
        return "token_required";
      }
      return { credential_type: "pat", host_scope: scope, blob: { token: tok } };
    }
    if (gitType === "ssh_key") {
      const key = gitPrivateKey;
      if (!key.trim()) {
        if (gitHasExistingSecret) return "no_change";
        setGitErrorKey("git.cred_blob_missing_key");
        return "key_required";
      }
      return { credential_type: "ssh_key", host_scope: scope, blob: { key } };
    }
    return null;
  };

  const handleSave = async () => {
    const uid = userId.trim();
    if (!uid) return;

    setGitErrorKey(undefined);

    // Git typed branch — supersedes the legacy env path for pat/ssh_key.
    if (isGit && gitType !== "env") {
      const payload = buildGitTypedPayload();
      if (typeof payload === "string") {
        // Client-side validation failure already set gitErrorKey above; bail
        // without toast so the inline field error is the single source of truth.
        if (payload === "no_change") {
          toast.success(i18next.t("cli-credentials:userCredentials.saved"));
          setView("list");
        }
        return;
      }
      if (payload === null) return;
      setSaving(true);
      try {
        await http.put(`/v1/cli-credentials/${binary.id}/user-credentials/${uid}`, payload);
        toast.success(i18next.t("cli-credentials:userCredentials.saved"));
        await loadList();
        setView("list");
      } catch (err) {
        // Backend writes typed errors with `code = error_key` so the shared
        // HttpClient surfaces them on err.code. Drive inline UI off that.
        const code = (err as { code?: string })?.code;
        if (code && code.startsWith("git.cred_")) {
          setGitErrorKey(code);
        } else {
          toast.error(
            i18next.t("cli-credentials:userCredentials.saveFailed"),
            err instanceof Error ? err.message : "",
          );
        }
      } finally {
        setSaving(false);
      }
      return;
    }

    // Legacy env-vars path (passthrough binaries + git's "env" fallback).
    const env = envPayloadFromEntries(envEntries);
    // New entry needs at least one variable; edits may clear all keys (empty object).
    if (!editEntry && Object.keys(env).length === 0) {
      toast.error(i18next.t("cli-credentials:userCredentials.envRequired"));
      return;
    }
    setSaving(true);
    try {
      await http.put(`/v1/cli-credentials/${binary.id}/user-credentials/${uid}`, { env });
      toast.success(i18next.t("cli-credentials:userCredentials.saved"));
      await loadList();
      setView("list");
    } catch (err) {
      toast.error(
        i18next.t("cli-credentials:userCredentials.saveFailed"),
        err instanceof Error ? err.message : "",
      );
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (entry: UserCredEntry) => {
    setDeletingId(entry.id);
    try {
      await http.delete(`/v1/cli-credentials/${binary.id}/user-credentials/${entry.user_id}`);
      toast.success(i18next.t("cli-credentials:userCredentials.deleted"));
      await loadList();
    } catch (err) {
      toast.error(
        i18next.t("cli-credentials:userCredentials.deleteFailed"),
        err instanceof Error ? err.message : "",
      );
    } finally {
      setDeletingId(null);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <KeyRound className="h-4 w-4" />
            {t("userCredentials.title")}
          </DialogTitle>
          <DialogDescription>
            {t("userCredentials.description", { name: binary.binary_name })}
          </DialogDescription>
        </DialogHeader>

        {view === "list" ? (
          <>
            {loadingList ? (
              <div className="flex justify-center py-8">
                <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
              </div>
            ) : entries.length === 0 ? (
              <p className="py-6 text-center text-sm text-muted-foreground">
                {t("userCredentials.empty")}
              </p>
            ) : (
              <div className="flex flex-col gap-2 max-h-[50vh] overflow-y-auto pr-1">
                {entries.map((entry) => (
                  <div
                    key={entry.id}
                    className="flex items-center justify-between rounded-md border px-3 py-2"
                  >
                    <div className="flex flex-col gap-1 min-w-0">
                      <div className="flex items-center gap-2 min-w-0">
                        <span className="font-mono text-sm truncate">{entry.user_id}</span>
                        {entry.credential_type && entry.credential_type !== "env" ? (
                          <Badge variant="default" className="shrink-0 text-xs uppercase">
                            {entry.credential_type === "pat"
                              ? t("userCredentials.credentialTypePAT")
                              : entry.credential_type === "ssh_key"
                                ? t("userCredentials.credentialTypeSSH")
                                : entry.credential_type}
                          </Badge>
                        ) : entry.has_env ? (
                          <Badge variant="secondary" className="shrink-0 text-xs">
                            env
                          </Badge>
                        ) : null}
                      </div>
                      {entry.host_scope && (
                        <p className="text-xs text-muted-foreground font-mono truncate" title={entry.host_scope}>
                          {entry.host_scope}
                        </p>
                      )}
                      {entry.env_keys && entry.env_keys.length > 0 && (
                        <p className="text-xs text-muted-foreground font-mono truncate" title={entry.env_keys.join(", ")}>
                          {entry.env_keys.join(", ")}
                        </p>
                      )}
                    </div>
                    <div className="flex items-center gap-1 shrink-0">
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8"
                        onClick={() => openEdit(entry)}
                        title={t("userCredentials.edit")}
                      >
                        <Pencil className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8 text-destructive hover:text-destructive"
                        onClick={() => handleDelete(entry)}
                        disabled={deleting === entry.id}
                        title={t("userCredentials.delete")}
                      >
                        {deleting === entry.id ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <Trash2 className="h-3.5 w-3.5" />
                        )}
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            )}
            <DialogFooter>
              <Button variant="outline" onClick={() => onOpenChange(false)}>
                {t("userCredentials.close")}
              </Button>
              <Button onClick={openAdd} className="gap-1">
                <Plus className="h-3.5 w-3.5" />
                {t("userCredentials.add")}
              </Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <div className="flex flex-col gap-4 max-h-[60vh] overflow-y-auto pr-1">
              <div className="flex flex-col gap-1.5">
                <Label>{t("userCredentials.userId")}</Label>
                <UserPickerCombobox
                  value={userSearchText}
                  onChange={setUserSearchText}
                  onSelect={(val) => { setUserId(val); setUserSearchText(val); }}
                  placeholder={t("userCredentials.userIdPlaceholder")}
                  source="tenant_user"
                  allowCustom
                />
                <p className="text-xs text-amber-600 dark:text-amber-400 bg-amber-50 dark:bg-amber-950/30 rounded-md px-2.5 py-1.5 border border-amber-200 dark:border-amber-800">{t("userCredentials.mergeHint")}</p>
              </div>

              {isGit ? (
                <CliCredentialGitFields
                  type={gitType}
                  onTypeChange={setGitType}
                  hostScope={gitHostScope}
                  onHostScopeChange={setGitHostScope}
                  token={gitToken}
                  onTokenChange={setGitToken}
                  privateKey={gitPrivateKey}
                  onPrivateKeyChange={setGitPrivateKey}
                  errorKey={gitErrorKey}
                  hasExistingSecret={gitHasExistingSecret}
                />
              ) : null}

              {(!isGit || gitType === "env") && (
                <div className="flex flex-col gap-1.5">
                  <Label>{t("userCredentials.env")}</Label>
                  <CliCredentialEnvVarsSection
                    isManualMode
                    activePreset={null}
                    envValues={{}}
                    setEnvValues={() => undefined}
                    manualEnvEntries={envEntries}
                    setManualEnvEntries={setEnvEntries}
                  />
                </div>
              )}
            </div>

            <DialogFooter>
              <Button
                variant="outline"
                onClick={() => setView("list")}
                disabled={saving}
              >
                {t("userCredentials.back")}
              </Button>
              <Button onClick={handleSave} disabled={saving}>
                {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin mr-1" /> : null}
                {t("userCredentials.save")}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
