import { useState, useCallback, useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { useParams, useNavigate } from "react-router";
import { PanelLeftOpen, RefreshCw, PackageCheck, FolderTree } from "lucide-react";
import { useAuthStore } from "@/stores/use-auth-store";
import { useIsMobile } from "@/hooks/use-media-query";
import { useVirtualKeyboard } from "@/hooks/use-virtual-keyboard";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { ChatThread } from "@/pages/chat/chat-thread";
import { ChatInput, type AttachedFile } from "@/components/chat/chat-input";
import { FileTreePanel } from "@/components/shared/file-tree";
import { BuilderSidebar } from "@/components/shared/builder-sidebar";
import { BuilderFileViewer } from "@/components/shared/builder-file-viewer";
import { useChatMessages } from "@/pages/chat/hooks/use-chat-messages";
import { useChatSend } from "@/pages/chat/hooks/use-chat-send";
import { useSkillBuilder } from "./hooks/use-skill-builder";
import { useSkillBuilderSessions } from "./hooks/use-skill-builder-sessions";
import { useDefaultAgentKey } from "@/hooks/use-default-agent";
import type { ChatMessage } from "@/types/chat";
import { toast } from "@/stores/use-toast-store";

async function deriveProjectName(key: string): Promise<string> {
  const data = new TextEncoder().encode(key);
  const hash = await crypto.subtle.digest("SHA-256", data);
  const hex = Array.from(new Uint8Array(hash)).map(b => b.toString(16).padStart(2, "0")).join("");
  return `skill-${hex.slice(0, 12)}`;
}

export function SkillBuilderPage() {
  const { t } = useTranslation("skills");
  const { sessionKey: urlSessionKey } = useParams<{ sessionKey: string }>();
  const navigate = useNavigate();
  const connected = useAuthStore((s) => s.connected);
  const userId = useAuthStore((s) => s.userId);
  const isMobile = useIsMobile();
  useVirtualKeyboard();

  // Resolve actual default agent key from API
  const { agentKey: builderAgentId, loading: agentLoading } = useDefaultAgentKey();

  // Session management
  const {
    sessions,
    loading: sessionsLoading,
    refresh: refreshSessions,
    buildNewBuilderSessionKey,
  } = useSkillBuilderSessions(builderAgentId);

  const [sessionKey, setSessionKey] = useState(() => urlSessionKey ?? "");
  const [sidebarOpen, setSidebarOpen] = useState(false);

  // Auto-select latest session or create new one when no URL param
  useEffect(() => {
    if (urlSessionKey || !userId || sessionsLoading || agentLoading) return;
    if (sessions.length > 0) {
      // Select the most recent session
      const latestKey = sessions[0]!.key;
      setSessionKey(latestKey);
      navigate(`/skills/builder/${encodeURIComponent(latestKey)}`, { replace: true });
    } else {
      // No sessions exist, create a new one
      const newKey = buildNewBuilderSessionKey(userId);
      setSessionKey(newKey);
      navigate(`/skills/builder/${encodeURIComponent(newKey)}`, { replace: true });
    }
  }, [urlSessionKey, userId, sessionsLoading, agentLoading, sessions, buildNewBuilderSessionKey, navigate]);

  // Sync URL param changes
  useEffect(() => {
    if (urlSessionKey && urlSessionKey !== sessionKey) {
      setSessionKey(urlSessionKey);
    }
  }, [urlSessionKey, sessionKey]);

  const [scrollTrigger, setScrollTrigger] = useState(0);

  // Chat hooks
  const {
    messages,
    summary,
    streamText,
    thinkingText,
    toolStream,
    isRunning,
    loading: messagesLoading,
    expectRun,
    addLocalMessage,
  } = useChatMessages(sessionKey, builderAgentId);

  const handleMessageAdded = useCallback(
    (msg: ChatMessage) => {
      addLocalMessage(msg);
    },
    [addLocalMessage],
  );

  const { send, abort, error: sendError } = useChatSend({
    agentId: builderAgentId,
    onMessageAdded: handleMessageAdded,
    onExpectRun: expectRun,
  });

  // Skill builder hooks
  const {
    projectId,
    setProjectId,
    tree,
    filesLoading,
    fileContent,
    fileContentLoading,
    createProject,
    fetchFileTree,
    fetchFileContent,
    publishProject,
  } = useSkillBuilder();

  // File tree state
  const [activePath, setActivePath] = useState<string | null>(null);
  const [treeSidebarOpen, setTreeSidebarOpen] = useState(false);

  // Track whether a run has completed (for showing Publish button)
  const [hasCompleted, setHasCompleted] = useState(false);
  const [publishing, setPublishing] = useState(false);

  // Auto-init: create project + send /skill-creator on new session.
  const initedKeyRef = useRef<string>("");

  useEffect(() => {
    // Reset UI state when session changes
    if (initedKeyRef.current && initedKeyRef.current !== sessionKey) {
      setHasCompleted(false);
      setActivePath(null);
    }

    // Guard: wait for loading to finish, agent to resolve, and session key to exist
    if (messagesLoading || agentLoading || !sessionKey) return;
    // Guard: already initialized this exact session
    if (initedKeyRef.current === sessionKey) return;

    // Mark as initialized for this session key immediately (before async work)
    initedKeyRef.current = sessionKey;

    // Both existing and new sessions need async project name derivation
    (async () => {
      const name = await deriveProjectName(sessionKey);
      if (messages.length > 0) {
        // Existing session: fetch tree
        setProjectId(name);
        fetchFileTree(name);
        return;
      }
      // New session: create project + send /skill-creator
      try {
        await createProject(name);
        await fetchFileTree(name);
        send("/skill-creator", sessionKey);
        setScrollTrigger((n) => n + 1);
      } catch {
        toast.error(t("builder.projectError"));
      }
    })();
  }, [messagesLoading, agentLoading, messages.length, sessionKey, createProject, fetchFileTree, send, setProjectId, t]);

  // File tree refresh + session refresh on run.completed
  const prevIsRunningRef = useRef(false);
  useEffect(() => {
    if (prevIsRunningRef.current && !isRunning) {
      setHasCompleted(true);
      if (projectId) {
        fetchFileTree(projectId);
      }
      refreshSessions();
    }
    prevIsRunningRef.current = isRunning;
  }, [isRunning, projectId, fetchFileTree, refreshSessions]);

  // Refresh file tree on tool.result (debounced)
  const toolRefreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const prevCompletedCountRef = useRef(0);
  useEffect(() => {
    if (!projectId) return;
    const completedCount = toolStream.filter((t) => t.phase === "completed").length;
    if (completedCount <= prevCompletedCountRef.current) {
      prevCompletedCountRef.current = completedCount;
      return;
    }
    prevCompletedCountRef.current = completedCount;

    // Debounce: wait 800ms after the last tool.result before refreshing
    if (toolRefreshTimerRef.current) clearTimeout(toolRefreshTimerRef.current);
    toolRefreshTimerRef.current = setTimeout(() => {
      fetchFileTree(projectId);
      // Also refresh the active file content if one is open
      if (activePath) fetchFileContent(projectId, activePath);
    }, 800);

    return () => {
      if (toolRefreshTimerRef.current) clearTimeout(toolRefreshTimerRef.current);
    };
  }, [toolStream, projectId, fetchFileTree, fetchFileContent, activePath]);

  // Session handlers
  const handleNewBuild = useCallback(() => {
    if (!userId) return;
    const newKey = buildNewBuilderSessionKey(userId);
    setSessionKey(newKey);
    navigate(`/skills/builder/${encodeURIComponent(newKey)}`);
    if (isMobile) setSidebarOpen(false);
  }, [buildNewBuilderSessionKey, userId, navigate, isMobile]);

  const handleSessionSelect = useCallback(
    (key: string) => {
      setSessionKey(key);
      navigate(`/skills/builder/${encodeURIComponent(key)}`);
      if (isMobile) setSidebarOpen(false);
    },
    [navigate, isMobile],
  );

  // Handle file selection
  const handleFileSelect = useCallback(
    (path: string) => {
      setActivePath(path);
      if (projectId) {
        fetchFileContent(projectId, path);
      }
      if (isMobile) {
        setTreeSidebarOpen(false);
      }
    },
    [projectId, fetchFileContent, isMobile],
  );

  // Handle file viewer close
  const handleCloseViewer = useCallback(() => {
    setActivePath(null);
  }, []);

  // Handle refresh
  const handleRefreshTree = useCallback(() => {
    if (projectId) {
      fetchFileTree(projectId);
    }
  }, [projectId, fetchFileTree]);

  // Handle send
  const handleSend = useCallback(
    (message: string, files?: AttachedFile[]) => {
      let key = sessionKey;
      if (!key) {
        if (!userId) return;
        key = buildNewBuilderSessionKey(userId);
        setSessionKey(key);
        navigate(`/skills/builder/${encodeURIComponent(key)}`, { replace: true });
      }
      send(message, key, files);
      setScrollTrigger((n) => n + 1);
    },
    [sessionKey, send, userId, buildNewBuilderSessionKey, navigate],
  );

  // Handle abort
  const handleAbort = useCallback(() => {
    abort(sessionKey);
  }, [abort, sessionKey]);

  // Handle Publish
  const handlePublish = useCallback(async () => {
    if (!projectId) return;
    setPublishing(true);
    try {
      await publishProject(projectId);
      toast.success(t("builder.published"));
    } catch (err) {
      toast.error(t("builder.publishFailed"), err instanceof Error ? err.message : "");
    } finally {
      setPublishing(false);
    }
  }, [projectId, publishProject, t]);

  const fileName = activePath?.includes("/") ? activePath.slice(activePath.lastIndexOf("/") + 1) : activePath;

  return (
    <div className="relative flex h-full">
      {/* Left: Session sidebar */}
      {isMobile ? (
        <>
          {sidebarOpen && (
            <div
              className="fixed inset-0 z-40 bg-black/50"
              onClick={() => setSidebarOpen(false)}
            />
          )}
          <div
            className={cn(
              "fixed inset-y-0 left-0 z-50 transition-transform duration-200 ease-in-out",
              sidebarOpen ? "translate-x-0" : "-translate-x-full",
            )}
          >
            <BuilderSidebar
              sessions={sessions}
              sessionsLoading={sessionsLoading}
              activeSessionKey={sessionKey}
              onSessionSelect={handleSessionSelect}
              onNewBuild={handleNewBuild}
              newButtonLabel={t("builder.newSkill")}
            />
          </div>
        </>
      ) : (
        <BuilderSidebar
          sessions={sessions}
          sessionsLoading={sessionsLoading}
          activeSessionKey={sessionKey}
          onSessionSelect={handleSessionSelect}
          onNewBuild={handleNewBuild}
          newButtonLabel={t("builder.newSkill")}
        />
      )}

      {/* Center: chat + optional file viewer */}
      <div className="flex flex-1 flex-col min-w-0">
        {/* Mobile top bar + Publish button */}
        {(isMobile || hasCompleted) && (
          <div className="flex items-center border-b px-3 py-2 landscape-compact">
            {isMobile && (
              <div className="flex items-center gap-1">
                <button
                  onClick={() => setSidebarOpen(true)}
                  className="rounded-md p-2.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                  title={t("builder.sessions")}
                  aria-label={t("builder.sessions")}
                >
                  <PanelLeftOpen className="h-4 w-4" />
                </button>
                <button
                  onClick={() => setTreeSidebarOpen(true)}
                  className="rounded-md p-2.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                  title={t("builder.openFiles")}
                  aria-label={t("builder.openFiles")}
                >
                  <FolderTree className="h-4 w-4" />
                </button>
              </div>
            )}
            <div className="flex-1" />
            {hasCompleted && projectId && (
              <Button
                size="sm"
                variant="outline"
                onClick={handlePublish}
                disabled={publishing}
                className="gap-1"
              >
                <PackageCheck className="h-3.5 w-3.5" />
                {publishing ? t("builder.publishing") : t("builder.publish")}
              </Button>
            )}
          </div>
        )}

        {sendError && (
          <div className="border-b bg-destructive/10 px-4 py-2 text-sm text-destructive">
            {sendError}
          </div>
        )}

        <ChatThread
          messages={messages}
          summary={summary}
          streamText={streamText}
          thinkingText={thinkingText}
          toolStream={toolStream}
          isRunning={isRunning}
          loading={messagesLoading}
          scrollTrigger={scrollTrigger}
        />

        {activePath && (
          <BuilderFileViewer
            content={fileContent}
            path={activePath}
            loading={fileContentLoading}
            onClose={handleCloseViewer}
            closeLabel={t("builder.closeFile")}
            fileLoadError={t("builder.fileLoadError", { name: fileName })}
          />
        )}

        <ChatInput
          onSend={handleSend}
          onAbort={handleAbort}
          isRunning={isRunning}
          disabled={!connected}
        />
      </div>

      {/* Right: File tree (desktop only) */}
      {!isMobile && (
        <aside className="hidden w-64 shrink-0 flex-col border-l md:flex">
          <div className="flex items-center justify-between border-b px-3 py-2">
            <span className="text-sm font-medium">{t("builder.projectFiles")}</span>
            <button
              onClick={handleRefreshTree}
              className="rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              title={t("builder.refreshFiles")}
            >
              <RefreshCw className={cn("h-3.5 w-3.5", filesLoading && "animate-spin")} />
            </button>
          </div>
          <div className="flex-1 overflow-y-auto overscroll-contain p-1">
            <FileTreePanel
              tree={tree}
              filesLoading={filesLoading}
              activePath={activePath}
              onSelect={handleFileSelect}
            />
          </div>
        </aside>
      )}

      {/* Mobile: File tree drawer (slides from right) */}
      {isMobile && (
        <>
          {treeSidebarOpen && (
            <div
              className="fixed inset-0 z-40 bg-black/50"
              onClick={() => setTreeSidebarOpen(false)}
            />
          )}
          <div
            className={cn(
              "fixed inset-y-0 right-0 z-50 transition-transform duration-200 ease-in-out",
              treeSidebarOpen ? "translate-x-0" : "translate-x-full",
            )}
          >
            <div className="flex h-full w-72 max-w-[85vw] flex-col border-l bg-background">
              <div className="flex items-center justify-between border-b px-3 py-2">
                <span className="text-sm font-medium">{t("builder.projectFiles")}</span>
                <button
                  onClick={handleRefreshTree}
                  className="rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                  title={t("builder.refreshFiles")}
                >
                  <RefreshCw className={cn("h-3.5 w-3.5", filesLoading && "animate-spin")} />
                </button>
              </div>
              <div className="flex-1 overflow-y-auto overscroll-contain p-1">
                <FileTreePanel
                  tree={tree}
                  filesLoading={filesLoading}
                  activePath={activePath}
                  onSelect={handleFileSelect}
                />
              </div>
            </div>
          </div>
        </>
      )}
    </div>
  );
}
