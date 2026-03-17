import { useMemo, memo } from "react";
import { groupTasksBy, KANBAN_STATUSES } from "./board-utils";
import { KanbanColumn } from "./kanban-column";
import type { GroupBy } from "../stores/use-board-store";
import type { TeamTaskData } from "@/types/team";

interface KanbanBoardProps {
  tasks: TeamTaskData[];
  isTeamV2?: boolean;
  groupBy: GroupBy;
  emojiLookup?: Map<string, string>;
  taskLookup?: Map<string, string>;
  onTaskClick: (task: TeamTaskData) => void;
  onDeleteTask?: (taskId: string) => void;
}

export const KanbanBoard = memo(function KanbanBoard({ tasks, isTeamV2, groupBy, emojiLookup, taskLookup, onTaskClick, onDeleteTask }: KanbanBoardProps) {
  const grouped = useMemo(() => groupTasksBy(tasks, groupBy), [tasks, groupBy]);

  const columns = useMemo(() => {
    if (groupBy === "status") {
      return [...KANBAN_STATUSES];
    }
    return [...grouped.keys()].sort();
  }, [groupBy, grouped]);

  return (
    <div
      className="flex h-full gap-3 overflow-x-auto overscroll-contain p-4 scroll-smooth snap-x snap-mandatory rounded-lg"
      style={{
        backgroundImage: "radial-gradient(circle, var(--color-border) 1px, transparent 1px)",
        backgroundSize: "24px 24px",
      }}
    >
      {columns.map((col) => (
        <div key={col} className="snap-start self-start max-h-full flex">
          <KanbanColumn
            columnId={col}
            title={col}
            tasks={grouped.get(col) ?? []}
            isTeamV2={isTeamV2}
            emojiLookup={emojiLookup}
            taskLookup={taskLookup}
            onTaskClick={onTaskClick}
            onDeleteTask={onDeleteTask}
          />
        </div>
      ))}
    </div>
  );
});
