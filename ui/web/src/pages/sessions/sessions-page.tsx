import { useState } from "react";
import { useParams, useNavigate } from "react-router";
import { History } from "lucide-react";
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
import { formatRelativeTime } from "@/lib/format";
import type { SessionInfo } from "@/types/session";

export function SessionsPage() {
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
    <div className="p-4 sm:p-6">
      <PageHeader title="Sessions" description="Browse conversation sessions" />

      <div className="mt-4">
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder="Search sessions..."
          className="max-w-sm"
        />
      </div>

      <div className="mt-6">
        {showSkeleton ? (
          <TableSkeleton rows={8} />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={History}
            title={search ? "No matching sessions" : "No sessions yet"}
            description={
              search
                ? "Try a different search term."
                : "Sessions will appear here once you start chatting."
            }
          />
        ) : (
          <div className="rounded-md border overflow-x-auto">
            <table className="w-full min-w-[600px]">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left text-sm font-medium">Session</th>
                  <th className="px-4 py-3 text-left text-sm font-medium">Agent</th>
                  <th className="px-4 py-3 text-left text-sm font-medium">Channel</th>
                  <th className="px-4 py-3 text-right text-sm font-medium">Messages</th>
                  <th className="px-4 py-3 text-right text-sm font-medium">Updated</th>
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
        <div className="text-xs text-muted-foreground">
          {session.metadata?.username ? `@${session.metadata.username}` : session.key}
        </div>
      </td>
      <td className="px-4 py-3">
        <Badge variant="outline">{session.agentName || parsed.agentId}</Badge>
      </td>
      <td className="px-4 py-3 text-sm text-muted-foreground">
        {session.channel || "ws"}
      </td>
      <td className="px-4 py-3 text-right text-sm">{session.messageCount}</td>
      <td className="px-4 py-3 text-right text-sm text-muted-foreground">
        {formatRelativeTime(session.updated)}
      </td>
    </tr>
  );
}
