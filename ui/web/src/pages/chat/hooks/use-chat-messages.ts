import { useState, useEffect, useCallback, useRef } from "react";
import { useWs } from "@/hooks/use-ws";
import { useWsEvent } from "@/hooks/use-ws-event";
import { Methods, Events } from "@/api/protocol";
import type { Message } from "@/types/session";
import type { ChatMessage, AgentEventPayload, ToolStreamEntry, RunActivity, ActiveTeamTask, MediaItem } from "@/types/chat";
import { toFileUrl, mediaKindFromMime } from "@/lib/file-helpers";
import { transformHistoryMessages } from "@/adapters/chat-message.adapter";
import { useChatTeamTasks } from "./use-chat-team-tasks";

/**
 * Manages chat message history and real-time streaming for a session.
 * Listens to "agent" events for chunks, tool calls, and run lifecycle.
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

  // Refs for values accessed inside event handler to avoid stale closures
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

  // Add a local message optimistically
  const addLocalMessage = useCallback((msg: ChatMessage) => {
    setMessages((prev) => [...prev, msg]);
  }, []);

  // Team task handling (extracted hook)
  const { teamTasks, setTeamTasks } = useChatTeamTasks(addLocalMessage);

  // Reset streaming/run state when session changes
  const prevKeyRef = useRef(sessionKey);
  useEffect(() => {
    if (sessionKey === prevKeyRef.current) return;
    const wasEmpty = !prevKeyRef.current;
    prevKeyRef.current = sessionKey;
    if (wasEmpty) return; // new-chat send flow, don't reset

    setStreamText(null); setThinkingText(null); setToolStream([]);
    setIsRunning(false); setActivity(null); setBlockReplies([]); setTeamTasks([]);
    runIdRef.current = null; expectingRunRef.current = false;
    streamRef.current = ""; thinkingRef.current = "";
    toolStreamRef.current = []; activityRef.current = null; blockRepliesRef.current = [];
    cancelAnimationFrame(rafHandleRef.current); rafPendingRef.current = false;
    if (!sessionKey) setMessages([]);
  }, [sessionKey, setTeamTasks]);

  // Load history
  const loadHistory = useCallback(async (mediaItems?: MediaItem[]) => {
    if (!ws.isConnected || !sessionKey) { setLoading(false); return; }
    try {
      const res = await ws.call<{ messages: Message[] }>(Methods.CHAT_HISTORY, { agentId, sessionKey });
      setMessages(transformHistoryMessages(res.messages ?? [], mediaItems));
    } catch { /* will retry */ } finally { setLoading(false); }
  }, [ws, agentId, sessionKey]);

  // Load history + restore running state when session changes
  useEffect(() => {
    let cancelled = false;
    if (sessionKey) {
      loadHistory();
      ws.call<{ isRunning?: boolean; runId?: string; activity?: RunActivity }>(Methods.CHAT_SESSION_STATUS, { sessionKey })
        .then((res) => {
          if (cancelled) return;
          if (res.isRunning) { setIsRunning(true); if (res.runId) runIdRef.current = res.runId; }
          if (res.activity) { setActivity(res.activity); activityRef.current = res.activity; }
        }).catch(() => {});
      ws.call<{ tasks?: ActiveTeamTask[] }>(Methods.TEAMS_TASK_ACTIVE_BY_SESSION, { sessionKey })
        .then((res) => { if (!cancelled && res.tasks?.length) setTeamTasks(res.tasks); }).catch(() => {});
    }
    return () => { cancelled = true; };
  }, [sessionKey, loadHistory, ws, setTeamTasks]);

  // Called before sending so event handler captures run.started
  const expectRun = useCallback(() => { expectingRunRef.current = true; }, []);

  // Agent event handler
  const handleAgentEvent = useCallback(
    (payload: unknown) => {
      const event = payload as AgentEventPayload;
      if (!event) return;
      if (event.channel && event.channel !== "ws" && !event.runKind) return;
      if (event.sessionKey && event.sessionKey !== sessionKeyRef.current) return;

      // Capture run.started
      if (event.type === "run.started" && event.agentId === agentIdRef.current) {
        if (expectingRunRef.current || event.runKind === "announce") {
          runIdRef.current = event.runId; expectingRunRef.current = false;
          setIsRunning(true); setStreamText(null); setThinkingText(null); setToolStream([]);
          streamRef.current = ""; thinkingRef.current = ""; toolStreamRef.current = [];
        }
        return;
      }

      if (!runIdRef.current || event.runId !== runIdRef.current) return;

      switch (event.type) {
        case "thinking": {
          thinkingRef.current += event.payload?.content ?? "";
          if (!rafPendingRef.current) {
            rafPendingRef.current = true;
            rafHandleRef.current = requestAnimationFrame(() => {
              rafPendingRef.current = false;
              setThinkingText(thinkingRef.current); setStreamText(streamRef.current);
            });
          }
          break;
        }
        case "chunk": {
          streamRef.current += event.payload?.content ?? "";
          if (!rafPendingRef.current) {
            rafPendingRef.current = true;
            rafHandleRef.current = requestAnimationFrame(() => {
              rafPendingRef.current = false;
              setStreamText(streamRef.current); setThinkingText(thinkingRef.current);
            });
          }
          break;
        }
        case "tool.call": {
          const entry: ToolStreamEntry = {
            toolCallId: event.payload?.id ?? "", runId: event.runId,
            name: event.payload?.name ?? "tool", arguments: event.payload?.arguments,
            phase: "calling", startedAt: Date.now(), updatedAt: Date.now(),
          };
          toolStreamRef.current = [...toolStreamRef.current, entry];
          setToolStream(toolStreamRef.current);
          break;
        }
        case "tool.result": {
          const isError = event.payload?.is_error;
          const resultId = event.payload?.id;
          const now = Date.now();
          toolStreamRef.current = toolStreamRef.current.map((t) =>
            t.toolCallId === resultId
              ? { ...t, phase: isError ? ("error" as const) : ("completed" as const), errorContent: isError ? event.payload?.content : undefined, result: event.payload?.result, updatedAt: now }
              : t,
          );
          setToolStream(toolStreamRef.current);
          break;
        }
        case "block.reply": {
          const content = event.payload?.content ?? "";
          if (content) {
            const blockMsg: ChatMessage = { role: "assistant", content, timestamp: Date.now(), isBlockReply: true };
            blockRepliesRef.current = [...blockRepliesRef.current, blockMsg];
            setBlockReplies(blockRepliesRef.current);
          }
          break;
        }
        case "activity": {
          const phase = event.payload?.phase as RunActivity["phase"];
          if (phase) {
            const newActivity: RunActivity = { phase, tool: event.payload?.tool as string | undefined, tools: event.payload?.tools as string[] | undefined, iteration: event.payload?.iteration as number | undefined };
            activityRef.current = newActivity; setActivity(newActivity);
          }
          break;
        }
        case "run.retrying": {
          activityRef.current = { phase: "retrying", retryAttempt: Number(event.payload?.attempt) || 0, retryMax: Number(event.payload?.maxAttempts) || 0 };
          setActivity(activityRef.current);
          break;
        }
        case "run.completed": {
          cancelAnimationFrame(rafHandleRef.current); rafPendingRef.current = false;
          setIsRunning(false); runIdRef.current = null;
          const hadTools = toolStreamRef.current.length > 0;
          const streamed = streamRef.current;
          setStreamText(null); setThinkingText(null); setToolStream([]);
          streamRef.current = ""; thinkingRef.current = ""; toolStreamRef.current = [];
          activityRef.current = null; setActivity(null);
          blockRepliesRef.current = []; setBlockReplies([]);
          const rawMedia = event.payload?.media;
          const mediaItems: MediaItem[] | undefined = rawMedia?.length
            ? rawMedia.map((m) => ({ path: toFileUrl(m.path), mimeType: m.content_type ?? "application/octet-stream", fileName: m.path.split("?")[0]?.split("/").pop() ?? "file", size: m.size, kind: mediaKindFromMime(m.content_type ?? "") }))
            : undefined;
          if (streamed && !hadTools) {
            setMessages((prev) => [...prev, { role: "assistant", content: streamed, timestamp: Date.now(), mediaItems }]);
          } else { loadHistory(mediaItems); }
          break;
        }
        case "run.failed": {
          cancelAnimationFrame(rafHandleRef.current); rafPendingRef.current = false;
          setIsRunning(false); runIdRef.current = null;
          setStreamText(null); setThinkingText(null); setToolStream([]);
          streamRef.current = ""; thinkingRef.current = "";
          activityRef.current = null; setActivity(null);
          blockRepliesRef.current = []; setBlockReplies([]);
          setMessages((prev) => [...prev, { role: "assistant", content: `Error: ${event.payload?.error ?? "Unknown error"}`, timestamp: Date.now() }]);
          break;
        }
        case "run.cancelled": {
          cancelAnimationFrame(rafHandleRef.current); rafPendingRef.current = false;
          setIsRunning(false); runIdRef.current = null;
          const streamed = streamRef.current;
          setStreamText(null); setThinkingText(null); setToolStream([]);
          streamRef.current = ""; thinkingRef.current = ""; toolStreamRef.current = [];
          activityRef.current = null; setActivity(null);
          blockRepliesRef.current = []; setBlockReplies([]);
          if (streamed) { setMessages((prev) => [...prev, { role: "assistant", content: streamed, timestamp: Date.now() }]); }
          else { loadHistory(); }
          break;
        }
      }
    },
    [loadHistory],
  );

  useWsEvent(Events.AGENT, handleAgentEvent);

  // Leader processing: backend emits when announce queue drains
  const handleLeaderProcessing = useCallback((payload: unknown) => {
    const event = payload as { agentId?: string; tasks?: number };
    if (event?.agentId === agentIdRef.current) {
      activityRef.current = { phase: "leader_processing" };
      setActivity(activityRef.current);
    }
  }, []);
  useWsEvent(Events.TEAM_LEADER_PROCESSING, handleLeaderProcessing);

  const isBusy = isRunning || teamTasks.length > 0 || activity?.phase === "leader_processing";

  return {
    messages, streamText, thinkingText, toolStream, isRunning, isBusy,
    loading, activity, blockReplies, teamTasks, expectRun, loadHistory, addLocalMessage,
  };
}
