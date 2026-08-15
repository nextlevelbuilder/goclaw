import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Users, Trash2, Bot, Crown } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatRelativeTime } from "@/lib/format";
import type { TeamData, TeamMemberData } from "@/types/team";

interface TeamCardProps {
  team: TeamData;
  onClick: () => void;
  onDelete?: () => void;
}

const MAX_VISIBLE = 4;

function MemberChip({ member, isLead }: { member: TeamMemberData; isLead: boolean }) {
  const name = member.display_name || member.agent_key || "?";
  return (
    <div className="flex items-start gap-2 rounded-lg bg-muted/50 px-2.5 py-1.5">
      {member.emoji ? (
        <span className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center text-sm leading-none">
          {member.emoji}
        </span>
      ) : (
        <Bot className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
      )}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1">
          <span className="truncate text-xs font-medium">{name}</span>
          {isLead && <Crown className="h-3 w-3 shrink-0 text-amber-500" />}
        </div>
        {member.frontmatter && (
          <p className="line-clamp-2 text-xs-plus leading-snug text-muted-foreground">
            {member.frontmatter}
          </p>
        )}
      </div>
    </div>
  );
}

export function TeamCard({ team, onClick, onDelete }: TeamCardProps) {
  const { t } = useTranslation("teams");
  const members = team.members ?? [];
  const memberCount = team.member_count ?? members.length;

  // Sort: lead first, then alphabetical — show max 4
  const visible = useMemo(() => {
    const sorted = [...members].sort((a, b) => {
      if (a.role === "lead" && b.role !== "lead") return -1;
      if (b.role === "lead" && a.role !== "lead") return 1;
      return (a.display_name || a.agent_key || "").localeCompare(b.display_name || b.agent_key || "");
    });
    return sorted.slice(0, MAX_VISIBLE);
  }, [members]);

  const overflow = memberCount - MAX_VISIBLE;

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
        {(team.settings as Record<string, unknown>)?.version != null && (
          <Badge variant="outline" className="shrink-0 text-2xs">
            v{String((team.settings as Record<string, unknown>).version)}
          </Badge>
        )}
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

      {/* Members — each as individual rounded chip */}
      {memberCount > 0 && (
        <div className="flex flex-col gap-1.5">
          {visible.map((m) => (
            <MemberChip key={m.agent_id} member={m} isLead={m.role === "lead"} />
          ))}
          {overflow > 0 && (
            <span className="pl-2 text-xs-plus text-muted-foreground">
              +{overflow} {t("detail.more")}
            </span>
          )}
        </div>
      )}

      {/* Bottom: time + delete */}
      <div className="flex items-center gap-1.5">
        <div className="ml-auto flex items-center gap-1.5">
          {team.created_at && (
            <span className="text-xs-plus text-muted-foreground">
              {formatRelativeTime(team.created_at)}
            </span>
          )}
          {onDelete && (
            <Button
              variant="ghost"
              size="xs"
              className="text-muted-foreground hover:text-destructive"
              onClick={(e) => {
                e.stopPropagation();
                onDelete();
              }}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>
      </div>
    </button>
  );
}
