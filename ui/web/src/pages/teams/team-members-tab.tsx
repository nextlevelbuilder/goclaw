import { useState, useMemo, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { UserPlus, Info } from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { useTranslation } from "react-i18next";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import type { TeamMemberData } from "@/types/team";
import { MemberList } from "./member-sections";
import { MEMBER_ROLE_OPTIONS } from "./member-sections/member-utils";

interface TeamMembersTabProps {
  teamId: string;
  members: TeamMemberData[];
  onAddMember?: (agentId: string, role?: string) => Promise<void>;
  onRemoveMember?: (agentId: string) => Promise<void>;
}

export function TeamMembersTab({ members, onAddMember, onRemoveMember }: TeamMembersTabProps) {
  const { t } = useTranslation("teams");
  const { agents, refresh: refreshAgents } = useAgents();
  const [selectedAgent, setSelectedAgent] = useState("");
  const [selectedRole, setSelectedRole] = useState("member");
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
      await onAddMember(selectedAgent, selectedRole);
      setSelectedAgent("");
      setSelectedRole("member");
    } catch {
      // error handled upstream
    } finally {
      setAdding(false);
    }
  };

  return (
    <div className="space-y-6">
      {onAddMember && (
        <div className="space-y-2">
          <Label className="inline-flex items-center gap-1">
            {t("members.addMember")}
            <TooltipProvider delayDuration={200}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Info className="h-3.5 w-3.5 text-muted-foreground cursor-help" />
                </TooltipTrigger>
                <TooltipContent side="top">
                  {t("members.addMemberTooltip")}
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
                placeholder={availableAgents.length === 0 ? t("members.noAvailableAgents") : t("members.searchAgents")}
              />
            </div>
            <Select value={selectedRole} onValueChange={setSelectedRole}>
              <SelectTrigger className="h-9 w-[120px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {MEMBER_ROLE_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {opt.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button
              size="sm"
              className="h-9 gap-1"
              disabled={!availableAgents.some((a) => a.value === selectedAgent) || adding}
              onClick={handleAdd}
            >
              <UserPlus className="h-4 w-4" />
              {adding ? t("members.adding") : t("members.add")}
            </Button>
          </div>
        </div>
      )}
      <MemberList members={members} onRemove={onRemoveMember} />
    </div>
  );
}
