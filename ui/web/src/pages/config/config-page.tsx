import { Settings, RefreshCw, ShieldAlert } from "lucide-react";
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
import { GatewaySection } from "./sections/gateway-section";
import { AgentsDefaultsSection } from "./sections/agents-defaults-section";
import { ToolsSection } from "./sections/tools-section";
import { SessionsSection } from "./sections/sessions-section";
import { TtsSection } from "./sections/tts-section";
import { CronSection } from "./sections/cron-section";
import { TelemetrySection } from "./sections/telemetry-section";
import { BindingsSection } from "./sections/bindings-section";
import { QuotaSection } from "./sections/quota-section";

export function ConfigPage() {
  const { config, hash, loading, saving, refresh, patch } = useConfig();
  const isMobile = useIsMobile();
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && !config);

  if (showSkeleton) {
    return (
      <div className="p-4 sm:p-6">
        <PageHeader title="Config" description="Gateway configuration" />
        <div className="mt-6">
          <DetailSkeleton />
        </div>
      </div>
    );
  }

  if (!config) {
    return (
      <div className="p-4 sm:p-6">
        <PageHeader title="Config" description="Gateway configuration" />
        <div className="mt-6">
          <EmptyState
            icon={Settings}
            title="No configuration"
            description="Could not load gateway configuration."
            action={
              <Button variant="outline" size="sm" onClick={refresh}>
                Retry
              </Button>
            }
          />
        </div>
      </div>
    );
  }

  return (
    <div className="p-4 sm:p-6">
      <PageHeader
        title="Config"
        description="Gateway configuration"
        actions={
          <div className="flex items-center gap-2">
            {hash && (
              <Badge variant="outline" className="font-mono text-xs">
                {hash.slice(0, 8)}
              </Badge>
            )}
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> Refresh
            </Button>
          </div>
        }
      />

      <div className="mt-4 flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2.5 text-sm text-amber-700 dark:text-amber-400">
        <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" />
        <span>
          This page manages gateway-level defaults. LLM providers and channels are managed on their
          dedicated pages. Secrets shown as <code className="rounded bg-muted px-1 font-mono text-xs">***</code> are
          stored encrypted in the database. Environment variables (if set) take highest precedence.
        </span>
      </div>

      <Tabs orientation={isMobile ? "horizontal" : "vertical"} defaultValue="general" className="mt-4 items-start">
        <TabsList
          variant={isMobile ? "default" : "line"}
          className={isMobile
            ? "w-full overflow-x-auto overflow-y-hidden"
            : "w-44 shrink-0 sticky top-6 rounded-lg border bg-card p-3 shadow-sm"
          }
        >
          <TabsTrigger value="general">General</TabsTrigger>
          <TabsTrigger value="quota">Quota</TabsTrigger>
          <TabsTrigger value="agents">Agents</TabsTrigger>
          <TabsTrigger value="sessions">Sessions</TabsTrigger>
          <TabsTrigger value="tools">Tools</TabsTrigger>
          <TabsTrigger value="advanced">Advanced</TabsTrigger>
        </TabsList>

        <TabsContent value="general" className="space-y-4">
          <GatewaySection
            data={config.gateway as any}
            onSave={(v) => patch({ gateway: v })}
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

        <TabsContent value="agents" className="space-y-4">
          <AgentsDefaultsSection
            data={config.agents as any}
            onSave={(v) => patch({ agents: v })}
            saving={saving}
          />
        </TabsContent>

        <TabsContent value="sessions" className="space-y-4">
          <SessionsSection
            data={config.sessions as any}
            onSave={(v) => patch({ sessions: v })}
            saving={saving}
          />
        </TabsContent>

        <TabsContent value="tools" className="space-y-4">
          <ToolsSection
            data={config.tools as any}
            onSave={(v) => patch({ tools: v })}
            saving={saving}
          />
        </TabsContent>

        <TabsContent value="advanced" className="space-y-4">
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
