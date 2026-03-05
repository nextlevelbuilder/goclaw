import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { ContextPruningConfig } from "@/types/agent";
import { ConfigSection, InfoLabel, numOrUndef } from "./config-section";

interface ContextPruningSectionProps {
  enabled: boolean;
  value: ContextPruningConfig;
  onToggle: (v: boolean) => void;
  onChange: (v: ContextPruningConfig) => void;
}

export function ContextPruningSection({ enabled, value, onToggle, onChange }: ContextPruningSectionProps) {
  return (
    <ConfigSection
      title="Context Pruning"
      description="Trim old tool results to save context window"
      enabled={enabled}
      onToggle={onToggle}
    >
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <InfoLabel tip="Pruning strategy. 'cache-ttl' trims old tool results based on their position in the conversation.">Mode</InfoLabel>
          <Select
            value={value.mode ?? ""}
            onValueChange={(v) =>
              onChange({ ...value, mode: v as ContextPruningConfig["mode"] })
            }
          >
            <SelectTrigger><SelectValue placeholder="off" /></SelectTrigger>
            <SelectContent>
              <SelectItem value="off">off</SelectItem>
              <SelectItem value="cache-ttl">cache-ttl</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <InfoLabel tip="Number of recent assistant turns whose tool results are always kept intact, never pruned.">Keep Last Assistants</InfoLabel>
          <Input
            type="number"
            placeholder="3"
            value={value.keepLastAssistants ?? ""}
            onChange={(e) =>
              onChange({ ...value, keepLastAssistants: numOrUndef(e.target.value) })
            }
          />
        </div>
      </div>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <InfoLabel tip="Context usage ratio (0-1) at which soft trimming begins. E.g. 0.3 means trimming starts when context is 30% full.">Soft Trim Ratio (0-1)</InfoLabel>
          <Input
            type="number"
            step="0.05"
            placeholder="0.3"
            value={value.softTrimRatio ?? ""}
            onChange={(e) => onChange({ ...value, softTrimRatio: numOrUndef(e.target.value) })}
          />
        </div>
        <div className="space-y-2">
          <InfoLabel tip="Context usage ratio (0-1) at which hard clearing kicks in. E.g. 0.5 means full clearing at 50% context usage.">Hard Clear Ratio (0-1)</InfoLabel>
          <Input
            type="number"
            step="0.05"
            placeholder="0.5"
            value={value.hardClearRatio ?? ""}
            onChange={(e) => onChange({ ...value, hardClearRatio: numOrUndef(e.target.value) })}
          />
        </div>
      </div>
      <div className="space-y-2">
        <InfoLabel tip="Only tool results with at least this many characters are eligible for pruning. Shorter results are left untouched.">Min Prunable Tool Chars</InfoLabel>
        <Input
          type="number"
          placeholder="50000"
          value={value.minPrunableToolChars ?? ""}
          onChange={(e) =>
            onChange({ ...value, minPrunableToolChars: numOrUndef(e.target.value) })
          }
        />
      </div>

      {/* Soft Trim */}
      <div className="space-y-3 rounded-md border border-dashed p-3">
        <h4 className="text-xs font-medium text-muted-foreground">Soft Trim</h4>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <div className="space-y-2">
            <InfoLabel tip="Tool results longer than this will be soft-trimmed, keeping only head and tail portions.">Max Chars</InfoLabel>
            <Input
              type="number"
              placeholder="4000"
              value={value.softTrim?.maxChars ?? ""}
              onChange={(e) =>
                onChange({ ...value, softTrim: { ...value.softTrim, maxChars: numOrUndef(e.target.value) } })
              }
            />
          </div>
          <div className="space-y-2">
            <InfoLabel tip="Number of characters to keep from the beginning of a trimmed tool result.">Head Chars</InfoLabel>
            <Input
              type="number"
              placeholder="1500"
              value={value.softTrim?.headChars ?? ""}
              onChange={(e) =>
                onChange({ ...value, softTrim: { ...value.softTrim, headChars: numOrUndef(e.target.value) } })
              }
            />
          </div>
          <div className="space-y-2">
            <InfoLabel tip="Number of characters to keep from the end of a trimmed tool result.">Tail Chars</InfoLabel>
            <Input
              type="number"
              placeholder="1500"
              value={value.softTrim?.tailChars ?? ""}
              onChange={(e) =>
                onChange({ ...value, softTrim: { ...value.softTrim, tailChars: numOrUndef(e.target.value) } })
              }
            />
          </div>
        </div>
      </div>

      {/* Hard Clear */}
      <div className="space-y-3 rounded-md border border-dashed p-3">
        <h4 className="text-xs font-medium text-muted-foreground">Hard Clear</h4>
        <div className="flex items-center gap-2">
          <Switch
            checked={value.hardClear?.enabled ?? true}
            onCheckedChange={(v) =>
              onChange({ ...value, hardClear: { ...value.hardClear, enabled: v } })
            }
          />
          <InfoLabel tip="When enabled, old tool results beyond the hard clear ratio are replaced entirely with placeholder text.">Enabled</InfoLabel>
        </div>
        <div className="space-y-2">
          <InfoLabel tip="Text that replaces cleared tool results. Helps the agent understand content was removed.">Placeholder Text</InfoLabel>
          <Input
            placeholder="[Old tool result content cleared]"
            value={value.hardClear?.placeholder ?? ""}
            onChange={(e) =>
              onChange({ ...value, hardClear: { ...value.hardClear, placeholder: e.target.value || undefined } })
            }
          />
        </div>
      </div>
    </ConfigSection>
  );
}
