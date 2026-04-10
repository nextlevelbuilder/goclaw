import { memo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { AgentSelector } from "@/components/chat/agent-selector";
import { SessionSwitcher } from "@/components/chat/session-switcher";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import type { SessionInfo } from "@/types/session";

interface ChatSidebarProps {
  agentId: string;
  onAgentChange: (agentId: string) => void;
  sessions: SessionInfo[];
  sessionsLoading: boolean;
  activeSessionKey: string;
  onSessionSelect: (key: string) => void;
  onDeleteSession?: (key: string) => void;
  onDeleteAllSessions?: () => Promise<unknown>;
  onNewChat: () => void;
}

export const ChatSidebar = memo(function ChatSidebar({
  agentId,
  onAgentChange,
  sessions,
  sessionsLoading,
  activeSessionKey,
  onSessionSelect,
  onDeleteSession,
  onDeleteAllSessions,
  onNewChat,
}: ChatSidebarProps) {
  const { t } = useTranslation("chat");
  const { t: tc } = useTranslation("common");
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false);
  const [isDeletingAll, setIsDeletingAll] = useState(false);
  const deleteAllDisabled = sessionsLoading || sessions.length === 0 || isDeletingAll;
  const hasAgentSelected = !!agentId;

  const handleConfirmDeleteAll = async () => {
    if (!onDeleteAllSessions || deleteAllDisabled) return;
    try {
      setIsDeletingAll(true);
      await onDeleteAllSessions();
      setBulkDeleteOpen(false);
    } finally {
      setIsDeletingAll(false);
    }
  };

  return (
    <>
      <div className="flex h-full w-72 max-w-[85vw] flex-col border-r bg-background">
      {/* Agent selector */}
      <div className="border-b p-3">
        <AgentSelector value={agentId} onChange={onAgentChange} />
      </div>

      {/* New chat button */}
      <div className="flex gap-2 p-3">
        <Button
          variant="outline"
          className="flex-1 justify-start gap-2"
          onClick={onNewChat}
        >
          <Plus className="h-4 w-4" />
          {t("newChat")}
        </Button>
        <Button
          type="button"
          variant="outline"
          size="icon"
          className="shrink-0 text-muted-foreground hover:border-destructive/30 hover:bg-destructive/10 hover:text-destructive"
          aria-label={t("deleteAllChats")}
          title={t("deleteAllChats")}
          disabled={deleteAllDisabled}
          onClick={() => setBulkDeleteOpen(true)}
        >
          <Trash2 className="h-4 w-4" />
        </Button>
      </div>

      {/* Session list */}
      <div className="flex-1 overflow-y-auto">
        <SessionSwitcher
          sessions={sessions}
          activeKey={activeSessionKey}
          onSelect={onSessionSelect}
          onDelete={onDeleteSession}
          loading={sessionsLoading}
        />
      </div>
      </div>

      <Dialog open={bulkDeleteOpen} onOpenChange={(open) => !isDeletingAll && setBulkDeleteOpen(open)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t(hasAgentSelected ? "deleteAgentChatsTitle" : "deleteAllChatsTitle")}
            </DialogTitle>
            <DialogDescription>
              {t(hasAgentSelected ? "deleteAgentChatsConfirm" : "deleteAllChatsConfirm")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setBulkDeleteOpen(false)} disabled={isDeletingAll}>
              {tc("cancel")}
            </Button>
            <Button variant="destructive" onClick={handleConfirmDeleteAll} disabled={deleteAllDisabled}>
              {tc("delete")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
});
