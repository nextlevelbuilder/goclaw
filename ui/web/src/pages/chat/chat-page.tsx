import { useState, useCallback, useEffect, useRef, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useParams, useNavigate } from "react-router";
import { Eye, PanelLeftOpen } from "lucide-react";
import { useAuthStore } from "@/stores/use-auth-store";
import { useIsMobile } from "@/hooks/use-media-query";
import { Sheet, SheetContent } from "@/components/ui/sheet";
import { ChatSidebar } from "./chat-sidebar";
import { ChatThread } from "./chat-thread";
import { ChatInput, type AttachedFile } from "@/components/chat/chat-input";
import { ChatTopBar } from "@/components/chat/chat-top-bar";
import { DropZone } from "@/components/chat/drop-zone";
import { AgentPickerPrompt } from "@/components/chat/agent-picker-prompt";
import { useChatSessions } from "./hooks/use-chat-sessions";
import { useChatMessages } from "./hooks/use-chat-messages";
import { useChatSend } from "./hooks/use-chat-send";
import { isOwnSession, parseSessionKey } from "@/lib/session-key";
import { useVirtualKeyboard } from "@/hooks/use-virtual-keyboard";
import { TaskPanel } from "@/components/chat/task-panel";
import { SecretRequestHost } from "@/components/chat/secret-request-host";
import { TakeoverRequestHost } from "@/components/chat/takeover-request-host";

