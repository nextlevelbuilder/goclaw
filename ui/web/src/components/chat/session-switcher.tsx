import { MessageSquare } from "lucide-react";
import { formatRelativeTime } from "@/lib/format";
import { parseSessionKey } from "@/lib/session-key";
import type { SessionInfo } from "@/types/session";

interface SessionSwitcherProps {
  sessions: SessionInfo[];
  activeKey: string;
  onSelect: (key: string) => void;
  loading?: boolean;
}

export function SessionSwitcher({
  sessions,
  activeKey,
  onSelect,
  loading,
}: SessionSwitcherProps) {
  if (sessions.length === 0 && loading) {
    return (
      <div className="space-y-2 p-2">
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className="h-14 animate-pulse rounded-lg bg-muted" />
        ))}
      </div>
    );
  }

  if (sessions.length === 0) {
    return (
      <div className="px-4 py-8 text-center text-sm text-muted-foreground">
        No sessions yet. Start a chat!
      </div>
    );
  }

  return (
    <div className="space-y-1 p-2">
      {sessions.map((session) => {
        const parsed = parseSessionKey(session.key);
        const isActive = session.key === activeKey;
        const label = session.metadata?.chat_title || session.metadata?.display_name || session.label || parsed.scope || session.key;

        return (
          <button
            key={session.key}
            type="button"
            onClick={() => onSelect(session.key)}
            className={`flex w-full items-start gap-2 rounded-lg px-3 py-2 text-left text-sm transition-colors ${
              isActive
                ? "bg-accent text-accent-foreground"
                : "hover:bg-muted"
            }`}
          >
            <MessageSquare className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
            <div className="min-w-0 flex-1">
              <div className="truncate font-medium">{label}</div>
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <span>{session.messageCount} messages</span>
                <span>{formatRelativeTime(session.updated)}</span>
              </div>
            </div>
          </button>
        );
      })}
    </div>
  );
}
