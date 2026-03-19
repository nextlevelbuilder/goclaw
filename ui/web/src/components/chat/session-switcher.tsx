import { memo } from "react";
import { useTranslation } from "react-i18next";
import { MessageSquare } from "lucide-react";
import { formatRelativeTime } from "@/lib/format";
import type { SessionInfo } from "@/types/session";

interface SessionSwitcherProps {
  sessions: SessionInfo[];
  activeKey: string;
  onSelect: (key: string) => void;
  loading?: boolean;
}

/** Build a human-friendly label from session metadata or key */
function sessionLabel(session: SessionInfo): string {
  // Prefer explicit titles/labels
  if (session.metadata?.chat_title) return session.metadata.chat_title;
  if (session.metadata?.display_name) return session.metadata.display_name;
  if (session.label) return session.label;

  // Parse the scope part from "agent:{id}:{scope}"
  const parts = session.key.split(":");
  const scope = parts.length >= 3 ? parts.slice(2).join(":") : session.key;

  // Friendly format for known patterns
  if (scope.startsWith("ws-")) {
    // "ws-system-mmxarlgc" → "Chat mmxarlgc"
    const segments = scope.split("-");
    const shortId = segments[segments.length - 1] ?? scope;
    return `Chat ${shortId}`;
  }
  if (scope.startsWith("ws:direct:")) {
    // "ws:direct:uuid" → "Chat uuid (short)"
    const uuid = scope.replace("ws:direct:", "");
    return `Chat ${uuid.slice(0, 8)}`;
  }
  if (scope.startsWith("team:")) return `Team ${scope.replace("team:", "").slice(0, 12)}`;
  if (scope.startsWith("cron:")) return `Cron ${scope.replace("cron:", "")}`;

  // Fallback: truncate long scope
  return scope.length > 24 ? scope.slice(0, 21) + "…" : scope;
}

export const SessionSwitcher = memo(function SessionSwitcher({ sessions, activeKey, onSelect, loading }: SessionSwitcherProps) {
  const { t } = useTranslation("common");

  if (sessions.length === 0 && loading) {
    return (
      <div className="space-y-2 p-2">
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className="h-12 animate-pulse rounded-lg bg-muted" />
        ))}
      </div>
    );
  }

  if (sessions.length === 0) {
    return (
      <div className="px-4 py-8 text-center text-sm text-muted-foreground">
        {t("noSessions")}
      </div>
    );
  }

  return (
    <div className="space-y-0.5 p-1.5">
      {sessions.map((session) => {
        const isActive = session.key === activeKey;
        const label = sessionLabel(session);

        return (
          <button
            key={session.key}
            type="button"
            onClick={() => onSelect(session.key)}
            className={`flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-sm transition-colors ${
              isActive ? "bg-accent text-accent-foreground" : "hover:bg-muted"
            }`}
          >
            <MessageSquare className="h-4 w-4 shrink-0 text-muted-foreground" />
            <div className="min-w-0 flex-1">
              <div className="truncate font-medium text-[13px]">{label}</div>
              <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
                <span>{session.messageCount} {t("messages")}</span>
                <span>·</span>
                <span>{formatRelativeTime(session.updated)}</span>
              </div>
            </div>
          </button>
        );
      })}
    </div>
  );
});
