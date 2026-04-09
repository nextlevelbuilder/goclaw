import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { KeyRound, Loader2 } from "lucide-react";
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
import { UserPickerCombobox } from "@/components/shared/user-picker-combobox";
import { toast } from "@/stores/use-toast-store";
import { useAuthStore } from "@/stores/use-auth-store";
import { useTenants } from "@/hooks/use-tenants";
import i18next from "i18next";
import type { MCPServerData, MCPUserCredentialStatus, MCPUserCredentialInput } from "./hooks/use-mcp";
import { mcpUserCredentialsSchema, type MCPUserCredentialsFormData } from "@/schemas/mcp-credentials.schema";

/** Header keys whose values should be masked. */
const SENSITIVE_HEADER_RE = /^(authorization|bearer)|(key|secret|token|password|credential)/i;
const isSensitiveHeader = (key: string) => SENSITIVE_HEADER_RE.test(key.trim());

/** Env var keys whose values should be masked. */
const SENSITIVE_ENV_RE = /^.*(key|secret|token|password|credential).*$/i;
const isSensitiveEnv = (key: string) => SENSITIVE_ENV_RE.test(key.trim());

interface MCPUserCredentialsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  server: MCPServerData;
  onGetCredentials: (serverId: string, userId?: string) => Promise<MCPUserCredentialStatus>;
  onSetCredentials: (serverId: string, creds: MCPUserCredentialInput, userId?: string) => Promise<void>;
  onDeleteCredentials: (serverId: string, userId?: string) => Promise<void>;
}

