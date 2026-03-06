import { useState, useMemo, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
import { Label } from "@/components/ui/label";
import { UserPlus, Info } from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import type { TeamMemberData } from "@/types/team";
import { MemberList } from "./member-sections";

interface TeamMembersTabProps {
  teamId: string;
  members: TeamMemberData[];
  onAddMember?: (agentId: string) => Promise<void>;
  onRemoveMember?: (agentId: string) => Promise<void>;
}

export function TeamMembersTab({ members, onAddMember, onRemoveMember }: TeamMembersTabProps) {
  const { agents, refresh: refreshAgents } = useAgents();
  const [selectedAgent, setSelectedAgent] = useState("");
  const [adding, setAdding] = useState(false);

  useEffect(() => {
    refreshAgents();
  }, [refreshAgents]);

  const memberIds = useMemo(() => new Set(members.map((m) => m.agent_id)), [members]);

  const availableAgents = useMemo(
    () =>
      agents
        .filter((a) => a.agent_type === "predefined" && a.status === "active" && !memberIds.has(a.id))
        .map((a) => ({ value: a.id, label: a.display_name || a.agent_key })),
    [agents, memberIds],
  );

  const handleAdd = async () => {
    if (!selectedAgent || !onAddMember) return;
    setAdding(true);
    try {
      await onAddMember(selectedAgent);
      setSelectedAgent("");
    } catch {
      // error handled upstream
    } finally {
      setAdding(false);
    }
  };

  return (
    <div className="max-w-2xl space-y-6">
      {onAddMember && (
        <div className="space-y-2">
          <Label className="inline-flex items-center gap-1">
            Add Member
            <TooltipProvider delayDuration={200}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Info className="h-3.5 w-3.5 text-muted-foreground cursor-help" />
                </TooltipTrigger>
                <TooltipContent side="top">
                  Only predefined agents can be added as team members.
                  <br />
                  Open agents are per-user and lack shared context needed for team collaboration.
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </Label>
          <div className="flex gap-2">
            <div className="flex-1">
              <Combobox
                value={selectedAgent}
                onChange={setSelectedAgent}
                options={availableAgents}
                placeholder={availableAgents.length === 0 ? "No available agents" : "Search agents..."}
              />
            </div>
            <Button
              size="sm"
              className="h-9 gap-1"
              disabled={!availableAgents.some((a) => a.value === selectedAgent) || adding}
              onClick={handleAdd}
            >
              <UserPlus className="h-4 w-4" />
              {adding ? "Adding..." : "Add"}
            </Button>
          </div>
        </div>
      )}
      <MemberList members={members} onRemove={onRemoveMember} />
    </div>
  );
}
