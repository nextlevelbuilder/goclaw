import { Users, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { useTranslation } from "react-i18next";
import type { TeamMemberData } from "@/types/team";
import { roleBadgeVariant } from "./member-utils";

interface MemberListProps {
  members: TeamMemberData[];
  onRemove?: (agentId: string) => void;
}

export function MemberList({ members, onRemove }: MemberListProps) {
  const { t } = useTranslation("teams");

  if (members.length === 0) {
    return (
      <div className="flex flex-col items-center gap-2 py-8 text-center">
        <Users className="h-8 w-8 text-muted-foreground/50" />
        <p className="text-sm text-muted-foreground">{t("members.noMembers")}</p>
      </div>
    );
  }

  return (
    <div className="rounded-lg border">
      <div className="grid grid-cols-[1fr_1fr_80px_40px] items-center gap-2 border-b bg-muted/50 px-4 py-2.5 text-xs font-medium text-muted-foreground">
        <span>{t("members.columns.agent")}</span>
        <span>{t("members.columns.frontmatter")}</span>
        <span>{t("members.columns.role")}</span>
        <span />
      </div>
      {members.map((member) => (
        <div
          key={member.agent_id}
          className="grid grid-cols-[1fr_1fr_80px_40px] items-center gap-2 border-b px-4 py-3 last:border-0"
        >
          <div className="min-w-0">
            <span className="truncate text-sm font-medium">
              {member.display_name || member.agent_key || member.agent_id.slice(0, 8)}
            </span>
            {member.display_name && member.agent_key && (
              <p className="truncate text-xs text-muted-foreground">
                {member.agent_key}
              </p>
            )}
          </div>
          <div className="min-w-0">
            {member.frontmatter ? (
              <p className="line-clamp-2 text-xs text-muted-foreground/70">
                {member.frontmatter}
              </p>
            ) : (
              <span className="text-xs text-muted-foreground/40">—</span>
            )}
          </div>
          <Badge variant={roleBadgeVariant(member.role)}>
            {member.role}
          </Badge>
          <div className="flex justify-center">
            {member.role !== "lead" && onRemove && members.filter((m) => m.role !== "lead").length > 1 && (
              <button
                type="button"
                onClick={() => onRemove(member.agent_id)}
                className="rounded p-1 text-muted-foreground/50 transition-colors hover:bg-destructive/10 hover:text-destructive"
                title={t("members.removeMember")}
              >
                <X className="h-4 w-4" />
              </button>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}
