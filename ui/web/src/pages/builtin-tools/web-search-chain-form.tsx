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
import { GripVertical, Lock, Loader2 } from "lucide-react";
import { uniqueId } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";

type ProviderKey = "exa" | "tavily" | "brave" | "duckduckgo";

interface ProviderEntry {
  id: string;
  name: ProviderKey;
  enabled: boolean;
  max_results?: number;
  /** Staged API key value — extracted and saved to config_secrets on PUT, never stored in settings */
  apiKey?: string;
}

interface Props {
  initialSettings: Record<string, unknown>;
  /** Boolean status map: "tools.web.<provider>.api_key" → true if a key is stored */
  secretsSet?: Record<string, boolean>;
  onSave: (settings: Record<string, unknown>) => Promise<void>;
  onCancel: () => void;
}

const SORTABLE_PROVIDERS: ProviderKey[] = ["exa", "tavily", "brave"];
const LOCKED_PROVIDER: ProviderKey = "duckduckgo";
const DEFAULT_ORDER: ProviderKey[] = ["exa", "tavily", "brave"];

const RAIL_COLOR: Record<ProviderKey, string> = {
  exa: "bg-blue-600",
  tavily: "bg-cyan-500",
  brave: "bg-orange-500",
  duckduckgo: "bg-slate-500",
};

function parseInitialEntries(settings: Record<string, unknown>): ProviderEntry[] {
  const rawOrder = Array.isArray(settings.provider_order)
    ? (settings.provider_order as string[]).filter((p): p is ProviderKey =>
        SORTABLE_PROVIDERS.includes(p as ProviderKey),
      )
    : DEFAULT_ORDER;

  return rawOrder.map((name) => {
    const cfg = (settings[name] ?? {}) as Record<string, unknown>;
    return {
      id: uniqueId(),
      name,
      enabled: Boolean(cfg.enabled ?? true),
      max_results: cfg.max_results != null ? Number(cfg.max_results) : undefined,
    };
  });
}

interface SortableCardProps {
  entry: ProviderEntry;
  index: number;
  secretsSet?: Record<string, boolean>;
  onUpdate: (id: string, patch: Partial<ProviderEntry>) => void;
}

function SortableProviderCard({ entry, index, secretsSet, onUpdate }: SortableCardProps) {
  const { t } = useTranslation("tools");
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: entry.id,
  });
  const [showKeyInput, setShowKeyInput] = useState(false);

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  };

  const secretKey = `tools.web.${entry.name}.api_key`;
  const keyIsSet = secretsSet?.[secretKey] === true;
  const showInput = showKeyInput || !keyIsSet;

  const displayName = t(`builtin.searchChain.providers.${entry.name}`, { defaultValue: entry.name });

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={`border rounded-lg bg-card flex overflow-hidden ${!entry.enabled ? "opacity-60" : ""}`}
    >
      <div className={`w-1 shrink-0 ${RAIL_COLOR[entry.name]}`} />
      <div className="flex-1 px-3 py-3">
        <div className="flex items-center gap-2">
          <button
            type="button"
            className="cursor-grab text-muted-foreground hover:text-foreground shrink-0"
            {...attributes}
            {...listeners}
          >
            <GripVertical className="size-4" />
          </button>
          <span className="text-xs text-muted-foreground font-mono shrink-0">#{index + 1}</span>
          <Switch
            size="sm"
            checked={entry.enabled}
            onCheckedChange={(v) => onUpdate(entry.id, { enabled: v })}
          />
          <span className="text-sm font-medium flex-1">{displayName}</span>
        </div>

        {/* Max results row */}
        <div className="flex items-center gap-1.5 mt-2 pl-10">
          <Label className="text-xs text-muted-foreground whitespace-nowrap">
            {t("builtin.searchChain.maxResults")}
          </Label>
          <Input
            type="number"
            min={1}
            max={10}
            value={entry.max_results ?? 5}
            onChange={(e) => onUpdate(entry.id, { max_results: Number(e.target.value) })}
            className="h-7 w-16 text-base md:text-sm"
          />
        </div>

        {/* API key row */}
        <div className="flex items-center gap-1.5 mt-2 pl-10">
          <Label className="text-xs text-muted-foreground whitespace-nowrap">
            {t("builtin.searchChain.apiKey")}
          </Label>
          {keyIsSet && !showInput ? (
            <div className="flex items-center gap-2">
              <span className="text-xs text-green-600 dark:text-green-400 font-medium">
                {t("builtin.searchChain.apiKeySet")}
              </span>
              <Button
                variant="ghost"
                size="sm"
                className="h-6 px-2 text-xs"
                onClick={() => setShowKeyInput(true)}
              >
                {t("builtin.searchChain.apiKeyChange")}
              </Button>
            </div>
          ) : (
            <Input
              type="password"
              autoComplete="off"
              placeholder={
                keyIsSet
                  ? t("builtin.searchChain.apiKeyReplacePlaceholder")
                  : t("builtin.searchChain.apiKeyPlaceholder")
              }
              value={entry.apiKey ?? ""}
              onChange={(e) => onUpdate(entry.id, { apiKey: e.target.value })}
              className="h-7 flex-1 text-base md:text-sm font-mono"
            />
          )}
        </div>
      </div>
    </div>
  );
}

