import { useState, useEffect, useRef } from "react";
import { Plus, Trash2 } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Button } from "@/components/ui/button";

interface KeyValuePair {
  key: string;
  value: string;
}

interface KeyValueEditorProps {
  value: Record<string, string>;
  onChange: (value: Record<string, string>) => void;
  keyPlaceholder?: string;
  valuePlaceholder?: string;
  addLabel?: string;
  /** Return true for keys whose values should be masked (type="password"). */
  maskValue?: (key: string) => boolean;
  /** Render value field as a single-line input (default) or multi-line textarea. */
  valueAs?: "input" | "textarea";
}

function toEntries(obj: Record<string, string>): KeyValuePair[] {
  const entries = Object.entries(obj).map(([key, value]) => ({ key, value }));
  return entries.length > 0 ? entries : [{ key: "", value: "" }];
}

function toObject(entries: KeyValuePair[]): Record<string, string> {
  const result: Record<string, string> = {};
  for (const { key, value } of entries) {
    if (key.trim()) {
      result[key.trim()] = value;
    }
  }
  return result;
}

export function KeyValueEditor({
  value,
  onChange,
  keyPlaceholder = "Key",
  valuePlaceholder = "Value",
  addLabel = "Add",
  maskValue,
  valueAs = "input",
}: KeyValueEditorProps) {
  const [entries, setEntries] = useState<KeyValuePair[]>(() => toEntries(value));
  const internalChange = useRef(false);

  // Sync from external value changes only (not our own onChange calls)
  useEffect(() => {
    if (internalChange.current) {
      internalChange.current = false;
      return;
    }
    setEntries(toEntries(value));
  }, [value]);

  const emitChange = (next: KeyValuePair[]) => {
    internalChange.current = true;
    setEntries(next);
    onChange(toObject(next));
  };

  const updateEntry = (idx: number, patch: Partial<KeyValuePair>) => {
    const next = entries.map((e, i) => (i === idx ? { ...e, ...patch } : e));
    emitChange(next);
  };

  const addEntry = () => {
    // Don't emit onChange — just add an empty row locally
    setEntries((prev) => [...prev, { key: "", value: "" }]);
  };

  const removeEntry = (idx: number) => {
    const next = entries.filter((_, i) => i !== idx);
    const result = next.length > 0 ? next : [{ key: "", value: "" }];
    emitChange(result);
  };

  return (
    <div className="space-y-2">
      {entries.map((entry, idx) => (
        <div key={idx} className={valueAs === "textarea" ? "flex items-start gap-2" : "flex items-center gap-2"}>
          <Input
            value={entry.key}
            onChange={(e) => updateEntry(idx, { key: e.target.value })}
            placeholder={keyPlaceholder}
            className="flex-1 font-mono text-sm"
          />
          {valueAs === "textarea" ? (
            <Textarea
              value={entry.value}
              onChange={(e) => updateEntry(idx, { value: e.target.value })}
              placeholder={valuePlaceholder}
              rows={2}
              className="flex-[2] text-base md:text-sm min-h-[38px] resize-y"
            />
          ) : (
            <Input
              type={maskValue?.(entry.key) ? "password" : "text"}
              value={entry.value}
              onChange={(e) => updateEntry(idx, { value: e.target.value })}
              placeholder={valuePlaceholder}
              className="flex-1 font-mono text-sm"
            />
          )}
          <Button
            variant="ghost"
            size="icon"
            className="h-9 w-9 shrink-0"
            onClick={() => removeEntry(idx)}
          >
            <Trash2 className="h-3.5 w-3.5 text-muted-foreground" />
          </Button>
        </div>
      ))}
      <Button variant="outline" size="sm" onClick={addEntry} className="gap-1.5">
        <Plus className="h-3.5 w-3.5" /> {addLabel}
      </Button>
    </div>
  );
}
