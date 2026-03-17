import { useTranslation } from "react-i18next";
import { Users, Trash2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip";
import { formatRelativeTime } from "@/lib/format";
import type { TeamData } from "@/types/team";

interface TeamListRowProps {
  team: TeamData;
  onClick: () => void;
  onDelete?: () => void;
}

export function TeamListRow({ team, onClick, onDelete }: TeamListRowProps) {
  const { t } = useTranslation("teams");
  const members = team.members ?? [];
  const memberCount = team.member_count ?? members.length;

  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full cursor-pointer items-center gap-3 rounded-lg border bg-card px-4 py-3 text-left transition-all hover:border-primary/30 hover:shadow-sm"
    >
      {/* Icon */}
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
        <Users className="h-4 w-4" />
      </div>

      {/* Name */}
      <div className="min-w-0 flex-1">
        <span className="truncate text-sm font-semibold">{team.name}</span>
      </div>

      {/* Members as comma-separated names */}
      {memberCount > 0 && (
        <div className="hidden shrink-0 items-center gap-1 text-xs text-muted-foreground sm:flex sm:max-w-[200px] md:max-w-[280px]">
          <span className="truncate">
            {members.map((m, i) => (
              <Tooltip key={m.agent_id}>
                <TooltipTrigger asChild>
                  <span className="cursor-default hover:text-foreground">
                    {i > 0 && ", "}
                    {m.display_name || m.agent_key}
                  </span>
                </TooltipTrigger>
                {m.frontmatter && (
                  <TooltipContent side="top" className="max-w-xs text-xs">
                    {m.frontmatter}
                  </TooltipContent>
                )}
              </Tooltip>
            ))}
          </span>
        </div>
      )}

      {/* Description */}
      {team.description && (
        <div className="hidden shrink-0 text-xs text-muted-foreground md:block md:w-40 md:truncate lg:w-56">
          {team.description}
        </div>
      )}

      {/* Lead agent */}
      {team.lead_agent_key && (
        <div className="hidden shrink-0 lg:block">
          <Badge variant="outline" className="text-[11px]">
            {t("detail.lead")}: {team.lead_display_name || team.lead_agent_key}
          </Badge>
        </div>
      )}

      {/* Version */}
      {(team.settings as Record<string, unknown>)?.version != null && (
        <Badge variant="outline" className="hidden shrink-0 text-[10px] sm:inline-flex">
          v{String((team.settings as Record<string, unknown>).version)}
        </Badge>
      )}

      {/* Status */}
      <Badge variant={team.status === "active" ? "success" : "secondary"} className="shrink-0">
        {team.status}
      </Badge>

      {/* Created date */}
      {team.created_at && (
        <span className="hidden shrink-0 text-[11px] text-muted-foreground lg:block">
          {formatRelativeTime(team.created_at)}
        </span>
      )}

      {/* Delete */}
      {onDelete && (
        <Button
          variant="ghost"
          size="xs"
          className="shrink-0 text-muted-foreground hover:text-destructive"
          onClick={(e) => {
            e.stopPropagation();
            onDelete();
          }}
        >
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      )}
    </button>
  );
}
