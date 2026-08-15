import { memo } from "react";
import { AnimatePresence, LayoutGroup } from "framer-motion";
import { Badge } from "@/components/ui/badge";
import { useTranslation } from "react-i18next";
import { STATUS_COLORS } from "./board-utils";
import { KanbanCard } from "./kanban-card";
import type { TeamTaskData } from "@/types/team";

interface KanbanColumnProps {
  columnId: string;
  title: string;
  tasks: TeamTaskData[];
  isTeamV2?: boolean;
  emojiLookup?: Map<string, string>;
  memberLookup?: Map<string, string>;
  taskLookup?: Map<string, string>;
  onTaskClick: (task: TeamTaskData) => void;
  onDeleteTask?: (taskId: string) => void;
}

export const KanbanColumn = memo(function KanbanColumn({ columnId, title, tasks, isTeamV2, emojiLookup, memberLookup, taskLookup, onTaskClick, onDeleteTask }: KanbanColumnProps) {
  const { t } = useTranslation("teams");

  return (
    <div className="flex max-h-full w-[280px] shrink-0 flex-col rounded-xl border bg-card shadow-sm">
      <div className="flex items-center gap-2 px-3 py-2.5">
        <span className={`h-2.5 w-2.5 rounded-full ${STATUS_COLORS[columnId] ?? "bg-gray-400"}`} />
        <span className="text-sm font-medium capitalize">{title.replace(/_/g, " ")}</span>
        <Badge variant="secondary" className="ml-auto text-2xs px-1.5 py-0">{tasks.length}</Badge>
      </div>

      <div className="flex flex-1 flex-col gap-2 overflow-y-auto overscroll-contain px-2 pb-2">
        {tasks.length === 0 ? (
          <div className="py-6 text-center text-xs text-muted-foreground">{t("board.emptyColumn")}</div>
        ) : (
          <LayoutGroup>
            <AnimatePresence mode="popLayout">
              {tasks.map((task) => (
                <KanbanCard
                  key={task.id}
                  task={task}
                  isTeamV2={isTeamV2}
                  emojiLookup={emojiLookup}
                  memberLookup={memberLookup}
                  taskLookup={taskLookup}
                  onClick={() => onTaskClick(task)}
                  onDelete={onDeleteTask}
                />
              ))}
            </AnimatePresence>
          </LayoutGroup>
        )}
      </div>
    </div>
  );
});