export function MCPUserCredentialsDialog({
  open,
  onOpenChange,
  server,
  onGetCredentials,
  onSetCredentials,
  onDeleteCredentials,
}: MCPUserCredentialsDialogProps) {
  const { t } = useTranslation("mcp");
  const role = useAuthStore((s) => s.role);
  const currentUserId = useAuthStore((s) => s.userId);
  const { currentTenant } = useTenants();

  const canManageUsers =
    role === "admin" || role === "owner" ||
    currentTenant?.role === "owner" ||
    currentTenant?.role === "admin";

  // UI-only state
  const [selectedUserId, setSelectedUserId] = useState(currentUserId);
  const [userSearchText, setUserSearchText] = useState("");
  const [status, setStatus] = useState<MCPUserCredentialStatus | null>(null);
  const [loadingStatus, setLoadingStatus] = useState(false);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [initialLoad, setInitialLoad] = useState(true);

  const form = useForm<MCPUserCredentialsFormData>({
    resolver: zodResolver(mcpUserCredentialsSchema),
    mode: "onChange",
    defaultValues: { apiKey: "", headers: {}, env: {} },
  });

  const { register, watch, setValue, reset } = form;
  const headers = watch("headers") as Record<string, string>;
  const env = watch("env") as Record<string, string>;

  // Reset state when dialog opens
  useEffect(() => {
    if (open) {
      setSelectedUserId(currentUserId);
      setUserSearchText("");
      setInitialLoad(true);
    }
  }, [open, currentUserId]);

  useEffect(() => {
    if (!open) return;
    reset({ apiKey: "", headers: {}, env: {} });
    if (initialLoad) {
      setStatus(null);
      setLoadingStatus(true);
    }
    const targetUser = canManageUsers ? selectedUserId : undefined;
    onGetCredentials(server.id, targetUser)
      .then(setStatus)
      .catch((err) => console.error("[MCPUserCredentials] load credentials failed:", err))
      .finally(() => { setLoadingStatus(false); setInitialLoad(false); });
  }, [open, server.id, onGetCredentials, canManageUsers, selectedUserId]); // eslint-disable-line react-hooks/exhaustive-deps

  const handleSave = async () => {
    setSaving(true);
    try {
      const data = form.getValues();
      const creds: MCPUserCredentialInput = {};
      if (data.apiKey.trim()) creds.api_key = data.apiKey.trim();
      if (Object.keys(data.headers).length > 0) creds.headers = data.headers as Record<string, string>;
      if (Object.keys(data.env).length > 0) creds.env = data.env as Record<string, string>;
      const targetUser = canManageUsers ? selectedUserId : undefined;
      await onSetCredentials(server.id, creds, targetUser);
      toast.success(i18next.t("mcp:userCredentials.saved"));
      onOpenChange(false);
    } catch (err) {
      toast.error(i18next.t("mcp:userCredentials.saveFailed"), err instanceof Error ? err.message : "");
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    setDeleting(true);
    try {
      const targetUser = canManageUsers ? selectedUserId : undefined;
      await onDeleteCredentials(server.id, targetUser);
      toast.success(i18next.t("mcp:userCredentials.deleted"));
      onOpenChange(false);
    } catch (err) {
      toast.error(i18next.t("mcp:userCredentials.deleteFailed"), err instanceof Error ? err.message : "");
    } finally {
      setDeleting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <KeyRound className="h-4 w-4" />
            {canManageUsers ? t("userCredentials.titleAdmin") : t("userCredentials.title")}
          </DialogTitle>
          <DialogDescription>
            {canManageUsers ? t("userCredentials.descriptionAdmin") : t("userCredentials.description")}
          </DialogDescription>
        </DialogHeader>

        {loadingStatus && initialLoad ? (
          <div className="flex justify-center py-8">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : (
          <div className="flex flex-col gap-4 max-h-[60vh] overflow-y-auto pr-1">
            {/* User selector — admin only */}
            {canManageUsers && (
              <div className="flex flex-col gap-1.5">
                <Label>{t("userCredentials.selectUser")}</Label>
                <UserPickerCombobox
                  value={userSearchText}
                  onChange={setUserSearchText}
                  onSelect={(val) => { setSelectedUserId(val); setUserSearchText(val); }}
                  placeholder={t("userCredentials.selectUser")}
                  source="tenant_user"
                />
                {selectedUserId && selectedUserId !== userSearchText && (
                  <p className="text-xs text-muted-foreground font-mono">{selectedUserId}</p>
                )}
                <p className="text-xs text-amber-600 dark:text-amber-400 bg-amber-50 dark:bg-amber-950/30 rounded-md px-2.5 py-1.5 border border-amber-200 dark:border-amber-800">{t("userCredentials.mergeHint")}</p>
              </div>
            )}

            {/* Current status badges */}
            {status && (
              <div className="flex flex-wrap gap-2">
                {!status.has_credentials ? (
                  <Badge variant="secondary">{t("userCredentials.noCredentials")}</Badge>
                ) : (
                  <>
                    {status.has_api_key && (
                      <Badge variant="default">{t("userCredentials.hasApiKey")}</Badge>
                    )}
                    {status.has_headers && (
                      <Badge variant="default">{t("userCredentials.hasHeaders")}</Badge>
                    )}
                    {status.has_env && (
                      <Badge variant="default">{t("userCredentials.hasEnv")}</Badge>
                    )}
                  </>
                )}
              </div>
            )}

            {/* API Key */}
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="uc-api-key">{t("userCredentials.apiKey")}</Label>
              <Input
                id="uc-api-key"
                type="password"
                placeholder={t("userCredentials.apiKeyPlaceholder")}
                className="text-base md:text-sm font-mono"
                {...register("apiKey")}
              />
            </div>

            {/* Headers */}
            <div className="flex flex-col gap-1.5">
              <Label>{t("userCredentials.headers")}</Label>
              <KeyValueEditor
                value={headers}
                onChange={(v) => setValue("headers", v)}
                keyPlaceholder="Header"
                valuePlaceholder="Value"
                addLabel={t("userCredentials.addHeader")}
                maskValue={isSensitiveHeader}
              />
            </div>

            {/* Env vars */}
            <div className="flex flex-col gap-1.5">
              <Label>{t("userCredentials.env")}</Label>
              <KeyValueEditor
                value={env}
                onChange={(v) => setValue("env", v)}
                keyPlaceholder="ENV_KEY"
                valuePlaceholder="value"
                addLabel={t("userCredentials.addEnv")}
                maskValue={isSensitiveEnv}
              />
            </div>
          </div>
        )}

        <DialogFooter className="flex-col sm:flex-row gap-2">
          {status?.has_credentials && (
            <Button
              variant="destructive"
              onClick={handleDelete}
              disabled={deleting || saving}
              className="sm:mr-auto"
            >
              {deleting ? <Loader2 className="h-3.5 w-3.5 animate-spin mr-1" /> : null}
              {t("userCredentials.deleteAll")}
            </Button>
          )}
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving || deleting}>
            {t("userCredentials.cancel")}
          </Button>
          <Button onClick={handleSave} disabled={saving || deleting || loadingStatus}>
            {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin mr-1" /> : null}
            {t("userCredentials.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
