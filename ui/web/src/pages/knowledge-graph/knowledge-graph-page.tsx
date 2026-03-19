import { useState, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Network } from "lucide-react";
import { EmptyState } from "@/components/shared/empty-state";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useSessions } from "@/pages/sessions/hooks/use-sessions";
import { parseSessionKey } from "@/lib/session-key";
import { KGEntitiesTab } from "@/pages/memory/kg-entities-tab";

export function KnowledgeGraphPage() {
  const { t } = useTranslation("memory");
  const { agents } = useAgents();
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
          <p className="text-xs text-muted-foreground">{t("kg.pageDescription")}</p>
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
          <KGEntitiesTab agentId={agentId} userId={userIdFilter || undefined} />
        )}
      </div>
    </div>
  );
}
