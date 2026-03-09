import { useState, useEffect, useCallback, useRef } from "react";
import { useWs } from "@/hooks/use-ws";
import { useWsEvent } from "@/hooks/use-ws-event";
import { Methods, Events } from "@/api/protocol";
import type { Message } from "@/types/session";
import type { ChatMessage, AgentEventPayload, ToolStreamEntry } from "@/types/chat";

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

  // Use refs for values accessed inside the event handler to avoid stale closures.
  const runIdRef = useRef<string | null>(null);
  const expectingRunRef = useRef(false);
  const streamRef = useRef("");
  const thinkingRef = useRef("");
  const toolStreamRef = useRef<ToolStreamEntry[]>([]);
  const agentIdRef = useRef(agentId);
  agentIdRef.current = agentId;

  // Synchronously clear state during render when session changes.
  // This prevents a flash of old messages before the useEffect fires.
  const [prevKey, setPrevKey] = useState(sessionKey);
  if (sessionKey !== prevKey) {
    setPrevKey(sessionKey);
    setMessages([]);
    setStreamText(null);
    setThinkingText(null);
    setToolStream([]);
    setIsRunning(false);
    setLoading(true);
    runIdRef.current = null;
    expectingRunRef.current = false;
    streamRef.current = "";
    thinkingRef.current = "";
    toolStreamRef.current = [];
  }

  // Load history (no loading spinner — the empty state placeholder is shown instead)
  const loadHistory = useCallback(async () => {
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
          timestamp: Date.now() - (allMsgs.length - i) * 1000,
        };
        // Reconstruct toolDetails for assistant messages with tool_calls
        if (m.role === "assistant" && m.tool_calls && m.tool_calls.length > 0) {
          chatMsg.toolDetails = m.tool_calls.map((tc) => {
            const toolMsg = toolResultMap.get(tc.id);
            return {
              toolCallId: tc.id,
              runId: "",
              name: tc.name,
              phase: (toolMsg ? "completed" : "calling") as ToolStreamEntry["phase"],
              startedAt: 0,
              updatedAt: 0,
              arguments: tc.arguments,
              result: toolMsg?.content,
            };
          });
        }
        return chatMsg;
      });
      setMessages(msgs);
    } catch {
      // will retry
    } finally {
      setLoading(false);
    }
  }, [ws, agentId, sessionKey]);

  // Load history when session changes
  useEffect(() => {
    if (sessionKey) {
      loadHistory();
    }
  }, [sessionKey, loadHistory]);

  // Called before sending a message so the event handler knows to capture run.started
  const expectRun = useCallback(() => {
    expectingRunRef.current = true;
  }, []);

  // Stable event handler using refs for mutable state
  const handleAgentEvent = useCallback(
    (payload: unknown) => {
      const event = payload as AgentEventPayload;
      if (!event) return;

      // Announce run.completed: reload history when a subagent/delegate announce finishes
      // for the current agent (these runs aren't tracked by runIdRef).
      if (event.type === "run.completed" && event.runKind === "announce" && event.agentId === agentIdRef.current) {
        loadHistory();
        return;
      }

      // Capture run.started when we are expecting a run for this agent
      if (event.type === "run.started") {
        if (expectingRunRef.current && event.agentId === agentIdRef.current) {
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
          setThinkingText(thinkingRef.current);
          break;
        }
        case "chunk": {
          const content = event.payload?.content ?? "";
          streamRef.current += content;
          setStreamText(streamRef.current);
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
        case "run.completed": {
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

          if (streamed && !hadTools) {
            setMessages((prev) => [
              ...prev,
              { role: "assistant", content: streamed, timestamp: Date.now() },
            ]);
          } else {
            loadHistory();
          }
          break;
        }
        case "run.failed": {
          setIsRunning(false);
          runIdRef.current = null;
          setStreamText(null);
          setThinkingText(null);
          setToolStream([]);
          streamRef.current = "";
          thinkingRef.current = "";
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

  // Add a local message optimistically (shown immediately, replaced on next loadHistory)
  const addLocalMessage = useCallback((msg: ChatMessage) => {
    setMessages((prev) => [...prev, msg]);
  }, []);

  return {
    messages,
    streamText,
    thinkingText,
    toolStream,
    isRunning,
    loading,
    expectRun,
    loadHistory,
    addLocalMessage,
  };
}
