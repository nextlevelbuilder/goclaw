import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Loader2, CheckCircle2, XCircle } from "lucide-react";
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

/** Split a string into shell-like tokens, treating commas and spaces outside quotes as delimiters. */
function splitShellTokens(input: string): string[] {
  const tokens: string[] = [];
  const re = /"([^"]*)"|'([^']*)'|[^\s,]+/g;
  let m;
  while ((m = re.exec(input)) !== null) {
    tokens.push(m[1] ?? m[2] ?? m[0]);
  }
  return tokens.filter(Boolean);
}

interface MCPFormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  server?: MCPServerData | null;
  onSubmit: (data: MCPServerInput) => Promise<unknown>;
  onTest: (data: {
    transport: string;
    command?: string;
    args?: string[];
    url?: string;
    headers?: Record<string, string>;
    env?: Record<string, string>;
  }) => Promise<{ success: boolean; tool_count?: number; error?: string }>;
}

export function MCPFormDialog({ open, onOpenChange, server, onSubmit, onTest }: MCPFormDialogProps) {
  const { t } = useTranslation("mcp");
  const [loading, setLoading] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ success: boolean; tool_count?: number; error?: string } | null>(null);
  const [error, setError] = useState("");

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
    },
  });

  const { watch, reset, handleSubmit: rhfHandleSubmit } = form;
  const transport = watch("transport");
  const command = watch("command");
  const args = watch("args");
  const url = watch("url");
  const headers = watch("headers") as Record<string, string>;
  const env = watch("env") as Record<string, string>;
  const isStdio = transport === "stdio";

  useEffect(() => {
    if (open) {
      reset({
        name: server?.name ?? "",
        displayName: server?.display_name ?? "",
        transport: (server?.transport as MCPFormData["transport"]) ?? "stdio",
        command: server?.command ?? "",
        args: Array.isArray(server?.args) ? server.args.join(", ") : "",
        url: server?.url ?? "",
        headers: server?.headers ?? {},
        env: server?.env ?? {},
        toolPrefix: (server?.tool_prefix ?? "").replace(/^mcp_/, ""),
        timeout: server?.timeout_sec ?? 60,
        enabled: server?.enabled ?? true,
        requireUserCreds: server?.settings?.require_user_credentials ?? false,
      });
      setError("");
      setTestResult(null);
    }
  }, [open, server, reset]);

  const buildConnectionData = () => {
    let parsedArgs: string[] | undefined = undefined;
    let resolvedCommand = command.trim();

    if (isStdio) {
      const cmdTokens = splitShellTokens(resolvedCommand);
      if (cmdTokens.length > 1) {
        resolvedCommand = cmdTokens[0]!;
        const extraArgs = cmdTokens.slice(1);
        const userArgs = args.trim() ? splitShellTokens(args) : [];
        parsedArgs = [...extraArgs, ...userArgs];
      } else if (args.trim()) {
        parsedArgs = splitShellTokens(args);
      }
    }

    return {
      transport,
      command: isStdio ? resolvedCommand : undefined,
      args: parsedArgs,
      url: !isStdio ? url.trim() : undefined,
      headers: !isStdio && Object.keys(headers).length > 0 ? headers : undefined,
      env: Object.keys(env).length > 0 ? env : undefined,
    };
  };

  const handleTest = async () => {
    if (isStdio && !command.trim()) { setError(t("form.errors.commandRequired")); return; }
    if (!isStdio && !url.trim()) { setError(t("form.errors.urlRequired")); return; }
    setTesting(true);
    setError("");
    setTestResult(null);
    try {
      const result = await onTest(buildConnectionData());
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

    setLoading(true);
    setError("");
    try {
      await onSubmit({
        name: data.name.trim(),
        display_name: data.displayName.trim() || undefined,
        ...buildConnectionData(),
        tool_prefix: data.toolPrefix.trim() || undefined,
        timeout_sec: data.timeout,
        settings: { require_user_credentials: data.requireUserCreds },
        enabled: data.enabled,
      });
      onOpenChange(false);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t("form.saving"));
    } finally {
      setLoading(false);
    }
  });

  return (
    <Dialog open={open} onOpenChange={(v) => !loading && onOpenChange(v)}>
      <DialogContent className="max-h-[85vh] flex flex-col sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{server ? t("form.editTitle") : t("form.createTitle")}</DialogTitle>
        </DialogHeader>

        <div className="grid gap-4 py-2 -mx-4 px-4 sm:-mx-6 sm:px-6 overflow-y-auto min-h-0">
          <McpConnectionFields form={form} />
          <McpSettingsFields form={form} />
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
            <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
              {t("form.cancel")}
            </Button>
            <Button onClick={handleSubmit} disabled={loading}>
              {loading ? t("form.saving") : server ? t("form.update") : t("form.create")}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
