import { useTranslation } from "react-i18next";
import { Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { SessionSwitcher } from "@/components/chat/session-switcher";
import type { SessionInfo } from "@/types/session";

interface MCPBuilderSidebarProps {
  sessions: SessionInfo[];
  sessionsLoading: boolean;
  activeSessionKey: string;
  onSessionSelect: (key: string) => void;
  onNewBuild: () => void;
}

export function MCPBuilderSidebar({
  sessions,
  sessionsLoading,
  activeSessionKey,
  onSessionSelect,
  onNewBuild,
}: MCPBuilderSidebarProps) {
  const { t } = useTranslation("mcp");
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
          {t("builder.newBuild")}
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
