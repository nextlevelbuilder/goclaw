import { useState } from "react";
import { Plus, ChevronDown, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Combobox, type ComboboxOption } from "@/components/ui/combobox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { AgentLinkSettings } from "@/types/agent";
import { DIRECTION_OPTIONS, buildSettings } from "./link-utils";

interface LinkCreateFormProps {
  agentOptions: ComboboxOption[];
  onSubmit: (params: {
    targetAgent: string;
    direction: string;
    description?: string;
    maxConcurrent?: number;
    settings?: AgentLinkSettings;
  }) => Promise<void>;
}

export function LinkCreateForm({ agentOptions, onSubmit }: LinkCreateFormProps) {
  const [targetAgent, setTargetAgent] = useState("");
  const [direction, setDirection] = useState("outbound");
  const [description, setDescription] = useState("");
  const [maxConcurrent, setMaxConcurrent] = useState("3");
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [userAllow, setUserAllow] = useState("");
  const [userDeny, setUserDeny] = useState("");
  const [creating, setCreating] = useState(false);

  const isValidTarget = agentOptions.some((o) => o.value === targetAgent);

  const handleCreate = async () => {
    if (!isValidTarget) return;
    setCreating(true);
    try {
      await onSubmit({
        targetAgent,
        direction,
        description: description || undefined,
        maxConcurrent: parseInt(maxConcurrent, 10) || 3,
        settings: buildSettings(userAllow, userDeny),
      });
      setTargetAgent("");
      setDirection("outbound");
      setDescription("");
      setMaxConcurrent("3");
      setUserAllow("");
      setUserDeny("");
      setShowAdvanced(false);
    } catch {
      // ignore
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="rounded-lg border p-4">
      <h3 className="mb-3 text-sm font-medium">Create Link</h3>
      <div className="space-y-3">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
          <div className="flex-1 space-y-1.5">
            <Label>Target Agent</Label>
            <Combobox
              value={targetAgent}
              onChange={setTargetAgent}
              options={agentOptions}
              placeholder="Select or search agent..."
            />
          </div>
          <div className="w-full space-y-1.5 sm:w-44">
            <Label>Direction</Label>
            <Select value={direction} onValueChange={setDirection}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {DIRECTION_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    <span className="flex flex-col">
                      <span>{opt.label}</span>
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>
        <p className="text-xs text-muted-foreground">
          {DIRECTION_OPTIONS.find((o) => o.value === direction)?.desc}
        </p>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
          <div className="flex-1 space-y-1.5">
            <Label>Description</Label>
            <Input
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Optional description of the link..."
            />
          </div>
          <div className="w-full space-y-1.5 sm:w-32">
            <Label>Max Concurrent</Label>
            <Input
              type="number"
              min={1}
              value={maxConcurrent}
              onChange={(e) => setMaxConcurrent(e.target.value)}
            />
          </div>
        </div>

        {/* Advanced settings */}
        <button
          type="button"
          onClick={() => setShowAdvanced(!showAdvanced)}
          className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
        >
          {showAdvanced ? (
            <ChevronDown className="h-3.5 w-3.5" />
          ) : (
            <ChevronRight className="h-3.5 w-3.5" />
          )}
          Advanced Settings
        </button>
        {showAdvanced && (
          <div className="space-y-3 rounded-md border border-dashed p-3">
            <div className="space-y-1.5">
              <Label>Allowed Users</Label>
              <Input
                value={userAllow}
                onChange={(e) => setUserAllow(e.target.value)}
                placeholder="user1, user2, ..."
              />
              <p className="text-xs text-muted-foreground">
                If set, only these users can trigger this delegation. Comma-separated.
              </p>
            </div>
            <div className="space-y-1.5">
              <Label>Denied Users</Label>
              <Input
                value={userDeny}
                onChange={(e) => setUserDeny(e.target.value)}
                placeholder="user3, user4, ..."
              />
              <p className="text-xs text-muted-foreground">
                These users are blocked from triggering this delegation. Comma-separated.
              </p>
            </div>
          </div>
        )}

        <div className="flex justify-end">
          <Button
            onClick={handleCreate}
            disabled={!isValidTarget || creating}
            className="gap-1.5"
          >
            <Plus className="h-4 w-4" />
            {creating ? "Creating..." : "Create Link"}
          </Button>
        </div>
      </div>
    </div>
  );
}
