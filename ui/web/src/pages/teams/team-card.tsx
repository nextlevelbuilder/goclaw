import { useTranslation } from "react-i18next";
import { Users, Trash2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { TeamData } from "@/types/team";

interface TeamCardProps {
  team: TeamData;
  onClick: () => void;
  onDelete?: () => void;
}

export function TeamCard({ team, onClick, onDelete }: TeamCardProps) {
  const { t } = useTranslation("teams");
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex cursor-pointer flex-col gap-3 rounded-lg border bg-card p-4 text-left transition-all hover:border-primary/30 hover:shadow-md"
    >
      {/* Top row: icon + name + status */}
      <div className="flex items-center gap-3">
        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
          <Users className="h-4.5 w-4.5" />
        </div>
        <div className="min-w-0 flex-1">
          <span className="truncate text-sm font-semibold">{team.name}</span>
        </div>
        <Badge variant={team.status === "active" ? "success" : "secondary"} className="shrink-0">
          {team.status}
        </Badge>
      </div>

      {/* Description */}
      {team.description && (
        <div className="line-clamp-2 text-xs text-muted-foreground/70">
          {team.description}
        </div>
      )}

      {/* Bottom badges */}
      <div className="flex items-center gap-1.5">
        {team.lead_agent_key && (
          <Badge variant="outline" className="text-[11px]">
            {t("settings.leadAgent")}: {team.lead_display_name || team.lead_agent_key}
          </Badge>
        )}
        {onDelete && (
          <Button
            variant="ghost"
            size="xs"
            className="ml-auto text-muted-foreground hover:text-destructive"
            onClick={(e) => {
              e.stopPropagation();
              onDelete();
            }}
          >
            <Trash2 className="h-3.5 w-3.5" />
            Delete
          </Button>
        )}
      </div>
    </button>
  );
}
