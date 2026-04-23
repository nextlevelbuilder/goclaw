import { useState, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { GripVertical, Trash2, ChevronDown, ChevronUp } from "lucide-react";
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
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useProviderModels } from "@/pages/providers/hooks/use-provider-models";
import { getChatGPTOAuthPoolOwnership } from "@/pages/providers/provider-utils";
import { MEDIA_PARAMS_SCHEMA } from "./media-provider-params-schema";
import { ParamFieldControl } from "./media-param-field-control";
import { buildDefaultParams } from "./media-provider-chain-helpers";

import type { ProviderEntry } from "./media-provider-chain-helpers";

interface SortableCardProps {
  entry: ProviderEntry;
  index: number;
  toolName: string;
  enabledProviders: ReturnType<typeof useProviders>["providers"];
  onUpdate: (id: string, patch: Partial<ProviderEntry>) => void;
  onRemove: (id: string) => void;
  portalRef: React.RefObject<HTMLDivElement | null>;
}

/** Single draggable/sortable provider card in the media chain form. */
export function SortableProviderCard({
  entry,
  index,
  toolName,
  enabledProviders,
  onUpdate,
  onRemove,
  portalRef,
}: SortableCardProps) {
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

  // Pool-aware dropdown: hide pool members so the user can only pick pool
  // owners (or standalone providers). Mirrors the Create Agent dropdown
  // pattern (`components/shared/provider-model-select.tsx`).
  const poolOwnership = useMemo(
    () => getChatGPTOAuthPoolOwnership(enabledProviders),
    [enabledProviders],
  );
  const dropdownProviders = useMemo(
    () => enabledProviders.filter((p) => !poolOwnership.ownerByMember.has(p.name)),
    [enabledProviders, poolOwnership],
  );

  const selectedProvider = enabledProviders.find((p) => p.id === entry.provider_id);
  const { models, loading: modelsLoading } = useProviderModels(
    entry.provider_id || undefined,
  );

  const paramSchema = MEDIA_PARAMS_SCHEMA[toolName]?.[selectedProvider?.provider_type ?? ""] ?? [];

  const handleProviderChange = (providerName: string) => {
    const pData = enabledProviders.find((p) => p.name === providerName);
    const newParams = pData ? buildDefaultParams(toolName, pData.provider_type) : {};
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
              {dropdownProviders.map((p) => (
                <SelectItem key={p.id} value={p.name}>
                  <span className="flex items-center gap-2">
                    {p.display_name || p.name}
                    {poolOwnership.membersByOwner.has(p.name) && (
                      <span className="rounded border border-primary/30 bg-primary/10 px-1.5 py-px text-2xs font-medium text-primary">
                        {t("providers:list.poolBadge")}
                      </span>
                    )}
                  </span>
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
