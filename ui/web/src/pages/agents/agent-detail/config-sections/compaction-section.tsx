import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import type { CompactionConfig } from "@/types/agent";
import { ConfigSection, InfoLabel, numOrUndef } from "./config-section";

interface CompactionSectionProps {
  enabled: boolean;
  value: CompactionConfig;
  onToggle: (v: boolean) => void;
  onChange: (v: CompactionConfig) => void;
}

export function CompactionSection({ enabled, value, onToggle, onChange }: CompactionSectionProps) {
  return (
    <ConfigSection
      title="Compaction"
      description="Context window compaction and memory flush settings"
      enabled={enabled}
      onToggle={onToggle}
    >
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <InfoLabel tip="Minimum tokens reserved for the LLM response. Higher values give more room for output but reduce available context for history.">Reserve Tokens Floor</InfoLabel>
          <Input
            type="number"
            placeholder="20000"
            value={value.reserveTokensFloor ?? ""}
            onChange={(e) => onChange({ ...value, reserveTokensFloor: numOrUndef(e.target.value) })}
          />
        </div>
        <div className="space-y-2">
          <InfoLabel tip="Maximum fraction of the context window used for conversation history before compaction triggers (e.g. 0.75 = 75%).">Max History Share (0-1)</InfoLabel>
          <Input
            type="number"
            step="0.05"
            placeholder="0.75"
            value={value.maxHistoryShare ?? ""}
            onChange={(e) => onChange({ ...value, maxHistoryShare: numOrUndef(e.target.value) })}
          />
        </div>
      </div>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <InfoLabel tip="Minimum number of messages in history before compaction can trigger, regardless of token usage.">Min Messages</InfoLabel>
          <Input
            type="number"
            placeholder="50"
            value={value.minMessages ?? ""}
            onChange={(e) => onChange({ ...value, minMessages: numOrUndef(e.target.value) })}
          />
        </div>
        <div className="space-y-2">
          <InfoLabel tip="Number of recent messages to preserve after compaction. The rest is replaced by a summary.">Keep Last Messages</InfoLabel>
          <Input
            type="number"
            placeholder="4"
            value={value.keepLastMessages ?? ""}
            onChange={(e) => onChange({ ...value, keepLastMessages: numOrUndef(e.target.value) })}
          />
        </div>
      </div>
      <div className="space-y-3">
        <div className="flex items-center gap-2">
          <Switch
            checked={value.memoryFlush?.enabled ?? false}
            onCheckedChange={(v) =>
              onChange({ ...value, memoryFlush: { ...value.memoryFlush, enabled: v } })
            }
          />
          <InfoLabel tip="When enabled, compacted history is also saved to long-term memory for future retrieval.">Memory Flush</InfoLabel>
        </div>
        {value.memoryFlush?.enabled && (
          <div className="space-y-2 pl-6">
            <InfoLabel tip="Token count threshold that triggers memory flush. When summary exceeds this, older memories are flushed to storage.">Soft Threshold (tokens)</InfoLabel>
            <Input
              type="number"
              placeholder="4000"
              value={value.memoryFlush?.softThresholdTokens ?? ""}
              onChange={(e) =>
                onChange({
                  ...value,
                  memoryFlush: {
                    ...value.memoryFlush,
                    enabled: true,
                    softThresholdTokens: numOrUndef(e.target.value),
                  },
                })
              }
            />
          </div>
        )}
      </div>
    </ConfigSection>
  );
}
