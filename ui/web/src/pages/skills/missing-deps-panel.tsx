import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Download, Loader2, AlertTriangle, CheckCircle2, XCircle } from "lucide-react";
import type { RuntimeStatus } from "./hooks/use-runtimes";

interface MissingDepsPanelProps {
  missing: string[];
  onInstallItem: (dep: string) => Promise<unknown>;
  runtimes?: RuntimeStatus | null;
}

type ItemStatus = "idle" | "installing" | "success" | "error";

export function MissingDepsPanel({ missing, onInstallItem, runtimes }: MissingDepsPanelProps) {
  const { t } = useTranslation("skills");
  const [itemStatus, setItemStatus] = useState<Record<string, ItemStatus>>({});

  const runtimesReady = runtimes?.ready ?? true;
  const missingRuntimes = runtimes?.runtimes?.filter((r) => !r.available) ?? [];

  const system = missing.filter((d) => !d.includes(":"));
  const pip = missing.filter((d) => d.startsWith("pip:")).map((d) => d.slice(4));
  const npm = missing.filter((d) => d.startsWith("npm:")).map((d) => d.slice(4));

  if (missing.length === 0 && runtimesReady) return null;

  async function handleInstall(dep: string) {
    setItemStatus((s) => ({ ...s, [dep]: "installing" }));
    try {
      await onInstallItem(dep);
      setItemStatus((s) => ({ ...s, [dep]: "success" }));
    } catch {
      setItemStatus((s) => ({ ...s, [dep]: "error" }));
    }
  }

  function renderDepRow(dep: string, label: string) {
    const status = itemStatus[dep] ?? "idle";
    return (
      <div key={dep} className="flex items-center justify-between gap-2 py-1 px-2 -mx-2 rounded hover:bg-amber-100/60 dark:hover:bg-amber-900/20 transition-colors">
        <span className="text-xs text-amber-700 dark:text-amber-300 font-mono">{label}</span>
        <div className="flex items-center gap-1.5 shrink-0">
          {status === "success" && <CheckCircle2 className="h-3.5 w-3.5 text-green-500" />}
          {status === "error" && <XCircle className="h-3.5 w-3.5 text-red-500" />}
          {status !== "success" && (
            <Button
              size="sm"
              variant="ghost"
              className="h-6 px-2 text-xs border border-amber-300 text-amber-800 hover:bg-amber-100 dark:border-amber-700 dark:text-amber-200 dark:hover:bg-amber-900/50"
              onClick={() => handleInstall(dep)}
              disabled={status === "installing" || !runtimesReady}
              title={!runtimesReady ? t("deps.runtimeRequired") : undefined}
            >
              {status === "installing" ? (
                <Loader2 className="h-3 w-3 animate-spin" />
              ) : (
                <>
                  <Download className="mr-1 h-3 w-3" />
                  {t("deps.installItem")}
                </>
              )}
            </Button>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-3 mb-4">
      {/* Runtime prerequisites warning */}
      {!runtimesReady && (
        <div className="rounded-lg border border-red-200 bg-red-50 dark:border-red-900/50 dark:bg-red-950/30 p-4">
          <div className="flex items-start gap-3">
            <AlertTriangle className="h-4 w-4 text-red-600 dark:text-red-400 mt-0.5 shrink-0" />
            <div className="space-y-1">
              <h3 className="text-sm font-medium text-red-800 dark:text-red-200">
                {t("deps.runtimeMissing")}
              </h3>
              <p className="text-xs text-red-700 dark:text-red-300">
                {t("deps.runtimeMissingDesc")}
              </p>
              <div className="flex flex-wrap gap-1.5 mt-2">
                {missingRuntimes.map((r) => (
                  <span
                    key={r.name}
                    className="inline-flex items-center rounded-md bg-red-100 dark:bg-red-900/40 px-2 py-0.5 text-xs font-medium text-red-700 dark:text-red-300"
                  >
                    {r.name}
                  </span>
                ))}
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Missing package dependencies — only show when runtimes are ready (installing deps without runtime is pointless) */}
      {missing.length > 0 && runtimesReady && (
        <div className="rounded-lg border border-amber-200 bg-amber-50 dark:border-amber-900/50 dark:bg-amber-950/30 p-4">
          <h3 className="text-sm font-medium text-amber-800 dark:text-amber-200 mb-2">
            {t("deps.missingTitle")}
          </h3>
          <div className="space-y-1">
            {system.length > 0 && (
              <div>
                <p className="text-xs font-medium text-amber-700 dark:text-amber-400 mb-0.5">
                  {t("deps.systemLabel")}
                </p>
                {system.map((pkg) => renderDepRow(pkg, pkg))}
              </div>
            )}
            {pip.length > 0 && (
              <div>
                <p className="text-xs font-medium text-amber-700 dark:text-amber-400 mb-0.5">
                  {t("deps.pythonLabel")}
                </p>
                {pip.map((pkg) => renderDepRow(`pip:${pkg}`, pkg))}
              </div>
            )}
            {npm.length > 0 && (
              <div>
                <p className="text-xs font-medium text-amber-700 dark:text-amber-400 mb-0.5">
                  {t("deps.nodeLabel")}
                </p>
                {npm.map((pkg) => renderDepRow(`npm:${pkg}`, pkg))}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
