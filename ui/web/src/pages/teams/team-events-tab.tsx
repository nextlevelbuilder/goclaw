import { useState, useRef, useEffect, useMemo, useCallback } from "react";
import { Radio, Trash2, Pause, Play, ArrowDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { EmptyState } from "@/components/shared/empty-state";
import { useTeamEventStore } from "@/stores/use-team-event-store";
import { EventCard } from "./event-sections";

const EVENT_CATEGORIES = [
  { label: "All", value: "all" },
  { label: "Delegation", value: "delegation" },
  { label: "Task", value: "team.task" },
  { label: "Message", value: "team.message" },
  { label: "Agent", value: "agent" },
  { label: "Team CRUD", value: "team.crud" },
  { label: "Agent Link", value: "agent_link" },
] as const;

interface TeamEventsTabProps {
  teamId: string;
}

export function TeamEventsTab({ teamId }: TeamEventsTabProps) {
  const allEvents = useTeamEventStore((s) => s.events);
  const paused = useTeamEventStore((s) => s.paused);
  const setPaused = useTeamEventStore((s) => s.setPaused);
  const clear = useTeamEventStore((s) => s.clear);

  const [categoryFilter, setCategoryFilter] = useState("all");
  const [isAtBottom, setIsAtBottom] = useState(true);
  const feedRef = useRef<HTMLDivElement>(null);

  // Filter events for this team (null teamId = global events like team.created)
  const teamEvents = useMemo(
    () => allEvents.filter((e) => e.teamId === teamId || e.teamId === null),
    [allEvents, teamId],
  );

  // Apply category filter
  const filteredEvents = useMemo(() => {
    if (categoryFilter === "all") return teamEvents;
    if (categoryFilter === "team.crud") {
      return teamEvents.filter(
        (e) =>
          e.event === "team.created" ||
          e.event === "team.updated" ||
          e.event === "team.deleted" ||
          e.event.startsWith("team.member."),
      );
    }
    return teamEvents.filter((e) => e.event.startsWith(categoryFilter));
  }, [teamEvents, categoryFilter]);

  // Auto-scroll to bottom when new events arrive (only if already at bottom)
  useEffect(() => {
    if (isAtBottom && feedRef.current) {
      feedRef.current.scrollTop = feedRef.current.scrollHeight;
    }
  }, [filteredEvents.length, isAtBottom]);

  const handleScroll = () => {
    if (!feedRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = feedRef.current;
    setIsAtBottom(scrollHeight - scrollTop - clientHeight < 50);
  };

  const scrollToBottom = useCallback(() => {
    if (feedRef.current) {
      feedRef.current.scrollTop = feedRef.current.scrollHeight;
    }
    setIsAtBottom(true);
  }, []);

  return (
    <div className="rounded-md border">
      {/* Header — status + controls */}
      <div className="flex items-center justify-between border-b bg-muted/50 px-4 py-2.5">
        <div className="flex items-center gap-2">
          <Badge variant={paused ? "warning" : "success"} className="text-xs">
            {paused ? "Paused" : "Live"}
          </Badge>
          <span className="text-xs text-muted-foreground">
            {filteredEvents.length} event{filteredEvents.length !== 1 ? "s" : ""}
            {teamEvents.length !== filteredEvents.length && (
              <span> / {teamEvents.length} total</span>
            )}
          </span>
        </div>
        <div className="flex items-center gap-1.5">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setPaused(!paused)}
            className="h-7 gap-1 px-2 text-xs"
          >
            {paused ? <Play className="h-3 w-3" /> : <Pause className="h-3 w-3" />}
            {paused ? "Resume" : "Pause"}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={clear}
            className="h-7 gap-1 px-2 text-xs"
          >
            <Trash2 className="h-3 w-3" /> Clear
          </Button>
        </div>
      </div>

      {/* Category filter pills */}
      <div className="flex flex-wrap items-center gap-1.5 border-b px-4 py-2">
        {EVENT_CATEGORIES.map((cat) => (
          <button
            key={cat.value}
            type="button"
            onClick={() => setCategoryFilter(cat.value)}
            className={`rounded-full px-2.5 py-0.5 text-xs transition-colors ${
              categoryFilter === cat.value
                ? "bg-primary text-primary-foreground"
                : "text-muted-foreground hover:bg-muted"
            }`}
          >
            {cat.label}
          </button>
        ))}
      </div>

      {/* Event feed */}
      {filteredEvents.length === 0 ? (
        <div className="px-4 py-12">
          <EmptyState
            icon={Radio}
            title="No events yet"
            description={
              paused
                ? "Event capture is paused. Resume to see new events."
                : "Waiting for real-time team events... Events will appear as team agents work."
            }
          />
        </div>
      ) : (
        <div className="relative">
          <div
            ref={feedRef}
            onScroll={handleScroll}
            className="max-h-[calc(100vh-400px)] min-h-[200px] space-y-2 overflow-y-auto p-3"
          >
            {filteredEvents.map((entry) => (
              <EventCard key={entry.id} entry={entry} />
            ))}
          </div>

          {/* Scroll-to-bottom FAB — shown when user scrolled up */}
          {!isAtBottom && (
            <button
              type="button"
              onClick={scrollToBottom}
              className="absolute bottom-4 right-4 flex h-8 w-8 items-center justify-center rounded-full border bg-background shadow-md transition-colors hover:bg-muted"
              title="Scroll to bottom"
            >
              <ArrowDown className="h-4 w-4" />
            </button>
          )}
        </div>
      )}
    </div>
  );
}
