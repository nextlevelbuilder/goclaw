import { lazy, Suspense } from "react";
import { useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import { RefreshCw } from "lucide-react";
import { PageHeader } from "@/components/shared/page-header";
import { ErrorBoundary } from "@/components/shared/error-boundary";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { useAuthStore } from "@/stores/use-auth-store";
import { usePackages } from "./hooks/use-packages";
import { usePackageRuntimes } from "./hooks/use-package-runtimes";
import { RuntimesStickyHeader } from "./runtimes-sticky-header";

// --- Lazy tab bodies (each is a separate chunk) ---
const SystemPackagesTab = lazy(() =>
  import("./tabs/system-packages-tab").then((m) => ({ default: m.SystemPackagesTab }))
);
const PythonPackagesTab = lazy(() =>
  import("./tabs/python-packages-tab").then((m) => ({ default: m.PythonPackagesTab }))
);
const NodePackagesTab = lazy(() =>
  import("./tabs/node-packages-tab").then((m) => ({ default: m.NodePackagesTab }))
);
const GithubBinariesTab = lazy(() =>
  import("./tabs/github-binaries-tab").then((m) => ({ default: m.GithubBinariesTab }))
);
const CliCredentialsTab = lazy(() =>
  import("./tabs/cli-credentials-tab").then((m) => ({ default: m.CliCredentialsTab }))
);

// --- Permission helper (mirrors require-role.tsx logic) ---
function hasMinRole(role: string, minRole: string): boolean {
  const levels: Record<string, number> = { owner: 4, admin: 3, operator: 2, viewer: 1 };
  return (levels[role] ?? 0) >= (levels[minRole] ?? 0);
}

// --- Valid tab ids ---
const VALID_TABS = ["system", "python", "node", "github", "cli-credentials"] as const;
type TabId = (typeof VALID_TABS)[number];

function isValidTab(v: string | null): v is TabId {
  return VALID_TABS.includes(v as TabId);
}

// --- Tab fallback skeleton ---
function TabLoader() {
  return (
    <div className="py-8 flex justify-center text-muted-foreground">
      <img src="/goclaw-icon.svg" alt="" className="h-6 w-6 animate-pulse opacity-40" />
    </div>
  );
}

export function PackagesPage() {
  const { t } = useTranslation("packages");
  const [searchParams, setSearchParams] = useSearchParams();
  const { refresh } = usePackages();
  const { refresh: refreshRuntimes } = usePackageRuntimes();
  const role = useAuthStore((s) => s.role);
  const isAdmin = hasMinRole(role, "admin");

  // Validate tab param — fall back to "system" for unknown values
  const rawTab = searchParams.get("tab");
  const activeTab: TabId =
    isValidTab(rawTab)
      ? // Non-admin trying to reach cli-credentials directly via URL → fall back
        rawTab === "cli-credentials" && !isAdmin
        ? "system"
        : rawTab
      : "system";

  function handleTabChange(next: string) {
    // Functional form preserves any other existing query params
    setSearchParams((prev) => {
      const updated = new URLSearchParams(prev);
      updated.set("tab", next);
      return updated;
    });
  }

  return (
    <div className="p-4 sm:p-6 space-y-4">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <Button
            variant="outline"
            size="sm"
            onClick={() => { refresh(); refreshRuntimes(); }}
          >
            <RefreshCw className="mr-2 h-4 w-4" />
            {t("actions.refresh", { defaultValue: "Refresh" })}
          </Button>
        }
      />

      {/* Runtimes always-visible strip */}
      <RuntimesStickyHeader />

      {/* Tabs */}
      <Tabs value={activeTab} onValueChange={handleTabChange}>
        {/* Tab list — horizontal scroll on mobile */}
        <div className="overflow-x-auto">
          <TabsList className="whitespace-nowrap w-auto">
            <TabsTrigger value="system">{t("tabs.system", { defaultValue: "System" })}</TabsTrigger>
            <TabsTrigger value="python">{t("tabs.python", { defaultValue: "Python" })}</TabsTrigger>
            <TabsTrigger value="node">{t("tabs.node", { defaultValue: "Node" })}</TabsTrigger>
            <TabsTrigger value="github">{t("tabs.github", { defaultValue: "GitHub" })}</TabsTrigger>
            {/* CLI Credentials tab: visible only to admins */}
            {isAdmin && (
              <TabsTrigger value="cli-credentials">
                {t("tabs.cliCredentials", { defaultValue: "CLI Credentials" })}
              </TabsTrigger>
            )}
          </TabsList>
        </div>

        {/* Tab bodies — each isolated in its own ErrorBoundary */}
        <TabsContent value="system">
          <ErrorBoundary key="tab-system">
            <Suspense fallback={<TabLoader />}>
              <SystemPackagesTab />
            </Suspense>
          </ErrorBoundary>
        </TabsContent>

        <TabsContent value="python">
          <ErrorBoundary key="tab-python">
            <Suspense fallback={<TabLoader />}>
              <PythonPackagesTab />
            </Suspense>
          </ErrorBoundary>
        </TabsContent>

        <TabsContent value="node">
          <ErrorBoundary key="tab-node">
            <Suspense fallback={<TabLoader />}>
              <NodePackagesTab />
            </Suspense>
          </ErrorBoundary>
        </TabsContent>

        <TabsContent value="github">
          <ErrorBoundary key="tab-github">
            <Suspense fallback={<TabLoader />}>
              <GithubBinariesTab />
            </Suspense>
          </ErrorBoundary>
        </TabsContent>

        {/* CLI Credentials: gate rendered body — direct URL by non-admin must NOT reach panel */}
        <TabsContent value="cli-credentials">
          <ErrorBoundary key="tab-cli-credentials">
            <Suspense fallback={<TabLoader />}>
              {isAdmin ? (
                <CliCredentialsTab />
              ) : (
                <div className="py-8 text-center text-sm text-muted-foreground">
                  {t("tabs.adminOnly", { defaultValue: "Admin access required." })}
                </div>
              )}
            </Suspense>
          </ErrorBoundary>
        </TabsContent>
      </Tabs>
    </div>
  );
}
