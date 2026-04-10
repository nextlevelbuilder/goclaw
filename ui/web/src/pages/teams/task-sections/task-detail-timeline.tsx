import { Clock } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { useTranslation } from "react-i18next";
import { formatDate } from "@/lib/format";
import { CollapsibleSection } from "./task-detail-content";
import type { TeamTaskEvent } from "@/types/team";

/* ── Timeline with vertical dot-line ──────────────────────────── */

interface TaskDetailTimelineProps {
  events: TeamTaskEvent[];
  resolveMember: (id?: string) => string | undefined;
}

export function TaskDetailTimeline({ events, resolveMember }: TaskDetailTimelineProps) {
  const { t } = useTranslation("teams");

  if (events.length === 0) return null;

  return (
    <CollapsibleSection
      icon={Clock}
      title={t("tasks.detail.timeline")}
      count={events.length}
      defaultOpen={false}
    >
      <div className="space-y-0">
        {events.map((e, i) => (
          <div key={e.id} className="relative flex gap-3 pb-4 last:pb-0">
            {/* Dot + vertical line */}
            <div className="flex flex-col items-center">
              <div className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-primary" />
              {i < events.length - 1 && <div className="flex-1 w-px bg-border" />}
            </div>
            {/* Content */}
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 text-xs">
                <Badge variant="outline" className="text-2xs">{e.event_type}</Badge>
                <span className="text-muted-foreground">
                  {e.actor_type === "human" ? "Human" : (resolveMember(e.actor_id) || e.actor_id?.slice(0, 8) || "\u2014")}
                </span>
              </div>
              <span className="text-xs-plus text-muted-foreground">{formatDate(e.created_at)}</span>
            </div>
          </div>
        ))}
      </div>
    </CollapsibleSection>
  );
}
