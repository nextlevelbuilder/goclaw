import { useState } from "react";
import { Shield, Plus, RefreshCw, Trash2, Loader2, HeartPulse } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { useProxyPool, type Proxy } from "./hooks/use-proxy-pool";
import { ProxyAddDialog } from "./proxy-add-dialog";
import { toast } from "@/stores/use-toast-store";

export function ProxyPoolPage() {
  const { t } = useTranslation("proxy-pool");
  const { proxies, loading, addProxy, removeProxy, toggleProxy, healthCheck, healthChecking, refresh } = useProxyPool();
  const [addOpen, setAddOpen] = useState(false);
  const [deletingId, setDeletingId] = useState<string | null>(null);

  const handleAdd = async (data: {
    name: string;
    url: string;
    username?: string;
    password?: string;
    geo?: string;
  }) => {
    await addProxy(data);
    toast.success(t("addSuccess"));
  };

  const handleDelete = async (proxy: Proxy) => {
    if (!confirm(t("deleteConfirm", { name: proxy.name }))) return;
    setDeletingId(proxy.id);
    try {
      await removeProxy(proxy.id);
      toast.success(t("deleteSuccess"));
    } finally {
      setDeletingId(null);
    }
  };

  const handleToggle = async (proxy: Proxy) => {
    await toggleProxy({ id: proxy.id, enabled: !proxy.isEnabled });
    toast.success(proxy.isEnabled ? t("disableSuccess") : t("enableSuccess"));
  };

  const handleHealthCheck = async () => {
    await healthCheck();
    toast.success(t("healthCheckSuccess"));
  };

  return (
    <div className="flex h-full flex-col gap-6 p-4 sm:p-6">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <>
            <Button variant="outline" size="sm" onClick={() => refresh()}>
              <RefreshCw className="mr-1.5 h-4 w-4" />
              {t("common:refresh", { defaultValue: "Refresh" })}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={handleHealthCheck}
              disabled={healthChecking || proxies.length === 0}
            >
              {healthChecking ? (
                <Loader2 className="mr-1.5 h-4 w-4 animate-spin" />
              ) : (
                <HeartPulse className="mr-1.5 h-4 w-4" />
              )}
              {t("healthCheckAll")}
            </Button>
            <Button size="sm" onClick={() => setAddOpen(true)}>
              <Plus className="mr-1.5 h-4 w-4" />
              {t("addProxy")}
            </Button>
          </>
        }
      />

      {loading ? (
        <div className="flex items-center justify-center py-16">
          <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        </div>
      ) : proxies.length === 0 ? (
        <EmptyState
          icon={Shield}
          title={t("noProxies")}
          description={t("noProxiesDescription")}
          action={
            <Button size="sm" onClick={() => setAddOpen(true)}>
              <Plus className="mr-1.5 h-4 w-4" />
              {t("addProxy")}
            </Button>
          }
        />
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[600px]">
            <thead>
              <tr className="border-b text-left text-sm text-muted-foreground">
                <th className="pb-3 font-medium">{t("columns.name")}</th>
                <th className="pb-3 font-medium">{t("columns.url")}</th>
                <th className="pb-3 font-medium">{t("columns.geo")}</th>
                <th className="pb-3 font-medium">{t("columns.enabled")}</th>
                <th className="pb-3 font-medium">{t("columns.health")}</th>
                <th className="pb-3 font-medium">{t("columns.lastCheck")}</th>
                <th className="pb-3 font-medium">{t("columns.actions")}</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {proxies.map((proxy) => (
                <ProxyRow
                  key={proxy.id}
                  proxy={proxy}
                  deleting={deletingId === proxy.id}
                  onDelete={() => handleDelete(proxy)}
                  onToggle={() => handleToggle(proxy)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      <ProxyAddDialog open={addOpen} onOpenChange={setAddOpen} onAdd={handleAdd} />
    </div>
  );
}

function ProxyRow({
  proxy,
  deleting,
  onDelete,
  onToggle,
}: {
  proxy: Proxy;
  deleting: boolean;
  onDelete: () => void;
  onToggle: () => void;
}) {
  const { t } = useTranslation("proxy-pool");

  const healthBadge = () => {
    if (!proxy.lastHealthCheck) {
      return <Badge variant="outline">{t("health.unknown")}</Badge>;
    }
    if (proxy.isHealthy) {
      return <Badge className="bg-green-500/10 text-green-600 border-green-500/20">{t("health.healthy")}</Badge>;
    }
    return <Badge variant="destructive">{t("health.unhealthy")}</Badge>;
  };

  // Mask the URL: show scheme + host but mask port details for display
  const maskedUrl = proxy.url;

  return (
    <tr className="text-sm">
      <td className="py-3 font-medium">{proxy.name}</td>
      <td className="py-3 font-mono text-xs text-muted-foreground">{maskedUrl}</td>
      <td className="py-3">
        {proxy.geo ? (
          <Badge variant="secondary">{proxy.geo}</Badge>
        ) : (
          <span className="text-muted-foreground">-</span>
        )}
      </td>
      <td className="py-3">
        <Switch
          checked={proxy.isEnabled}
          onCheckedChange={onToggle}
          aria-label={proxy.isEnabled ? t("toggle.disable") : t("toggle.enable")}
        />
      </td>
      <td className="py-3">{healthBadge()}</td>
      <td className="py-3 text-muted-foreground">
        {proxy.lastHealthCheck
          ? new Date(proxy.lastHealthCheck).toLocaleString()
          : "-"}
      </td>
      <td className="py-3">
        <Button
          variant="ghost"
          size="icon"
          className="h-8 w-8 text-destructive hover:text-destructive"
          onClick={onDelete}
          disabled={deleting}
        >
          {deleting ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <Trash2 className="h-4 w-4" />
          )}
        </Button>
      </td>
    </tr>
  );
}
