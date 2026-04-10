import { QrCode, Radio, Trash2, ChevronDown, ChevronRight, Users } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type {
  ChannelInstanceData,
  ChannelRuntimeStatus,
} from "@/types/channel";
import type { AgentData } from "@/types/agent";
import type { ChannelContact } from "@/types/contact";
import {
  channelTypeLabels,
  getChannelCheckedLabel,
  getChannelFailureKindLabel,
  getChannelRemediationMeta,
  getChannelStatusMeta,
} from "./channels-status-view";
import { channelsWithAuth } from "./channel-wizard-registry";

interface WhatsAppGroupConfig {
  agent_id?: string; // agent_key (slug), NOT agent.id
  enabled?: boolean;
}

function getWhatsAppGroups(
  config: Record<string, unknown> | null,
): Record<string, WhatsAppGroupConfig> {
  if (!config?.groups) return {};
  return (config.groups as Record<string, WhatsAppGroupConfig>) ?? {};
}

function resolveAgentNameByKey(
  agents: AgentData[],
  agentKey?: string,
): string {
  if (!agentKey) return "";
  const agent = agents.find((a) => a.agent_key === agentKey);
  return agent?.display_name || agent?.agent_key || agentKey;
}

interface ChannelListRowProps {
  instance: ChannelInstanceData;
  status: ChannelRuntimeStatus | null;
  agentName: string;
  agents: AgentData[];
  waGroupContacts: ChannelContact[];
  groupsExpanded: boolean;
  onToggleGroups: () => void;
  onClick: () => void;
  onAuth?: () => void;
  onDelete?: () => void;
}

