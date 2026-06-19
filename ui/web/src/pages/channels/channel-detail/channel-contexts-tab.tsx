import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, ChevronRight, Loader2, RefreshCw, Settings2, ShieldCheck, Users } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/shared/empty-state";
import type { ChannelCapability, ChannelContextData, ChannelContextMember } from "@/types/channel";
import {
  ChannelContextCapabilityAdminDialog,
  type ChannelContextCapabilityTarget,
} from "./channel-context-capability-admin-dialog";
import type { ChannelCredentialPayload } from "../hooks/use-channel-detail";

interface ChannelContextsTabProps {
  listContexts: () => Promise<ChannelContextData[]>;
  listContextMembers: (scopeType: string, scopeKey: string) => Promise<ChannelContextMember[]>;
  listContextCapabilities: (scopeType: string, scopeKey: string) => Promise<ChannelCapability[]>;
  onGrantCapability: (target: ChannelContextCapabilityTarget) => Promise<void>;
  onRevokeCapability: (target: ChannelContextCapabilityTarget) => Promise<void>;
  onSaveCredentials: (target: ChannelContextCapabilityTarget, payload: ChannelCredentialPayload) => Promise<void>;
  onDeleteCredentials: (target: ChannelContextCapabilityTarget) => Promise<void>;
}

function contextKey(ctx: ChannelContextData) {
  return `${ctx.scope_type}:${ctx.scope_key}`;
}

function capabilityLabel(cap: ChannelCapability) {
  return cap.display_name || cap.name;
}

