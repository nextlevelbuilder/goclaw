import { useTranslation } from "react-i18next";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { GripVertical, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
import { useProviderModels } from "@/pages/providers/hooks/use-provider-models";
import type { ModelFallbackCandidate } from "@/types/agent";
import type { ProviderData } from "@/types/provider";

interface SortableFallbackRowProps {
  id: string;
  candidate: ModelFallbackCandidate;
  providers: ProviderData[];
  onChange: (candidate: ModelFallbackCandidate) => void;
  onRemove: () => void;
}

function providerLabel(provider: ProviderData): string {
  return provider.display_name || provider.name;
}

export function SortableFallbackRow({
  id,
  candidate,
  providers,
  onChange,
  onRemove,
}: SortableFallbackRowProps) {
  const { t } = useTranslation("agents");
  const selectedProvider = providers.find((provider) => provider.name === candidate.provider);
  const { models } = useProviderModels(selectedProvider?.id);
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id });

  const modelOptions = models.map((model) => ({
    value: model.id,
    label: model.name || model.id,
  }));

  return (
    <div
      ref={setNodeRef}
      style={{
        transform: CSS.Transform.toString(transform),
        transition,
      }}
      className={`grid gap-2 rounded-md border bg-background p-2 sm:grid-cols-[auto_minmax(0,1fr)_minmax(0,1fr)_auto] ${
        isDragging ? "shadow-md" : ""
      }`}
    >
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="h-9 w-9 cursor-grab text-muted-foreground active:cursor-grabbing"
        aria-label={t("configSections.modelFallback.reorder")}
        {...attributes}
        {...listeners}
      >
        <GripVertical className="h-4 w-4" />
      </Button>
      <Combobox
        value={candidate.provider ?? ""}
        onChange={(provider) => onChange({ provider, model: "" })}
        options={providers.map((provider) => ({
          value: provider.name,
          label: providerLabel(provider),
        }))}
        placeholder={t("configSections.modelFallback.providerPlaceholder")}
      />
      <Combobox
        value={candidate.model ?? ""}
        onChange={(model) => onChange({ ...candidate, model })}
        options={modelOptions}
        placeholder={t("configSections.modelFallback.modelPlaceholder")}
        allowCustom
      />
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="h-9 w-9 text-muted-foreground hover:text-destructive"
        aria-label={t("configSections.modelFallback.remove")}
        onClick={onRemove}
      >
        <Trash2 className="h-4 w-4" />
      </Button>
    </div>
  );
}
