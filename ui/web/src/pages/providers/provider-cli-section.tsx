import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Loader2, CheckCircle2, AlertTriangle, RefreshCw } from "lucide-react";
import { useHttp } from "@/hooks/use-ws";

interface CLIAuthStatus {
  logged_in: boolean;
  email?: string;
  subscription_type?: string;
  error?: string;
  in_docker?: boolean;
}

type CLIKind = "claude" | "codex" | "gemini";

export function CLISection({ open, kind = "claude" }: { open: boolean; kind?: CLIKind }) {
  const { t } = useTranslation("providers");
  const http = useHttp();
  const [cliAuth, setCliAuth] = useState<CLIAuthStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const binary = kind === "codex" ? "codex" : kind === "gemini" ? "gemini" : "claude";
  const authPath = kind === "codex"
    ? "/v1/providers/codex-cli/auth-status"
    : kind === "gemini"
    ? "/v1/providers/gemini-cli/auth-status"
    : "/v1/providers/claude-cli/auth-status";
  const loginCommand = kind === "codex"
    ? "codex login"
    : kind === "gemini"
    ? "gemini login"
    : "claude auth login";
  const switchCommand = kind === "codex"
    ? "codex logout && codex login --device-auth"
    : kind === "gemini"
    ? "gemini logout && gemini login"
    : "claude auth logout && claude auth login";
  const dockerLoginCommand = kind === "codex"
    ? "docker compose exec -u goclaw goclaw codex login --device-auth"
    : kind === "gemini"
    ? "docker compose exec -u goclaw goclaw gemini login"
    : "docker compose exec -u goclaw goclaw claude auth login";
  const dockerSwitchCommand = kind === "codex"
    ? "docker compose exec -u goclaw goclaw codex logout && docker compose exec -u goclaw goclaw codex login --device-auth"
    : kind === "gemini"
    ? "docker compose exec -u goclaw goclaw gemini logout && docker compose exec -u goclaw goclaw gemini login"
    : "docker compose exec -u goclaw goclaw claude auth logout && docker compose exec -u goclaw goclaw claude auth login";

  const checkAuth = useCallback(() => {
    setLoading(true);
    http
      .get<CLIAuthStatus>(authPath)
      .then(setCliAuth)
      .catch(() => setCliAuth({ logged_in: false, error: "Failed to check auth status" }))
      .finally(() => setLoading(false));
  }, [http, authPath]);

  useEffect(() => {
    if (open) {
      checkAuth();
    } else {
      setCliAuth(null);
    }
  }, [open, checkAuth]);

  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">
        {t("cli.description")} <code className="rounded bg-muted px-1 py-0.5">{binary}</code> {t("cli.descriptionSuffix")}
      </p>
      {loading ? (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          {t("cli.checkingAuth")}
        </div>
      ) : cliAuth?.logged_in ? (
        <div className="space-y-2">
          <div className="flex items-center justify-between rounded-md border border-green-200 bg-green-50 px-3 py-2 dark:border-green-800 dark:bg-green-950">
            <div className="flex items-center gap-2">
              <CheckCircle2 className="h-4 w-4 text-green-600 dark:text-green-400" />
              <p className="text-sm text-green-700 dark:text-green-300">
                {t("cli.authenticatedAs")} <strong>{cliAuth.email}</strong>
                {cliAuth.subscription_type && (
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
            <summary className="cursor-pointer hover:text-foreground">{t("cli.switchAccount")}</summary>
            <div className="mt-1.5 space-y-1 rounded-md border bg-muted/50 px-3 py-2">
              <p>{t("cli.switchAccountInstructions")}</p>
              <code className="block rounded bg-muted px-2 py-1 font-mono">
                {cliAuth?.in_docker
                  ? dockerSwitchCommand
                  : switchCommand}
              </code>
              <p>{t("cli.switchAccountRecheck")} <RefreshCw className="inline h-3 w-3" /> {t("cli.switchAccountRecheckSuffix")}</p>
            </div>
          </details>
        </div>
      ) : cliAuth ? (
        <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 dark:border-amber-800 dark:bg-amber-950">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <AlertTriangle className="h-4 w-4 text-amber-600 dark:text-amber-400" />
              <p className="text-sm font-medium text-amber-700 dark:text-amber-300">{t("cli.notAuthenticated")}</p>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-7 px-2 text-amber-700 hover:text-amber-800 dark:text-amber-400 dark:hover:text-amber-300"
              onClick={checkAuth}
            >
              <RefreshCw className="h-3.5 w-3.5 mr-1" />
              <span className="text-xs">{t("cli.recheckButton")}</span>
            </Button>
          </div>
          <p className="mt-1 text-sm text-amber-600 dark:text-amber-400">
            {t("cli.runOnServer")}
          </p>
          <code className="mt-1 block rounded bg-amber-100 px-2 py-1 text-xs font-mono dark:bg-amber-900 dark:text-amber-300">
            {cliAuth.in_docker ? dockerLoginCommand : loginCommand}
          </code>
          {cliAuth.error && (
            <p className="mt-1 text-xs text-amber-500">{cliAuth.error}</p>
          )}
        </div>
      ) : null}
    </div>
  );
}
