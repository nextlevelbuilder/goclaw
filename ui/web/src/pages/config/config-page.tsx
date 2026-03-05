import { useState, useEffect } from "react";
import { Settings, Save, RefreshCw, AlertCircle, ShieldAlert, ArrowRight } from "lucide-react";
import { Link } from "react-router";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { Card, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { DetailSkeleton } from "@/components/shared/loading-skeleton";
import { useConfig } from "./hooks/use-config";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useIsMobile } from "@/hooks/use-media-query";
import { ROUTES } from "@/lib/constants";
import { GatewaySection } from "./sections/gateway-section";
import { ProvidersSection } from "./sections/providers-section";
import { AgentsDefaultsSection } from "./sections/agents-defaults-section";
import { ToolsSection } from "./sections/tools-section";
import { ChannelsSection } from "./sections/channels-section";
import { SessionsSection } from "./sections/sessions-section";
import { TtsSection } from "./sections/tts-section";
import { CronSection } from "./sections/cron-section";
import { TelemetrySection } from "./sections/telemetry-section";
import { BindingsSection } from "./sections/bindings-section";
import { QuotaSection } from "./sections/quota-section";

export function ConfigPage() {
  const { config, hash, configPath, loading, saving, error, refresh, applyRaw, patch } = useConfig();
  const isMobile = useIsMobile();
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && !config);
  const [rawText, setRawText] = useState("");
  const [dirty, setDirty] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  useEffect(() => {
    if (config) {
      const text = JSON.stringify(config, null, 2);
      setRawText(text);
      setDirty(false);
      setSaveError(null);
    }
  }, [config]);

  const handleRawSave = async () => {
    setSaveError(null);
    try {
      await applyRaw(rawText);
      setDirty(false);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "Failed to save");
    }
  };

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

  const isManaged = (config.database as any)?.mode === "managed";

  return (
    <div className="p-4 sm:p-6">
      <PageHeader
        title="Config"
        description="Gateway configuration"
        actions={
          <div className="flex items-center gap-2">
            {configPath && (
              <span className="text-xs text-muted-foreground">{configPath}</span>
            )}
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
          API keys and tokens are managed via environment variables and are not shown here.
          Fields displaying <code className="rounded bg-muted px-1 font-mono text-xs">***</code> are
          read-only secrets — edit them in your <code className="rounded bg-muted px-1 font-mono text-xs">.env.local</code> file
          or server environment.
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
          {isManaged && <TabsTrigger value="quota">Quota</TabsTrigger>}
          <TabsTrigger value="agents">Agents</TabsTrigger>
          <TabsTrigger value="tools">Tools</TabsTrigger>
          <TabsTrigger value="connections">Connections</TabsTrigger>
          <TabsTrigger value="advanced">Advanced</TabsTrigger>
          {!isMobile && <div className="my-2 h-px w-full bg-border" />}
          <TabsTrigger value="raw">Raw</TabsTrigger>
        </TabsList>

        <TabsContent value="general" className="space-y-4">
          <GatewaySection
            data={config.gateway as any}
            onSave={(v) => patch({ gateway: v })}
            saving={saving}
          />
        </TabsContent>

        {isManaged && (
          <TabsContent value="quota" className="space-y-4">
            <QuotaSection
              data={config.gateway as any}
              onSave={(v) => patch({ gateway: v })}
              saving={saving}
            />
          </TabsContent>
        )}

        <TabsContent value="agents" className="space-y-4">
          {isManaged ? (
            <ManagedRedirect
              title="LLM Providers"
              description="Managed via the Providers page in managed mode."
              to={ROUTES.PROVIDERS}
            />
          ) : (
            <ProvidersSection
              data={config.providers as any}
              onSave={(v) => patch({ providers: v })}
              saving={saving}
            />
          )}
          <AgentsDefaultsSection
            data={config.agents as any}
            onSave={(v) => patch({ agents: v })}
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

        <TabsContent value="connections" className="space-y-4">
          <SessionsSection
            data={config.sessions as any}
            onSave={(v) => patch({ sessions: v })}
            saving={saving}
          />
          {isManaged ? (
            <ManagedRedirect
              title="Channels"
              description="Managed via the Channels page in managed mode."
              to={ROUTES.CHANNELS}
            />
          ) : (
            <ChannelsSection
              data={config.channels as any}
              onSave={(v) => patch({ channels: v })}
              saving={saving}
            />
          )}
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

        <TabsContent value="raw">
          <div className="space-y-3">
            <Textarea
              value={rawText}
              onChange={(e) => {
                setRawText(e.target.value);
                setDirty(true);
              }}
              className="min-h-[500px] font-mono text-sm"
              placeholder="JSON configuration..."
            />

            {(saveError || error) && (
              <div className="flex items-center gap-2 rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                <AlertCircle className="h-4 w-4" />
                {saveError || error}
              </div>
            )}

            <div className="flex items-center gap-2">
              <Button
                onClick={handleRawSave}
                disabled={!dirty || saving}
                className="gap-1"
              >
                <Save className="h-3.5 w-3.5" />
                {saving ? "Saving..." : "Save"}
              </Button>
              {dirty && (
                <span className="text-xs text-muted-foreground">Unsaved changes</span>
              )}
            </div>
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}

/** Compact redirect card shown in managed mode for sections that have dedicated pages. */
function ManagedRedirect({ title, description, to }: { title: string; description: string; to: string }) {
  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="text-base">{title}</CardTitle>
            <CardDescription>{description}</CardDescription>
          </div>
          <Button variant="outline" size="sm" className="gap-1.5 shrink-0" asChild>
            <Link to={to}>
              Manage <ArrowRight className="h-3.5 w-3.5" />
            </Link>
          </Button>
        </div>
      </CardHeader>
    </Card>
  );
}
