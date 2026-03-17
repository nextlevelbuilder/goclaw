import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ArrowLeft, Settings, Trash2, Users } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { TeamData, TeamMemberData, TeamAccessSettings } from "@/types/team";

interface BoardHeaderProps {
  team: TeamData;
  members: TeamMemberData[];
  onBack: () => void;
  onDelete: () => void;
  onSettings: () => void;
  onMembers: () => void;
  onV2Click?: () => void;
}

export function BoardHeader({ team, members, onBack, onDelete, onSettings, onMembers, onV2Click }: BoardHeaderProps) {
  const { t } = useTranslation("teams");
  const settings = (team.settings ?? {}) as TeamAccessSettings;
  const isV2 = (settings.version ?? 1) >= 2;
  const leadMember = members.find((m) => m.role === "lead");
  const leadName = leadMember?.display_name || leadMember?.agent_key
    || team.lead_display_name || team.lead_agent_key;
  const memberCount = members.length;

  return (
    <div className="flex items-center gap-3 border-b bg-card px-4 py-2.5 landscape-compact">
      <Button variant="ghost" size="icon" onClick={onBack} className="shrink-0">
        <ArrowLeft className="h-4 w-4" />
      </Button>

      {/* Team info */}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <h2 className="truncate text-base font-semibold sm:text-lg">{team.name}</h2>
          <Badge variant={team.status === "active" ? "success" : "secondary"} className="text-[10px]">
            {team.status}
          </Badge>
          {isV2 && (
            <Badge
              className="bg-gradient-to-r from-violet-500 to-indigo-500 text-[10px] px-2 py-0.5 text-white border-0 font-semibold shadow-sm cursor-pointer hover:opacity-80 transition-opacity"
              onClick={onV2Click}
            >
              v2 Super Team (Beta)
            </Badge>
          )}
        </div>
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          {leadName && (
            <span>{t("detail.lead")}: {leadName}</span>
          )}
          <span>
            {memberCount !== 1
              ? t("detail.memberCountPlural", { count: memberCount })
              : t("detail.memberCount", { count: memberCount })}
          </span>
        </div>
      </div>

      {/* Actions */}
      <Button variant="ghost" size="sm" onClick={onMembers} className="shrink-0 gap-1.5">
        <Users className="h-4 w-4" />
        <span className="hidden sm:inline">{t("members.title")}</span>
      </Button>
      <Button variant="ghost" size="sm" onClick={onSettings} className="shrink-0 gap-1.5">
        <Settings className="h-4 w-4" />
        <span className="hidden sm:inline">{t("detail.tabs.settings")}</span>
      </Button>
      <Button
        variant="ghost"
        size="sm"
        className="shrink-0 gap-1.5 text-muted-foreground hover:text-destructive"
        onClick={onDelete}
      >
        <Trash2 className="h-4 w-4" />
        <span className="hidden sm:inline">{t("delete.title")}</span>
      </Button>
    </div>
  );
}
