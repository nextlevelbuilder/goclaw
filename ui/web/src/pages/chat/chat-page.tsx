import { useState, useCallback, useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { useParams, useNavigate } from "react-router";
import { Eye, PanelLeftOpen } from "lucide-react";
import { useAuthStore } from "@/stores/use-auth-store";
import { useIsMobile } from "@/hooks/use-media-query";
import { cn } from "@/lib/utils";
import { ChatSidebar } from "./chat-sidebar";
import { ChatThread } from "./chat-thread";
import { ChatInput, type AttachedFile } from "@/components/chat/chat-input";
import { useChatSessions } from "./hooks/use-chat-sessions";
import { useChatMessages } from "./hooks/use-chat-messages";
import { useChatSend } from "./hooks/use-chat-send";
import { isOwnSession, parseSessionKey } from "@/lib/session-key";
import { useVirtualKeyboard } from "@/hooks/use-virtual-keyboard";

export function ChatPage() {
  const { t } = useTranslation("chat");
  const { sessionKey: urlSessionKey } = useParams<{ sessionKey: string }>();
  const navigate = useNavigate();
  const connected = useAuthStore((s) => s.connected);
  const userId = useAuthStore((s) => s.userId);

  const [scrollTrigger, setScrollTrigger] = useState(0);

  const [agentId, setAgentId] = useState(() => {
    if (urlSessionKey) {
      const { agentId: parsed } = parseSessionKey(urlSessionKey);
      if (parsed) return parsed;
    }
    return "default";
  });
  const [sessionKey, setSessionKey] = useState(urlSessionKey ?? "");

  const {
    sessions,
    loading: sessionsLoading,
    refresh: refreshSessions,
    buildNewSessionKey,
  } = useChatSessions(agentId);

  const {
    messages,
    streamText,
    thinkingText,
    toolStream,
    isRunning,
    loading: messagesLoading,
    expectRun,
    addLocalMessage,
  } = useChatMessages(sessionKey, agentId);

  // Sync URL param to state
  useEffect(() => {
    if (urlSessionKey && urlSessionKey !== sessionKey) {
      setSessionKey(urlSessionKey);
    }
  }, [urlSessionKey, sessionKey]);

  // Refresh sessions when run completes
  const prevIsRunningRef = useRef(false);
  useEffect(() => {
    if (prevIsRunningRef.current && !isRunning) {
      refreshSessions();
    }
    prevIsRunningRef.current = isRunning;
  }, [isRunning, refreshSessions]);

  const isOwn = !sessionKey || isOwnSession(sessionKey, userId);

  const handleMessageAdded = useCallback(
    (msg: { role: "user" | "assistant" | "tool"; content: string; timestamp?: number }) => {
      addLocalMessage(msg);
    },
    [addLocalMessage],
  );

  const { send, abort, error: sendError } = useChatSend({
    agentId,
    onMessageAdded: handleMessageAdded,
    onExpectRun: expectRun,
  });

  const handleNewChat = useCallback(() => {
    const newKey = buildNewSessionKey();
    setSessionKey(newKey);
    navigate(`/chat/${encodeURIComponent(newKey)}`);
  }, [buildNewSessionKey, navigate]);

  const handleSessionSelect = useCallback(
    (key: string) => {
      // Sync agentId from session key to ensure correct routing
      const { agentId: parsed } = parseSessionKey(key);
      if (parsed && parsed !== agentId) {
        setAgentId(parsed);
      }
      setSessionKey(key);
      navigate(`/chat/${encodeURIComponent(key)}`);
    },
    [navigate, agentId],
  );

  const handleAgentChange = useCallback(
    (newAgentId: string) => {
      setAgentId(newAgentId);
      const newKey = `agent:${newAgentId}:ws-${userId}-${Date.now().toString(36)}`;
      setSessionKey(newKey);
      navigate(`/chat/${encodeURIComponent(newKey)}`);
    },
    [navigate, userId],
  );

  const handleSend = useCallback(
    (message: string, files?: AttachedFile[]) => {
      let key = sessionKey;
      if (!key) {
        key = buildNewSessionKey();
        setSessionKey(key);
        navigate(`/chat/${encodeURIComponent(key)}`);
      }
      // Pass key directly so send() doesn't use a stale closure value
      send(message, key, files);
      setScrollTrigger((n) => n + 1);
    },
    [sessionKey, send, buildNewSessionKey, navigate],
  );

  const handleAbort = useCallback(() => {
    abort(sessionKey);
  }, [abort, sessionKey]);

  const isMobile = useIsMobile();
  useVirtualKeyboard();
  const [chatSidebarOpen, setChatSidebarOpen] = useState(false);

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
    <div className="relative flex h-full">
      {/* Chat Sidebar */}
      {isMobile ? (
        <>
          {chatSidebarOpen && (
            <div
              className="fixed inset-0 z-40 bg-black/50"
              onClick={() => setChatSidebarOpen(false)}
            />
          )}
          <div
            className={cn(
              "fixed inset-y-0 left-0 z-50 transition-transform duration-200 ease-in-out",
              chatSidebarOpen ? "translate-x-0" : "-translate-x-full",
            )}
          >
            <ChatSidebar
              agentId={agentId}
              onAgentChange={handleAgentChange}
              sessions={sessions}
              sessionsLoading={sessionsLoading}
              activeSessionKey={sessionKey}
              onSessionSelect={handleSessionSelectMobile}
              onNewChat={handleNewChatMobile}
            />
          </div>
        </>
      ) : (
        <ChatSidebar
          agentId={agentId}
          onAgentChange={handleAgentChange}
          sessions={sessions}
          sessionsLoading={sessionsLoading}
          activeSessionKey={sessionKey}
          onSessionSelect={handleSessionSelect}
          onNewChat={handleNewChat}
        />
      )}

      {/* Main chat area */}
      <div className="flex flex-1 flex-col">
        {isMobile && (
          <div className="flex items-center border-b px-3 py-2 landscape-compact">
            <button
              onClick={() => setChatSidebarOpen(true)}
              className="rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              title={t("openSessions")}
            >
              <PanelLeftOpen className="h-4 w-4" />
            </button>
          </div>
        )}

        {sendError && (
          <div className="border-b bg-destructive/10 px-4 py-2 text-sm text-destructive">
            {sendError}
          </div>
        )}

        <ChatThread
          messages={messages}
          streamText={streamText}
          thinkingText={thinkingText}
          toolStream={toolStream}
          isRunning={isRunning}
          loading={messagesLoading}
          scrollTrigger={scrollTrigger}
        />

        {isOwn ? (
          <ChatInput
            onSend={handleSend}
            onAbort={handleAbort}
            isRunning={isRunning}
            disabled={!connected}
          />
        ) : (
          <div className="flex items-center gap-2 border-t bg-muted/50 px-4 py-3 text-sm text-muted-foreground">
            <Eye className="h-4 w-4" />
            {t("readOnly")}
          </div>
        )}
      </div>
    </div>
  );
}
