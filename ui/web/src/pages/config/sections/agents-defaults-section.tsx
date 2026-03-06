import { useState, useEffect } from "react";
import { Save, ChevronDown, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { InfoLabel } from "@/components/shared/info-label";
import { ProviderModelSelect } from "@/components/shared/provider-model-select";

/* eslint-disable @typescript-eslint/no-explicit-any */
type AgentsData = Record<string, any>;

const DEFAULT: AgentsData = { defaults: {} };

interface Props {
  data: AgentsData | undefined;
  onSave: (value: AgentsData) => Promise<void>;
  saving: boolean;
}

export function AgentsDefaultsSection({ data, onSave, saving }: Props) {
  const [draft, setDraft] = useState<AgentsData>(data ?? DEFAULT);
  const [dirty, setDirty] = useState(false);
  const [openSubs, setOpenSubs] = useState<Set<string>>(new Set());

  useEffect(() => {
    setDraft(data ?? DEFAULT);
    setDirty(false);
  }, [data]);

  const defaults = draft.defaults ?? {};

  const updateDefaults = (patch: Record<string, any>) => {
    setDraft((prev) => ({ ...prev, defaults: { ...(prev.defaults ?? {}), ...patch } }));
    setDirty(true);
  };

  const updateNested = (key: string, patch: Record<string, any>) => {
    setDraft((prev) => ({
      ...prev,
      defaults: {
        ...(prev.defaults ?? {}),
        [key]: { ...((prev.defaults ?? {})[key] ?? {}), ...patch },
      },
    }));
    setDirty(true);
  };

  const toggleSub = (key: string) => {
    setOpenSubs((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  if (!data) return null;

  const subagents = defaults.subagents ?? {};
  const memory = defaults.memory ?? {};
  const compaction = defaults.compaction ?? {};
  const pruning = defaults.contextPruning ?? {};
  const sandbox = defaults.sandbox ?? {};

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">Agent Defaults</CardTitle>
        <CardDescription>Default settings for all agents. Per-agent overrides are managed on the Agents page.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Core fields */}
        <ProviderModelSelect
          provider={defaults.provider ?? ""}
          onProviderChange={(v) => updateDefaults({ provider: v })}
          model={defaults.model ?? ""}
          onModelChange={(v) => updateDefaults({ model: v })}
          providerTip="Default LLM provider for all agents. Must match a configured provider name."
          modelTip="Default model ID for all agents. Agents can override this in their own settings."
          showVerify
        />

        <div className="grid grid-cols-4 gap-4">
          <div className="grid gap-1.5">
            <InfoLabel tip="Maximum output tokens per LLM response. Higher values allow longer responses.">Max Tokens</InfoLabel>
            <Input
              type="number"
              value={defaults.max_tokens ?? ""}
              onChange={(e) => updateDefaults({ max_tokens: Number(e.target.value) })}
              placeholder="8192"
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip="Sampling temperature (0-2). Lower = more deterministic, higher = more creative.">Temperature</InfoLabel>
            <Input
              type="number"
              step="0.1"
              value={defaults.temperature ?? ""}
              onChange={(e) => updateDefaults({ temperature: Number(e.target.value) })}
              placeholder="0.7"
              min={0}
              max={2}
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip="Maximum number of tool calls per agent request before stopping.">Max Tool Iterations</InfoLabel>
            <Input
              type="number"
              value={defaults.max_tool_iterations ?? ""}
              onChange={(e) => updateDefaults({ max_tool_iterations: Number(e.target.value) })}
              placeholder="20"
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip="Maximum context size in tokens. Should match the model's context window limit.">Context Window</InfoLabel>
            <Input
              type="number"
              value={defaults.context_window ?? ""}
              onChange={(e) => updateDefaults({ context_window: Number(e.target.value) })}
              placeholder="200000"
            />
          </div>
        </div>

        <div className="grid grid-cols-2 gap-4">
          <div className="grid gap-1.5">
            <InfoLabel tip="Default filesystem workspace path for agent file operations.">Workspace</InfoLabel>
            <Input
              value={defaults.workspace ?? ""}
              onChange={(e) => updateDefaults({ workspace: e.target.value })}
              placeholder="~/.goclaw/workspace"
            />
          </div>
          <div className="flex items-center justify-between">
            <InfoLabel tip="When enabled, agents can only read/write files within the workspace directory.">Restrict to Workspace</InfoLabel>
            <Switch
              checked={defaults.restrict_to_workspace ?? false}
              onCheckedChange={(v) => updateDefaults({ restrict_to_workspace: v })}
            />
          </div>
        </div>

        {/* Collapsible sub-sections */}
        <SubSection
          title="Subagents"
          desc="Concurrent subagent settings"
          open={openSubs.has("subagents")}
          onToggle={() => toggleSub("subagents")}
        >
          <div className="grid grid-cols-2 gap-4">
            <Field label="Max Concurrent" tip="Maximum subagents running at the same time." type="number" value={subagents.maxConcurrent} onChange={(v) => updateNested("subagents", { maxConcurrent: Number(v) })} placeholder="20" />
            <Field label="Max Spawn Depth" tip="Maximum nesting depth for subagent spawning (agent → subagent → sub-subagent)." type="number" value={subagents.maxSpawnDepth} onChange={(v) => updateNested("subagents", { maxSpawnDepth: Number(v) })} placeholder="1" />
            <Field label="Max Children/Agent" tip="Maximum child subagents a single agent can spawn." type="number" value={subagents.maxChildrenPerAgent} onChange={(v) => updateNested("subagents", { maxChildrenPerAgent: Number(v) })} placeholder="5" />
            <Field label="Archive After (min)" tip="Auto-archive idle subagent sessions after this many minutes." type="number" value={subagents.archiveAfterMinutes} onChange={(v) => updateNested("subagents", { archiveAfterMinutes: Number(v) })} placeholder="60" />
          </div>
          <Field label="Model Override" tip="Use a different model for subagents. Leave empty to inherit the parent agent's model." value={subagents.model} onChange={(v) => updateNested("subagents", { model: v })} placeholder="Use default" />
        </SubSection>

        <SubSection
          title="Memory"
          desc="Embedding-based memory recall"
          open={openSubs.has("memory")}
          onToggle={() => toggleSub("memory")}
        >
          <div className="flex items-center justify-between">
            <Label>Enabled</Label>
            <Switch checked={memory.enabled !== false} onCheckedChange={(v) => updateNested("memory", { enabled: v })} />
          </div>
          <ProviderModelSelect
            provider={memory.embedding_provider ?? ""}
            onProviderChange={(v) => updateNested("memory", { embedding_provider: v || undefined })}
            model={memory.embedding_model ?? ""}
            onModelChange={(v) => updateNested("memory", { embedding_model: v || undefined })}
            providerLabel="Embedding Provider"
            modelLabel="Embedding Model"
            providerTip="Provider for generating text embeddings. Auto-detects from agent's main provider if empty."
            modelTip="Embedding model name (e.g. text-embedding-3-small). Must be supported by the provider."
            providerPlaceholder="(auto)"
            modelPlaceholder="text-embedding-3-small"
          />
          <div className="grid grid-cols-2 gap-4">
            <Field label="Max Results" tip="Maximum memory entries returned per search query." type="number" value={memory.max_results} onChange={(v) => updateNested("memory", { max_results: Number(v) })} placeholder="6" />
            <Field label="Min Score" tip="Minimum similarity score (0-1) for memory search results. Lower = more results but less relevant." type="number" step="0.01" value={memory.min_score} onChange={(v) => updateNested("memory", { min_score: Number(v) })} placeholder="0.35" />
          </div>
        </SubSection>

        <SubSection
          title="Compaction"
          desc="Context compaction & memory flush"
          open={openSubs.has("compaction")}
          onToggle={() => toggleSub("compaction")}
        >
          <div className="grid grid-cols-2 gap-4">
            <Field label="Reserve Tokens Floor" tip="Minimum tokens to reserve for new content after compaction. Prevents over-compaction." type="number" value={compaction.reserveTokensFloor} onChange={(v) => updateNested("compaction", { reserveTokensFloor: Number(v) })} placeholder="20000" />
            <Field label="Max History Share" tip="Maximum fraction (0-1) of context window that conversation history can occupy before compaction triggers." type="number" step="0.05" value={compaction.maxHistoryShare} onChange={(v) => updateNested("compaction", { maxHistoryShare: Number(v) })} placeholder="0.75" />
          </div>
        </SubSection>

        <SubSection
          title="Context Pruning"
          desc="Automatic pruning of stale context"
          open={openSubs.has("pruning")}
          onToggle={() => toggleSub("pruning")}
        >
          <div className="grid grid-cols-2 gap-4">
            <div className="grid gap-1.5">
              <Label>Mode</Label>
              <Select value={pruning.mode ?? "off"} onValueChange={(v) => updateNested("contextPruning", { mode: v })}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="off">Off</SelectItem>
                  <SelectItem value="cache-ttl">Cache TTL</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <Field label="Keep Last Assistants" tip="Number of recent assistant messages to always keep when pruning context." type="number" value={pruning.keepLastAssistants} onChange={(v) => updateNested("contextPruning", { keepLastAssistants: Number(v) })} placeholder="3" />
          </div>
        </SubSection>

        <SubSection
          title="Sandbox"
          desc="Docker-based code sandbox"
          open={openSubs.has("sandbox")}
          onToggle={() => toggleSub("sandbox")}
        >
          <div className="grid grid-cols-2 gap-4">
            <div className="grid gap-1.5">
              <Label>Mode</Label>
              <Select value={sandbox.mode ?? "off"} onValueChange={(v) => updateNested("sandbox", { mode: v })}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="off">Off</SelectItem>
                  <SelectItem value="non-main">Non-Main Only</SelectItem>
                  <SelectItem value="all">All</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <Field label="Image" tip="Docker image name for the sandbox container." value={sandbox.image} onChange={(v) => updateNested("sandbox", { image: v })} placeholder="goclaw-sandbox:bookworm-slim" />
            <Field label="Memory (MB)" tip="Memory limit in MB for the sandbox container." type="number" value={sandbox.memory_mb} onChange={(v) => updateNested("sandbox", { memory_mb: Number(v) })} placeholder="512" />
            <Field label="CPUs" tip="CPU limit for the sandbox container (fractional values allowed)." type="number" step="0.5" value={sandbox.cpus} onChange={(v) => updateNested("sandbox", { cpus: Number(v) })} placeholder="1.0" />
            <Field label="Timeout (sec)" tip="Maximum execution time in seconds before the sandbox is killed." type="number" value={sandbox.timeout_sec} onChange={(v) => updateNested("sandbox", { timeout_sec: Number(v) })} placeholder="300" />
            <div className="flex items-center justify-between">
              <Label>Network Enabled</Label>
              <Switch checked={sandbox.network_enabled ?? false} onCheckedChange={(v) => updateNested("sandbox", { network_enabled: v })} />
            </div>
          </div>
        </SubSection>

        {dirty && (
          <div className="flex justify-end pt-2">
            <Button size="sm" onClick={() => onSave(draft)} disabled={saving} className="gap-1.5">
              <Save className="h-3.5 w-3.5" /> {saving ? "Saving..." : "Save"}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

/* --- Helper components --- */

function SubSection({
  title,
  desc,
  open,
  onToggle,
  children,
}: {
  title: string;
  desc: string;
  open: boolean;
  onToggle: () => void;
  children: React.ReactNode;
}) {
  return (
    <div className="rounded-md border">
      <button
        type="button"
        className="flex w-full cursor-pointer items-center gap-2 px-3 py-2.5 text-left text-sm hover:bg-muted/50"
        onClick={onToggle}
      >
        {open ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
        <div>
          <span className="font-medium">{title}</span>
          <span className="ml-2 text-xs text-muted-foreground">{desc}</span>
        </div>
      </button>
      {open && <div className="space-y-3 border-t px-4 py-3">{children}</div>}
    </div>
  );
}

function Field({
  label,
  tip,
  value,
  onChange,
  placeholder,
  type = "text",
  step,
}: {
  label: string;
  tip?: string;
  value: any;
  onChange: (v: string) => void;
  placeholder?: string;
  type?: string;
  step?: string;
}) {
  return (
    <div className="grid gap-1.5">
      {tip ? <InfoLabel tip={tip}>{label}</InfoLabel> : <Label>{label}</Label>}
      <Input
        type={type}
        step={step}
        value={value ?? ""}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
      />
    </div>
  );
}
