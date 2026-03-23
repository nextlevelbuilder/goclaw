import { useState, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Network } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { EmptyState } from "@/components/shared/empty-state";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useEmbeddingStatus } from "@/hooks/use-embedding-status";
import { useSessions } from "@/pages/sessions/hooks/use-sessions";
import { parseSessionKey } from "@/lib/session-key";
import { KGEntitiesTab } from "@/pages/memory/kg-entities-tab";

const KG_AGENT_PLACEHOLDER_VALUE = "__select_agent__";
const KG_SCOPE_PLACEHOLDER_VALUE = "__all_scope__";

export function KnowledgeGraphPage() {
  const { t } = useTranslation("memory");
  const { t: to } = useTranslation("overview");
  const { agents } = useAgents();
  const { status: embStatus } = useEmbeddingStatus();
  const [agentId, setAgentId] = useState("");
  const [userIdFilter, setUserIdFilter] = useState("");

  // Resolve agent UUID → agent_key (sessions filter uses agent_key in session key pattern)
  const selectedAgent = agents.find((a) => a.id === agentId);
  const agentKey = selectedAgent?.agent_key ?? "";

  // Fetch sessions for selected agent to build scope picker (DM + group chats)
  const { sessions } = useSessions({ agentFilter: agentKey || undefined, limit: 200 });

  // Dedupe sessions by userID → scope options showing chat title / display name
  const scopeOptions = useMemo(() => {
    const seen = new Map<string, string>();
    for (const s of sessions) {
      const uid = s.userID || parseSessionKey(s.key).scope;
      if (!uid || seen.has(uid)) continue;
      const meta = s.metadata;
      const label = meta?.chat_title || meta?.display_name
        || (meta?.username ? `@${meta.username}` : null)
        || uid;
      seen.set(uid, label);
    }
    return Array.from(seen.entries())
      .map(([value, label]) => ({ value, label }))
      .sort((a, b) => a.label.localeCompare(b.label));
  }, [sessions]);

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
        <Select
          value={agentId || KG_AGENT_PLACEHOLDER_VALUE}
          onValueChange={(value) => {
            setAgentId(value === KG_AGENT_PLACEHOLDER_VALUE ? "" : value);
            setUserIdFilter("");
          }}
        >
          <SelectTrigger id="kg-agent" className="h-8 min-w-[180px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent align="start">
            <SelectItem value={KG_AGENT_PLACEHOLDER_VALUE}>{t("filters.selectAgent")}</SelectItem>
            {agents.map((a) => (
              <SelectItem key={a.id} value={a.id}>
                {a.display_name || a.agent_key}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {agentId && (
          <Select
            value={userIdFilter || KG_SCOPE_PLACEHOLDER_VALUE}
            onValueChange={(value) => setUserIdFilter(value === KG_SCOPE_PLACEHOLDER_VALUE ? "" : value)}
          >
            <SelectTrigger id="kg-scope" className="h-8 min-w-[220px] max-w-[280px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent align="start">
              <SelectItem value={KG_SCOPE_PLACEHOLDER_VALUE}>{t("filters.allScope")}</SelectItem>
              {scopeOptions.map((o) => (
                <SelectItem key={o.value} value={o.value}>
                  {o.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
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
              <Select
                value={agentId || KG_AGENT_PLACEHOLDER_VALUE}
                onValueChange={(value) => {
                  setAgentId(value === KG_AGENT_PLACEHOLDER_VALUE ? "" : value);
                  setUserIdFilter("");
                }}
              >
                <SelectTrigger className="mt-2 h-9 min-w-[220px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent align="start">
                  <SelectItem value={KG_AGENT_PLACEHOLDER_VALUE}>{t("filters.selectAgent")}</SelectItem>
                  {agents.map((a) => (
                    <SelectItem key={a.id} value={a.id}>
                      {a.display_name || a.agent_key}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            }
          />
        ) : (
          <KGEntitiesTab agentId={agentId} userId={userIdFilter || undefined} />
        )}
      </div>
    </div>
  );
}
