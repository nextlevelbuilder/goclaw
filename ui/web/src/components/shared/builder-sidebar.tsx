import { Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { SessionSwitcher } from "@/components/chat/session-switcher";
import type { SessionInfo } from "@/types/session";

export interface BuilderSidebarProps {
  sessions: SessionInfo[];
  sessionsLoading: boolean;
  activeSessionKey: string;
  onSessionSelect: (key: string) => void;
  onNewBuild: () => void;
  newButtonLabel: string;
}

export function BuilderSidebar({
  sessions,
  sessionsLoading,
  activeSessionKey,
  onSessionSelect,
  onNewBuild,
  newButtonLabel,
}: BuilderSidebarProps) {
  return (
    <div className="flex h-full w-72 max-w-[85vw] flex-col border-r bg-background">
      {/* New build button */}
      <div className="p-3">
        <Button
          variant="outline"
          className="w-full justify-start gap-2"
          onClick={onNewBuild}
        >
          <Plus className="h-4 w-4" />
          {newButtonLabel}
        </Button>
      </div>

      {/* Session list */}
      <div className="flex-1 overflow-y-auto">
        <SessionSwitcher
          sessions={sessions}
          activeKey={activeSessionKey}
          onSelect={onSessionSelect}
          loading={sessionsLoading}
        />
      </div>
    </div>
  );
}
