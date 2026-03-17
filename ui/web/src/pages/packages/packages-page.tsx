import { useState } from "react";
import { useTranslation } from "react-i18next";
import { RefreshCw, Loader2, Trash2, Download, CheckCircle2, XCircle } from "lucide-react";
import { PageHeader } from "@/components/shared/page-header";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { Button } from "@/components/ui/button";
import { usePackages, type PackageInfo } from "./hooks/use-packages";
import { usePackageRuntimes } from "./hooks/use-package-runtimes";

type ActionStatus = "idle" | "loading" | "success" | "error";

export function PackagesPage() {
  const { t } = useTranslation("packages");
  const { packages, loading, refresh, installPackage, uninstallPackage } = usePackages();
  const { runtimes, loading: runtimesLoading, refresh: refreshRuntimes } = usePackageRuntimes();

  return (
    <div className="p-4 sm:p-6 space-y-6">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <Button
            variant="outline"
            size="sm"
            onClick={() => { refresh(); refreshRuntimes(); }}
            disabled={loading || runtimesLoading}
          >
            <RefreshCw className={`mr-2 h-4 w-4 ${loading || runtimesLoading ? "animate-spin" : ""}`} />
            {t("actions.refresh", { defaultValue: "Refresh" })}
          </Button>
        }
      />

      {/* Runtimes Section */}
      <section>
        <h2 className="text-lg font-medium mb-3">{t("runtimes.title")}</h2>
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3">
          {runtimes?.runtimes?.map((rt) => (
            <div
              key={rt.name}
              className={`rounded-lg border p-3 ${
                rt.available
                  ? "border-green-200 bg-green-50 dark:border-green-900/50 dark:bg-green-950/20"
                  : "border-red-200 bg-red-50 dark:border-red-900/50 dark:bg-red-950/20"
              }`}
            >
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">{rt.name}</span>
                {rt.available ? (
                  <CheckCircle2 className="h-4 w-4 text-green-600 dark:text-green-400" />
                ) : (
                  <XCircle className="h-4 w-4 text-red-600 dark:text-red-400" />
                )}
              </div>
              {rt.version && (
                <p className="text-xs text-muted-foreground mt-1 font-mono truncate">{rt.version}</p>
              )}
              {!rt.available && (
                <p className="text-xs text-red-600 dark:text-red-400 mt-1">{t("runtimes.missing")}</p>
              )}
            </div>
          ))}
        </div>
      </section>

      {/* Package Sections */}
      <PackageSection
        title={t("system.title")}
        placeholder={t("system.placeholder")}
        packages={packages?.system}
        loading={loading}
        onInstall={(pkg) => installPackage(pkg, t)}
        onUninstall={(pkg) => uninstallPackage(pkg, t)}
      />

      <PackageSection
        title={t("pip.title")}
        placeholder={t("pip.placeholder")}
        packages={packages?.pip}
        loading={loading}
        onInstall={(pkg) => installPackage(`pip:${pkg}`, t)}
        onUninstall={(pkg) => uninstallPackage(`pip:${pkg}`, t)}
      />

      <PackageSection
        title={t("npm.title")}
        placeholder={t("npm.placeholder")}
        packages={packages?.npm}
        loading={loading}
        onInstall={(pkg) => installPackage(`npm:${pkg}`, t)}
        onUninstall={(pkg) => uninstallPackage(`npm:${pkg}`, t)}
      />
    </div>
  );
}

interface PackageSectionProps {
  title: string;
  placeholder: string;
  packages: PackageInfo[] | null | undefined;
  loading: boolean;
  onInstall: (pkg: string) => Promise<unknown>;
  onUninstall: (pkg: string) => Promise<unknown>;
}

