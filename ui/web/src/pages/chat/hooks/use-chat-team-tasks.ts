import { useState, useCallback, useMemo } from "react";
import { useWsEvent } from "@/hooks/use-ws-event";
import { Events } from "@/api/protocol";
import type { ChatMessage, ActiveTeamTask } from "@/types/chat";

/**
 * Manages active team task state and inline notification messages
 * from team task WebSocket events for a chat session.
 */
export function useChatTeamTasks(
  addMessage: (msg: ChatMessage) => void,
) {
  const [teamTasks, setTeamTasks] = useState<ActiveTeamTask[]>([]);

  // Team task event handler (curried by event name)
  const handleTeamTaskEvent = useCallback(
    (eventName: string) => (payload: unknown) => {
      const event = payload as {
        team_id?: string;
        task_id?: string;
        task_number?: number;
        subject?: string;
        status?: string;
        owner_agent_key?: string;
        owner_display_name?: string;
        progress_percent?: number;
        progress_step?: string;
        reason?: string;
        channel?: string;
      };
      if (!event?.team_id) return;

      // Only process team task events from WS channel
      if (event.channel && event.channel !== "ws") return;

      setTeamTasks((prev) => {
        const existing = prev.find((t) => t.taskId === event.task_id);

        if (eventName === "team.task.dispatched" || eventName === "team.task.assigned") {
          if (existing) {
            return prev.map((t) =>
              t.taskId === event.task_id
                ? { ...t, status: event.status ?? "in_progress", ownerAgentKey: event.owner_agent_key, ownerDisplayName: event.owner_display_name }
                : t,
            );
          }
          return [
            ...prev,
            {
              taskId: event.task_id ?? "",
              taskNumber: event.task_number ?? 0,
              subject: event.subject ?? "",
              status: event.status ?? "in_progress",
              ownerAgentKey: event.owner_agent_key,
              ownerDisplayName: event.owner_display_name,
            },
          ];
        }

        if (eventName === "team.task.progress" && existing) {
          return prev.map((t) =>
            t.taskId === event.task_id
              ? { ...t, progressPercent: event.progress_percent, progressStep: event.progress_step }
              : t,
          );
        }

        if (eventName === "team.task.completed" || eventName === "team.task.failed" || eventName === "team.task.cancelled") {
          return prev.filter((t) => t.taskId !== event.task_id);
        }

        if (eventName === "team.task.commented" && existing) {
          return prev.map((t) =>
            t.taskId === event.task_id ? { ...t, commentCount: (t.commentCount ?? 0) + 1 } : t,
          );
        }
        if (eventName === "team.task.attachment_added" && existing) {
          return prev.map((t) =>
            t.taskId === event.task_id ? { ...t, attachmentCount: (t.attachmentCount ?? 0) + 1 } : t,
          );
        }

        return prev;
      });

      // Add inline notification message for key events
      if (
        eventName === "team.task.dispatched" ||
        eventName === "team.task.completed" ||
        eventName === "team.task.failed" ||
        eventName === "team.task.commented" ||
        eventName === "team.task.attachment_added"
      ) {
        let icon: string;
        let text: string;
        if (eventName === "team.task.completed") { icon = "✅"; text = `Task #${event.task_number} "${event.subject}" completed`; }
        else if (eventName === "team.task.failed") { icon = "❌"; text = `Task #${event.task_number} "${event.subject}" failed${event.reason ? ": " + event.reason : ""}`; }
        else if (eventName === "team.task.commented") { icon = "💬"; text = `Comment on #${event.task_number} "${event.subject}"`; }
        else if (eventName === "team.task.attachment_added") { icon = "📎"; text = `File attached to #${event.task_number} "${event.subject}"`; }
        else { icon = "📋"; text = `Task #${event.task_number} "${event.subject}" → ${event.owner_display_name || event.owner_agent_key}`; }

        addMessage({
          role: "assistant" as const,
          content: `${icon} ${text}`,
          timestamp: Date.now(),
          isNotification: true,
          notificationType: eventName,
        });
      }
    },
    [addMessage],
  );

  // Memoize bound handlers so useWsEvent doesn't re-register on every render
  const onTaskDispatched = useMemo(() => handleTeamTaskEvent("team.task.dispatched"), [handleTeamTaskEvent]);
  const onTaskCompleted = useMemo(() => handleTeamTaskEvent("team.task.completed"), [handleTeamTaskEvent]);
  const onTaskFailed = useMemo(() => handleTeamTaskEvent("team.task.failed"), [handleTeamTaskEvent]);
  const onTaskCancelled = useMemo(() => handleTeamTaskEvent("team.task.cancelled"), [handleTeamTaskEvent]);
  const onTaskProgress = useMemo(() => handleTeamTaskEvent("team.task.progress"), [handleTeamTaskEvent]);
  const onTaskAssigned = useMemo(() => handleTeamTaskEvent("team.task.assigned"), [handleTeamTaskEvent]);
  const onTaskCommented = useMemo(() => handleTeamTaskEvent("team.task.commented"), [handleTeamTaskEvent]);
  const onTaskAttached = useMemo(() => handleTeamTaskEvent("team.task.attachment_added"), [handleTeamTaskEvent]);

  useWsEvent(Events.TEAM_TASK_DISPATCHED, onTaskDispatched);
  useWsEvent(Events.TEAM_TASK_COMPLETED, onTaskCompleted);
  useWsEvent(Events.TEAM_TASK_FAILED, onTaskFailed);
  useWsEvent(Events.TEAM_TASK_CANCELLED, onTaskCancelled);
  useWsEvent(Events.TEAM_TASK_PROGRESS, onTaskProgress);
  useWsEvent(Events.TEAM_TASK_ASSIGNED, onTaskAssigned);
  useWsEvent(Events.TEAM_TASK_COMMENTED, onTaskCommented);
  useWsEvent(Events.TEAM_TASK_ATTACHMENT_ADDED, onTaskAttached);

  return { teamTasks, setTeamTasks };
}
