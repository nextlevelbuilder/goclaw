import { useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  verticalListSortingStrategy,
  useSortable,
  arrayMove,
  sortableKeyboardCoordinates,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { GripVertical, Loader2 } from "lucide-react";
import { uniqueId } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";

interface ExtractorEntry {
  id: string;
  name: string;
  enabled: boolean;
  timeout: number;
  base_url: string;
}

interface Props {
  initialSettings: Record<string, unknown>;
  onSave: (settings: Record<string, unknown>) => Promise<void>;
  onCancel: () => void;
}

const FIXED_EXTRACTORS = ["defuddle", "html-to-markdown"] as const;

function parseInitialEntries(settings: Record<string, unknown>): ExtractorEntry[] {
  const raw = Array.isArray(settings.extractors)
    ? (settings.extractors as Record<string, unknown>[])
    : [];

  // Build a map from name → raw entry for fast lookup
  const byName = new Map(raw.map((e) => [String(e.name ?? ""), e]));

  return FIXED_EXTRACTORS.map((name) => {
    const e = byName.get(name) ?? {};
    return {
      id: uniqueId(),
      name,
      enabled: Boolean(e.enabled ?? true),
      timeout: Number(e.timeout ?? 0),
      base_url: String(e.base_url ?? ""),
    };
  });
}

interface SortableCardProps {
  entry: ExtractorEntry;
  index: number;
  onUpdate: (id: string, patch: Partial<ExtractorEntry>) => void;
}

function SortableExtractorCard({ entry, index, onUpdate }: SortableCardProps) {
  const { t } = useTranslation("tools");
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: entry.id,
  });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  };

  const displayName = t(`builtin.extractorChain.extractors.${entry.name}`, {
    defaultValue: entry.name,
  });
  const desc = t(`builtin.extractorChain.extractors.${entry.name}Desc`, {
    defaultValue: "",
  });
  const showTimeout = entry.timeout > 0 || entry.name === "defuddle";
  const showBaseUrl = entry.name === "defuddle";

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={`border rounded-lg bg-card ${!entry.enabled ? "opacity-60" : ""}`}
    >
      {/* Row 1: drag handle, index, toggle, name */}
      <div className="flex items-center gap-2 px-3 pt-3 pb-1">
        <button
          type="button"
          className="cursor-grab text-muted-foreground hover:text-foreground shrink-0"
          {...attributes}
          {...listeners}
        >
          <GripVertical className="size-4" />
        </button>

        <span className="text-xs text-muted-foreground font-mono shrink-0">
          #{index + 1}
        </span>

        <Switch
          size="sm"
          checked={entry.enabled}
          onCheckedChange={(v) => onUpdate(entry.id, { enabled: v })}
        />

        <div className="flex-1 min-w-0">
          <span className="text-sm font-medium">{displayName}</span>
          {desc && (
            <p className="text-xs text-muted-foreground mt-0.5 truncate">{desc}</p>
          )}
        </div>
      </div>

      {/* Optional fields */}
      {(showTimeout || showBaseUrl) && (
        <div className="flex flex-wrap items-center gap-3 px-3 pb-3 pt-1">
          {showTimeout && (
            <div className="flex items-center gap-1.5">
              <Label className="text-xs text-muted-foreground whitespace-nowrap">
                {t("builtin.extractorChain.timeout")}
              </Label>
              <div className="relative">
                <Input
                  type="number"
                  min={0}
                  max={600}
                  value={entry.timeout}
                  onChange={(e) => onUpdate(entry.id, { timeout: Number(e.target.value) })}
                  className="h-7 w-20 text-base md:text-sm pr-5"
                />
                <span className="absolute right-2 top-1/2 -translate-y-1/2 text-xs text-muted-foreground pointer-events-none">
                  s
                </span>
              </div>
            </div>
          )}
          {showBaseUrl && (
            <div className="flex items-center gap-1.5 flex-1 min-w-[200px]">
              <Label className="text-xs text-muted-foreground whitespace-nowrap">
                {t("builtin.extractorChain.baseUrl")}
              </Label>
              <Input
                value={entry.base_url}
                onChange={(e) => onUpdate(entry.id, { base_url: e.target.value })}
                placeholder="https://fetch.goclaw.sh/"
                className="h-7 text-base md:text-sm"
              />
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export function WebFetchExtractorChainForm({ initialSettings, onSave, onCancel }: Props) {
  const { t } = useTranslation("tools");
  const [entries, setEntries] = useState<ExtractorEntry[]>(() =>
    parseInitialEntries(initialSettings),
  );
  const [saving, setSaving] = useState(false);

  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const handleDragEnd = useCallback((event: DragEndEvent) => {
    const { active, over } = event;
    if (over && active.id !== over.id) {
      setEntries((prev) => {
        const oldIndex = prev.findIndex((e) => e.id === active.id);
        const newIndex = prev.findIndex((e) => e.id === over.id);
        return arrayMove(prev, oldIndex, newIndex);
      });
    }
  }, []);

  const handleUpdate = useCallback((id: string, patch: Partial<ExtractorEntry>) => {
    setEntries((prev) => prev.map((e) => (e.id === id ? { ...e, ...patch } : e)));
  }, []);

  const handleSave = async () => {
    setSaving(true);
    try {
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      const serialized = entries.map(({ id: _id, ...rest }) => rest);
      await onSave({ extractors: serialized });
    } catch {
      // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <div>
      <DialogHeader>
        <DialogTitle>{t("builtin.extractorChain.title")}</DialogTitle>
        <DialogDescription>{t("builtin.extractorChain.description")}</DialogDescription>
      </DialogHeader>

      <div className="space-y-2 max-h-[60vh] overflow-y-auto pr-1 my-4">
        <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
          <SortableContext items={entries.map((e) => e.id)} strategy={verticalListSortingStrategy}>
            {entries.map((entry, index) => (
              <SortableExtractorCard
                key={entry.id}
                entry={entry}
                index={index}
                onUpdate={handleUpdate}
              />
            ))}
          </SortableContext>
        </DndContext>

      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onCancel}>
          {t("builtin.extractorChain.cancel")}
        </Button>
        <Button onClick={handleSave} disabled={saving}>
          {saving && <Loader2 className="h-4 w-4 animate-spin" />}
          {saving ? t("builtin.extractorChain.saving") : t("builtin.extractorChain.save")}
        </Button>
      </DialogFooter>
    </div>
  );
}
