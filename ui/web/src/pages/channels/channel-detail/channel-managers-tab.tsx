import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Trash2, Loader2, RefreshCw, Users, ChevronDown, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/shared/empty-state";
import type { GroupManagerGroupInfo, GroupManagerData } from "../hooks/use-channel-detail";
import { InlineAddForm } from "./channel-manager-inline-add-form";

interface ChannelManagersTabProps {
  listManagerGroups: () => Promise<GroupManagerGroupInfo[]>;
  listManagers: (groupId: string) => Promise<GroupManagerData[]>;
  addManager: (groupId: string, userId: string, displayName?: string, username?: string) => Promise<void>;
  removeManager: (groupId: string, userId: string) => Promise<void>;
}

/** Strips the "group:<channel>:" prefix for display, e.g. "group:telegram:-100123" → "-100123" */
function shortGroupId(id: string): string {
  const m = id.match(/^group:[^:]+:(.+)$/);
  return m?.[1] ?? id;
}

export function ChannelManagersTab({
  listManagerGroups,
  listManagers,
  addManager,
  removeManager,
}: ChannelManagersTabProps) {
  const { t } = useTranslation("channels");
  const [groups, setGroups] = useState<GroupManagerGroupInfo[]>([]);
  const [loadingGroups, setLoadingGroups] = useState(true);

  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [managersMap, setManagersMap] = useState<Record<string, GroupManagerData[]>>({});
  const [loadingMap, setLoadingMap] = useState<Record<string, boolean>>({});

  const refreshGroups = useCallback(async () => {
    setLoadingGroups(true);
    try {
      const g = await listManagerGroups();
      setGroups(g);
    } catch {
      // handled by http hook
    } finally {
      setLoadingGroups(false);
    }
  }, [listManagerGroups]);

  useEffect(() => {
    refreshGroups();
  }, [refreshGroups]);

  const loadManagersForGroup = useCallback(
    async (groupId: string) => {
      setLoadingMap((prev) => ({ ...prev, [groupId]: true }));
      try {
        const w = await listManagers(groupId);
        setManagersMap((prev) => ({ ...prev, [groupId]: w }));
      } catch {
        setManagersMap((prev) => ({ ...prev, [groupId]: [] }));
      } finally {
        setLoadingMap((prev) => ({ ...prev, [groupId]: false }));
      }
    },
    [listManagers],
  );

  const toggleGroup = (groupId: string) => {
    const isExpanding = !expanded[groupId];
    setExpanded((prev) => ({ ...prev, [groupId]: isExpanding }));
    if (isExpanding && !managersMap[groupId]) {
      loadManagersForGroup(groupId);
    }
  };

  const handleRemoveManager = async (groupId: string, userId: string) => {
    try {
      await removeManager(groupId, userId);
      setManagersMap((prev) => ({
        ...prev,
        [groupId]: (prev[groupId] ?? []).filter((w) => w.user_id !== userId),
      }));
      await refreshGroups();
    } catch {
      // handled by http hook
    }
  };

  const handleAddManager = async (gid: string, uid: string, dn: string, un: string) => {
    await addManager(gid, uid, dn, un);
    if (expanded[gid]) {
      await loadManagersForGroup(gid);
    }
    await refreshGroups();
    if (!expanded[gid]) {
      setExpanded((prev) => ({ ...prev, [gid]: true }));
      loadManagersForGroup(gid);
    }
  };

  return (
    <div className="space-y-5">
      <p className="text-sm text-muted-foreground">
        {t("detail.managers.description")}
      </p>

      {/* Groups accordion */}
      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-medium">
            {t("detail.managers.groups")}
            {groups.length > 0 && (
              <span className="ml-1.5 text-muted-foreground font-normal">({groups.length})</span>
            )}
          </h3>
          <Button variant="ghost" size="icon" className="h-7 w-7" onClick={refreshGroups} disabled={loadingGroups}>
            {loadingGroups
              ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
              : <RefreshCw className="h-3.5 w-3.5" />
            }
          </Button>
        </div>

        {groups.length === 0 && !loadingGroups ? (
          <EmptyState
            icon={Users}
            title={t("detail.managers.noManagerGroups")}
            description={t("detail.managers.noManagerGroupsHint")}
          />
        ) : (
          <div className="rounded-md border divide-y">
            {groups.map((g) => {
              const isOpen = expanded[g.group_id];
              const groupManagers = managersMap[g.group_id] ?? [];
              const isLoading = loadingMap[g.group_id];

              return (
                <div key={g.group_id}>
                  <button
                    type="button"
                    aria-expanded={isOpen}
                    className="flex w-full items-center gap-3 px-4 py-3 text-left hover:bg-muted/30 transition-colors"
                    onClick={() => toggleGroup(g.group_id)}
                  >
                    {isOpen
                      ? <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" />
                      : <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />
                    }
                    <div className="min-w-0 flex-1">
                      <span className="font-mono text-sm">{shortGroupId(g.group_id)}</span>
                      <span className="ml-2 text-xs text-muted-foreground">{g.group_id}</span>
                    </div>
                    <span className="shrink-0 rounded-full bg-muted px-2 py-0.5 text-xs font-medium tabular-nums">
                      {g.writer_count === 1
                        ? t("detail.managers.managersCount", { count: g.writer_count })
                        : t("detail.managers.managersCountPlural", { count: g.writer_count })}
                    </span>
                  </button>

                  {isOpen && (
                    <div className="border-t bg-muted/10 px-4 py-3 space-y-3">
                      {isLoading ? (
                        <p className="text-sm text-muted-foreground py-2">{t("detail.managers.loadingManagers")}</p>
                      ) : groupManagers.length === 0 ? (
                        <p className="text-sm text-muted-foreground py-2">{t("detail.managers.noManagers")}</p>
                      ) : (
                        <div className="overflow-x-auto rounded-md border bg-background">
                          <table className="w-full min-w-[500px] text-sm">
                            <thead>
                              <tr className="border-b bg-muted/50">
                                <th className="px-3 py-2 text-left font-medium text-xs uppercase tracking-wide text-muted-foreground">{t("detail.managers.columns.userId")}</th>
                                <th className="px-3 py-2 text-left font-medium text-xs uppercase tracking-wide text-muted-foreground">{t("detail.managers.columns.name")}</th>
                                <th className="px-3 py-2 text-left font-medium text-xs uppercase tracking-wide text-muted-foreground">{t("detail.managers.columns.username")}</th>
                                <th className="px-3 py-2 w-10" />
                              </tr>
                            </thead>
                            <tbody>
                              {groupManagers.map((w) => (
                                <tr key={w.user_id} className="border-b last:border-0 hover:bg-muted/20">
                                  <td className="px-3 py-2 font-mono text-xs">{w.user_id}</td>
                                  <td className="px-3 py-2">{w.display_name || <span className="text-muted-foreground">-</span>}</td>
                                  <td className="px-3 py-2">{w.username ? <span className="text-muted-foreground">@{w.username}</span> : <span className="text-muted-foreground">-</span>}</td>
                                  <td className="px-3 py-2 text-right">
                                    <Button
                                      variant="ghost"
                                      size="icon"
                                      className="h-7 w-7 text-muted-foreground hover:text-destructive"
                                      onClick={() => handleRemoveManager(g.group_id, w.user_id)}
                                    >
                                      <Trash2 className="h-3.5 w-3.5" />
                                    </Button>
                                  </td>
                                </tr>
                              ))}
                            </tbody>
                          </table>
                        </div>
                      )}

                      <InlineAddForm
                        groupId={g.group_id}
                        onAdd={handleAddManager}
                      />
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Add to new group */}
      <InlineAddForm
        showGroupField
        onAdd={handleAddManager}
      />
    </div>
  );
}
