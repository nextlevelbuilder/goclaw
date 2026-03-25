import { useState, useEffect, useCallback, useRef, useMemo } from "react";
import { useWs } from "@/hooks/use-ws";
import { useWsEvent } from "@/hooks/use-ws-event";
import { Methods, Events } from "@/api/protocol";
import type { Message } from "@/types/session";
import type { ChatMessage, AgentEventPayload, ToolStreamEntry, RunActivity, ActiveTeamTask, MediaItem } from "@/types/chat";
import { toFileUrl, mediaKindFromMime } from "@/lib/file-helpers";
import { messageToTimestamp } from "@/lib/message-utils";

/**
 * Manages chat message history and real-time streaming for a session.
 * Listens to "agent" events for chunks, tool calls, and run lifecycle.
 *
 * The runId is captured from the first "run.started" event (not from the
 * chat.send RPC response, which only arrives after the run completes).
 */
export function useChatMessages(sessionKey: string, agentId: string) {
  const ws = useWs();
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [streamText, setStreamText] = useState<string | null>(null);
  const [thinkingText, setThinkingText] = useState<string | null>(null);
  const [toolStream, setToolStream] = useState<ToolStreamEntry[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const [loading, setLoading] = useState(false);
  const [activity, setActivity] = useState<RunActivity | null>(null);
  const [blockReplies, setBlockReplies] = useState<ChatMessage[]>([]);
  const [teamTasks, setTeamTasks] = useState<ActiveTeamTask[]>([]);

  // Use refs for values accessed inside the event handler to avoid stale closures.
  const runIdRef = useRef<string | null>(null);
  const expectingRunRef = useRef(false);
  const streamRef = useRef("");
  const thinkingRef = useRef("");
  const toolStreamRef = useRef<ToolStreamEntry[]>([]);
  const agentIdRef = useRef(agentId);
  agentIdRef.current = agentId;
  const sessionKeyRef = useRef(sessionKey);
  sessionKeyRef.current = sessionKey;
  const activityRef = useRef<RunActivity | null>(null);
  const blockRepliesRef = useRef<ChatMessage[]>([]);
  const rafPendingRef = useRef(false);
  const rafHandleRef = useRef(0);

  // Reset streaming/run state when session changes.
  // Messages are NOT cleared here — loadHistory() will replace them atomically.
  // loading is NOT set to true — avoids full-page flash while history loads.
  const prevKeyRef = useRef(sessionKey);
  useEffect(() => {
    if (sessionKey === prevKeyRef.current) return;
    prevKeyRef.current = sessionKey;
    setStreamText(null);
    setThinkingText(null);
    setToolStream([]);
    setIsRunning(false);
    setActivity(null);
    setBlockReplies([]);
    setTeamTasks([]);
    runIdRef.current = null;
    expectingRunRef.current = false;
    streamRef.current = "";
    thinkingRef.current = "";
    toolStreamRef.current = [];
    activityRef.current = null;
    blockRepliesRef.current = [];
    cancelAnimationFrame(rafHandleRef.current);
    rafPendingRef.current = false;
    // Clear messages when navigating away from a session (empty key).
    // When switching to another session, loadHistory() will replace them.
    if (!sessionKey) {
      setMessages([]);
    }
  }, [sessionKey]);

  // Load history (no loading spinner — the empty state placeholder is shown instead)
  const loadHistory = useCallback(async (mediaItems?: MediaItem[]) => {
    if (!ws.isConnected || !sessionKey) {
      setLoading(false);
      return;
    }
    try {
      const res = await ws.call<{ messages: Message[] }>(Methods.CHAT_HISTORY, {
        agentId,
        sessionKey,
      });
      const allMsgs = res.messages ?? [];
      // Build a map of tool_call_id -> tool message for result lookup
      const toolResultMap = new Map<string, Message>();
      for (const m of allMsgs) {
        if (m.role === "tool" && m.tool_call_id) {
          toolResultMap.set(m.tool_call_id, m);
        }
      }
      const msgs: ChatMessage[] = allMsgs.map((m: Message, i: number) => {
        const chatMsg: ChatMessage = {
          ...m,
          timestamp: messageToTimestamp(m, i, allMsgs.length),
        };
        // Convert persisted media_refs to mediaItems for gallery display
        if (m.role === "assistant" && m.media_refs && m.media_refs.length > 0) {
          chatMsg.mediaItems = m.media_refs.map((ref) => ({
            path: toFileUrl(ref.path || ref.id),
            mimeType: ref.mime_type,
            fileName: ref.path?.split("/").pop() ?? ref.id,
            kind: (ref.kind as MediaItem["kind"]) || "document",
          }));
        }
        // Reconstruct toolDetails for assistant messages with tool_calls
        if (m.role === "assistant" && m.tool_calls && m.tool_calls.length > 0) {
          chatMsg.toolDetails = m.tool_calls.map((tc) => {
            const toolMsg = toolResultMap.get(tc.id);
            return {
              toolCallId: tc.id,
              runId: "",
              name: tc.name,
              phase: (toolMsg ? (toolMsg.is_error ? "error" : "completed") : "calling") as ToolStreamEntry["phase"],
              startedAt: 0,
              updatedAt: 0,
              arguments: tc.arguments,
              result: toolMsg?.content,
            };
          });
        }
        return chatMsg;
      });
      // Attach media to the last assistant message if provided
      if (mediaItems?.length && msgs.length > 0) {
        for (let i = msgs.length - 1; i >= 0; i--) {
          if (msgs[i]!.role === "assistant") {
            msgs[i] = { ...msgs[i]!, mediaItems };
            break;
          }
        }
      }
      setMessages(msgs);
    } catch {
      // will retry
    } finally {
      setLoading(false);
    }
  }, [ws, agentId, sessionKey]);

  // Load history, restore running state and active tasks when session changes.
  // Cancelled flag prevents stale RPC responses from overwriting state on rapid switches.
  useEffect(() => {
    let cancelled = false;
    if (sessionKey) {
      loadHistory();
      // Restore session running state
      ws.call<{ isRunning?: boolean; activity?: RunActivity }>(Methods.CHAT_SESSION_STATUS, { sessionKey })
        .then((res) => {
          if (cancelled) return;
          if (res.isRunning) setIsRunning(true);
          if (res.activity) {
            setActivity(res.activity);
            activityRef.current = res.activity;
          }
        })
        .catch(() => {});
      // Restore active team tasks (teams module may not be configured)
      ws.call<{ tasks?: ActiveTeamTask[] }>(Methods.TEAMS_TASK_ACTIVE_BY_SESSION, { sessionKey })
        .then((res) => {
          if (cancelled) return;
          if (res.tasks && res.tasks.length > 0) setTeamTasks(res.tasks);
        })
        .catch(() => {});
    }
    return () => { cancelled = true; };
  }, [sessionKey, loadHistory, ws]);

  // Called before sending a message so the event handler knows to capture run.started
  const expectRun = useCallback(() => {
    expectingRunRef.current = true;
  }, []);

  // Stable event handler using refs for mutable state
  const handleAgentEvent = useCallback(
    (payload: unknown) => {
      const event = payload as AgentEventPayload;
      if (!event) return;

      // Only process events from WS channel targeting the active session.
      // Delegations/announces (runKind set) are allowed regardless of channel.
      if (event.channel && event.channel !== "ws" && !event.runKind) {
        return;
      }
      if (event.sessionKey && event.sessionKey !== sessionKeyRef.current) {
        return;
      }

      // Capture run.started when we are expecting a run for this agent,
      // OR when an announce run starts (leader summarising team results).
      if (event.type === "run.started" && event.agentId === agentIdRef.current) {
        if (expectingRunRef.current || event.runKind === "announce") {
          runIdRef.current = event.runId;
          expectingRunRef.current = false;
          setIsRunning(true);
          setStreamText(null);
          setThinkingText(null);
          setToolStream([]);
          streamRef.current = "";
          thinkingRef.current = "";
          toolStreamRef.current = [];
        }
        return;
      }

      // All other events must match the active runId
      if (!runIdRef.current || event.runId !== runIdRef.current) return;

      switch (event.type) {
        case "thinking": {
          const content = event.payload?.content ?? "";
          thinkingRef.current += content;
          // Batch state updates: only one setState per animation frame
          if (!rafPendingRef.current) {
            rafPendingRef.current = true;
            rafHandleRef.current = requestAnimationFrame(() => {
              rafPendingRef.current = false;
              setThinkingText(thinkingRef.current);
              setStreamText(streamRef.current);
            });
          }
          break;
        }
        case "chunk": {
          const content = event.payload?.content ?? "";
          streamRef.current += content;
          // Batch state updates: only one setState per animation frame
          if (!rafPendingRef.current) {
            rafPendingRef.current = true;
            rafHandleRef.current = requestAnimationFrame(() => {
              rafPendingRef.current = false;
              setStreamText(streamRef.current);
              setThinkingText(thinkingRef.current);
            });
          }
          break;
        }
        case "tool.call": {
          const entry: ToolStreamEntry = {
            toolCallId: event.payload?.id ?? "",
            runId: event.runId,
            name: event.payload?.name ?? "tool",
            arguments: event.payload?.arguments,
            phase: "calling",
            startedAt: Date.now(),
            updatedAt: Date.now(),
          };
          toolStreamRef.current = [...toolStreamRef.current, entry];
          setToolStream(toolStreamRef.current);
          break;
        }
        case "tool.result": {
          const isError = event.payload?.is_error;
          const resultId = event.payload?.id;
          const now = Date.now();
          // Update ref first so subsequent tool.call events don't overwrite with stale data.
          toolStreamRef.current = toolStreamRef.current.map((t) =>
            t.toolCallId === resultId
              ? {
                  ...t,
                  phase: isError ? ("error" as const) : ("completed" as const),
                  errorContent: isError ? event.payload?.content : undefined,
                  result: event.payload?.result,
                  updatedAt: now,
                }
              : t,
          );
          setToolStream(toolStreamRef.current);
          break;
        }
        case "block.reply": {
          const content = event.payload?.content ?? "";
          if (content) {
            const blockMsg: ChatMessage = {
              role: "assistant",
              content,
              timestamp: Date.now(),
              isBlockReply: true,
            };
            blockRepliesRef.current = [...blockRepliesRef.current, blockMsg];
            setBlockReplies(blockRepliesRef.current);
          }
          break;
        }
        case "activity": {
          const phase = event.payload?.phase as RunActivity["phase"];
          if (phase) {
            const newActivity: RunActivity = {
              phase,
              tool: event.payload?.tool as string | undefined,
              tools: event.payload?.tools as string[] | undefined,
              iteration: event.payload?.iteration as number | undefined,
            };
            activityRef.current = newActivity;
            setActivity(newActivity);
          }
          break;
        }
        case "run.retrying": {
          const attempt = Number(event.payload?.attempt) || 0;
          const maxAttempts = Number(event.payload?.maxAttempts) || 0;
          activityRef.current = {
            phase: "retrying",
            retryAttempt: attempt,
            retryMax: maxAttempts,
          };
          setActivity(activityRef.current);
          break;
        }
        case "run.completed": {
          // Cancel any pending rAF to prevent stale state overwrite
          cancelAnimationFrame(rafHandleRef.current);
          rafPendingRef.current = false;

          setIsRunning(false);
          runIdRef.current = null;

          // If we have streamed text and no tool calls, promote it directly
          // to avoid a flash caused by loadHistory() replacing the DOM.
          // When tools were used, reload to get structured tool_calls data.
          const hadTools = toolStreamRef.current.length > 0;
          const streamed = streamRef.current;

          setStreamText(null);
          setThinkingText(null);
          setToolStream([]);
          streamRef.current = "";
          thinkingRef.current = "";
          toolStreamRef.current = [];
          activityRef.current = null;
          setActivity(null);
          blockRepliesRef.current = [];
          setBlockReplies([]);

          // Convert media from run.completed event to MediaItem[]
          const rawMedia = event.payload?.media;
          const mediaItems: MediaItem[] | undefined = rawMedia?.length
            ? rawMedia.map((m) => ({
                path: toFileUrl(m.path),
                mimeType: m.content_type ?? "application/octet-stream",
                fileName: m.path.split("/").pop() ?? "file",
                size: m.size,
                kind: mediaKindFromMime(m.content_type ?? ""),
              }))
            : undefined;

          if (streamed && !hadTools) {
            setMessages((prev) => [
              ...prev,
              { role: "assistant", content: streamed, timestamp: Date.now(), mediaItems },
            ]);
          } else {
            loadHistory(mediaItems);
          }
          break;
        }
        case "run.failed": {
          cancelAnimationFrame(rafHandleRef.current);
          rafPendingRef.current = false;

          setIsRunning(false);
          runIdRef.current = null;
          setStreamText(null);
          setThinkingText(null);
          setToolStream([]);
          streamRef.current = "";
          thinkingRef.current = "";
          activityRef.current = null;
          setActivity(null);
          blockRepliesRef.current = [];
          setBlockReplies([]);
          setMessages((prev) => [
            ...prev,
            {
              role: "assistant",
              content: `Error: ${event.payload?.error ?? "Unknown error"}`,
              timestamp: Date.now(),
            },
          ]);
          break;
        }
      }
    },
    [loadHistory],
  );

  useWsEvent(Events.AGENT, handleAgentEvent);

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
      };
      if (!event?.team_id) return;

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

        if (
          eventName === "team.task.completed" ||
          eventName === "team.task.failed" ||
          eventName === "team.task.cancelled"
        ) {
          return prev.filter((t) => t.taskId !== event.task_id);
        }

        // Increment counters for comment/attachment events
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
        let icon = "📋";
        let text = "";
        if (eventName === "team.task.completed") {
          icon = "✅";
          text = `Task #${event.task_number} "${event.subject}" completed`;
        } else if (eventName === "team.task.failed") {
          icon = "❌";
          text = `Task #${event.task_number} "${event.subject}" failed${event.reason ? ": " + event.reason : ""}`;
        } else if (eventName === "team.task.commented") {
          icon = "💬";
          text = `Comment on #${event.task_number} "${event.subject}"`;
        } else if (eventName === "team.task.attachment_added") {
          icon = "📎";
          text = `File attached to #${event.task_number} "${event.subject}"`;
        } else {
          icon = "📋";
          text = `Task #${event.task_number} "${event.subject}" → ${event.owner_display_name || event.owner_agent_key}`;
        }

        setMessages((prev) => [
          ...prev,
          {
            role: "assistant" as const,
            content: `${icon} ${text}`,
            timestamp: Date.now(),
            isNotification: true,
            notificationType: eventName,
          },
        ]);
      }
    },
    [],
  );

  // Memoize bound handlers so useWsEvent doesn't re-register on every render.
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

  // Leader processing: backend emits when announce queue drains (before announce run starts).
  const handleLeaderProcessing = useCallback((payload: unknown) => {
    const event = payload as { agentId?: string; tasks?: number };
    if (event?.agentId === agentIdRef.current) {
      activityRef.current = { phase: "leader_processing" };
      setActivity(activityRef.current);
    }
  }, []);
  useWsEvent(Events.TEAM_LEADER_PROCESSING, handleLeaderProcessing);

  // Add a local message optimistically (shown immediately, replaced on next loadHistory)
  const addLocalMessage = useCallback((msg: ChatMessage) => {
    setMessages((prev) => [...prev, msg]);
  }, []);

  // isBusy: true when main agent is running, team tasks are active,
  // or leader is processing team results before announce run starts.
  const isBusy = isRunning || teamTasks.length > 0 || activity?.phase === "leader_processing";

  return {
    messages,
    streamText,
    thinkingText,
    toolStream,
    isRunning,
    isBusy,
    loading,
    activity,
    blockReplies,
    teamTasks,
    expectRun,
    loadHistory,
    addLocalMessage,
  };
}
