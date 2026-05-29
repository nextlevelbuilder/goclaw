import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Loader2, Download, Trash2, CheckCircle2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { usePackages, type PackageInfo } from "../hooks/use-packages";

type ActionStatus = "idle" | "loading" | "success" | "error";

export function PythonPackagesTab() {
  const { t } = useTranslation("packages");
  const { packages, loading, installPackage, uninstallPackage } = usePackages();

  return (
    <PackageSectionBody
      title={t("pip.title")}
      placeholder={t("pip.placeholder")}
      packages={packages?.pip}
      loading={loading}
      onInstall={(pkg) => installPackage(`pip:${pkg}`, t)}
      onUninstall={(pkg) => uninstallPackage(`pip:${pkg}`, t)}
    />
  );
}

interface PackageSectionBodyProps {
  title: string;
  placeholder: string;
  packages: PackageInfo[] | null | undefined;
  loading: boolean;
  onInstall: (pkg: string) => Promise<{ ok: boolean }>;
  onUninstall: (pkg: string) => Promise<{ ok: boolean }>;
}

function PackageSectionBody({ title, placeholder, packages, loading, onInstall, onUninstall }: PackageSectionBodyProps) {
  const { t } = useTranslation("packages");
  const [input, setInput] = useState("");
  const [installStatus, setInstallStatus] = useState<ActionStatus>("idle");
  const [actionStatuses, setActionStatuses] = useState<Record<string, ActionStatus>>({});
  const [uninstallTarget, setUninstallTarget] = useState<string | null>(null);

  async function handleInstall() {
    const pkg = input.trim();
    if (!pkg) return;
    setInstallStatus("loading");
    const res = await onInstall(pkg);
    if (res.ok) {
      setInstallStatus("success");
      setInput("");
      setTimeout(() => setInstallStatus("idle"), 2000);
    } else {
      setInstallStatus("error");
      setTimeout(() => setInstallStatus("idle"), 3000);
    }
  }

  async function handleUninstall(name: string) {
    setActionStatuses((s) => ({ ...s, [name]: "loading" }));
    const res = await onUninstall(name);
    if (res.ok) {
      setActionStatuses((s) => ({ ...s, [name]: "success" }));
      setTimeout(() => setActionStatuses((s) => ({ ...s, [name]: "idle" })), 2000);
    } else {
      setActionStatuses((s) => ({ ...s, [name]: "error" }));
      setTimeout(() => setActionStatuses((s) => ({ ...s, [name]: "idle" })), 3000);
    }
  }

  return (
    <section>
      <h2 className="text-lg font-medium mb-3">{title}</h2>

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
        <Button size="sm" onClick={handleInstall} disabled={!input.trim() || installStatus === "loading"} className="h-auto">
          {installStatus === "loading" ? <Loader2 className="mr-1.5 h-4 w-4 animate-spin" /> : <Download className="mr-1.5 h-4 w-4" />}
          {installStatus === "loading" ? t("actions.installing") : t("actions.install")}
        </Button>
      </div>

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
              <tr><td colSpan={3} className="py-8 text-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin mx-auto" /></td></tr>
            ) : !packages?.length ? (
              <tr><td colSpan={3} className="py-6 text-center text-muted-foreground text-sm">{t("table.empty")}</td></tr>
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
                          variant="ghost" size="sm"
                          className="h-7 px-2 text-destructive hover:text-destructive hover:bg-destructive/10"
                          onClick={() => setUninstallTarget(pkg.name)}
                          disabled={status === "loading"}
                        >
                          {status === "loading" ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
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
