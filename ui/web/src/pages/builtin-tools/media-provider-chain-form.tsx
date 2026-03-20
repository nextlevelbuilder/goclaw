import { useState, useCallback, useRef } from "react";
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
import { GripVertical, Trash2, ChevronDown, ChevronUp, Plus, Loader2 } from "lucide-react";
import { uniqueId } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Combobox } from "@/components/ui/combobox";
import { DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useProviderModels } from "@/pages/providers/hooks/use-provider-models";
import { MEDIA_PARAMS_SCHEMA, type ParamField } from "./media-provider-params-schema";

interface ProviderEntry {
  id: string;
  provider_id: string;
  provider: string;
  model: string;
  enabled: boolean;
  timeout: number;
  max_retries: number;
  params: Record<string, unknown>;
}

interface MediaProviderChainFormProps {
  toolName: string;
  initialSettings: Record<string, unknown>;
  onSave: (settings: Record<string, unknown>) => Promise<void>;
  onCancel: () => void;
}

function formatToolTitle(name: string): string {
  return name
    .replace(/_/g, " ")
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

function buildDefaultParams(toolName: string, providerType: string): Record<string, unknown> {
  const schema = MEDIA_PARAMS_SCHEMA[toolName]?.[providerType] ?? [];
  const defaults: Record<string, unknown> = {};
  for (const field of schema) {
    if (field.default !== undefined) {
      defaults[field.key] = field.default;
    }
  }
  return defaults;
}

function parseInitialEntries(
  settings: Record<string, unknown>,
  providers: ReturnType<typeof useProviders>["providers"],
): ProviderEntry[] {
  // New format: { providers: [...] }
  if (Array.isArray(settings.providers)) {
    return (settings.providers as Record<string, unknown>[]).map((p) => ({
      id: uniqueId(),
      provider_id: String(p.provider_id ?? ""),
      provider: String(p.provider ?? ""),
      model: String(p.model ?? ""),
      enabled: Boolean(p.enabled ?? true),
      timeout: Number(p.timeout ?? 120),
      max_retries: Number(p.max_retries ?? 2),
      params: (p.params as Record<string, unknown>) ?? {},
    }));
  }

  // Legacy format: { provider, model }
  if (settings.provider || settings.model) {
    const providerName = String(settings.provider ?? "");
    const providerData = providers.find((p) => p.name === providerName);
    return [
      {
        id: uniqueId(),
        provider_id: providerData?.id ?? "",
        provider: providerName,
        model: String(settings.model ?? ""),
        enabled: true,
        timeout: 120,
        max_retries: 2,
        params: {},
      },
    ];
  }

  return [];
}

// Sub-component for a single sortable provider card
interface SortableCardProps {
  entry: ProviderEntry;
  index: number;
  toolName: string;
  enabledProviders: ReturnType<typeof useProviders>["providers"];
  onUpdate: (id: string, patch: Partial<ProviderEntry>) => void;
  onRemove: (id: string) => void;
  portalRef: React.RefObject<HTMLDivElement | null>;
}

function SortableProviderCard({ entry, index, toolName, enabledProviders, onUpdate, onRemove, portalRef }: SortableCardProps) {
  const { t } = useTranslation("tools");
  const [expanded, setExpanded] = useState(false);
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: entry.id,
  });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  };

  const selectedProvider = enabledProviders.find((p) => p.id === entry.provider_id);
  const { models, loading: modelsLoading } = useProviderModels(
    entry.provider_id || undefined,
    selectedProvider?.provider_type,
  );

  const paramSchema: ParamField[] =
    MEDIA_PARAMS_SCHEMA[toolName]?.[selectedProvider?.provider_type ?? ""] ?? [];

  const handleProviderChange = (providerName: string) => {
    const pData = enabledProviders.find((p) => p.name === providerName);
    const newParams = pData
      ? buildDefaultParams(toolName, pData.provider_type)
      : {};
    onUpdate(entry.id, {
      provider_id: pData?.id ?? "",
      provider: providerName,
      model: "",
      params: newParams,
    });
  };

  const handleParamChange = (key: string, value: unknown) => {
    onUpdate(entry.id, { params: { ...entry.params, [key]: value } });
  };

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={`border rounded-lg bg-card ${!entry.enabled ? "opacity-60" : ""}`}
    >
      {/* Row 1: drag handle, index, toggle, delete */}
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

        <span className="text-sm font-medium truncate">
          {selectedProvider?.display_name || entry.provider || t("builtin.mediaChain.newProvider")}
        </span>

        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="ml-auto h-7 w-7 p-0 shrink-0 text-muted-foreground hover:text-destructive"
          onClick={() => onRemove(entry.id)}
        >
          <Trash2 className="size-3.5" />
        </Button>
      </div>

      {/* Row 2: provider + model selects */}
      <div className="grid grid-cols-1 gap-2 px-3 py-1.5 sm:grid-cols-2">
        <div className="space-y-1">
          <Label className="text-xs text-muted-foreground">{t("builtin.mediaChain.provider")}</Label>
          <Select value={entry.provider} onValueChange={handleProviderChange}>
            <SelectTrigger className="h-8 text-sm">
              <SelectValue placeholder={t("builtin.mediaChain.selectProvider")} />
            </SelectTrigger>
            <SelectContent>
              {enabledProviders.map((p) => (
                <SelectItem key={p.id} value={p.name}>
                  {p.display_name || p.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1">
          <Label className="text-xs text-muted-foreground">{t("builtin.mediaChain.model")}</Label>
          <Combobox
            value={entry.model}
            onChange={(v) => onUpdate(entry.id, { model: v })}
            options={models.map((m) => ({ value: m.id, label: m.name ?? m.id }))}
            placeholder={modelsLoading ? t("builtin.mediaChain.loadingModels") : t("builtin.mediaChain.selectModel")}
            className="h-8 text-sm"
            portalContainer={portalRef}
          />
        </div>
      </div>

      {/* Row 3: timeout, retries, expand button */}
      <div className="flex items-center gap-3 px-3 pb-3 pt-1">
        <div className="flex items-center gap-1.5">
          <Label className="text-xs text-muted-foreground whitespace-nowrap">{t("builtin.mediaChain.timeout")}</Label>
          <div className="relative">
            <Input
              type="number"
              min={1}
              max={600}
              value={entry.timeout}
              onChange={(e) => onUpdate(entry.id, { timeout: Number(e.target.value) })}
              className="h-7 w-20 text-sm pr-5"
            />
            <span className="absolute right-2 top-1/2 -translate-y-1/2 text-xs text-muted-foreground pointer-events-none">s</span>
          </div>
        </div>
        <div className="flex items-center gap-1.5">
          <Label className="text-xs text-muted-foreground whitespace-nowrap">{t("builtin.mediaChain.retries")}</Label>
          <Input
            type="number"
            min={0}
            max={10}
            value={entry.max_retries}
            onChange={(e) => onUpdate(entry.id, { max_retries: Number(e.target.value) })}
            className="h-7 w-16 text-sm"
          />
        </div>

        {paramSchema.length > 0 && (
          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            className="ml-auto flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
          >
            {t("builtin.mediaChain.settings")}
            {expanded ? <ChevronUp className="size-3" /> : <ChevronDown className="size-3" />}
          </button>
        )}
      </div>

      {/* Collapsible params */}
      {expanded && paramSchema.length > 0 && (
        <div className="border-t px-3 py-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
          {paramSchema.map((field) => (
            <ParamFieldControl
              key={field.key}
              field={field}
              value={entry.params[field.key] ?? field.default}
              onChange={(v) => handleParamChange(field.key, v)}
            />
          ))}
        </div>
      )}
    </div>
  );
}

// Renders a single param field based on its type
function ParamFieldControl({
  field,
  value,
  onChange,
}: {
  field: ParamField;
  value: unknown;
  onChange: (v: unknown) => void;
}) {
  return (
    <div className="space-y-1">
      <Label className="text-xs">{field.label}</Label>
      {field.type === "select" && field.options && (
        <Select value={String(value ?? "")} onValueChange={onChange}>
          <SelectTrigger className="h-8 text-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {field.options.map((opt) => (
              <SelectItem key={opt.value} value={opt.value}>
                {opt.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      {field.type === "toggle" && (
        <div className="flex items-center h-8">
          <Switch
            size="sm"
            checked={Boolean(value)}
            onCheckedChange={onChange}
          />
        </div>
      )}
      {field.type === "number" && (
        <Input
          type="number"
          min={field.min}
          max={field.max}
          step={field.step}
          value={Number(value ?? 0)}
          onChange={(e) => onChange(Number(e.target.value))}
          className="h-8 text-sm"
        />
      )}
      {field.type === "text" && (
        <div className="space-y-1">
          <Input
            value={String(value ?? "")}
            onChange={(e) => onChange(e.target.value)}
            placeholder={field.description}
            className="h-8 text-sm"
          />
          {field.description && (
            <p className="text-xs text-muted-foreground">{field.description}</p>
          )}
        </div>
      )}
    </div>
  );
}

// Main form component
export function MediaProviderChainForm({
  toolName,
  initialSettings,
  onSave,
  onCancel,
}: MediaProviderChainFormProps) {
  const { t } = useTranslation("tools");
  const { providers } = useProviders();
  const enabledProviders = providers.filter((p) => p.enabled);
  const portalRef = useRef<HTMLDivElement | null>(null);

  const [entries, setEntries] = useState<ProviderEntry[]>(() =>
    parseInitialEntries(initialSettings, providers),
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

  const handleRemove = useCallback((id: string) => {
    setEntries((prev) => prev.filter((e) => e.id !== id));
  }, []);

  const handleAdd = () => {
    setEntries((prev) => [
      ...prev,
      {
        id: uniqueId(),
        provider_id: "",
        provider: "",
        model: "",
        enabled: true,
        timeout: 120,
        max_retries: 2,
        params: {},
      },
    ]);
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      const serialized = entries.map(({ id: _id, ...rest }) => rest);
      await onSave({ providers: serialized });
    } catch {
      // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <div ref={portalRef} className="relative">
      <DialogHeader>
        <DialogTitle>{formatToolTitle(toolName)} {t("builtin.mediaChain.providerChainSuffix")}</DialogTitle>
        <DialogDescription>
          {t("builtin.mediaChain.description")}
        </DialogDescription>
      </DialogHeader>

      <div className="space-y-2 max-h-[60vh] overflow-y-auto pr-1 my-4">
        <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
          <SortableContext items={entries.map((e) => e.id)} strategy={verticalListSortingStrategy}>
            {entries.map((entry, index) => (
              <SortableProviderCard
                key={entry.id}
                entry={entry}
                index={index}
                toolName={toolName}
                enabledProviders={enabledProviders}
                onUpdate={handleUpdate}
                onRemove={handleRemove}
                portalRef={portalRef}
              />
            ))}
          </SortableContext>
        </DndContext>

        {entries.length === 0 && (
          <p className="text-sm text-muted-foreground text-center py-6">
            {t("builtin.mediaChain.noProviders")}
          </p>
        )}

        <Button type="button" variant="outline" size="sm" className="w-full" onClick={handleAdd}>
          <Plus className="size-3.5 mr-1.5" />
          {t("builtin.mediaChain.addProvider")}
        </Button>
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onCancel}>
          {t("builtin.mediaChain.cancel")}
        </Button>
        <Button onClick={handleSave} disabled={saving}>
          {saving && <Loader2 className="h-4 w-4 animate-spin" />}
          {saving ? t("builtin.mediaChain.saving") : t("builtin.mediaChain.save")}
        </Button>
      </DialogFooter>
    </div>
  );
}
