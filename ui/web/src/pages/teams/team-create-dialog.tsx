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
import { X, Info } from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
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
          <DialogTitle>Create Team</DialogTitle>
        </DialogHeader>
        <div className="space-y-4 py-4 overflow-y-auto min-h-0">
          <div className="space-y-2">
            <Label htmlFor="teamName">Name *</Label>
            <Input
              id="teamName"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Research Team"
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="teamDesc">Description</Label>
            <Input
              id="teamDesc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Optional team description..."
            />
          </div>

          <div className="space-y-2">
            <Label>Lead Agent *</Label>
            <Combobox
              value={lead}
              onChange={setLead}
              options={leadOptions}
              placeholder={agentsLoading ? "Loading agents..." : "Select lead agent..."}
            />
            {!agentsLoading && leadOptions.length === 0 && (
              <p className="text-xs text-muted-foreground">No active agents found. Create agents first.</p>
            )}
          </div>

          <div className="space-y-2">
            <Label className="inline-flex items-center gap-1">
              Members
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
              placeholder={agentsLoading ? "Loading agents..." : "Search and add members..."}
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
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            Cancel
          </Button>
          <Button onClick={handleCreate} disabled={!name.trim() || !leadOptions.some((o) => o.value === lead) || loading}>
            {loading ? "Creating..." : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
