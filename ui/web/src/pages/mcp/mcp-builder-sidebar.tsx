import { useTranslation } from "react-i18next";
import { BuilderSidebar } from "@/components/shared/builder-sidebar";
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
    <BuilderSidebar
      sessions={sessions}
      sessionsLoading={sessionsLoading}
      activeSessionKey={activeSessionKey}
      onSessionSelect={onSessionSelect}
      onNewBuild={onNewBuild}
      newButtonLabel={t("builder.newBuild")}
    />
  );
}
