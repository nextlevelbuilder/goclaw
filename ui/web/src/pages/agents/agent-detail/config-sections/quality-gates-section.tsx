import { Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { QualityGateConfig } from "@/types/agent";
import { ConfigSection, InfoLabel, numOrUndef } from "./config-section";

interface QualityGatesSectionProps {
  enabled: boolean;
  value: QualityGateConfig[];
  onToggle: (v: boolean) => void;
  onChange: (v: QualityGateConfig[]) => void;
}

const defaultGate: QualityGateConfig = {
  event: "delegation.completed",
  type: "agent",
  block_on_failure: true,
  max_retries: 2,
  timeout_seconds: 120,
};

export function QualityGatesSection({ enabled, value, onToggle, onChange }: QualityGatesSectionProps) {
  const updateGate = (i: number, patch: Partial<QualityGateConfig>) => {
    const next = [...value];
    next[i] = { ...next[i], ...patch } as QualityGateConfig;
    onChange(next);
  };

  const removeGate = (i: number) => {
    onChange(value.filter((_, idx) => idx !== i));
  };

  const addGate = () => {
    onChange([...value, { ...defaultGate }]);
  };

  return (
    <ConfigSection
      title="Quality Gates"
      description="Validate delegation output before returning to the caller"
      enabled={enabled}
      onToggle={onToggle}
    >
      {value.length === 0 ? (
        <p className="text-xs text-muted-foreground italic">
          No quality gates configured. Click &ldquo;Add Gate&rdquo; to create one.
        </p>
      ) : (
        value.map((gate, i) => (
          <div key={i} className="relative rounded-md border p-3 space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium text-muted-foreground">Gate #{i + 1}</span>
              <Button
                variant="ghost"
                size="icon"
                className="h-6 w-6"
                onClick={() => removeGate(i)}
              >
                <Trash2 className="h-3.5 w-3.5 text-muted-foreground" />
              </Button>
            </div>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <InfoLabel tip="Validation method. 'Agent' delegates to a reviewer agent, 'Command' runs a shell command and checks exit code.">Type</InfoLabel>
                <Select
                  value={gate.type}
                  onValueChange={(v) => updateGate(i, {
                    type: v as "agent" | "command",
                    agent: v === "agent" ? gate.agent : undefined,
                    command: v === "command" ? gate.command : undefined,
                  })}
                >
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="agent">Agent</SelectItem>
                    <SelectItem value="command">Command</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                {gate.type === "agent" ? (
                  <>
                    <InfoLabel tip="Key of the agent used to review and validate the delegation output.">Reviewer Agent Key</InfoLabel>
                    <Input
                      placeholder="qa-reviewer"
                      value={gate.agent ?? ""}
                      onChange={(e) => updateGate(i, { agent: e.target.value || undefined })}
                    />
                  </>
                ) : (
                  <>
                    <InfoLabel tip="Shell command to run for validation. Exit code 0 = pass, non-zero = fail.">Command</InfoLabel>
                    <Input
                      placeholder="npm test"
                      value={gate.command ?? ""}
                      onChange={(e) => updateGate(i, { command: e.target.value || undefined })}
                    />
                  </>
                )}
              </div>
            </div>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="flex items-center gap-2">
                <Switch
                  checked={gate.block_on_failure}
                  onCheckedChange={(v) => updateGate(i, { block_on_failure: v })}
                />
                <InfoLabel tip="When enabled, a failed gate prevents the result from being returned to the caller and triggers retries.">Block on Failure</InfoLabel>
              </div>
              {gate.block_on_failure && (
                <div className="space-y-2">
                  <InfoLabel tip="Number of times to retry the delegation if the quality gate fails.">Max Retries</InfoLabel>
                  <Input
                    type="number"
                    placeholder="2"
                    value={gate.max_retries ?? ""}
                    onChange={(e) => updateGate(i, { max_retries: numOrUndef(e.target.value) })}
                  />
                </div>
              )}
            </div>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <InfoLabel tip="Maximum time in seconds to wait for the quality gate check to complete before timing out.">Timeout (seconds)</InfoLabel>
                <Input
                  type="number"
                  placeholder="120"
                  value={gate.timeout_seconds ?? ""}
                  onChange={(e) => updateGate(i, { timeout_seconds: numOrUndef(e.target.value) })}
                />
              </div>
            </div>
          </div>
        ))
      )}
      <Button variant="outline" size="sm" onClick={addGate} className="gap-1">
        <Plus className="h-3.5 w-3.5" /> Add Gate
      </Button>
    </ConfigSection>
  );
}
