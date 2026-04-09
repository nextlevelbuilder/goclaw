import { useState, useEffect, useCallback, useMemo } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Plus, RefreshCw, Pencil, Trash2, Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useMinLoading } from "@/hooks/use-min-loading";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useAgentLinks, type AgentLinkData } from "../hooks/use-agent-links";
import { LinkCreateDialog } from "./link-create-dialog";

const DIRECTION_VARIANT: Record<string, "default" | "secondary" | "outline"> = {
  outbound: "default",
  inbound: "secondary",
  bidirectional: "outline",
};

const STATUS_VARIANT: Record<string, "success" | "secondary"> = {
  active: "success",
  disabled: "secondary",
};

export function TeamLinksTab() {
  const { t } = useTranslation("teams");
  const { agents, refresh: refreshAgents } = useAgents();
  const { listLinks, deleteLink } = useAgentLinks();

  const [links, setLinks] = useState<AgentLinkData[]>([]);
  const [loading, setLoading] = useState(false);
  const spinning = useMinLoading(loading);
  const [createOpen, setCreateOpen] = useState(false);
  const [editLink, setEditLink] = useState<AgentLinkData | null>(null);
  const [deleteTargetId, setDeleteTargetId] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  const agentIds = useMemo(
    () => agents.filter((a) => a.status === "active").map((a) => a.id),
    [agents],
  );

  // Load links for all agents and deduplicate
  const load = useCallback(async () => {
    if (agentIds.length === 0) return;
    setLoading(true);
    try {
      const seen = new Set<string>();
      const all: AgentLinkData[] = [];
      await Promise.all(
        agentIds.map(async (agentId) => {
          const agentLinks = await listLinks(agentId, "all");
          for (const l of agentLinks) {
            if (!seen.has(l.id)) {
              seen.add(l.id);
              all.push(l);
            }
          }
        }),
      );
      setLinks(all);
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  }, [agentIds, listLinks]);

  // Load agents on mount, then load links when agentIds change
  useEffect(() => { refreshAgents(); }, [refreshAgents]);
  useEffect(() => { load(); }, [load]);

  const handleDelete = async () => {
    if (!deleteTargetId) return;
    setDeleting(true);
    try {
      await deleteLink(deleteTargetId);
      setDeleteTargetId(null);
      await load();
    } catch {
      // toast handled by hook
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div className="flex flex-col gap-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h3 className="text-base font-semibold">{t("links.title")}</h3>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={load} disabled={spinning} className="gap-1">
            <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} />
          </Button>
          <Button size="sm" className="gap-1.5" onClick={() => setCreateOpen(true)}>
            <Plus className="h-3.5 w-3.5" />
            {t("links.create")}
          </Button>
        </div>
      </div>

      {/* Table */}
      <div className="overflow-x-auto rounded-lg border">
        <table className="min-w-[640px] w-full text-sm">
          <thead className="bg-muted/40 text-xs text-muted-foreground">
            <tr>
              <th className="px-4 py-2.5 text-left font-medium">{t("links.source")}</th>
              <th className="px-4 py-2.5 text-left font-medium">{t("links.target")}</th>
              <th className="px-4 py-2.5 text-left font-medium">{t("links.direction")}</th>
              <th className="px-4 py-2.5 text-left font-medium">{t("links.status")}</th>
              <th className="px-4 py-2.5 text-left font-medium">{t("links.description")}</th>
              <th className="px-4 py-2.5 text-right font-medium">{t("members.columns.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {loading && links.length === 0 ? (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-muted-foreground">
                  <Loader2 className="inline h-4 w-4 animate-spin" />
                </td>
              </tr>
            ) : links.length === 0 ? (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-muted-foreground">
                  {t("links.empty")}
                </td>
              </tr>
            ) : (
              links.map((link) => (
                <tr key={link.id} className="border-t hover:bg-muted/30">
                  <td className="px-4 py-3 text-sm">
                    {link.source_emoji && <span className="mr-1">{link.source_emoji}</span>}
                    {link.source_display_name || link.source_agent_key}
                  </td>
                  <td className="px-4 py-3 text-sm">
                    {link.target_emoji && <span className="mr-1">{link.target_emoji}</span>}
                    {link.target_display_name || link.target_agent_key}
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant={DIRECTION_VARIANT[link.direction] ?? "outline"} className="text-2xs">
                      {t(`links.${link.direction}`)}
                    </Badge>
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant={STATUS_VARIANT[link.status] ?? "secondary"} className="text-2xs">
                      {t(`links.${link.status}`)}
                    </Badge>
                  </td>
                  <td className="max-w-[200px] truncate px-4 py-3 text-muted-foreground">
                    {link.description || "—"}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7"
                        onClick={() => { setEditLink(link); setCreateOpen(true); }}
                      >
                        <Pencil className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7 text-muted-foreground hover:text-destructive"
                        onClick={() => setDeleteTargetId(link.id)}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {/* Create / Edit dialog */}
      <LinkCreateDialog
        open={createOpen}
        onOpenChange={(v) => { setCreateOpen(v); if (!v) setEditLink(null); }}
        editLink={editLink}
        onSaved={() => load()}
      />

      {/* Delete confirm */}
      <ConfirmDialog
        open={!!deleteTargetId}
        onOpenChange={(v) => !v && setDeleteTargetId(null)}
        title={t("links.deleteTitle")}
        description={t("links.confirmDelete")}
        confirmLabel={t("tasks.delete")}
        variant="destructive"
        onConfirm={handleDelete}
        loading={deleting}
      />
    </div>
  );
}