export function ChatPage() {
  const { t } = useTranslation("chat");
  const { sessionKey: urlSessionKey } = useParams<{ sessionKey: string }>();
  const navigate = useNavigate();
  const connected = useAuthStore((s) => s.connected);
  const userId = useAuthStore((s) => s.userId);

  const [scrollTrigger, setScrollTrigger] = useState(0);
  const [files, setFiles] = useState<AttachedFile[]>([]);
  /** Per-request model override ("<provider>/<model>"); "" = agent's own model. */
  const [modelOverride, setModelOverride] = useState("");

  // sessionKey derived from URL — single source of truth, no separate state
  const sessionKey = urlSessionKey ?? "";

  // Fallback agent ID used only when URL has no session key
  const [agentIdFallback, setAgentIdFallback] = useState("");

  // Agent is confirmed when URL has a session (agentId parsed) or user explicitly picked one
  const agentConfirmed = !!urlSessionKey || !!agentIdFallback;

  // Derive agentId from URL (source of truth), fallback to state when no session
  const agentId = useMemo(() => {
    if (urlSessionKey) {
      const { agentId: parsed } = parseSessionKey(urlSessionKey);
      if (parsed) return parsed;
    }
    return agentIdFallback;
  }, [urlSessionKey, agentIdFallback]);

  const {
    sessions,
    loading: sessionsLoading,
    refresh: refreshSessions,
    buildNewSessionKey,
    deleteSession,
  } = useChatSessions(agentId);

  const {
    messages,
    streamText,
    thinkingText,
    toolStream,
    isRunning,
    isBusy,
    loading: messagesLoading,
    activity,
    blockReplies,
    teamTasks,
    expectRun,
    addLocalMessage,
  } = useChatMessages(sessionKey, agentId);

  // Refresh sessions when all work completes (main agent + team tasks)
  const prevIsBusyRef = useRef(false);
  useEffect(() => {
    if (prevIsBusyRef.current && !isBusy) {
      refreshSessions();
    }
    prevIsBusyRef.current = isBusy;
  }, [isBusy, refreshSessions]);

  const isOwn = !sessionKey || isOwnSession(sessionKey, userId);

  const handleMessageAdded = useCallback(
    (msg: { role: "user" | "assistant" | "tool"; content: string; timestamp?: number }, key?: string) => {
      addLocalMessage(msg, key);
    },
    [addLocalMessage],
  );

  const { send, abort, error: sendError } = useChatSend({
    agentId,
    onMessageAdded: handleMessageAdded,
    onExpectRun: expectRun,
    modelOverride,
  });

  // A model chosen for one agent must not silently apply to another: dropping
  // the override restores the agent's own model.
  useEffect(() => {
    setModelOverride("");
  }, [agentId]);

  const handleNewChat = useCallback(() => {
    navigate(`/chat/${encodeURIComponent(buildNewSessionKey())}`);
  }, [buildNewSessionKey, navigate]);

  const handleSessionSelect = useCallback(
    (key: string) => {
      const { agentId: parsed } = parseSessionKey(key);
      if (parsed) setAgentIdFallback(parsed);
      navigate(`/chat/${encodeURIComponent(key)}`);
    },
    [navigate],
  );

  const handleDeleteSession = useCallback(async (key: string) => {
    await deleteSession(key);
    if (key === sessionKey) {
      const next = sessions.find((s) => s.key !== key);
      if (next) {
        handleSessionSelect(next.key);
      } else {
        handleNewChat();
      }
    }
  }, [deleteSession, sessionKey, sessions, handleSessionSelect, handleNewChat]);

  const handleAgentChange = useCallback(
    (newAgentId: string) => {
      setAgentIdFallback(newAgentId);
      if (sessionKey) {
        navigate("/chat");
      }
    },
    [navigate],
  );

  const handleSend = useCallback(
    (message: string, sendFiles?: AttachedFile[]) => {
      let key = sessionKey;
      if (!key) {
        key = buildNewSessionKey();
        navigate(`/chat/${encodeURIComponent(key)}`, { replace: true });
      }
      send(message, key, sendFiles);
      setScrollTrigger((n) => n + 1);
    },
    [sessionKey, send, buildNewSessionKey, navigate],
  );

  const handleDropFiles = useCallback((dropped: File[]) => {
    setFiles((prev) => [...prev, ...dropped.map((f) => ({ file: f }))]);
  }, []);

  const handleAbort = useCallback(() => {
    abort(sessionKey);
  }, [abort, sessionKey]);

  const isMobile = useIsMobile();
  useVirtualKeyboard();
  const [chatSidebarOpen, setChatSidebarOpen] = useState(false);
  const [taskPanelOpen, setTaskPanelOpen] = useState(false);

  // Auto-open task panel when first task appears, auto-close when all done.
  const prevTaskCountRef = useRef(0);
  useEffect(() => {
    const prev = prevTaskCountRef.current;
    const curr = teamTasks.length;
    if (prev === 0 && curr > 0) setTaskPanelOpen(true);
    if (curr === 0 && prev > 0) setTaskPanelOpen(false);
    prevTaskCountRef.current = curr;
  }, [teamTasks.length]);

  const handleSessionSelectMobile = useCallback(
    (key: string) => {
      handleSessionSelect(key);
      setChatSidebarOpen(false);
    },
    [handleSessionSelect],
  );

  const handleNewChatMobile = useCallback(() => {
    handleNewChat();
    setChatSidebarOpen(false);
  }, [handleNewChat]);

  return (
    <div className="relative flex h-full overflow-hidden">
      {/* Chat Sidebar */}
      {isMobile ? (
        <Sheet open={chatSidebarOpen} onOpenChange={setChatSidebarOpen}>
          <SheetContent
            side="left"
            title={t("openSessions")}
            showCloseButton={false}
            className="w-72 max-w-[85vw] gap-0 border-r-0 p-0"
          >
            <ChatSidebar
              agentId={agentId}
              onAgentChange={handleAgentChange}
              sessions={sessions}
              sessionsLoading={sessionsLoading}
              activeSessionKey={sessionKey}
              onSessionSelect={handleSessionSelectMobile}
              onDeleteSession={handleDeleteSession}
              onNewChat={handleNewChatMobile}
            />
          </SheetContent>
        </Sheet>
      ) : (
        <ChatSidebar
          agentId={agentId}
          onAgentChange={handleAgentChange}
          sessions={sessions}
          sessionsLoading={sessionsLoading}
          activeSessionKey={sessionKey}
          onSessionSelect={handleSessionSelect}
          onDeleteSession={handleDeleteSession}
          onNewChat={handleNewChat}
        />
      )}

      {/* Main chat area */}
      <div className="flex min-w-0 flex-1 min-h-0 flex-col">
        {isMobile && (
          <div className="flex shrink-0 items-center border-b px-3 py-2 landscape-compact">
            <button
              onClick={() => setChatSidebarOpen(true)}
              className="rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              title={t("openSessions")}
            >
              <PanelLeftOpen className="h-4 w-4" />
            </button>
          </div>
        )}

        <div className="shrink-0">
          <ChatTopBar
            agentId={agentId}
            isRunning={isRunning}
            isBusy={isBusy}
            activity={activity}
            teamTasks={teamTasks}
            onToggleTaskPanel={() => setTaskPanelOpen((v) => !v)}
            taskPanelOpen={taskPanelOpen}
            session={sessions.find((s) => s.key === sessionKey) ?? null}
            modelOverride={modelOverride}
            onModelOverrideChange={setModelOverride}
          />
        </div>

        {sendError && (
          <div className="shrink-0 border-b bg-destructive/10 px-4 py-2 text-sm text-destructive">
            {sendError}
          </div>
        )}

        <DropZone onDrop={handleDropFiles} disabled={isBusy || !connected || !isOwn || !agentConfirmed}>
          <ChatThread
            messages={messages}
            streamText={streamText}
            thinkingText={thinkingText}
            toolStream={toolStream}
            blockReplies={blockReplies}
            activity={activity}
            teamTasks={teamTasks}
            isRunning={isRunning}
            isBusy={isBusy}
            loading={messagesLoading}
            scrollTrigger={scrollTrigger}
            onToggleTaskPanel={() => setTaskPanelOpen((v) => !v)}
          />

          {!isOwn ? (
            <div className="mx-3 mb-3 flex items-center gap-2 rounded-xl border bg-muted/50 px-4 py-3 text-sm text-muted-foreground shadow-sm">
              <Eye className="h-4 w-4" />
              {t("readOnly")}
            </div>
          ) : !agentConfirmed ? (
            <AgentPickerPrompt onSelect={handleAgentChange} />
          ) : (
            <ChatInput
              onSend={handleSend}
              onAbort={handleAbort}
              isBusy={isBusy}
              disabled={!connected}
              files={files}
              onFilesChange={setFiles}
            />
          )}
        </DropZone>
      </div>

      {/* Task panel — toggleable sidebar on the right (Sheet on mobile) */}
      <TaskPanel tasks={teamTasks} open={taskPanelOpen} onClose={() => setTaskPanelOpen(false)} />
      <SecretRequestHost />
      <TakeoverRequestHost />
    </div>
  );
}
