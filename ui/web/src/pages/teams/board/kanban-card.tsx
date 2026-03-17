import { memo } from "react";
import { motion } from "framer-motion";
import { Trash2, Ban } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { useTranslation } from "react-i18next";
import { isTaskLocked } from "./board-utils";
import { isTerminalStatus } from "../task-sections/task-utils";
import type { TeamTaskData } from "@/types/team";

const PRIORITY_LABELS: Record<number, { label: string; color: string }> = {
  0: { label: "P-0", color: "text-slate-400" },
  1: { label: "P-1", color: "text-blue-500" },
  2: { label: "P-2", color: "text-amber-500" },
  3: { label: "P-3", color: "text-red-500" },
};

const PRIORITY_TOOLTIPS: Record<number, string> = {
  0: "Low",
  1: "Medium",
  2: "High",
  3: "Critical",
};

interface KanbanCardProps {
  task: TeamTaskData;
  isTeamV2?: boolean;
  emojiLookup?: Map<string, string>;
  taskLookup?: Map<string, string>;
  onClick: () => void;
  onDelete?: (taskId: string) => void;
}

export const KanbanCard = memo(function KanbanCard({ task, isTeamV2, emojiLookup, taskLookup, onClick, onDelete }: KanbanCardProps) {
  const { t } = useTranslation("teams");
  const locked = isTaskLocked(task);
  const blocked = task.status === "blocked";
  const ownerEmoji = task.owner_agent_id && emojiLookup?.get(task.owner_agent_id);
  const canDelete = onDelete && isTerminalStatus(task.status);
  const prio = PRIORITY_LABELS[task.priority] ?? PRIORITY_LABELS[0]!;
  const hasBlockers = task.blocked_by && task.blocked_by.length > 0;

  return (
    <motion.div
      layoutId={task.id}
      layout
      initial={false}
      transition={{ type: "spring", stiffness: 350, damping: 30 }}
      className={
        "group relative cursor-pointer rounded-lg border bg-card p-3 shadow-sm transition-colors hover:bg-accent/50" +
        (locked ? " border-l-2 border-l-green-500" : blocked ? " border-l-2 border-l-amber-500" : "")
      }
      onClick={onClick}
    >
      {/* Top row: identifier + running badge + delete button */}
      <div className="mb-1 flex items-center justify-between">
        <span className="font-mono text-[10px] text-muted-foreground">
          {task.identifier || `#${task.task_number ?? ""}`}
        </span>
        <div className="flex items-center gap-1.5">
          {locked && (
            <span className="flex items-center gap-1 text-[10px] text-green-600 dark:text-green-400">
              <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-green-500" />
              {t("board.running")}
            </span>
          )}
          {canDelete && (
            <button
              className="hidden rounded p-0.5 text-muted-foreground hover:bg-destructive/10 hover:text-destructive group-hover:inline-flex"
              title={t("tasks.delete")}
              onClick={(e) => { e.stopPropagation(); onDelete(task.id); }}
            >
              <Trash2 className="h-3 w-3" />
            </button>
          )}
        </div>
      </div>

      <p className="line-clamp-2 text-xs font-medium leading-snug">{task.subject}</p>

      {/* Blocked-by row */}
      {hasBlockers && (
        <p className="mt-1 flex items-center gap-1 text-[10px] text-amber-600 dark:text-amber-400">
          <Ban className="h-3 w-3 shrink-0" />
          <span className="truncate">
            {task.blocked_by!.map((id) => taskLookup?.get(id) || id.slice(0, 8)).join(", ")}
          </span>
        </p>
      )}

      {/* Bottom row: owner + type badge + priority */}
      <div className="mt-2 flex items-center gap-1.5">
        {ownerEmoji && <span className="text-sm leading-none">{ownerEmoji}</span>}
        <span className="truncate text-xs text-muted-foreground">
          {task.owner_agent_key || t("board.unassigned")}
        </span>
        {task.task_type && task.task_type !== "general" && (
          <Badge variant="outline" className="text-[10px] px-1 py-0">{task.task_type}</Badge>
        )}
        <span
          className={`ml-auto shrink-0 font-mono text-[10px] font-medium ${prio.color}`}
          title={`${PRIORITY_TOOLTIPS[task.priority] ?? "Low"} priority`}
        >
          {prio.label}
        </span>
      </div>

      {isTeamV2 && task.progress_percent != null && task.progress_percent > 0 && !isTerminalStatus(task.status) && (
        <div className="mt-2 flex items-center gap-1.5">
          <div className="h-1.5 flex-1 rounded-full bg-muted">
            <div className="h-full rounded-full bg-primary transition-all" style={{ width: `${task.progress_percent}%` }} />
          </div>
          <span className="text-[10px] text-muted-foreground">{task.progress_percent}%</span>
        </div>
      )}
    </motion.div>
  );
});
