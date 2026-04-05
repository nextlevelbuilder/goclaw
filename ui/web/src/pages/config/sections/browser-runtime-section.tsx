import { useState, useEffect } from "react";
import { Save, Monitor, Container, Globe, Network, Circle } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { InfoLabel } from "@/components/shared/info-label";
import { useHttp } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { cn } from "@/lib/utils";

/* eslint-disable @typescript-eslint/no-explicit-any */
type ToolsData = Record<string, any>;

interface Props {
  data: ToolsData | undefined;
  onSave: (value: ToolsData) => Promise<void>;
  saving: boolean;
}

interface BrowserStatus {
  running: boolean;
  tabs?: number;
  engine?: string;
  headless?: boolean;
}

const MODES = ["host", "docker", "remote", "k8s"] as const;
type Mode = (typeof MODES)[number];

const MODE_ICONS: Record<Mode, typeof Monitor> = {
  host: Monitor,
  docker: Container,
  remote: Globe,
  k8s: Network,
};

export function BrowserRuntimeSection({ data, onSave, saving }: Props) {
  const { t } = useTranslation("config");
  const http = useHttp();
  const connected = useAuthStore((s) => s.connected);
  const [draft, setDraft] = useState<ToolsData>(data ?? {});
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(data ?? {});
    setDirty(false);
  }, [data]);

  const browser = draft.browser ?? {};

  // Resolve current mode from draft
  const resolvedMode: Mode = (() => {
    if (browser.mode) return browser.mode as Mode;
    if (browser.remote_url) return "remote";
    if (browser.container_image) return "docker";
    return "host";
  })();

  const updateBrowser = (patch: Record<string, any>) => {
    setDraft((prev) => ({
      ...prev,
      browser: { ...(prev.browser ?? {}), ...patch },
    }));
    setDirty(true);
  };

  const setMode = (m: Mode) => {
    updateBrowser({ mode: m });
  };

  // Runtime status
  const { data: status } = useQuery({
    queryKey: ["browser", "status"],
    queryFn: () => http.get<BrowserStatus>("/browser/status"),
    refetchInterval: 5000,
    enabled: connected && browser.enabled !== false,
  });

  if (!data) return null;

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="text-base">{t("tools.browserRuntime")}</CardTitle>
            <CardDescription>{t("tools.browser")}</CardDescription>
          </div>
          {/* Runtime status badge */}
          {browser.enabled !== false && status && (
            <div className="flex items-center gap-3 text-sm">
              <div className="flex items-center gap-1.5">
                <Circle
                  className={cn(
                    "h-2.5 w-2.5 fill-current",
                    status.running ? "text-emerald-500" : "text-muted-foreground/40",
                  )}
                />
                <span className="text-muted-foreground">
                  {status.running ? t("tools.browserStatusRunning") : t("tools.browserStatusStopped")}
                </span>
              </div>
              {status.running && status.engine && (
                <span className="text-xs text-muted-foreground">{status.engine}</span>
              )}
              {status.running && status.tabs !== undefined && (
                <span className="text-xs text-muted-foreground">
                  {t("tools.browserTabs")}: {status.tabs}
                </span>
              )}
            </div>
          )}
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Enabled toggle */}
        <div className="flex items-center gap-2">
          <Label>{t("tools.browserEnabled")}</Label>
          <Switch
            checked={browser.enabled !== false}
            onCheckedChange={(v) => updateBrowser({ enabled: v })}
          />
        </div>

        <Separator />

        {/* Mode selector */}
        <div className="space-y-2">
          <Label>{t("tools.browserMode")}</Label>
          <div className="flex flex-wrap gap-1">
            {MODES.map((m) => {
              const Icon = MODE_ICONS[m];
              const label = t(`tools.browserMode${m.charAt(0).toUpperCase() + m.slice(1)}`);
              return (
                <Button
                  key={m}
                  variant={resolvedMode === m ? "default" : "outline"}
                  size="sm"
                  className="gap-1.5"
                  onClick={() => setMode(m)}
                >
                  <Icon className="h-3.5 w-3.5" />
                  {label}
                </Button>
              );
            })}
          </div>
        </div>

        {/* Per-mode settings */}
        {resolvedMode === "host" && (
          <div className="space-y-3 rounded-md border p-3">
            <p className="text-xs text-muted-foreground">{t("tools.browserHostDesc")}</p>
            <div className="flex items-center gap-2">
              <Label>{t("tools.browserHeadless")}</Label>
              <Switch
                checked={browser.headless !== false}
                onCheckedChange={(v) => updateBrowser({ headless: v })}
              />
            </div>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="grid gap-1.5">
                <Label className="text-xs text-muted-foreground">{t("tools.browserBinaryPath")}</Label>
                <Input
                  value={browser.binary_path ?? ""}
                  onChange={(e) => updateBrowser({ binary_path: e.target.value })}
                  placeholder="/usr/bin/chromium"
                  className="font-mono text-base md:text-xs"
                />
              </div>
              <div className="grid gap-1.5">
                <Label className="text-xs text-muted-foreground">{t("tools.browserProfilesDir")}</Label>
                <Input
                  value={browser.profiles_dir ?? ""}
                  onChange={(e) => updateBrowser({ profiles_dir: e.target.value })}
                  placeholder={t("tools.browserProfilesDirPlaceholder")}
                  className="font-mono text-base md:text-xs"
                />
              </div>
            </div>
          </div>
        )}

        {resolvedMode === "docker" && (
          <div className="space-y-3 rounded-md border p-3">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="grid gap-1.5 sm:col-span-2">
                <InfoLabel tip={t("tools.browserImagePresetTip")}>{t("tools.browserImagePreset")}</InfoLabel>
                <div className="flex flex-wrap gap-1">
                  {(["basic", "stealth", "custom"] as const).map((p) => (
                    <Button
                      key={p}
                      variant={(browser.image_preset ?? "basic") === p ? "default" : "outline"}
                      size="sm"
                      onClick={() => updateBrowser({ image_preset: p, ...(p !== "custom" ? { container_image: "" } : {}) })}
                    >
                      {t(`tools.browserImagePreset_${p}`)}
                    </Button>
                  ))}
                </div>
                {(browser.image_preset ?? "basic") === "basic" && (
                  <p className="text-xs text-muted-foreground">{t("tools.browserImagePresetBasicDesc")}</p>
                )}
                {browser.image_preset === "stealth" && (
                  <p className="text-xs text-muted-foreground">{t("tools.browserImagePresetStealthDesc")}</p>
                )}
              </div>
              {browser.image_preset === "custom" && (
                <div className="grid gap-1.5 sm:col-span-2">
                  <Label className="text-xs text-muted-foreground">{t("tools.browserContainerImage")}</Label>
                  <Input
                    value={browser.container_image ?? ""}
                    onChange={(e) => updateBrowser({ container_image: e.target.value })}
                    placeholder="my-registry/chrome:latest"
                    className="font-mono text-base md:text-xs"
                  />
                </div>
              )}
              <div className="grid gap-1.5">
                <Label className="text-xs text-muted-foreground">{t("tools.browserDockerNetwork")}</Label>
                <Input
                  value={browser.container_network ?? ""}
                  onChange={(e) => updateBrowser({ container_network: e.target.value })}
                  placeholder="bridge"
                  className="font-mono text-base md:text-xs"
                />
              </div>
              <div className="grid gap-1.5">
                <Label className="text-xs text-muted-foreground">{t("tools.browserDockerMemory")}</Label>
                <Input
                  type="number"
                  value={browser.container_memory_mb ?? ""}
                  onChange={(e) => updateBrowser({ container_memory_mb: Number(e.target.value) || undefined })}
                  placeholder="512"
                  min={128}
                />
              </div>
              <div className="grid gap-1.5">
                <Label className="text-xs text-muted-foreground">{t("tools.browserDockerCPU")}</Label>
                <Input
                  type="number"
                  value={browser.container_cpu ?? ""}
                  onChange={(e) => updateBrowser({ container_cpu: Number(e.target.value) || undefined })}
                  placeholder="1.0"
                  min={0.25}
                  step={0.25}
                />
              </div>
              <div className="grid gap-1.5">
                <Label className="text-xs text-muted-foreground">{t("tools.browserDockerPool")}</Label>
                <Input
                  type="number"
                  value={browser.container_pool ?? ""}
                  onChange={(e) => updateBrowser({ container_pool: Number(e.target.value) || undefined })}
                  placeholder="0"
                  min={0}
                />
              </div>
            </div>
          </div>
        )}

        {resolvedMode === "remote" && (
          <div className="space-y-3 rounded-md border p-3">
            <div className="grid gap-1.5">
              <InfoLabel tip={t("tools.browserRemoteUrlTip")}>{t("tools.browserRemoteUrl")}</InfoLabel>
              <Input
                value={browser.remote_url ?? ""}
                onChange={(e) => updateBrowser({ remote_url: e.target.value })}
                placeholder="ws://chrome:9222"
                className="font-mono text-base md:text-xs"
              />
            </div>
          </div>
        )}

        {resolvedMode === "k8s" && (
          <div className="space-y-4 rounded-md border p-3">
            <p className="text-xs text-muted-foreground">{t("tools.browserK8sDesc")}</p>

            {/* Cluster connection */}
            <div className="space-y-3">
              <Label className="text-xs font-medium">{t("tools.browserK8sCluster")}</Label>
              <div className="flex gap-1">
                {(["in-cluster", "manual"] as const).map((c) => (
                  <Button
                    key={c}
                    variant={(browser.k8s_connection ?? "in-cluster") === c ? "default" : "outline"}
                    size="sm"
                    onClick={() => updateBrowser({ k8s_connection: c })}
                  >
                    {t(`tools.browserK8sConnection${c === "in-cluster" ? "InCluster" : "Manual"}`)}
                  </Button>
                ))}
              </div>
              {(browser.k8s_connection ?? "in-cluster") === "in-cluster" && (
                <p className="text-xs text-muted-foreground">{t("tools.browserK8sInClusterDesc")}</p>
              )}
              {browser.k8s_connection === "manual" && (
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <div className="grid gap-1.5 sm:col-span-2">
                    <Label className="text-xs text-muted-foreground">{t("tools.browserK8sAPIServer")}</Label>
                    <Input
                      value={browser.k8s_api_server ?? ""}
                      onChange={(e) => updateBrowser({ k8s_api_server: e.target.value })}
                      placeholder="https://k8s.example.com:6443"
                      className="font-mono text-base md:text-xs"
                    />
                  </div>
                  <div className="grid gap-1.5 sm:col-span-2">
                    <Label className="text-xs text-muted-foreground">{t("tools.browserK8sCACert")}</Label>
                    <Textarea
                      value={browser.k8s_ca_cert ?? ""}
                      onChange={(e) => updateBrowser({ k8s_ca_cert: e.target.value })}
                      placeholder="-----BEGIN CERTIFICATE-----"
                      className="font-mono text-base md:text-xs"
                      rows={3}
                    />
                  </div>
                  <div className="grid gap-1.5 sm:col-span-2">
                    <Label className="text-xs text-muted-foreground">{t("tools.browserK8sSAToken")}</Label>
                    <Input
                      type="password"
                      value={browser.k8s_sa_token ?? ""}
                      onChange={(e) => updateBrowser({ k8s_sa_token: e.target.value })}
                      placeholder="eyJhbGciOi..."
                      className="font-mono text-base md:text-xs"
                    />
                  </div>
                  <div className="grid gap-1.5">
                    <Label className="text-xs text-muted-foreground">{t("tools.browserK8sContext")}</Label>
                    <Input
                      value={browser.k8s_context ?? ""}
                      onChange={(e) => updateBrowser({ k8s_context: e.target.value })}
                      placeholder="default"
                      className="font-mono text-base md:text-xs"
                    />
                  </div>
                </div>
              )}
            </div>

            <Separator />

            {/* Pod config */}
            <div className="space-y-3">
              <Label className="text-xs font-medium">{t("tools.browserK8sPodConfig")}</Label>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div className="grid gap-1.5">
                  <Label className="text-xs text-muted-foreground">{t("tools.browserK8sNamespace")}</Label>
                  <Input
                    value={browser.k8s_namespace ?? ""}
                    onChange={(e) => updateBrowser({ k8s_namespace: e.target.value })}
                    placeholder="goclaw"
                    className="font-mono text-base md:text-xs"
                  />
                </div>
                <div className="grid gap-1.5">
                  <Label className="text-xs text-muted-foreground">{t("tools.browserK8sImage")}</Label>
                  <Input
                    value={browser.k8s_image ?? ""}
                    onChange={(e) => updateBrowser({ k8s_image: e.target.value })}
                    placeholder="chromedp/headless-shell:latest"
                    className="font-mono text-base md:text-xs"
                  />
                </div>
                <div className="grid gap-1.5">
                  <Label className="text-xs text-muted-foreground">{t("tools.browserK8sMemory")}</Label>
                  <Input
                    value={browser.k8s_memory ?? ""}
                    onChange={(e) => updateBrowser({ k8s_memory: e.target.value })}
                    placeholder="512Mi"
                    className="font-mono text-base md:text-xs"
                  />
                </div>
                <div className="grid gap-1.5">
                  <Label className="text-xs text-muted-foreground">{t("tools.browserK8sCPU")}</Label>
                  <Input
                    value={browser.k8s_cpu ?? ""}
                    onChange={(e) => updateBrowser({ k8s_cpu: e.target.value })}
                    placeholder="500m"
                    className="font-mono text-base md:text-xs"
                  />
                </div>
                <div className="grid gap-1.5">
                  <Label className="text-xs text-muted-foreground">{t("tools.browserK8sPool")}</Label>
                  <Input
                    type="number"
                    value={browser.k8s_pool ?? ""}
                    onChange={(e) => updateBrowser({ k8s_pool: Number(e.target.value) || undefined })}
                    placeholder="0"
                    min={0}
                  />
                </div>
              </div>
            </div>

            <Separator />

            {/* Scheduling */}
            <div className="space-y-3">
              <Label className="text-xs font-medium">{t("tools.browserK8sScheduling")}</Label>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div className="grid gap-1.5">
                  <InfoLabel tip={t("tools.browserK8sNodeSelectorTip")}>{t("tools.browserK8sNodeSelector")}</InfoLabel>
                  <Input
                    value={browser.k8s_node_selector ? Object.entries(browser.k8s_node_selector as Record<string, string>).map(([k, v]) => `${k}=${v}`).join(", ") : ""}
                    onChange={(e) => {
                      const val = e.target.value;
                      if (!val.trim()) {
                        updateBrowser({ k8s_node_selector: undefined });
                        return;
                      }
                      const selector: Record<string, string> = {};
                      val.split(",").forEach((pair) => {
                        const [k, v] = pair.split("=").map((s) => s.trim());
                        if (k && v) selector[k] = v;
                      });
                      updateBrowser({ k8s_node_selector: selector });
                    }}
                    placeholder="workload=browser"
                    className="font-mono text-base md:text-xs"
                  />
                </div>
                <div className="grid gap-1.5">
                  <InfoLabel tip={t("tools.browserK8sTolerationsTip")}>{t("tools.browserK8sTolerations")}</InfoLabel>
                  <Input
                    value={(browser.k8s_tolerations as string[] | undefined)?.join(", ") ?? ""}
                    onChange={(e) => {
                      const val = e.target.value;
                      if (!val.trim()) {
                        updateBrowser({ k8s_tolerations: undefined });
                        return;
                      }
                      updateBrowser({ k8s_tolerations: val.split(",").map((s: string) => s.trim()).filter(Boolean) });
                    }}
                    placeholder="browser-workload"
                    className="font-mono text-base md:text-xs"
                  />
                </div>
              </div>
            </div>

            <Separator />

            {/* Lifecycle */}
            <div className="space-y-3">
              <Label className="text-xs font-medium">{t("tools.browserK8sLifecycle")}</Label>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                <div className="grid gap-1.5">
                  <Label className="text-xs text-muted-foreground">{t("tools.browserK8sPodTimeout")}</Label>
                  <Input
                    type="number"
                    value={browser.k8s_pod_timeout_hours ?? ""}
                    onChange={(e) => updateBrowser({ k8s_pod_timeout_hours: Number(e.target.value) || undefined })}
                    placeholder="2"
                    min={1}
                  />
                </div>
                <div className="grid gap-1.5">
                  <Label className="text-xs text-muted-foreground">{t("tools.browserK8sHeartbeat")}</Label>
                  <Input
                    type="number"
                    value={browser.k8s_heartbeat_min ?? ""}
                    onChange={(e) => updateBrowser({ k8s_heartbeat_min: Number(e.target.value) || undefined })}
                    placeholder="5"
                    min={1}
                  />
                </div>
                <div className="grid gap-1.5">
                  <Label className="text-xs text-muted-foreground">{t("tools.browserK8sOrphanTimeout")}</Label>
                  <Input
                    type="number"
                    value={browser.k8s_orphan_timeout_min ?? ""}
                    onChange={(e) => updateBrowser({ k8s_orphan_timeout_min: Number(e.target.value) || undefined })}
                    placeholder="15"
                    min={1}
                  />
                </div>
              </div>
            </div>
          </div>
        )}

        <Separator />

        {/* Common settings */}
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div className="grid gap-1.5">
            <Label className="text-xs text-muted-foreground">{t("tools.browserMaxPages")}</Label>
            <Input
              type="number"
              value={browser.max_pages ?? ""}
              onChange={(e) => updateBrowser({ max_pages: Number(e.target.value) || undefined })}
              placeholder="5"
              min={1}
            />
          </div>
          <div className="grid gap-1.5">
            <Label className="text-xs text-muted-foreground">{t("tools.browserActionTimeout")}</Label>
            <Input
              type="number"
              value={browser.action_timeout_ms ?? ""}
              onChange={(e) => updateBrowser({ action_timeout_ms: Number(e.target.value) || undefined })}
              placeholder="30000"
              min={1000}
            />
          </div>
          <div className="grid gap-1.5">
            <Label className="text-xs text-muted-foreground">{t("tools.browserIdleTimeout")}</Label>
            <Input
              type="number"
              value={browser.idle_timeout_ms ?? ""}
              onChange={(e) => updateBrowser({ idle_timeout_ms: Number(e.target.value) || undefined })}
              placeholder="600000"
            />
          </div>
          <div className="grid gap-1.5">
            <Label className="text-xs text-muted-foreground">{t("tools.browserViewportWidth")}</Label>
            <Input
              type="number"
              value={browser.viewport_width ?? ""}
              onChange={(e) => updateBrowser({ viewport_width: Number(e.target.value) || undefined })}
              placeholder="1280"
              min={320}
            />
          </div>
          <div className="grid gap-1.5">
            <Label className="text-xs text-muted-foreground">{t("tools.browserViewportHeight")}</Label>
            <Input
              type="number"
              value={browser.viewport_height ?? ""}
              onChange={(e) => updateBrowser({ viewport_height: Number(e.target.value) || undefined })}
              placeholder="720"
              min={240}
            />
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Label>{t("tools.browserAudit")}</Label>
          <Switch
            checked={browser.audit_enabled ?? false}
            onCheckedChange={(v) => updateBrowser({ audit_enabled: v })}
          />
        </div>

        {dirty && (
          <div className="flex justify-end pt-2">
            <Button size="sm" onClick={() => onSave(draft)} disabled={saving} className="gap-1.5">
              <Save className="h-3.5 w-3.5" /> {saving ? t("saving") : t("save")}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