export function ChannelContextsTab({
  listContexts,
  listContextMembers,
  listContextCapabilities,
  onGrantCapability,
  onRevokeCapability,
  onSaveCredentials,
  onDeleteCredentials,
}: ChannelContextsTabProps) {
  const { t } = useTranslation("channels");
  const [contexts, setContexts] = useState<ChannelContextData[]>([]);
  const [loading, setLoading] = useState(true);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [members, setMembers] = useState<Record<string, ChannelContextMember[]>>({});
  const [capabilities, setCapabilities] = useState<Record<string, ChannelCapability[]>>({});
  const [loadingRows, setLoadingRows] = useState<Record<string, boolean>>({});
  const [adminTarget, setAdminTarget] = useState<ChannelContextCapabilityTarget | null>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      setContexts(await listContexts());
    } finally {
      setLoading(false);
    }
  }, [listContexts]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const loadContextDetail = useCallback(
    async (ctx: ChannelContextData) => {
      const key = contextKey(ctx);
      setLoadingRows((prev) => ({ ...prev, [key]: true }));
      try {
        const [nextMembers, nextCapabilities] = await Promise.all([
          listContextMembers(ctx.scope_type, ctx.scope_key),
          listContextCapabilities(ctx.scope_type, ctx.scope_key),
        ]);
        setMembers((prev) => ({ ...prev, [key]: nextMembers }));
        setCapabilities((prev) => ({ ...prev, [key]: nextCapabilities }));
      } finally {
        setLoadingRows((prev) => ({ ...prev, [key]: false }));
      }
    },
    [listContextCapabilities, listContextMembers],
  );

  const rows = useMemo(() => contexts, [contexts]);

  const toggle = (ctx: ChannelContextData) => {
    const key = contextKey(ctx);
    const open = !expanded[key];
    setExpanded((prev) => ({ ...prev, [key]: open }));
    if (open && !capabilities[key]) {
      loadContextDetail(ctx);
    }
  };

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">
          {t("detail.contexts.description")}
        </p>
        <Button variant="ghost" size="icon" className="h-8 w-8 shrink-0" onClick={refresh} disabled={loading}>
          {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
        </Button>
      </div>

      {!loading && rows.length === 0 ? (
        <EmptyState
          icon={ShieldCheck}
          title={t("detail.contexts.emptyTitle")}
          description={t("detail.contexts.emptyHint")}
        />
      ) : (
        <div className="rounded-md border divide-y">
          {rows.map((ctx) => {
            const key = contextKey(ctx);
            const isOpen = !!expanded[key];
            const rowMembers = members[key] ?? [];
            const rowCapabilities = capabilities[key] ?? [];
            const isLoading = !!loadingRows[key];
            const scopedCapabilities = rowCapabilities.filter(
              (cap) => cap.context_grant_configured || cap.context_credentials_configured || cap.source === ctx.scope_type,
            );

            return (
              <div key={key}>
                <button
                  type="button"
                  aria-expanded={isOpen}
                  className="flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/30"
                  onClick={() => toggle(ctx)}
                >
                  {isOpen
                    ? <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" />
                    : <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />
                  }
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="truncate text-sm font-medium">{ctx.display_name || ctx.scope_key}</span>
                      <Badge variant="outline">{ctx.scope_type}</Badge>
                      {ctx.source && <Badge variant="secondary">{ctx.source}</Badge>}
                      {ctx.live_members_supported && <Badge variant="success">{t("detail.contexts.live")}</Badge>}
                    </div>
                    <div className="mt-1 truncate font-mono text-xs text-muted-foreground">{ctx.scope_key}</div>
                  </div>
                  {typeof ctx.member_count === "number" && (
                    <span className="shrink-0 rounded-full bg-muted px-2 py-0.5 text-xs font-medium tabular-nums">
                      {ctx.member_count}
                    </span>
                  )}
                </button>

                {isOpen && (
                  <div className="space-y-4 border-t bg-muted/10 px-4 py-4">
                    {isLoading ? (
                      <div className="flex items-center gap-2 text-sm text-muted-foreground">
                        <Loader2 className="h-4 w-4 animate-spin" />
                        {t("detail.contexts.loading")}
                      </div>
                    ) : (
                      <>
                        <div className="space-y-2">
                          <h3 className="text-sm font-medium">{t("detail.contexts.capabilities")}</h3>
                          {rowCapabilities.length === 0 ? (
                            <p className="text-sm text-muted-foreground">{t("detail.contexts.noCapabilities")}</p>
                          ) : (
                            <div className="overflow-x-auto rounded-md border bg-background">
                              <table className="w-full min-w-[680px] text-sm">
                                <thead>
                                  <tr className="border-b bg-muted/50">
                                    <th className="px-3 py-2 text-left text-xs font-medium uppercase text-muted-foreground">{t("detail.contexts.columns.name")}</th>
                                    <th className="px-3 py-2 text-left text-xs font-medium uppercase text-muted-foreground">{t("detail.contexts.columns.type")}</th>
                                    <th className="px-3 py-2 text-left text-xs font-medium uppercase text-muted-foreground">{t("detail.contexts.columns.source")}</th>
                                    <th className="px-3 py-2 text-left text-xs font-medium uppercase text-muted-foreground">{t("detail.contexts.columns.credentials")}</th>
                                    <th className="px-3 py-2 text-left text-xs font-medium uppercase text-muted-foreground">{t("detail.contexts.columns.tools")}</th>
                                    <th className="px-3 py-2 text-right text-xs font-medium uppercase text-muted-foreground">{t("detail.contexts.columns.actions")}</th>
                                  </tr>
                                </thead>
                                <tbody>
                                  {rowCapabilities.map((cap) => (
                                    <tr key={`${cap.type}:${cap.id}`} className="border-b last:border-0 hover:bg-muted/20">
                                      <td className="px-3 py-2">
                                        <div className="font-medium">{capabilityLabel(cap)}</div>
                                        <div className="font-mono text-xs text-muted-foreground">{cap.name}</div>
                                      </td>
                                      <td className="px-3 py-2"><Badge variant="outline">{cap.type}</Badge></td>
                                      <td className="px-3 py-2">{cap.source}</td>
                                      <td className="px-3 py-2">
                                        <Badge variant={cap.has_credential ? "success" : "secondary"}>
                                          {cap.has_credential ? cap.credential_source : t("detail.contexts.none")}
                                        </Badge>
                                      </td>
                                      <td className="px-3 py-2 text-xs text-muted-foreground">
                                        {(cap.tool_allow?.length ?? 0) > 0 ? cap.tool_allow?.join(", ") : t("detail.contexts.allTools")}
                                      </td>
                                      <td className="px-3 py-2 text-right">
                                        <Button
                                          type="button"
                                          variant="ghost"
                                          size="icon-sm"
                                          onClick={() => setAdminTarget({ context: ctx, capability: cap })}
                                          aria-label={t("detail.contexts.manageCapability")}
                                        >
                                          <Settings2 className="h-4 w-4" />
                                        </Button>
                                      </td>
                                    </tr>
                                  ))}
                                </tbody>
                              </table>
                            </div>
                          )}
                          {scopedCapabilities.length > 0 && (
                            <p className="text-xs text-muted-foreground">
                              {t("detail.contexts.scopedCount", { count: scopedCapabilities.length })}
                            </p>
                          )}
                        </div>

                        <div className="space-y-2">
                          <h3 className="text-sm font-medium">{t("detail.contexts.members")}</h3>
                          {rowMembers.length === 0 ? (
                            <p className="text-sm text-muted-foreground">{t("detail.contexts.noMembers")}</p>
                          ) : (
                            <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                              {rowMembers.map((member) => (
                                <div key={member.platform_id} className="rounded-md border bg-background px-3 py-2">
                                  <div className="flex items-center gap-2">
                                    <Users className="h-3.5 w-3.5 text-muted-foreground" />
                                    <span className="truncate text-sm font-medium">{member.display_name || member.username || member.platform_id}</span>
                                  </div>
                                  <div className="mt-1 truncate font-mono text-xs text-muted-foreground">{member.user_id || member.platform_id}</div>
                                </div>
                              ))}
                            </div>
                          )}
                        </div>
                      </>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
      <ChannelContextCapabilityAdminDialog
        target={adminTarget}
        open={!!adminTarget}
        onOpenChange={(open) => { if (!open) setAdminTarget(null); }}
        onGrant={async (target) => {
          await onGrantCapability(target);
          await loadContextDetail(target.context);
        }}
        onRevoke={async (target) => {
          await onRevokeCapability(target);
          await loadContextDetail(target.context);
        }}
        onSaveCredentials={async (target, payload) => {
          await onSaveCredentials(target, payload);
          await loadContextDetail(target.context);
        }}
        onDeleteCredentials={async (target) => {
          await onDeleteCredentials(target);
          await loadContextDetail(target.context);
        }}
      />
    </div>
  );
}
