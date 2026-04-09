import { useState } from "react";
import { useParams, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { History, RefreshCw } from "lucide-react";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { Pagination } from "@/components/shared/pagination";
import { Badge } from "@/components/ui/badge";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useSessions } from "./hooks/use-sessions";
import { SessionDetailPage } from "./session-detail-page";
import { parseSessionKey } from "@/lib/session-key";
import { formatRelativeTime, formatTokens } from "@/lib/format";
import type { SessionInfo } from "@/types/session";

export function SessionsPage() {
  const { t } = useTranslation("sessions");
  const { key: detailKey } = useParams<{ key: string }>();
  const navigate = useNavigate();
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);

  const { sessions, total, loading, preview, deleteSession, resetSession, patchSession } = useSessions({
    limit: pageSize,
    offset: (page - 1) * pageSize,
  });
  const showSkeleton = useDeferredLoading(loading && sessions.length === 0);

  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  const detailSession = detailKey
    ? sessions.find((s) => s.key === decodeURIComponent(detailKey))
    : null;

  if (detailSession) {
    return (
      <SessionDetailPage
        session={detailSession}
        onBack={() => navigate("/sessions")}
        onPreview={preview}
        onDelete={async (key) => {
          await deleteSession(key);
          navigate("/sessions");
        }}
        onReset={resetSession}
        onPatch={patchSession}
      />
    );
  }

  const filtered = sessions.filter((s) => {
    const q = search.toLowerCase();
    const meta = s.metadata;
    return (
      s.key.toLowerCase().includes(q) ||
      (s.label ?? "").toLowerCase().includes(q) ||
      (meta?.display_name ?? "").toLowerCase().includes(q) ||
      (meta?.username ?? "").toLowerCase().includes(q) ||
      (meta?.chat_title ?? "").toLowerCase().includes(q)
    );
  });

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader title={t("title")} description={t("description")} />

      <div className="mt-4">
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder={t("searchPlaceholder")}
          className="max-w-sm"
        />
      </div>

      <div className="mt-6">
        {showSkeleton ? (
          <TableSkeleton rows={8} />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={History}
            title={search ? t("noMatchTitle") : t("emptyTitle")}
            description={search ? t("noMatchDescription") : t("emptyDescription")}
          />
        ) : (
          <div className="rounded-md border overflow-x-auto">
            <table className="w-full min-w-[750px]">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left text-sm font-medium">{t("columns.session")}</th>
                  <th className="px-4 py-3 text-left text-sm font-medium">{t("columns.agent")}</th>
                  <th className="px-4 py-3 text-left text-sm font-medium">{t("columns.context")}</th>
                  <th className="px-4 py-3 text-right text-sm font-medium">{t("columns.messages")}</th>
                  <th className="px-4 py-3 text-right text-sm font-medium">{t("columns.updated")}</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((session) => (
                  <SessionRow
                    key={session.key}
                    session={session}
                    onClick={() => navigate(`/sessions/${encodeURIComponent(session.key)}`)}
                  />
                ))}
              </tbody>
            </table>
            <Pagination
              page={page}
              pageSize={pageSize}
              total={total}
              totalPages={totalPages}
              onPageChange={setPage}
              onPageSizeChange={(size) => { setPageSize(size); setPage(1); }}
            />
          </div>
        )}
      </div>
    </div>
  );
}

function SessionRow({
  session,
  onClick,
}: {
  session: SessionInfo;
  onClick: () => void;
}) {
  const { t } = useTranslation("sessions");
  const parsed = parseSessionKey(session.key);

  return (
    <tr
      className="cursor-pointer border-b transition-colors hover:bg-muted/50"
      onClick={onClick}
    >
      <td className="px-4 py-3">
        <div className="text-sm font-medium">
          {session.metadata?.chat_title || session.metadata?.display_name || session.label || parsed.scope}
        </div>
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          {session.metadata?.username ? `@${session.metadata.username}` : session.key}
          {session.channel && session.channel !== "ws" && (
            <Badge variant="secondary" className="text-2xs px-1 py-0">{session.channel}</Badge>
          )}
        </div>
      </td>
      <td className="px-4 py-3">
        <Badge variant="outline">{session.agentName || parsed.agentId}</Badge>
      </td>
      <td className="px-4 py-3">
        <ContextUsageBar
          estimatedTokens={session.estimatedTokens ?? 0}
          contextWindow={session.contextWindow ?? 0}
          compactionCount={session.compactionCount ?? 0}
          t={t}
        />
      </td>
      <td className="px-4 py-3 text-right text-sm">{session.messageCount}</td>
      <td className="px-4 py-3 text-right text-sm text-muted-foreground">
        {formatRelativeTime(session.updated)}
      </td>
    </tr>
  );
}

/** Inline context usage progress bar with compaction count. */
function ContextUsageBar({
  estimatedTokens,
  contextWindow,
  compactionCount,
  t,
}: {
  estimatedTokens: number;
  contextWindow: number;
  compactionCount: number;
  t: (key: string, opts?: Record<string, unknown>) => string;
}) {
  if (contextWindow <= 0) return <span className="text-xs text-muted-foreground">—</span>;

  const threshold = contextWindow * 0.75;
  const pct = Math.min(Math.round((estimatedTokens / threshold) * 100), 100);

  let barColor = "bg-emerald-500";
  if (pct >= 85) barColor = "bg-red-500";
  else if (pct >= 60) barColor = "bg-amber-500";

  const tooltip = `~${formatTokens(estimatedTokens)} / ${formatTokens(contextWindow)} tokens (${pct}%)`;

  return (
    <div className="flex items-center gap-2 min-w-[120px]">
      <div className="flex-1">
        <div
          className="h-2 w-full rounded-full bg-muted overflow-hidden"
          title={tooltip}
        >
          <div
            className={`h-full rounded-full transition-all ${barColor}`}
            style={{ width: `${pct}%` }}
          />
        </div>
        <div className="mt-0.5 flex items-center gap-1 text-2xs text-muted-foreground">
          <span>{formatTokens(estimatedTokens)} / {formatTokens(contextWindow)}</span>
          {compactionCount > 0 && (
            <span
              className="inline-flex items-center gap-0.5"
              title={t("contextBar.compacted", { count: compactionCount })}
            >
              · <RefreshCw className="h-2.5 w-2.5" />{compactionCount}
            </span>
          )}
        </div>
      </div>
    </div>
  );
}
