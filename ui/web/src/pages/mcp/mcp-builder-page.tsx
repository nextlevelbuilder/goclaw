import { useState, useCallback, useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { useParams, useNavigate } from "react-router";
import { PanelLeftOpen, RefreshCw, PackageCheck } from "lucide-react";
import { useAuthStore } from "@/stores/use-auth-store";
import { useIsMobile } from "@/hooks/use-media-query";
import { useVirtualKeyboard } from "@/hooks/use-virtual-keyboard";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { ChatThread } from "@/pages/chat/chat-thread";
import { ChatInput, type AttachedFile } from "@/components/chat/chat-input";
import { FileTreePanel } from "@/components/shared/file-tree";
import { useChatMessages } from "@/pages/chat/hooks/use-chat-messages";
import { useChatSend } from "@/pages/chat/hooks/use-chat-send";
import { useMCPBuilder } from "./hooks/use-mcp-builder";
import { MCPBuilderFileViewer } from "./mcp-builder-file-viewer";
import type { ChatMessage } from "@/types/chat";
import { toast } from "@/stores/use-toast-store";

const BUILDER_AGENT_ID = "default";

function buildBuilderSessionKey(userId: string) {
  return `agent:${BUILDER_AGENT_ID}:mcp-builder-${userId}-${Date.now().toString(36)}`;
}

function deriveProjectName(key: string): string {
  const parts = key.split(":");
  const suffix = parts[parts.length - 1] || key;
  return suffix
    .toLowerCase()
    .replace(/[^a-z0-9-]/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 40);
}

export function MCPBuilderPage() {
  const { t } = useTranslation("mcp");
  const { sessionKey: urlSessionKey } = useParams<{ sessionKey: string }>();
  const navigate = useNavigate();
  const connected = useAuthStore((s) => s.connected);
  const userId = useAuthStore((s) => s.userId);
  const isMobile = useIsMobile();
  useVirtualKeyboard();

  // Session key management
  const [sessionKey, setSessionKey] = useState(() => {
    if (urlSessionKey) return urlSessionKey;
    return "";
  });

  // Auto-create session key and navigate on mount if no sessionKey param
  useEffect(() => {
    if (!urlSessionKey && userId) {
      const newKey = buildBuilderSessionKey(userId);
      setSessionKey(newKey);
      navigate(`/mcp/builder/${encodeURIComponent(newKey)}`, { replace: true });
    }
  }, [urlSessionKey, userId, navigate]);

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
    streamText,
    thinkingText,
    toolStream,
    isRunning,
    loading: messagesLoading,
    expectRun,
    addLocalMessage,
  } = useChatMessages(sessionKey, BUILDER_AGENT_ID);

  const handleMessageAdded = useCallback(
    (msg: ChatMessage) => {
      addLocalMessage(msg);
    },
    [addLocalMessage],
  );

  const { send, abort, error: sendError } = useChatSend({
    agentId: BUILDER_AGENT_ID,
    onMessageAdded: handleMessageAdded,
    onExpectRun: expectRun,
  });

  // MCP builder hooks
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
    registerProject,
  } = useMCPBuilder();

  // File tree state
  const [activePath, setActivePath] = useState<string | null>(null);
  const [treeSidebarOpen, setTreeSidebarOpen] = useState(false);

  // Track whether a run has completed (for showing Build & Register button)
  const [hasCompleted, setHasCompleted] = useState(false);
  const [registering, setRegistering] = useState(false);

  // Auto-init: create project + send /mcp-builder on new session
  const hasAutoInitRef = useRef(false);
  useEffect(() => {
    if (messagesLoading || !sessionKey || hasAutoInitRef.current) return;
    if (messages.length > 0) {
      // Existing session: derive project name and fetch tree
      const name = deriveProjectName(sessionKey);
      setProjectId(name);
      fetchFileTree(name);
      return;
    }
    hasAutoInitRef.current = true;

    (async () => {
      const name = deriveProjectName(sessionKey);
      try {
        await createProject(name);
        await fetchFileTree(name);
        send("/mcp-builder", sessionKey);
        setScrollTrigger((n) => n + 1);
      } catch {
        toast.error(t("builder.projectError"));
      }
    })();
  }, [messagesLoading, messages.length, sessionKey, createProject, fetchFileTree, send, setProjectId, t]);

  // File tree refresh on run.completed
  const prevIsRunningRef = useRef(false);
  useEffect(() => {
    if (prevIsRunningRef.current && !isRunning) {
      setHasCompleted(true);
      if (projectId) {
        fetchFileTree(projectId);
      }
    }
    prevIsRunningRef.current = isRunning;
  }, [isRunning, projectId, fetchFileTree]);

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
        key = buildBuilderSessionKey(userId);
        setSessionKey(key);
        navigate(`/mcp/builder/${encodeURIComponent(key)}`, { replace: true });
      }
      send(message, key, files);
      setScrollTrigger((n) => n + 1);
    },
    [sessionKey, send, userId, navigate],
  );

  // Handle abort
  const handleAbort = useCallback(() => {
    abort(sessionKey);
  }, [abort, sessionKey]);

  // Handle Build & Register
  const handleRegister = useCallback(async () => {
    if (!projectId) return;
    setRegistering(true);
    try {
      await registerProject(projectId);
      toast.success(t("builder.registered"));
    } catch (err) {
      toast.error(t("builder.registerFailed"), err instanceof Error ? err.message : "");
    } finally {
      setRegistering(false);
    }
  }, [projectId, registerProject, t]);

  return (
    <div className="relative flex h-full">
      {/* Left: File tree sidebar */}
      {isMobile ? (
        <>
          {treeSidebarOpen && (
            <div
              className="fixed inset-0 z-40 bg-black/50"
              onClick={() => setTreeSidebarOpen(false)}
            />
          )}
          <div
            className={cn(
              "fixed inset-y-0 left-0 z-50 transition-transform duration-200 ease-in-out",
              treeSidebarOpen ? "translate-x-0" : "-translate-x-full",
            )}
          >
            <div className="flex h-full w-72 max-w-[85vw] flex-col border-r bg-background">
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
      ) : (
        <aside className="hidden w-64 shrink-0 flex-col border-r md:flex">
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

      {/* Center: chat + optional file viewer */}
      <div className="flex flex-1 flex-col min-w-0">
        {/* Mobile top bar + Build & Register button */}
        {(isMobile || hasCompleted) && (
          <div className="flex items-center border-b px-3 py-2 landscape-compact">
            {isMobile && (
              <button
                onClick={() => setTreeSidebarOpen(true)}
                className="rounded-md p-2.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                title={t("builder.openFiles")}
                aria-label={t("builder.openFiles")}
              >
                <PanelLeftOpen className="h-4 w-4" />
              </button>
            )}
            <div className="flex-1" />
            {hasCompleted && projectId && (
              <Button
                size="sm"
                variant="outline"
                onClick={handleRegister}
                disabled={registering}
                className="gap-1"
              >
                <PackageCheck className="h-3.5 w-3.5" />
                {registering ? t("builder.registering") : t("builder.buildAndRegister")}
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
          streamText={streamText}
          thinkingText={thinkingText}
          toolStream={toolStream}
          isRunning={isRunning}
          loading={messagesLoading}
          scrollTrigger={scrollTrigger}
        />

        {activePath && (
          <MCPBuilderFileViewer
            content={fileContent}
            path={activePath}
            loading={fileContentLoading}
            onClose={handleCloseViewer}
          />
        )}

        <ChatInput
          onSend={handleSend}
          onAbort={handleAbort}
          isRunning={isRunning}
          disabled={!connected}
        />
      </div>
    </div>
  );
}