function PackageSection({ title, placeholder, packages, loading, onInstall, onUninstall }: PackageSectionProps) {
  const { t } = useTranslation("packages");
  const [input, setInput] = useState("");
  const [installStatus, setInstallStatus] = useState<ActionStatus>("idle");
  const [actionStatuses, setActionStatuses] = useState<Record<string, ActionStatus>>({});
  const [uninstallTarget, setUninstallTarget] = useState<string | null>(null);

  async function handleInstall() {
    const pkg = input.trim();
    if (!pkg) return;
    setInstallStatus("loading");
    try {
      await onInstall(pkg);
      setInstallStatus("success");
      setInput("");
      setTimeout(() => setInstallStatus("idle"), 2000);
    } catch {
      setInstallStatus("error");
      setTimeout(() => setInstallStatus("idle"), 3000);
    }
  }

  async function handleUninstall(name: string) {
    setActionStatuses((s) => ({ ...s, [name]: "loading" }));
    try {
      await onUninstall(name);
      setActionStatuses((s) => ({ ...s, [name]: "success" }));
    } catch {
      setActionStatuses((s) => ({ ...s, [name]: "error" }));
      setTimeout(() => setActionStatuses((s) => ({ ...s, [name]: "idle" })), 3000);
    }
  }

  return (
    <section>
      <h2 className="text-lg font-medium mb-3">{title}</h2>

      {/* Install input */}
      <div className="flex gap-2 mb-3">
        <input
          type="text"
          className="flex-1 rounded-md border border-input bg-background px-3 py-2 text-base md:text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          placeholder={placeholder}
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && handleInstall()}
          disabled={installStatus === "loading"}
        />
        <Button
          size="sm"
          onClick={handleInstall}
          disabled={!input.trim() || installStatus === "loading"}
          className="h-auto"
        >
          {installStatus === "loading" ? (
            <Loader2 className="mr-1.5 h-4 w-4 animate-spin" />
          ) : (
            <Download className="mr-1.5 h-4 w-4" />
          )}
          {installStatus === "loading" ? t("actions.installing") : t("actions.install")}
        </Button>
      </div>

      {/* Package table */}
      <div className="overflow-x-auto">
        <table className="w-full min-w-[400px] text-sm">
          <thead>
            <tr className="border-b">
              <th className="text-left py-2 px-3 font-medium text-muted-foreground">{t("table.name")}</th>
              <th className="text-left py-2 px-3 font-medium text-muted-foreground">{t("table.version")}</th>
              <th className="text-right py-2 px-3 font-medium text-muted-foreground">{t("table.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {loading && !packages ? (
              <tr>
                <td colSpan={3} className="py-8 text-center text-muted-foreground">
                  <Loader2 className="h-5 w-5 animate-spin mx-auto" />
                </td>
              </tr>
            ) : !packages?.length ? (
              <tr>
                <td colSpan={3} className="py-6 text-center text-muted-foreground text-sm">
                  {t("table.empty")}
                </td>
              </tr>
            ) : (
              packages.map((pkg) => {
                const status = actionStatuses[pkg.name] ?? "idle";
                return (
                  <tr key={pkg.name} className="border-b last:border-0 hover:bg-muted/50 transition-colors">
                    <td className="py-2 px-3 font-mono text-sm">{pkg.name}</td>
                    <td className="py-2 px-3 text-muted-foreground font-mono text-sm">{pkg.version}</td>
                    <td className="py-2 px-3 text-right">
                      {status === "success" ? (
                        <CheckCircle2 className="h-4 w-4 text-green-500 inline" />
                      ) : (
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 px-2 text-destructive hover:text-destructive hover:bg-destructive/10"
                          onClick={() => setUninstallTarget(pkg.name)}
                          disabled={status === "loading"}
                        >
                          {status === "loading" ? (
                            <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          ) : (
                            <Trash2 className="h-3.5 w-3.5" />
                          )}
                        </Button>
                      )}
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>

      <ConfirmDialog
        open={!!uninstallTarget}
        onOpenChange={() => setUninstallTarget(null)}
        title={t("confirmUninstall.title")}
        description={t("confirmUninstall.description", { name: uninstallTarget })}
        confirmLabel={t("actions.uninstall")}
        variant="destructive"
        onConfirm={async () => {
          if (uninstallTarget) {
            await handleUninstall(uninstallTarget);
            setUninstallTarget(null);
          }
        }}
      />
    </section>
  );
}
