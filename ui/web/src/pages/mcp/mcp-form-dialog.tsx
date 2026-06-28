import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Loader2, CheckCircle2, XCircle, ShieldCheck } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import type { MCPServerData, MCPServerInput } from "./hooks/use-mcp";
import { isValidSlug } from "@/lib/slug";
import { mcpFormSchema, type MCPFormData } from "@/schemas/mcp.schema";
import { McpConnectionFields } from "./mcp-connection-fields";
import { McpSettingsFields } from "./mcp-settings-fields";
import { formatShellArgs, parseShellArgs } from "./mcp-args";

interface MCPFormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  server?: MCPServerData | null;
  onSubmit: (data: MCPServerInput) => Promise<MCPServerData | void>;
  onTest: (data: {
    server_id?: string;
    transport: string;
    command?: string;
    args?: string[];
    url?: string;
    headers?: Record<string, string>;
    env?: Record<string, string>;
  }) => Promise<{ success: boolean; tool_count?: number; error?: string }>;
  /** Called after a successful save (or immediately for edit) to open the OAuth authorization dialog. */
  onAuthorize?: (server: MCPServerData) => void;
}

export function MCPFormDialog({ open, onOpenChange, server, onSubmit, onTest, onAuthorize }: MCPFormDialogProps) {
  const { t } = useTranslation("mcp");
  const [loading, setLoading] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ success: boolean; tool_count?: number; error?: string } | null>(null);
  const [error, setError] = useState("");
  // Tracks a server created via handleAuthorize (ADD mode) to prevent duplicate POST on "Create" click.
  const [createdServer, setCreatedServer] = useState<MCPServerData | null>(null);

  const form = useForm<MCPFormData>({
    resolver: zodResolver(mcpFormSchema),
    mode: "onChange",
    defaultValues: {
      name: "",
      displayName: "",
      transport: "stdio",
      command: "",
      args: "",
      url: "",
      headers: {},
      env: {},
      toolPrefix: "",
      timeout: 60,
      enabled: true,
      requireUserCreds: false,
      toolHintsGlobal: "",
      toolHintsTools: {},
      oauthEnabled: false,
      oauthUseDcr: true,
      oauthGrantType: "pkce",
      oauthAuthEndpoint: "",
      oauthTokenEndpoint: "",
      oauthClientId: "",
      oauthClientSecret: "",
      oauthScope: "",
    },
  });

  const { watch, reset, handleSubmit: rhfHandleSubmit } = form;
  const transport = watch("transport");
  const command = watch("command");
  const args = watch("args");
  const url = watch("url");
  const headers = watch("headers") as Record<string, string>;
  const env = watch("env") as Record<string, string>;
  const oauthEnabled = watch("oauthEnabled");
  const isStdio = transport === "stdio";

  useEffect(() => {
    if (open) {
      const oauth = server?.settings?.oauth;
      reset({
        name: server?.name ?? "",
        displayName: server?.display_name ?? "",
        transport: (server?.transport as MCPFormData["transport"]) ?? "stdio",
        command: server?.command ?? "",
        args: Array.isArray(server?.args) ? formatShellArgs(server.args) : "",
        url: server?.url ?? "",
        headers: server?.headers ?? {},
        env: server?.env ?? {},
        toolPrefix: server?.tool_prefix ?? "",
        timeout: server?.timeout_sec ?? 60,
        enabled: server?.enabled ?? true,
        requireUserCreds: server?.settings?.require_user_credentials ?? false,
        toolHintsGlobal: server?.settings?.tool_hints?.global ?? "",
        toolHintsTools: server?.settings?.tool_hints?.tools ?? {},
        oauthEnabled: oauth?.auth_type === "oauth",
        oauthUseDcr: oauth?.use_dcr ?? true,
        oauthGrantType: (oauth?.grant_type as MCPFormData["oauthGrantType"]) ?? "pkce",
        oauthAuthEndpoint: oauth?.auth_endpoint ?? "",
        oauthTokenEndpoint: oauth?.token_endpoint ?? "",
        oauthClientId: oauth?.client_id ?? "",
        oauthClientSecret: "",     // never pre-fill secret
        oauthScope: oauth?.scope ?? "",
      });
      setError("");
      setTestResult(null);
      setCreatedServer(null);
    }
  }, [open, server, reset]);

  const buildConnectionData = () => {
    let parsedArgs: string[] | undefined = undefined;
    let resolvedCommand = command.trim();

    if (isStdio) {
      const cmdTokens = parseShellArgs(resolvedCommand);
      if (cmdTokens.length > 1) {
        resolvedCommand = cmdTokens[0]!;
        const extraArgs = cmdTokens.slice(1);
        const userArgs = args.trim() ? parseShellArgs(args) : [];
        parsedArgs = [...extraArgs, ...userArgs];
      } else if (args.trim()) {
        parsedArgs = parseShellArgs(args);
      }
    }

    // Always send the collection fields (even when emptied) so clearing all rows
    // actually clears them server-side. UpdateServer applies a partial update keyed
    // by the fields present in the request body — omitting an emptied field would
    // leave the previous value untouched, making it impossible to delete the last
    // header/env/arg. stdio servers don't use headers/url; remote servers don't use args.
    return {
      transport,
      command: isStdio ? resolvedCommand : undefined,
      args: isStdio ? (parsedArgs ?? []) : undefined,
      url: !isStdio ? url.trim() : undefined,
      headers: isStdio ? undefined : headers,
      env,
    };
  };

  const buildPayload = (data: MCPFormData): MCPServerInput => {
    const trimmedGlobal = data.toolHintsGlobal.trim();
    const trimmedTools: Record<string, string> = {};
    for (const [k, v] of Object.entries(data.toolHintsTools)) {
      const key = k.trim();
      const val = v.trim();
      if (key && val) trimmedTools[key] = val;
    }
    const hasHints = trimmedGlobal !== "" || Object.keys(trimmedTools).length > 0;
    const settings: NonNullable<MCPServerInput["settings"]> = {
      require_user_credentials: data.requireUserCreds,
    };
    if (hasHints) {
      settings.tool_hints = {
        ...(trimmedGlobal ? { global: trimmedGlobal } : {}),
        ...(Object.keys(trimmedTools).length > 0 ? { tools: trimmedTools } : {}),
      };
    }
    if (!isStdio && data.oauthEnabled) {
      settings.oauth = {
        auth_type: "oauth",
        use_dcr: data.oauthUseDcr,
        grant_type: data.oauthGrantType,
        // Manual endpoints only apply when DCR is off; never persist stale values
        // left in form state after toggling DCR back on (keeps backend + the
        // token-purge fingerprint consistent with the actual auth mode).
        ...(!data.oauthUseDcr && data.oauthAuthEndpoint.trim() ? { auth_endpoint: data.oauthAuthEndpoint.trim() } : {}),
        ...(!data.oauthUseDcr && data.oauthTokenEndpoint.trim() ? { token_endpoint: data.oauthTokenEndpoint.trim() } : {}),
        ...(data.oauthClientId.trim() ? { client_id: data.oauthClientId.trim() } : {}),
        ...(data.oauthClientSecret.trim() ? { client_secret: data.oauthClientSecret.trim() } : {}),
        ...(data.oauthScope.trim() ? { scope: data.oauthScope.trim() } : {}),
      };
    }
    return {
      name: data.name.trim(),
      display_name: data.displayName.trim() || undefined,
      ...buildConnectionData(),
      tool_prefix: data.toolPrefix.trim() || undefined,
      timeout_sec: data.timeout,
      settings,
      enabled: data.enabled,
    };
  };

  const handleTest = async () => {
    if (isStdio && !command.trim()) { setError(t("form.errors.commandRequired")); return; }
    if (!isStdio && !url.trim()) { setError(t("form.errors.urlRequired")); return; }
    setTesting(true);
    setError("");
    setTestResult(null);
    try {
      const result = await onTest({ server_id: server?.id, ...buildConnectionData() });
      setTestResult(result);
    } catch (err: unknown) {
      setTestResult({ success: false, error: err instanceof Error ? err.message : t("form.errors.connectionFailed") });
    } finally {
      setTesting(false);
    }
  };

  const handleSubmit = rhfHandleSubmit(async (data) => {
    if (!isValidSlug(data.name.trim())) { setError(t("form.errors.nameSlug")); return; }
    if (isStdio && !data.command.trim()) { setError(t("form.errors.commandRequired")); return; }
    if (!isStdio && !data.url.trim()) { setError(t("form.errors.urlRequired")); return; }

    // ADD mode: server already created via Authorize flow — don't POST again (duplicate key).
    if (!server && createdServer) {
      onOpenChange(false);
      return;
    }

    setLoading(true);
    setError("");
    try {
      await onSubmit(buildPayload(data));
      onOpenChange(false);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t("form.errors.saveFailed", "Save failed"));
    } finally {
      setLoading(false);
    }
  });

  const handleAuthorize = async () => {
    if (!onAuthorize) return;
    const values = form.getValues();

    if (!isValidSlug(values.name.trim())) { setError(t("form.errors.nameSlug")); return; }
    if (isStdio && !values.command.trim()) { setError(t("form.errors.commandRequired")); return; }
    if (!isStdio && !values.url.trim()) { setError(t("form.errors.urlRequired")); return; }

    if (server) {
      // EDIT: if URL changed in form, save first so OAuth flow uses the new URL.
      const currentUrl = values.url.trim();
      if (!isStdio && currentUrl && currentUrl !== server.url) {
        setLoading(true);
        setError("");
        try {
          await onSubmit(buildPayload(values));
          onAuthorize({ ...server, url: currentUrl });
        } catch (err: unknown) {
          setError(err instanceof Error ? err.message : t("form.errors.saveFailed", "Save failed"));
        } finally {
          setLoading(false);
        }
        return;
      }
      onAuthorize(server);
      return;
    }

    // ADD: save server first, then open OAuth dialog (dialog stays open).
    setLoading(true);
    setError("");
    try {
      const created = await onSubmit(buildPayload(values));
      if (created && "id" in created) {
        setCreatedServer(created as MCPServerData);
        onAuthorize(created as MCPServerData);
      }
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t("form.errors.saveFailed", "Save failed"));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !loading && onOpenChange(v)}>
      <DialogContent className="max-h-[85vh] flex flex-col sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{server ? t("form.editTitle") : t("form.createTitle")}</DialogTitle>
        </DialogHeader>

        <div className="grid gap-4 py-2 -mx-4 px-4 sm:-mx-6 sm:px-6 overflow-y-auto min-h-0">
          <McpConnectionFields form={form} />
          <McpSettingsFields form={form} isEditing={!!server} />
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>

        <DialogFooter className="flex-col sm:flex-row gap-2">
          <div className="flex items-center gap-2 mr-auto">
            <Button type="button" variant="secondary" size="sm" onClick={handleTest} disabled={loading || testing}>
              {testing
                ? <><Loader2 className="h-3.5 w-3.5 animate-spin mr-1" />{t("form.testing")}</>
                : t("form.testConnection")}
            </Button>
            {testResult && (
              <span className={`flex items-center gap-1 text-xs ${testResult.success ? "text-emerald-600 dark:text-emerald-400" : "text-destructive"}`}>
                {testResult.success
                  ? <><CheckCircle2 className="h-3.5 w-3.5" />{t("form.toolsFound", { count: testResult.tool_count })}</>
                  : <><XCircle className="h-3.5 w-3.5" />{testResult.error}</>}
              </span>
            )}
          </div>
          <div className="flex gap-2">
            {!isStdio && oauthEnabled && onAuthorize && (
              <Button
                type="button"
                variant="secondary"
                size="sm"
                onClick={handleAuthorize}
                disabled={loading || testing}
                className="gap-1"
              >
                {loading
                  ? <><Loader2 className="h-3.5 w-3.5 animate-spin" />{t("form.oauth.savingFirst")}</>
                  : <><ShieldCheck className="h-3.5 w-3.5" />{t("form.oauth.authorize")}</>}
              </Button>
            )}
            <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
              {t("form.cancel")}
            </Button>
            <Button onClick={handleSubmit} disabled={loading}>
              {loading ? t("form.saving") : (server || createdServer) ? t("form.update") : t("form.create")}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
