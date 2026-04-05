import { useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { AgentExportPanel } from "./agent-export-panel";
import { AgentImportPanel } from "./agent-import-panel";
import { TeamExportPanel } from "./team-export-panel";
import { TeamImportPanel } from "./team-import-panel";
import { CapabilitiesExportPanel } from "./capabilities-export-panel";
import { CapabilitiesImportPanel } from "./capabilities-import-panel";
import { useTeams } from "@/pages/teams/hooks/use-teams";

export function ImportExportPage() {
  const { t } = useTranslation("import-export");
  const [params, setParams] = useSearchParams();
  const { teams, loading: teamsLoading, load: loadTeams } = useTeams();

  const scopeTab = params.get("tab") ?? "teams";
  const innerTab = params.get("inner") ?? "export";

  const setScopeTab = (v: string) => {
    const next = new URLSearchParams(params);
    next.set("tab", v);
    setParams(next, { replace: true });
  };

  const setInnerTab = (v: string) => {
    const next = new URLSearchParams(params);
    next.set("inner", v);
    setParams(next, { replace: true });
  };

  return (
    <div className="p-4 sm:p-6 pb-10 space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">
            {t("title")} <Badge variant="warning" className="ml-1 align-middle text-xs">{t("beta")}</Badge>
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">{t("description")}</p>
        </div>
      </div>

      {/* Beta warning */}
      <div className="mx-auto max-w-3xl flex items-start gap-2.5 rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900/40 dark:bg-amber-950/20 dark:text-amber-300">
        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
        <span>{t("betaWarning")}</span>
      </div>

      {/* Scope tabs */}
      <div className="mx-auto max-w-3xl">
        <Tabs value={scopeTab} onValueChange={setScopeTab}>
          <TabsList>
            <TabsTrigger value="teams">{t("tabs.teams")}</TabsTrigger>
            <TabsTrigger value="agents">{t("tabs.agents")}</TabsTrigger>
            <TabsTrigger value="skills-mcp">{t("tabs.skillsMcp")}</TabsTrigger>
          </TabsList>

          {/* Teams */}
          <TabsContent value="teams" className="mt-4">
            <Tabs value={innerTab} onValueChange={setInnerTab}>
              <TabsList>
                <TabsTrigger value="export">{t("tabs.export")}</TabsTrigger>
                <TabsTrigger value="import">{t("tabs.import")}</TabsTrigger>
              </TabsList>
              <TabsContent value="export" className="mt-4">
                <TeamExportPanel teams={teams} loading={teamsLoading} loadTeams={loadTeams} />
              </TabsContent>
              <TabsContent value="import" className="mt-4">
                <TeamImportPanel />
              </TabsContent>
            </Tabs>
          </TabsContent>

          {/* Agents */}
          <TabsContent value="agents" className="mt-4">
            <Tabs value={innerTab} onValueChange={setInnerTab}>
              <TabsList>
                <TabsTrigger value="export">{t("tabs.export")}</TabsTrigger>
                <TabsTrigger value="import">{t("tabs.import")}</TabsTrigger>
              </TabsList>
              <TabsContent value="export" className="mt-4">
                <AgentExportPanel />
              </TabsContent>
              <TabsContent value="import" className="mt-4">
                <AgentImportPanel />
              </TabsContent>
            </Tabs>
          </TabsContent>

          {/* Skills & MCP */}
          <TabsContent value="skills-mcp" className="mt-4">
            <Tabs value={innerTab} onValueChange={setInnerTab}>
              <TabsList>
                <TabsTrigger value="export">{t("tabs.export")}</TabsTrigger>
                <TabsTrigger value="import">{t("tabs.import")}</TabsTrigger>
              </TabsList>
              <TabsContent value="export" className="mt-4">
                <CapabilitiesExportPanel />
              </TabsContent>
              <TabsContent value="import" className="mt-4">
                <CapabilitiesImportPanel />
              </TabsContent>
            </Tabs>
          </TabsContent>
        </Tabs>
      </div>
    </div>
  );
}
