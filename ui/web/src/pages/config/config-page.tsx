import { Settings, RefreshCw, ShieldAlert } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { DetailSkeleton } from "@/components/shared/loading-skeleton";
import { useConfig } from "./hooks/use-config";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useIsMobile } from "@/hooks/use-media-query";
import { ServerSection } from "./sections/server-section";
import { BehaviorSection } from "./sections/behavior-section";
import { AiDefaultsSection } from "./sections/ai-defaults-section";
import { QuotaSection } from "./sections/quota-section";
import { ToolsProfileSection } from "./sections/tools-profile-section";
import { ToolsExecSection } from "./sections/tools-exec-section";
import { ToolsWebSection } from "./sections/tools-web-section";
import { ShellSecuritySection } from "./sections/shell-security-section";
import { TtsSection } from "./sections/tts-section";
import { CronSection } from "./sections/cron-section";
import { TelemetrySection } from "./sections/telemetry-section";
import { BindingsSection } from "./sections/bindings-section";

export function ConfigPage() {
  const { t } = useTranslation("config");
  const { config, hash, loading, saving, refresh, patch } = useConfig();
  const isMobile = useIsMobile();
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && !config);

  if (showSkeleton) {
    return (
      <div className="p-4 sm:p-6 pb-10">
        <PageHeader title={t("title")} description={t("description")} />
        <div className="mt-6">
          <DetailSkeleton />
        </div>
      </div>
    );
  }

  if (!config) {
    return (
      <div className="p-4 sm:p-6 pb-10">
        <PageHeader title={t("title")} description={t("description")} />
        <div className="mt-6">
          <EmptyState
            icon={Settings}
            title={t("noConfig")}
            description={t("noConfigDescription")}
            action={
              <Button variant="outline" size="sm" onClick={refresh}>
                {t("retry")}
              </Button>
            }
          />
        </div>
      </div>
    );
  }

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex items-center gap-2">
            {hash && (
              <Badge variant="outline" className="font-mono text-xs">
                {hash.slice(0, 8)}
              </Badge>
            )}
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {t("common:refresh", "Refresh")}
            </Button>
          </div>
        }
      />

      <div className="mt-4 flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2.5 text-sm text-amber-700 dark:text-amber-400">
        <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" />
        <span>{t("warning")}</span>
      </div>

      <Tabs orientation={isMobile ? "horizontal" : "vertical"} defaultValue="server" className="mt-4 items-start">
        <TabsList
          variant={isMobile ? "default" : "line"}
          className={isMobile
            ? "w-full overflow-x-auto overflow-y-hidden"
            : "w-44 shrink-0 sticky top-6 rounded-lg border bg-card p-3 shadow-sm"
          }
        >
          <TabsTrigger value="server">{t("tabs.server")}</TabsTrigger>
          <TabsTrigger value="behavior">{t("tabs.behavior")}</TabsTrigger>
          <TabsTrigger value="aiDefaults">{t("tabs.aiDefaults")}</TabsTrigger>
          <TabsTrigger value="quota">{t("tabs.quota")}</TabsTrigger>
          <TabsTrigger value="tools">{t("tabs.tools")}</TabsTrigger>
          <TabsTrigger value="integrations">{t("tabs.integrations")}</TabsTrigger>
        </TabsList>

        <TabsContent value="server" className="space-y-4">
          <ServerSection
            data={config.gateway as any}
            onSave={(v) => patch({ gateway: v })}
            saving={saving}
          />
        </TabsContent>

        <TabsContent value="behavior" className="space-y-4">
          <BehaviorSection
            config={config as any}
            onPatch={patch}
            saving={saving}
          />
        </TabsContent>

        <TabsContent value="aiDefaults" className="space-y-4">
          <AiDefaultsSection
            data={config.agents as any}
            onSave={(v) => patch({ agents: v })}
            saving={saving}
          />
        </TabsContent>

        <TabsContent value="quota" className="space-y-4">
          <QuotaSection
            data={config.gateway as any}
            onSave={(v) => patch({ gateway: v })}
            saving={saving}
          />
        </TabsContent>

        <TabsContent value="tools" className="space-y-4">
          <ToolsProfileSection
            data={config.tools as any}
            onSave={(v) => patch({ tools: v })}
            saving={saving}
          />
          <ToolsExecSection
            data={config.tools as any}
            onSave={(v) => patch({ tools: v })}
            saving={saving}
          />
          <ToolsWebSection
            data={config.tools as any}
            onSave={(v) => patch({ tools: v })}
            saving={saving}
          />
          <ShellSecuritySection
            data={config.tools as any}
            onSave={(v) => patch({ tools: v })}
            saving={saving}
          />
        </TabsContent>

        <TabsContent value="integrations" className="space-y-4">
          <TtsSection data={config.tts as any} />
          <CronSection
            data={config.cron as any}
            onSave={(v) => patch({ cron: v })}
            saving={saving}
          />
          <TelemetrySection
            data={config.telemetry as any}
            onSave={(v) => patch({ telemetry: v })}
            saving={saving}
          />
          <BindingsSection
            data={config.bindings as any}
            onSave={(v) => patch({ bindings: v })}
            saving={saving}
          />
        </TabsContent>
      </Tabs>
    </div>
  );
}
