import { useState, useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
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
import { Combobox } from "@/components/ui/combobox";
import { toast } from "@/stores/use-toast-store";
import { useAuthStore } from "@/stores/use-auth-store";
import { useTenants } from "@/hooks/use-tenants";
import { useTenantUsersList } from "@/pages/contacts/hooks/use-tenant-users-list";
import i18next from "i18next";
import type { MCPServerData, MCPUserCredentialStatus, MCPUserCredentialInput } from "./hooks/use-mcp";

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
  const { users } = useTenantUsersList();

  const canManageUsers =
    role === "admin" ||
    currentTenant?.role === "owner" ||
    currentTenant?.role === "admin";

  const [selectedUserId, setSelectedUserId] = useState(currentUserId);

  const [status, setStatus] = useState<MCPUserCredentialStatus | null>(null);
  const [loadingStatus, setLoadingStatus] = useState(false);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const [apiKey, setApiKey] = useState("");
  const [headers, setHeaders] = useState<Record<string, string>>({});
  const [env, setEnv] = useState<Record<string, string>>({});

  const userOptions = useMemo(
    () =>
      users.map((u) => ({
        value: u.user_id,
        label: u.display_name || u.user_id,
      })),
    [users],
  );

  // Reset selected user when dialog opens
  useEffect(() => {
    if (open) setSelectedUserId(currentUserId);
  }, [open, currentUserId]);

  useEffect(() => {
    if (!open) return;
    setApiKey("");
    setHeaders({});
    setEnv({});
    setStatus(null);
    setLoadingStatus(true);
    const targetUser = canManageUsers ? selectedUserId : undefined;
    onGetCredentials(server.id, targetUser)
      .then(setStatus)
      .catch(() => {})
      .finally(() => setLoadingStatus(false));
  }, [open, server.id, onGetCredentials, canManageUsers, selectedUserId]);

  const handleSave = async () => {
    setSaving(true);
    try {
      const creds: MCPUserCredentialInput = {};
      if (apiKey.trim()) creds.api_key = apiKey.trim();
      if (Object.keys(headers).length > 0) creds.headers = headers;
      if (Object.keys(env).length > 0) creds.env = env;
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

        {loadingStatus ? (
          <div className="flex justify-center py-8">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : (
          <div className="flex flex-col gap-4 max-h-[60vh] overflow-y-auto pr-1">
            {/* User selector — admin only */}
            {canManageUsers && (
              <div className="flex flex-col gap-1.5">
                <Label>{t("userCredentials.selectUser")}</Label>
                <Combobox
                  value={selectedUserId}
                  onChange={(val) => setSelectedUserId(val)}
                  options={userOptions}
                  placeholder={t("userCredentials.selectUser")}
                />
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
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                placeholder={t("userCredentials.apiKeyPlaceholder")}
                className="text-base md:text-sm font-mono"
              />
            </div>

            {/* Headers — KeyValueEditor with sensitive masking */}
            <div className="flex flex-col gap-1.5">
              <Label>{t("userCredentials.headers")}</Label>
              <KeyValueEditor
                value={headers}
                onChange={setHeaders}
                keyPlaceholder="Header"
                valuePlaceholder="Value"
                addLabel={t("userCredentials.addHeader")}
                maskValue={isSensitiveHeader}
              />
            </div>

            {/* Env vars — KeyValueEditor with sensitive masking */}
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
