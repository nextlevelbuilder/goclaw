import type { TeamTaskData, TeamMemberData } from "@/types/team";

/** All kanban column statuses in display order */
export const KANBAN_STATUSES = [
  "pending",
  "in_progress",
  "completed",
  "blocked",
  "failed",
  "cancelled",
] as const;

export type KanbanStatus = (typeof KANBAN_STATUSES)[number];

/** Status display colors for column headers */
export const STATUS_COLORS: Record<string, string> = {
  pending: "bg-slate-400",
  in_progress: "bg-blue-500",
  completed: "bg-green-500",
  blocked: "bg-amber-500",
  failed: "bg-red-500",
  cancelled: "bg-gray-400",
};

/** Group tasks by a field for kanban columns */
export function groupTasksBy(
  tasks: TeamTaskData[],
  key: "status" | "owner" | "type",
): Map<string, TeamTaskData[]> {
  const map = new Map<string, TeamTaskData[]>();
  for (const task of tasks) {
    let groupKey: string;
    switch (key) {
      case "status":
        groupKey = task.status;
        break;
      case "owner":
        groupKey = task.owner_agent_key || "unassigned";
        break;
      case "type":
        groupKey = task.task_type || "general";
        break;
    }
    const arr = map.get(groupKey) ?? [];
    arr.push(task);
    map.set(groupKey, arr);
  }
  return map;
}

/** Build task ID -> subject lookup for resolving blocked_by references */
export function buildTaskLookup(tasks: TeamTaskData[]): Map<string, string> {
  const map = new Map<string, string>();
  for (const t of tasks) map.set(t.id, t.subject);
  return map;
}

/** Build agent_id -> display name lookup from members */
export function buildMemberLookup(
  members: TeamMemberData[],
): Map<string, string> {
  const map = new Map<string, string>();
  for (const m of members)
    map.set(m.agent_id, m.display_name || m.agent_key || m.agent_id.slice(0, 8));
  return map;
}

/** Build agent_id -> emoji lookup from members */
export function buildEmojiLookup(
  members: TeamMemberData[],
): Map<string, string> {
  const map = new Map<string, string>();
  for (const m of members) {
    if (m.emoji) map.set(m.agent_id, m.emoji);
  }
  return map;
}

/** Check if agent is actively running on a task */
export function isTaskLocked(task: TeamTaskData): boolean {
  if (!task.locked_at) return false;
  const expiry = task.lock_expires_at ? new Date(task.lock_expires_at) : null;
  return !expiry || expiry > new Date();
}
