import { useState, useMemo, useEffect } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Combobox } from "@/components/ui/combobox";
import { Badge } from "@/components/ui/badge";
import { X, Info, Loader2 } from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { useTranslation } from "react-i18next";
import { useAgents } from "@/pages/agents/hooks/use-agents";

interface TeamCreateDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreate: (data: {
    name: string;
    lead: string;
    members: string[];
    description?: string;
  }) => Promise<void>;
}

export function TeamCreateDialog({ open, onOpenChange, onCreate }: TeamCreateDialogProps) {
  const { t } = useTranslation("teams");
  const { agents, loading: agentsLoading, refresh: refreshAgents } = useAgents();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [lead, setLead] = useState("");
  const [memberSearch, setMemberSearch] = useState("");
  const [members, setMembers] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);

  // Refresh agents when dialog opens to ensure fresh data
  useEffect(() => {
    if (open) refreshAgents();
  }, [open, refreshAgents]);

  // Lead can be any active agent (open or predefined) — it receives user requests
  const leadOptions = useMemo(
    () =>
      agents
        .filter((a) => a.status === "active")
        .map((a) => ({
          value: a.id,
          label: a.display_name || a.agent_key,
        })),
    [agents],
  );

  // Members must be predefined (need agent-level context for team collaboration)
  const memberOptions = useMemo(
    () =>
      agents
        .filter((a) => a.agent_type === "predefined" && a.status === "active" && a.id !== lead && !members.includes(a.id))
        .map((a) => ({
          value: a.id,
          label: a.display_name || a.agent_key,
        })),
    [agents, lead, members],
  );

  const addMember = (agentId: string) => {
    if (agentId && !members.includes(agentId) && agentId !== lead) {
      setMembers((prev) => [...prev, agentId]);
      setMemberSearch("");
    }
  };

  const removeMember = (agentId: string) => {
    setMembers((prev) => prev.filter((id) => id !== agentId));
  };

  const agentLabel = (agentId: string) => {
    const opt = leadOptions.find((o) => o.value === agentId);
    return opt?.label || agentId.slice(0, 8);
  };

  const handleCreate = async () => {
    if (!name.trim() || !lead) return;
    setLoading(true);
    try {
      await onCreate({
        name: name.trim(),
        lead,
        members,
        description: description.trim() || undefined,
      });
      onOpenChange(false);
      setName("");
      setDescription("");
      setLead("");
      setMembers([]);
      setMemberSearch("");
    } catch {
      // error handled upstream
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{t("create.title")}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4 py-4 -mx-4 px-4 sm:-mx-6 sm:px-6 overflow-y-auto min-h-0">
          <div className="space-y-2">
            <Label htmlFor="teamName">{t("create.name")}</Label>
            <Input
              id="teamName"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("create.namePlaceholder")}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="teamDesc">{t("create.description")}</Label>
            <Input
              id="teamDesc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t("create.descriptionPlaceholder")}
            />
          </div>

          <div className="space-y-2">
            <Label>{t("create.leadAgent")}</Label>
            <Combobox
              value={lead}
              onChange={setLead}
              options={leadOptions}
              placeholder={agentsLoading ? t("create.loadingAgents") : t("create.selectLeadAgent")}
            />
            {!agentsLoading && leadOptions.length === 0 && (
              <p className="text-xs text-muted-foreground">{t("create.noActiveAgents")}</p>
            )}
          </div>

          <div className="space-y-2">
            <Label className="inline-flex items-center gap-1">
              {t("create.members")}
              <TooltipProvider delayDuration={200}>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Info className="h-3.5 w-3.5 text-muted-foreground cursor-help" />
                  </TooltipTrigger>
                  <TooltipContent side="top">
                    {t("create.membersTooltip")}
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            </Label>
            <Combobox
              value={memberSearch}
              onChange={(val) => {
                // If an option was selected (value matches an agent ID), add it
                if (memberOptions.some((o) => o.value === val)) {
                  addMember(val);
                } else {
                  setMemberSearch(val);
                }
              }}
              options={memberOptions}
              placeholder={agentsLoading ? t("create.loadingAgents") : t("create.searchMembers")}
            />
            {members.length > 0 && (
              <div className="flex flex-wrap gap-1.5 pt-1">
                {members.map((id) => (
                  <Badge key={id} variant="secondary" className="gap-1 pr-1">
                    {agentLabel(id)}
                    <button
                      type="button"
                      onClick={() => removeMember(id)}
                      className="ml-0.5 rounded-full p-0.5 hover:bg-muted"
                    >
                      <X className="h-3 w-3" />
                    </button>
                  </Badge>
                ))}
              </div>
            )}
            {lead && members.length === 0 && memberOptions.length > 0 && (
              <p className="text-xs text-amber-600 dark:text-amber-400">{t("create.needMembers")}</p>
            )}
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            {t("create.cancel")}
          </Button>
          <Button onClick={handleCreate} disabled={!name.trim() || !leadOptions.some((o) => o.value === lead) || members.length === 0 || loading} className="gap-1">
            {loading && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            {loading ? t("create.creating") : t("create.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
