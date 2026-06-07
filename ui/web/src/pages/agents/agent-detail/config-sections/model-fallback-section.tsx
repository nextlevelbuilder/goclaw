import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  DndContext,
  PointerSensor,
  KeyboardSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { Plus } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import type { ModelFallbackCandidate, ModelFallbackConfig } from "@/types/agent";
import type { ProviderData } from "@/types/provider";
import { SortableFallbackRow } from "./model-fallback-row";

interface ModelFallbackSectionProps {
  primaryProvider: string;
  primaryModel: string;
  providers: ProviderData[];
  value: ModelFallbackConfig;
  onChange: (value: ModelFallbackConfig) => void;
}

export function ModelFallbackSection({
  primaryProvider,
  primaryModel,
  providers,
  value,
  onChange,
}: ModelFallbackSectionProps) {
  const { t } = useTranslation("agents");
  const candidates = value.candidates ?? [];
  const enabledProviders = useMemo(() => {
    const selectedNames = new Set(candidates.map((candidate) => candidate.provider));
    return providers.filter((provider) => provider.enabled || selectedNames.has(provider.name));
  }, [candidates, providers]);
  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );
  const itemIds = candidates.map((_, index) => `fallback-${index}`);

  const updateCandidate = (index: number, candidate: ModelFallbackCandidate) => {
    const next = [...candidates];
    next[index] = candidate;
    onChange({ ...value, candidates: next });
  };

  const removeCandidate = (index: number) => {
    const next = candidates.filter((_, candidateIndex) => candidateIndex !== index);
    onChange({ ...value, enabled: value.enabled && next.length > 0, candidates: next });
  };

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const oldIndex = itemIds.indexOf(String(active.id));
    const newIndex = itemIds.indexOf(String(over.id));
    if (oldIndex < 0 || newIndex < 0) return;
    onChange({ ...value, candidates: arrayMove(candidates, oldIndex, newIndex) });
  };

  return (
    <section className="space-y-3">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h3 className="text-sm font-medium">{t("configSections.modelFallback.title")}</h3>
          <p className="text-xs text-muted-foreground">
            {t("configSections.modelFallback.description")}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Label htmlFor="agent-model-fallback" className="text-xs text-muted-foreground">
            {t("configSections.modelFallback.enabled")}
          </Label>
          <Switch
            id="agent-model-fallback"
            checked={Boolean(value.enabled)}
            onCheckedChange={(enabled) => onChange({ ...value, enabled })}
          />
        </div>
      </div>

      <div className="space-y-3 rounded-lg border p-3 sm:p-4">
        <div className="grid gap-2 rounded-md border bg-muted/30 p-2 sm:grid-cols-[auto_minmax(0,1fr)_minmax(0,1fr)]">
          <Badge variant="secondary" className="h-6 w-fit">
            {t("configSections.modelFallback.primary")}
          </Badge>
          <div className="min-w-0 truncate text-sm">{primaryProvider}</div>
          <div className="min-w-0 truncate text-sm text-muted-foreground">{primaryModel}</div>
        </div>

        {candidates.length > 0 ? (
          <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
            <SortableContext items={itemIds} strategy={verticalListSortingStrategy}>
              <div className="space-y-2">
                {candidates.map((candidate, index) => {
                  const id = itemIds[index] ?? `fallback-${index}`;
                  return (
                    <SortableFallbackRow
                      key={id}
                      id={id}
                      candidate={candidate}
                      providers={enabledProviders}
                      onChange={(next) => updateCandidate(index, next)}
                      onRemove={() => removeCandidate(index)}
                    />
                  );
                })}
              </div>
            </SortableContext>
          </DndContext>
        ) : (
          <div className="rounded-md border border-dashed px-3 py-4 text-sm text-muted-foreground">
            {t("configSections.modelFallback.empty")}
          </div>
        )}

        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-2">
            <Switch
              checked={value.cooldown_enabled ?? true}
              onCheckedChange={(cooldownEnabled) =>
                onChange({ ...value, cooldown_enabled: cooldownEnabled })
              }
            />
            <Label className="text-xs text-muted-foreground">
              {t("configSections.modelFallback.cooldown")}
            </Label>
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() =>
              onChange({
                ...value,
                enabled: true,
                strategy: "priority_order",
                candidates: [...candidates, { provider: "", model: "" }],
              })
            }
          >
            <Plus className="h-4 w-4" />
            {t("configSections.modelFallback.add")}
          </Button>
        </div>
      </div>
    </section>
  );
}
