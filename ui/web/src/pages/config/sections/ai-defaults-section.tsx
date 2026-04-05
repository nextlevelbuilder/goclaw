import { useState, useEffect } from "react";
import { Save, ChevronDown, ChevronRight, Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";
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

export function AiDefaultsSection({ data, onSave, saving }: Props) {
  const { t } = useTranslation("config");
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
        <CardTitle className="text-base">{t("agents.title")}</CardTitle>
        <CardDescription>{t("agents.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <ProviderModelSelect
          provider={defaults.provider ?? ""}
          onProviderChange={(v) => updateDefaults({ provider: v })}
          model={defaults.model ?? ""}
          onModelChange={(v) => updateDefaults({ model: v })}
          providerTip={t("agents.providerTip")}
          modelTip={t("agents.modelTip")}
          showVerify
        />

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <div className="grid gap-1.5">
            <InfoLabel tip={t("agents.maxTokensTip")}>{t("agents.maxTokens")}</InfoLabel>
            <Input
              type="number"
              value={defaults.max_tokens ?? ""}
              onChange={(e) => updateDefaults({ max_tokens: Number(e.target.value) })}
              placeholder="8192"
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("agents.temperatureTip")}>{t("agents.temperature")}</InfoLabel>
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
            <InfoLabel tip={t("agents.maxToolIterationsTip")}>{t("agents.maxToolIterations")}</InfoLabel>
            <Input
              type="number"
              value={defaults.max_tool_iterations ?? ""}
              onChange={(e) => updateDefaults({ max_tool_iterations: Number(e.target.value) })}
              placeholder="20"
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("agents.contextWindowTip")}>{t("agents.contextWindow")}</InfoLabel>
            <Input
              type="number"
              value={defaults.context_window ?? ""}
              onChange={(e) => updateDefaults({ context_window: Number(e.target.value) })}
              placeholder="200000"
            />
          </div>
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="grid gap-1.5">
            <InfoLabel tip={t("agents.workspaceTip")}>{t("agents.workspace")}</InfoLabel>
            <Input
              value={defaults.workspace ?? ""}
              onChange={(e) => updateDefaults({ workspace: e.target.value })}
              placeholder="~/.goclaw/workspace"
            />
          </div>
        </div>

        {/* Collapsible sub-sections */}
        <SubSection
          title={t("agents.subagents.title")}
          desc={t("agents.subagents.desc")}
          open={openSubs.has("subagents")}
          onToggle={() => toggleSub("subagents")}
        >
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field label={t("agents.subagents.maxConcurrent")} tip={t("agents.subagents.maxConcurrentTip")} type="number" value={subagents.maxConcurrent} onChange={(v) => updateNested("subagents", { maxConcurrent: Number(v) })} placeholder="20" />
            <Field label={t("agents.subagents.maxSpawnDepth")} tip={t("agents.subagents.maxSpawnDepthTip")} type="number" value={subagents.maxSpawnDepth} onChange={(v) => updateNested("subagents", { maxSpawnDepth: Number(v) })} placeholder="1" />
            <Field label={t("agents.subagents.maxChildrenPerAgent")} tip={t("agents.subagents.maxChildrenPerAgentTip")} type="number" value={subagents.maxChildrenPerAgent} onChange={(v) => updateNested("subagents", { maxChildrenPerAgent: Number(v) })} placeholder="5" />
            <Field label={t("agents.subagents.archiveAfterMin")} tip={t("agents.subagents.archiveAfterMinTip")} type="number" value={subagents.archiveAfterMinutes} onChange={(v) => updateNested("subagents", { archiveAfterMinutes: Number(v) })} placeholder="60" />
          </div>
          <Field label={t("agents.subagents.modelOverride")} tip={t("agents.subagents.modelOverrideTip")} value={subagents.model} onChange={(v) => updateNested("subagents", { model: v })} placeholder={t("agents.subagents.modelOverridePlaceholder")} />
        </SubSection>

        <SubSection
          title={t("agents.memory.title")}
          desc={t("agents.memory.desc")}
          open={openSubs.has("memory")}
          onToggle={() => toggleSub("memory")}
        >
          <div className="flex items-center justify-between">
            <Label>{t("agents.memory.enabled")}</Label>
            <Switch checked={memory.enabled !== false} onCheckedChange={(v) => updateNested("memory", { enabled: v })} />
          </div>
          <ProviderModelSelect
            provider={memory.embedding_provider ?? ""}
            onProviderChange={(v) => updateNested("memory", { embedding_provider: v || undefined })}
            model={memory.embedding_model ?? ""}
            onModelChange={(v) => updateNested("memory", { embedding_model: v || undefined })}
            providerLabel={t("agents.memory.embeddingProvider")}
            modelLabel={t("agents.memory.embeddingModel")}
            providerTip={t("agents.memory.embeddingProviderTip")}
            modelTip={t("agents.memory.embeddingModelTip")}
            providerPlaceholder="(auto)"
            modelPlaceholder="text-embedding-3-small"
            allowEmpty
          />
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field label={t("agents.memory.maxResults")} tip={t("agents.memory.maxResultsTip")} type="number" value={memory.max_results} onChange={(v) => updateNested("memory", { max_results: Number(v) })} placeholder="6" />
            <Field label={t("agents.memory.minScore")} tip={t("agents.memory.minScoreTip")} type="number" step="0.01" value={memory.min_score} onChange={(v) => updateNested("memory", { min_score: Number(v) })} placeholder="0.35" />
          </div>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field label={t("agents.memory.maxChunkLen")} tip={t("agents.memory.maxChunkLenTip")} type="number" value={memory.max_chunk_len} onChange={(v) => updateNested("memory", { max_chunk_len: Number(v) })} placeholder="1000" />
            <Field label={t("agents.memory.chunkOverlap")} tip={t("agents.memory.chunkOverlapTip")} type="number" value={memory.chunk_overlap} onChange={(v) => updateNested("memory", { chunk_overlap: Number(v) })} placeholder="200" />
          </div>
        </SubSection>

        <SubSection
          title={t("agents.compaction.title")}
          desc={t("agents.compaction.desc")}
          open={openSubs.has("compaction")}
          onToggle={() => toggleSub("compaction")}
        >
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field label={t("agents.compaction.reserveTokensFloor")} tip={t("agents.compaction.reserveTokensFloorTip")} type="number" value={compaction.reserveTokensFloor} onChange={(v) => updateNested("compaction", { reserveTokensFloor: Number(v) })} placeholder="20000" />
            <Field label={t("agents.compaction.maxHistoryShare")} tip={t("agents.compaction.maxHistoryShareTip")} type="number" step="0.05" value={compaction.maxHistoryShare} onChange={(v) => updateNested("compaction", { maxHistoryShare: Number(v) })} placeholder="0.75" />
          </div>
        </SubSection>

        <SubSection
          title={t("agents.pruning.title")}
          desc={t("agents.pruning.desc")}
          open={openSubs.has("pruning")}
          onToggle={() => toggleSub("pruning")}
        >
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="grid gap-1.5">
              <Label>{t("agents.pruning.mode")}</Label>
              <Select value={pruning.mode ?? "auto"} onValueChange={(v) => updateNested("contextPruning", { mode: v })}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="off">Off</SelectItem>
                  <SelectItem value="cache-ttl">Cache TTL</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <Field label={t("agents.pruning.keepLastAssistants")} tip={t("agents.pruning.keepLastAssistantsTip")} type="number" value={pruning.keepLastAssistants} onChange={(v) => updateNested("contextPruning", { keepLastAssistants: Number(v) })} placeholder="3" />
          </div>
        </SubSection>

        <SubSection
          title={t("agents.sandbox.title")}
          desc={t("agents.sandbox.desc")}
          open={openSubs.has("sandbox")}
          onToggle={() => toggleSub("sandbox")}
        >
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="grid gap-1.5">
              <Label>{t("agents.sandbox.mode")}</Label>
              <Select value={sandbox.mode ?? "off"} onValueChange={(v) => updateNested("sandbox", { mode: v })}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="off">Off</SelectItem>
                  <SelectItem value="non-main">Non-Main Only</SelectItem>
                  <SelectItem value="all">All</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <Field label={t("agents.sandbox.image")} tip={t("agents.sandbox.imageTip")} value={sandbox.image} onChange={(v) => updateNested("sandbox", { image: v })} placeholder="goclaw-sandbox:bookworm-slim" />
            <Field label={t("agents.sandbox.memoryMb")} tip={t("agents.sandbox.memoryMbTip")} type="number" value={sandbox.memory_mb} onChange={(v) => updateNested("sandbox", { memory_mb: Number(v) })} placeholder="512" />
            <Field label={t("agents.sandbox.cpus")} tip={t("agents.sandbox.cpusTip")} type="number" step="0.5" value={sandbox.cpus} onChange={(v) => updateNested("sandbox", { cpus: Number(v) })} placeholder="1.0" />
            <Field label={t("agents.sandbox.timeoutSec")} tip={t("agents.sandbox.timeoutSecTip")} type="number" value={sandbox.timeout_sec} onChange={(v) => updateNested("sandbox", { timeout_sec: Number(v) })} placeholder="300" />
            <div className="flex items-center justify-between">
              <Label>{t("agents.sandbox.networkEnabled")}</Label>
              <Switch checked={sandbox.network_enabled ?? false} onCheckedChange={(v) => updateNested("sandbox", { network_enabled: v })} />
            </div>
          </div>
        </SubSection>

        {dirty && (
          <div className="flex justify-end pt-2">
            <Button size="sm" onClick={async () => {
              try { await onSave(draft); } catch { /* toast shown by hook */ }
            }} disabled={saving} className="gap-1.5">
              {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
              {saving ? t("saving") : t("save")}
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
  value: any; // eslint-disable-line @typescript-eslint/no-explicit-any
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