function LockedDuckDuckGoCard({ settings }: { settings: Record<string, unknown> }) {
  const { t } = useTranslation("tools");
  const cfg = (settings[LOCKED_PROVIDER] ?? {}) as Record<string, unknown>;
  const enabled = Boolean(cfg.enabled ?? true);

  return (
    <div className={`border rounded-lg bg-card flex overflow-hidden ${!enabled ? "opacity-60" : ""}`}>
      <div className={`w-1 shrink-0 ${RAIL_COLOR.duckduckgo}`} />
      <div className="flex-1 px-3 py-3">
        <div className="flex items-center gap-2">
          <Lock className="size-4 text-muted-foreground shrink-0" />
          <span className="text-xs text-muted-foreground font-mono shrink-0">#4</span>
          <Switch size="sm" checked disabled />
          <span className="text-sm font-medium flex-1">
            {t("builtin.searchChain.providers.duckduckgo")}
          </span>
          <span className="text-xs text-muted-foreground">{t("builtin.searchChain.locked")}</span>
        </div>
      </div>
    </div>
  );
}

export function WebSearchChainForm({ initialSettings, secretsSet, onSave, onCancel }: Props) {
  const { t } = useTranslation("tools");
  const [entries, setEntries] = useState<ProviderEntry[]>(() =>
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

  const handleUpdate = useCallback((id: string, patch: Partial<ProviderEntry>) => {
    setEntries((prev) => prev.map((e) => (e.id === id ? { ...e, ...patch } : e)));
  }, []);

  const handleSave = async () => {
    setSaving(true);
    try {
      const providerOrder = entries.map((e) => e.name);
      const settings: Record<string, unknown> = { provider_order: providerOrder };
      for (const entry of entries) {
        const cfg: Record<string, unknown> = { enabled: entry.enabled };
        if (entry.max_results != null) cfg.max_results = entry.max_results;
        // Include api_key only when user typed a new value — backend extracts and strips it
        if (entry.apiKey && entry.apiKey.trim() !== "") {
          cfg.api_key = entry.apiKey.trim();
        }
        settings[entry.name] = cfg;
      }
      settings[LOCKED_PROVIDER] = { enabled: true };
      await onSave(settings);
    } catch {
      // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <div>
      <DialogHeader>
        <DialogTitle>{t("builtin.searchChain.title")}</DialogTitle>
        <DialogDescription>{t("builtin.searchChain.description")}</DialogDescription>
      </DialogHeader>

      <div className="space-y-2 max-h-[60vh] overflow-y-auto pr-1 my-4">
        <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
          <SortableContext items={entries.map((e) => e.id)} strategy={verticalListSortingStrategy}>
            {entries.map((entry, index) => (
              <SortableProviderCard
                key={entry.id}
                entry={entry}
                index={index}
                secretsSet={secretsSet}
                onUpdate={handleUpdate}
              />
            ))}
          </SortableContext>
        </DndContext>
        <LockedDuckDuckGoCard settings={initialSettings} />
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onCancel}>
          {t("builtin.searchChain.cancel")}
        </Button>
        <Button onClick={handleSave} disabled={saving}>
          {saving && <Loader2 className="h-4 w-4 animate-spin" />}
          {saving ? t("builtin.searchChain.saving") : t("builtin.searchChain.save")}
        </Button>
      </DialogFooter>
    </div>
  );
}
