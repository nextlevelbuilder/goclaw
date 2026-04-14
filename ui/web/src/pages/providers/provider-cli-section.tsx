import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Loader2, CheckCircle2, AlertTriangle, RefreshCw } from "lucide-react";
import { useHttp } from "@/hooks/use-ws";

interface CLIAuthStatus {
  logged_in: boolean;
  email?: string;
  subscription_type?: string;
  auth_method?: string;
  type?: string;
  error?: string;
  in_docker?: boolean;
}

type CLIType = "claude" | "qwen";

interface CLISectionProps {
  open: boolean;
  cliType?: CLIType;
}

const CLI_CONFIG: Record<CLIType, {
  binary: string;
  endpoint: string;
  translationPrefix: string;
  authCommand: string;
  logoutCommand: string;
}> = {
  claude: {
    binary: "claude",
    endpoint: "/v1/providers/claude-cli/auth-status",
    translationPrefix: "cli",
    authCommand: "claude auth login",
    logoutCommand: "claude auth logout",
  },
  qwen: {
    binary: "qwen",
    endpoint: "/v1/providers/qwen-cli/auth-status",
    translationPrefix: "qwenCli",
    authCommand: "qwen auth",
    logoutCommand: "rm ~/.qwen/oauth_creds.json && qwen auth",
  },
};

export function CLISection({ open, cliType = "claude" }: CLISectionProps) {
  const { t } = useTranslation("providers");
  const http = useHttp();
  const [cliAuth, setCliAuth] = useState<CLIAuthStatus | null>(null);
  const [loading, setLoading] = useState(false);

  const config = CLI_CONFIG[cliType];

  const checkAuth = useCallback(() => {
    setLoading(true);
    http
      .get<CLIAuthStatus>(config.endpoint)
      .then(setCliAuth)
      .catch(() => setCliAuth({ logged_in: false, error: "Failed to check auth status" }))
      .finally(() => setLoading(false));
  }, [http, config.endpoint]);

  useEffect(() => {
    if (open) {
      checkAuth();
    } else {
      setCliAuth(null);
    }
  }, [open, checkAuth]);

  const dockerPrefix = cliAuth?.in_docker ? "docker compose exec goclaw " : "";
  const authCmd = `${dockerPrefix}${config.authCommand}`;
  const switchCmd = `${dockerPrefix}${config.logoutCommand} && ${dockerPrefix}${config.authCommand}`;

  // Get authentication display info based on CLI type
  const getAuthDisplay = () => {
    if (cliType === "qwen") {
      const parts: string[] = [];
      if (cliAuth?.auth_method) parts.push(cliAuth.auth_method);
      if (cliAuth?.type) parts.push(cliAuth.type);
      return parts.length > 0 ? parts.join(" - ") : "Authenticated";
    }
    return cliAuth?.email || "Authenticated";
  };

  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">
        {t(`${config.translationPrefix}.description`)} <code className="rounded bg-muted px-1 py-0.5">{config.binary}</code> {t(`${config.translationPrefix}.descriptionSuffix`)}
      </p>
      {loading ? (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          {t(`${config.translationPrefix}.checkingAuth`)}
        </div>
      ) : cliAuth?.logged_in ? (
        <div className="space-y-2">
          <div className="flex items-center justify-between rounded-md border border-green-200 bg-green-50 px-3 py-2 dark:border-green-800 dark:bg-green-950">
            <div className="flex items-center gap-2">
              <CheckCircle2 className="h-4 w-4 text-green-600 dark:text-green-400" />
              <p className="text-sm text-green-700 dark:text-green-300">
                {t(`${config.translationPrefix}.authenticatedAs`)} <strong>{getAuthDisplay()}</strong>
                {cliType === "claude" && cliAuth.subscription_type && (
                  <span className="ml-1 text-xs opacity-75">({cliAuth.subscription_type})</span>
                )}
              </p>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-7 px-2 text-green-700 hover:text-green-800 dark:text-green-400 dark:hover:text-green-300"
              onClick={checkAuth}
            >
              <RefreshCw className="h-3.5 w-3.5" />
            </Button>
          </div>
          <details className="text-xs text-muted-foreground">
            <summary className="cursor-pointer hover:text-foreground">{t(`${config.translationPrefix}.switchAccount`)}</summary>
            <div className="mt-1.5 space-y-1 rounded-md border bg-muted/50 px-3 py-2">
              <p>{t(`${config.translationPrefix}.switchAccountInstructions`)}</p>
              <code className="block rounded bg-muted px-2 py-1 font-mono">
                {switchCmd}
              </code>
              <p>{t(`${config.translationPrefix}.switchAccountRecheck`)} <RefreshCw className="inline h-3 w-3" /> {t(`${config.translationPrefix}.switchAccountRecheckSuffix`)}</p>
            </div>
          </details>
        </div>
      ) : cliAuth ? (
        <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 dark:border-amber-800 dark:bg-amber-950">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <AlertTriangle className="h-4 w-4 text-amber-600 dark:text-amber-400" />
              <p className="text-sm font-medium text-amber-700 dark:text-amber-300">{t(`${config.translationPrefix}.notAuthenticated`)}</p>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-7 px-2 text-amber-700 hover:text-amber-800 dark:text-amber-400 dark:hover:text-amber-300"
              onClick={checkAuth}
            >
              <RefreshCw className="h-3.5 w-3.5 mr-1" />
              <span className="text-xs">{t(`${config.translationPrefix}.recheckButton`)}</span>
            </Button>
          </div>
          <p className="mt-1 text-sm text-amber-600 dark:text-amber-400">
            {t(`${config.translationPrefix}.runOnServer`)}
          </p>
          <code className="mt-1 block rounded bg-amber-100 px-2 py-1 text-xs font-mono dark:bg-amber-900 dark:text-amber-300">
            {authCmd}
          </code>
          {cliAuth.error && (
            <p className="mt-1 text-xs text-amber-500">{cliAuth.error}</p>
          )}
        </div>
      ) : null}
    </div>
  );
}
