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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { KeyValueEditor } from "@/components/shared/key-value-editor";
import { toast } from "@/stores/use-toast-store";
import { useHttp } from "@/hooks/use-ws";
import i18next from "i18next";
import type { SecureCLIBinary } from "./hooks/use-cli-credentials";

interface UserCredEntry {
  id: string;
  binary_id: string;
  user_id: string;
  has_env: boolean;
  /** Env variable names (no values) for display */
  env_keys?: string[];
  created_at: string;
  updated_at: string;
}

interface CLIUserCredentialsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  binary: SecureCLIBinary;
}

const SENSITIVE_ENV_RE = /^.*(key|secret|token|password|credential).*$/i;
const isSensitiveEnv = (key: string) => SENSITIVE_ENV_RE.test(key.trim());

type ViewState = "list" | "form";

export function CLIUserCredentialsDialog({ open, onOpenChange, binary }: CLIUserCredentialsDialogProps) {
  const { t } = useTranslation("cli-credentials");
  const http = useHttp();

  const [view, setView] = useState<ViewState>("list");
  const [entries, setEntries] = useState<UserCredEntry[]>([]);
  const [loadingList, setLoadingList] = useState(false);

  // Form state
  const [editEntry, setEditEntry] = useState<UserCredEntry | null>(null);
  const [userId, setUserId] = useState("");
  const [env, setEnv] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const [deleting, setDeletingId] = useState<string | null>(null);

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
    setEnv({});
    loadList();
  }, [open, loadList]);

  const openAdd = () => {
    setEditEntry(null);
    setUserId("");
    setEnv({});
    setView("form");
  };

  const openEdit = async (entry: UserCredEntry) => {
    setEditEntry(entry);
    setUserId(entry.user_id);
    setEnv({});
    setView("form");
    // Load existing env for edit
    try {
      const res = await http.get<{ user_id: string; env: Record<string, string> | null }>(
        `/v1/cli-credentials/${binary.id}/user-credentials/${entry.user_id}`,
      );
      setEnv(res.env ?? {});
    } catch {
      // leave env empty — user can re-enter
    }
  };

  const handleSave = async () => {
    const uid = userId.trim();
    if (!uid) return;
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
                        {entry.has_env && (
                          <Badge variant="secondary" className="shrink-0 text-xs">
                            env
                          </Badge>
                        )}
                      </div>
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
                <Label htmlFor="uc-user-id">{t("userCredentials.userId")}</Label>
                <Input
                  id="uc-user-id"
                  value={userId}
                  onChange={(e) => setUserId(e.target.value)}
                  placeholder={t("userCredentials.userIdPlaceholder")}
                  disabled={!!editEntry}
                  className="text-base md:text-sm font-mono"
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <Label>{t("userCredentials.env")}</Label>
                <KeyValueEditor
                  value={env}
                  onChange={setEnv}
                  keyPlaceholder="ENV_KEY"
                  valuePlaceholder="value"
                  addLabel={t("userCredentials.addEnv")}
                  maskValue={isSensitiveEnv}
                />
              </div>
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
