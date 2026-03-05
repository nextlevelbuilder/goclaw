import { Link2, Pencil, Trash2, Users } from "lucide-react";
import { Link } from "react-router";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import type { AgentLinkData } from "@/types/agent";
import { directionBadgeVariant, linkTargetName } from "./link-utils";

interface LinkListProps {
  links: AgentLinkData[];
  loading: boolean;
  agentId: string;
  onStatusToggle: (link: AgentLinkData) => void;
  onEdit: (link: AgentLinkData) => void;
  onDelete: (link: AgentLinkData) => void;
}

export function LinkList({
  links,
  loading,
  agentId,
  onStatusToggle,
  onEdit,
  onDelete,
}: LinkListProps) {
  if (loading && links.length === 0) {
    return (
      <div className="py-8 text-center text-sm text-muted-foreground">
        Loading links...
      </div>
    );
  }

  if (links.length === 0) {
    return (
      <div className="flex flex-col items-center gap-2 py-8 text-center">
        <Link2 className="h-8 w-8 text-muted-foreground/50" />
        <p className="text-sm text-muted-foreground">No agent links yet</p>
        <p className="text-xs text-muted-foreground">
          Create links to allow agents to delegate tasks to each other.
        </p>
      </div>
    );
  }

  return (
    <div className="overflow-x-auto rounded-lg border">
      <div className="grid min-w-[500px] grid-cols-[1fr_100px_60px_60px_80px] items-center gap-2 border-b bg-muted/50 px-4 py-2.5 text-xs font-medium text-muted-foreground">
        <span>Target</span>
        <span>Direction</span>
        <span>Status</span>
        <span>Limit</span>
        <span />
      </div>
      {links.map((link) => (
        <div
          key={link.id}
          className="grid min-w-[500px] grid-cols-[1fr_100px_60px_60px_80px] items-center gap-2 border-b px-4 py-3 last:border-0"
        >
          <div className="min-w-0">
            <div className="flex items-center gap-1.5">
              <span className="truncate text-sm font-medium">
                {linkTargetName(link, agentId)}
              </span>
              {link.team_id && (
                <Link to={`/teams/${link.team_id}`}>
                  <Badge variant="outline" className="gap-0.5 text-[10px] px-1.5 py-0 hover:bg-muted cursor-pointer">
                    <Users className="h-3 w-3" />
                    {link.team_name || "Team"}
                  </Badge>
                </Link>
              )}
            </div>
            {link.target_description && link.source_agent_id === agentId && (
              <p className="truncate text-xs text-muted-foreground">
                {link.target_description}
              </p>
            )}
            {link.description && (
              <p className="truncate text-xs text-muted-foreground/70">
                {link.description}
              </p>
            )}
          </div>
          <Badge variant={directionBadgeVariant(link.direction)}>
            {link.direction}
          </Badge>
          <Switch
            checked={link.status === "active"}
            onCheckedChange={() => onStatusToggle(link)}
          />
          <span className="text-sm text-muted-foreground">
            {link.max_concurrent}
          </span>
          <div className="flex gap-1">
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8"
              onClick={() => onEdit(link)}
            >
              <Pencil className="h-3.5 w-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8"
              onClick={() => onDelete(link)}
            >
              <Trash2 className="h-3.5 w-3.5 text-destructive" />
            </Button>
          </div>
        </div>
      ))}
    </div>
  );
}