export function ChannelListRow({
  instance,
  status,
  agentName,
  agents,
  waGroupContacts,
  groupsExpanded,
  onToggleGroups,
  onClick,
  onAuth,
  onDelete,
}: ChannelListRowProps) {
  const { t } = useTranslation("channels");
  const displayName = instance.display_name || instance.name;
  const supportsReauth = channelsWithAuth.has(instance.channel_type);
  const statusMeta = getChannelStatusMeta(status, instance.enabled, t);
  const failureKind = getChannelFailureKindLabel(status?.failure_kind, t);
  const checkedLabel = getChannelCheckedLabel(status, t);
  const remediation = getChannelRemediationMeta(status, supportsReauth, t);
  const summaryLine = status?.summary || statusMeta.label;
  const streakLabel =
    status?.consecutive_failures && status.consecutive_failures > 1
      ? t("list.failureStreak", {
          defaultValue: "{{count}} failures in a row",
          count: status.consecutive_failures,
        })
      : checkedLabel;
  const nextStepLabel =
    remediation?.label || t("actions.inspect", { defaultValue: "Inspect issue" });
  const nextStepHint =
    remediation?.headline ||
    t("list.openChannelDetail", {
      defaultValue: "Open channel detail for the latest diagnosis",
    });

  const isWhatsApp = instance.channel_type === "whatsapp";

  // Merge discovered contacts with configured overrides
  const configGroups = isWhatsApp
    ? getWhatsAppGroups(instance.config as Record<string, unknown> | null)
    : {};

  // Build merged group list: all contacts + any configured-only groups
  const allGroupJids = new Set<string>();
  for (const c of waGroupContacts) {
    allGroupJids.add(c.sender_id);
  }
  for (const jid of Object.keys(configGroups)) {
    allGroupJids.add(jid);
  }

  const hasGroups = allGroupJids.size > 0;
  const sortedGroupJids = [...allGroupJids].sort();

  return (
    <div
      className={cn(
        "rounded-xl border bg-card shadow-sm transition-colors hover:border-primary/30",
        statusMeta.attention && statusMeta.surfaceClass,
      )}
    >
      <div className="flex items-stretch gap-2 p-3 sm:p-4">
        <button
          type="button"
          onClick={onClick}
          className="flex-1 text-left"
        >
          <div className="grid gap-3 lg:grid-cols-[minmax(0,1.05fr)_minmax(0,1fr)_minmax(180px,0.7fr)]">
            <div className="flex min-w-0 gap-3">
              <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary">
                <Radio className="h-4 w-4" />
              </div>
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="truncate text-sm font-semibold">
                    {displayName}
                  </span>
                  <span
                    className={cn(
                      "inline-block h-2 w-2 shrink-0 rounded-full",
                      statusMeta.dotClass,
                    )}
                  />
                  <Badge variant="outline" className="text-xs-plus">
                    {channelTypeLabels[instance.channel_type] || instance.channel_type}
                  </Badge>
                </div>
                <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                  <span className="font-mono">{instance.name}</span>
                  <span className="text-border">·</span>
                  <span className="truncate">{agentName}</span>
                </div>
              </div>
            </div>

            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant={statusMeta.badgeVariant}>{statusMeta.label}</Badge>
                {failureKind && <Badge variant="outline">{failureKind}</Badge>}
              </div>
              <p className="mt-2 truncate text-sm font-medium">{summaryLine}</p>
              {streakLabel && (
                <p className="mt-1 truncate text-xs text-muted-foreground">
                  {streakLabel}
                </p>
              )}
            </div>

            <div className="min-w-0">
              <p className="text-xs-plus font-medium uppercase tracking-[0.16em] text-muted-foreground">
                {t("list.nextStep", { defaultValue: "Next step" })}
              </p>
              <p className="mt-2 truncate text-sm font-medium">{nextStepLabel}</p>
              <p className="mt-1 truncate text-xs text-muted-foreground">
                {nextStepHint}
              </p>
            </div>
          </div>
        </button>

        <div className="flex shrink-0 items-start gap-1">
          {onAuth && supportsReauth && (
            <Button
              variant="ghost"
              size="xs"
              className="text-muted-foreground hover:text-primary"
              onClick={(e) => {
                e.stopPropagation();
                onAuth();
              }}
            >
              <QrCode className="h-3.5 w-3.5" />
            </Button>
          )}
          {onDelete && !instance.is_default && (
            <Button
              variant="ghost"
              size="xs"
              className="text-muted-foreground hover:text-destructive"
              onClick={(e) => {
                e.stopPropagation();
                onDelete();
              }}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>
      </div>

      {/* WhatsApp groups section - always visible for WhatsApp instances */}
      {isWhatsApp && (
        <div className="border-t">
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onToggleGroups();
            }}
            className="flex w-full items-center gap-2 px-4 py-2 text-left text-sm font-medium text-muted-foreground transition-colors hover:bg-muted/50 hover:text-foreground"
          >
            {groupsExpanded ? (
              <ChevronDown className="h-4 w-4 shrink-0" />
            ) : (
              <ChevronRight className="h-4 w-4 shrink-0" />
            )}
            <Users className="h-4 w-4 shrink-0" />
            <span>
              {hasGroups
                ? t("list.groupsCount", { count: allGroupJids.size })
                : t("list.showGroups")}
            </span>
          </button>

          {groupsExpanded && (
            <div className="border-t px-4 pb-3 pt-1">
              {hasGroups ? (
                sortedGroupJids.map((jid) => {
                  const config = configGroups[jid];
                  const isOverride =
                    config?.agent_id && config.agent_id !== "__default__";
                  const groupAgentName = isOverride
                    ? resolveAgentNameByKey(agents, config.agent_id)
                    : agentName;
                  const isDisabled = config?.enabled === false;

                  return (
                    <button
                      key={jid}
                      type="button"
                      onClick={onClick}
                      className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left transition-colors hover:bg-muted/50"
                    >
                      <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                        <Users className="h-3.5 w-3.5" />
                      </div>
                      <div className="min-w-0 flex-1">
                        <span className="truncate font-mono text-xs text-muted-foreground">
                          {jid}
                        </span>
                      </div>
                      <div className="flex shrink-0 items-center gap-2">
                        {isDisabled && (
                          <Badge variant="outline" className="text-xs text-muted-foreground">
                            {t("list.groupDisabled")}
                          </Badge>
                        )}
                        <Badge
                          variant={isOverride ? "default" : "secondary"}
                          className="text-xs-plus"
                        >
                          {isOverride
                            ? groupAgentName
                            : `${t("list.defaultAgent")} · ${groupAgentName}`}
                        </Badge>
                      </div>
                    </button>
                  );
                })
              ) : (
                <p className="px-3 py-2 text-xs text-muted-foreground">
                  {t("list.noGroups")}
                </p>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
