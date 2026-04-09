import { useState, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Network } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { EmptyState } from "@/components/shared/empty-state";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useEmbeddingStatus } from "@/hooks/use-embedding-status";
import { useContactResolver } from "@/hooks/use-contact-resolver";
import { formatUserLabel } from "@/lib/format-user-label";
import { useKGStats } from "@/pages/memory/hooks/use-knowledge-graph";
import { KGEntitiesTab } from "@/pages/memory/knowledge-graph/kg-entities-tab";

export function KnowledgeGraphPage() {
  const { t } = useTranslation("memory");
  const { t: to } = useTranslation("overview");
  const { agents } = useAgents();
  const { status: embStatus } = useEmbeddingStatus();
  const [agentId, setAgentId] = useState("");
  const [userIdFilter, setUserIdFilter] = useState("");

  // Fetch KG stats for the agent — includes distinct user_ids from KG entities
  const { stats } = useKGStats(agentId);
  const userIds = stats?.user_ids ?? [];
  const { resolve } = useContactResolver(userIds);

  // Build scope options from KG entity user IDs (more reliable than sessions)
  const scopeOptions = useMemo(() => {
    return userIds
      .map((uid) => ({ value: uid, label: formatUserLabel(uid, resolve) }))
      .sort((a, b) => a.label.localeCompare(b.label));
  }, [userIds, resolve]);

  return (
    <div className="flex h-full flex-col p-4 sm:p-6">
      {/* Header + filters in one row */}
      <div className="flex flex-wrap items-center gap-3">
        <div className="mr-auto">
          <h1 className="text-lg font-semibold">{t("kg.pageTitle")}</h1>
          <p className="flex items-center gap-2 text-xs text-muted-foreground flex-wrap">
            {t("kg.pageDescription")}
            {embStatus && (
              <Badge variant={embStatus.configured ? "outline" : "secondary"} className="text-xs font-normal">
                {embStatus.configured ? `${to("embedding.title")}: ${embStatus.model}` : `${to("embedding.title")}: ${to("embedding.notConfigured")}`}
              </Badge>
            )}
          </p>
        </div>
        <select
          id="kg-agent"
          value={agentId}
          onChange={(e) => { setAgentId(e.target.value); setUserIdFilter(""); }}
          className="h-8 rounded-md border bg-background px-2 text-base md:text-sm"
        >
          <option value="">{t("filters.selectAgent")}</option>
          {agents.map((a) => (
            <option key={a.id} value={a.id}>
              {a.display_name || a.agent_key}
            </option>
          ))}
        </select>
        {agentId && (
          <select
            id="kg-scope"
            value={userIdFilter}
            onChange={(e) => setUserIdFilter(e.target.value)}
            className="h-8 rounded-md border bg-background px-2 text-base md:text-sm max-w-[240px]"
          >
            <option value="">{t("filters.allScope")}</option>
            {scopeOptions.map((o) => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </select>
        )}
      </div>

      {/* Content */}
      <div className="mt-3 min-h-0 flex-1">
        {!agentId ? (
          <EmptyState
            icon={Network}
            title={t("kg.selectAgentTitle")}
            description={t("kg.selectAgentDescription")}
            action={
              <select
                value={agentId}
                onChange={(e) => { setAgentId(e.target.value); setUserIdFilter(""); }}
                className="mt-2 h-9 rounded-md border bg-background px-3 text-base md:text-sm"
              >
                <option value="">{t("filters.selectAgent")}</option>
                {agents.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.display_name || a.agent_key}
                  </option>
                ))}
              </select>
            }
          />
        ) : (
          <KGEntitiesTab key={`${agentId}-${userIdFilter}`} agentId={agentId} userId={userIdFilter || undefined} />
        )}
      </div>
    </div>
  );
}
