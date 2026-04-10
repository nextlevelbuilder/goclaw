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
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { GripVertical, Lock } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { isSecret } from "@/lib/secret";
import { toast } from "@/stores/use-toast-store";

export type ProviderId = "exa" | "tavily" | "brave" | "duckduckgo";

type ProviderConfig = {
  enabled?: boolean;
  api_key?: string;
  max_results?: number;
};

interface Props {
  providerOrder: ProviderId[];
  onProviderOrderChange: (order: ProviderId[]) => void;
  exa: ProviderConfig;
  tavily: ProviderConfig;
  brave: ProviderConfig;
  duckduckgo: ProviderConfig;
  onProviderPatch: (
    provider: Exclude<ProviderId, "duckduckgo">,
    patch: Partial<ProviderConfig>,
  ) => void;
  onDuckDuckGoPatch: (patch: Partial<ProviderConfig>) => void;
}

type SortableProviderId = Exclude<ProviderId, "duckduckgo">;

const SORTABLE_IDS: SortableProviderId[] = ["exa", "tavily", "brave"];

const META: Record<
  ProviderId,
  {
    label: string;
    kind: "API" | "Built-in";
    railClass: string;
    textClass: string;
    borderClass: string;
    gradient: string;
    glow: string;
    keyPlaceholder?: string;
  }
> = {
  exa: {
    label: "Exa",
    kind: "API",
    railClass: "bg-blue-600",
    textClass: "text-blue-700 dark:text-blue-300",
    borderClass: "border-blue-600/50",
    gradient: "from-blue-700 via-blue-500 to-blue-700",
    glow: "shadow-blue-600/50",
    keyPlaceholder: "tools.exaApiKeyPlaceholder",
  },
  tavily: {
    label: "Tavily",
    kind: "API",
    railClass: "bg-cyan-500",
    textClass: "text-cyan-700 dark:text-cyan-300",
    borderClass: "border-cyan-500/50",
    gradient: "from-cyan-600 via-cyan-400 to-cyan-600",
    glow: "shadow-cyan-500/50",
    keyPlaceholder: "tools.tavilyApiKeyPlaceholder",
  },
  brave: {
    label: "Brave Search",
    kind: "API",
    railClass: "bg-orange-500",
    textClass: "text-orange-700 dark:text-orange-300",
    borderClass: "border-orange-500/50",
    gradient: "from-orange-600 via-orange-400 to-orange-600",
    glow: "shadow-orange-500/50",
    keyPlaceholder: "tools.braveApiKeyPlaceholder",
  },
  duckduckgo: {
    label: "DuckDuckGo",
    kind: "Built-in",
    railClass: "bg-slate-500",
    textClass: "text-slate-700 dark:text-slate-300",
    borderClass: "border-slate-500/50",
    gradient: "from-slate-600 via-slate-400 to-slate-600",
    glow: "shadow-slate-500/50",
  },
};

function normalizeOrder(order: ProviderId[]): SortableProviderId[] {
  const filtered = order.filter(
    (provider, index): provider is SortableProviderId =>
      provider !== "duckduckgo" &&
      SORTABLE_IDS.includes(provider) &&
      order.indexOf(provider) === index,
  );
  return [...filtered, ...SORTABLE_IDS.filter((provider) => !filtered.includes(provider))];
}

function hasUsableKey(value?: string): boolean {
  return typeof value === "string" && value.trim().length > 0;
}

const SPINE_LEFT = "58px";

interface SortableRowProps {
  provider: SortableProviderId;
  index: number;
  config: ProviderConfig;
  onProviderPatch: (provider: SortableProviderId, patch: Partial<ProviderConfig>) => void;
}

function SortableProviderRow({
  provider,
  index,
  config,
  onProviderPatch,
}: SortableRowProps) {
  const { t } = useTranslation("config");
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({
      id: provider,
    });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  };

  const meta = META[provider];
  const enabled = config.enabled ?? false;
  const ready = hasUsableKey(config.api_key);

  const handleEnabledChange = (value: boolean) => {
    if (value && !ready) {
      toast.error(
        t("tools.providerKeyRequiredTitle"),
        t("tools.providerKeyRequiredMessage", { provider: meta.label }),
      );
      return;
    }
    onProviderPatch(provider, { enabled: value });
  };

  const handleApiKeyChange = (value: string) => {
    onProviderPatch(provider, {
      api_key: value,
      ...(enabled && value.trim().length === 0 ? { enabled: false } : {}),
    });
  };

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={cn("relative z-10", isDragging && "z-50")}
    >
      <div
        className={cn(
          "rounded-lg border border-border/70 bg-background transition-all duration-300",
          isDragging ? "shadow-xl ring-1 ring-border/50 opacity-90" : "shadow-sm",
          enabled && cn("border-l-2", meta.borderClass)
        )}
      >
        {/* Chain Track */}
        <div 
          className="absolute top-0 bottom-0 flex flex-col items-center justify-center transition-all duration-500"
          style={{ left: SPINE_LEFT, width: "12px", transform: "translateX(-50%)" }}
        >
          {/* Top Spine segment */}
          <div className={cn(
            "w-[3px] flex-1 transition-all duration-700",
            enabled 
              ? cn("bg-gradient-to-b", meta.gradient) 
              : cn("bg-border/10", meta.borderClass.replace("/50", "/20"))
          )} />
          
          {/* Coupler Node - Pristine Circle */}
          <div className={cn(
            "relative h-3 w-3 shrink-0 rounded-full transition-all duration-500 z-10",
            enabled 
              ? cn(meta.railClass, meta.glow, "scale-110 shadow-[0_0_12px_1px]") 
              : cn("bg-background border-2", meta.borderClass.replace("/50", "/40"))
          )}>
            {enabled && (
              <div className="absolute inset-0 rounded-full bg-gradient-to-br from-white/30 to-transparent opacity-60" />
            )}
          </div>

          {/* Bottom Spine segment */}
          <div className={cn(
            "w-[3px] flex-1 transition-all duration-700",
            enabled 
              ? cn("bg-gradient-to-b", meta.gradient) 
              : cn("bg-border/10", meta.borderClass.replace("/50", "/20"))
          )} />
        </div>

        <div className="flex gap-4 px-3 py-2.5">
          <button
            type="button"
            aria-label={`Reorder ${meta.label}`}
            className="flex h-8 w-8 shrink-0 cursor-grab items-center justify-center rounded border border-border/50 bg-muted/10 text-muted-foreground transition-colors hover:bg-muted/20 active:cursor-grabbing"
            {...attributes}
            {...listeners}
          >
            <GripVertical className="h-3.5 w-3.5" />
          </button>

          <div className="w-4" /> {/* Space for Coupler */}

          <div className="min-w-0 flex-1 space-y-2">
            <div className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <span className="text-[11px] font-bold tabular-nums text-muted-foreground/60">
                  {String(index + 1).padStart(2, "0")}
                </span>
                <span className="text-sm font-semibold tracking-tight">
                  {meta.label}
                </span>
                <Badge
                  variant="outline"
                  className={cn(
                    "rounded-full text-[10px] h-4 uppercase tracking-[0.08em] px-1.5",
                    meta.textClass,
                  )}
                >
                  {meta.kind}
                </Badge>
                <Badge variant={enabled ? "success" : "secondary"} className="h-4 px-1.5 py-0 text-[10px]">
                  {enabled
                    ? t("tools.providerStateEnabled")
                    : t("tools.providerStateDisabled")}
                </Badge>
                <Badge variant={ready ? "info" : "warning"} className="h-4 px-1.5 py-0 text-[10px]">
                  {ready
                    ? t("tools.providerStateKeyReady")
                    : t("tools.providerStateNeedsKey")}
                </Badge>
              </div>

              <Switch
                checked={enabled}
                onCheckedChange={handleEnabledChange}
              />
            </div>

            <div className="grid grid-cols-1 gap-2 md:grid-cols-[110px_minmax(0,1fr)]">
              <div className="grid gap-1">
                <Label className="text-[10px] uppercase tracking-wider text-muted-foreground/70">
                  {t("tools.maxResults")}
                </Label>
                <Input
                  type="number"
                  min={1}
                  value={config.max_results ?? ""}
                  onChange={(event) =>
                    onProviderPatch(provider, {
                      max_results: Number(event.target.value),
                    })
                  }
                  className="h-7 px-2 text-xs"
                  placeholder="5"
                />
              </div>

              <div className="grid gap-1">
                <Label className="text-[10px] uppercase tracking-wider text-muted-foreground/70">
                  {t("tools.apiKeyLabel")}
                </Label>
                <Input
                  type="password"
                  value={isSecret(config.api_key) ? "" : (config.api_key ?? "")}
                  onChange={(event) => handleApiKeyChange(event.target.value)}
                  className={cn(
                    "h-7 px-2 text-xs transition-colors md:text-xs",
                    !enabled && "bg-muted/10 opacity-60",
                  )}
                  placeholder={meta.keyPlaceholder ? t(meta.keyPlaceholder) : ""}
                  autoComplete="off"
                />
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

export function WebSearchProviderSequence({
  providerOrder,
  onProviderOrderChange,
  exa,
  tavily,
  brave,
  duckduckgo,
  onProviderPatch,
  onDuckDuckGoPatch,
}: Props) {
  const { t } = useTranslation("config");
  const ordered = normalizeOrder(providerOrder);
  const providerConfigs: Record<SortableProviderId, ProviderConfig> = {
    exa,
    tavily,
    brave,
  };

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 6 },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) {
      return;
    }
    const oldIndex = ordered.findIndex((provider) => provider === active.id);
    const newIndex = ordered.findIndex((provider) => provider === over.id);
    if (oldIndex === -1 || newIndex === -1) {
      return;
    }
    onProviderOrderChange(arrayMove(ordered, oldIndex, newIndex));
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
        <div className="text-sm font-semibold tracking-tight">
          {t("tools.providerOrder")}
        </div>
        <div className="inline-flex items-center gap-1.5 text-[10px] font-medium uppercase tracking-[0.16em] text-muted-foreground/60">
          <GripVertical className="h-3.5 w-3.5" />
          {t("tools.providerSequenceHint")}
        </div>
      </div>

      <div className="relative">
        {/* Background Continuous Spine */}
        <div 
          className="absolute bottom-0 top-0 w-[2px] -translate-x-1/2 bg-border/40"
          style={{ left: SPINE_LEFT }}
        />

        <DndContext
          sensors={sensors}
          collisionDetection={closestCenter}
          onDragEnd={handleDragEnd}
        >
          <SortableContext items={ordered} strategy={verticalListSortingStrategy}>
            <div className="space-y-3">
              {ordered.map((provider, index) => (
                <SortableProviderRow
                  key={provider}
                  provider={provider}
                  index={index}
                  config={providerConfigs[provider]}
                  onProviderPatch={onProviderPatch}
                />
              ))}
            </div>
          </SortableContext>
        </DndContext>

        <div className="relative mt-3">
          <div
            className={cn(
              "rounded-lg border border-border/70 bg-background/50 shadow-sm border-l-2",
              META.duckduckgo.borderClass
            )}
          >
            {/* Chain Track */}
            <div 
              className="absolute top-0 bottom-0 flex flex-col items-center justify-center transition-all duration-500"
              style={{ left: SPINE_LEFT, width: "12px", transform: "translateX(-50%)" }}
            >
              {/* Top Spine segment */}
              <div className={cn(
                "w-[3px] flex-1 transition-all duration-700 bg-gradient-to-b",
                META.duckduckgo.gradient
              )} />
              
              {/* Coupler Node - Pristine Circle */}
              <div className={cn(
                "relative h-3 w-3 shrink-0 rounded-full transition-all duration-500 z-10",
                META.duckduckgo.railClass,
                META.duckduckgo.glow,
                "scale-110 shadow-[0_0_12px_1px]"
              )}>
                <div className="absolute inset-0 rounded-full bg-gradient-to-br from-white/30 to-transparent opacity-60" />
              </div>

              {/* Bottom Spine segment */}
              <div className={cn(
                "w-[3px] flex-1 transition-all duration-700 bg-gradient-to-b",
                META.duckduckgo.gradient
              )} />
            </div>

            <div className="flex gap-4 px-3 py-2.5">
              <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded border border-border/50 bg-muted/10 text-muted-foreground/50">
                <Lock className="h-3.5 w-3.5" />
              </div>

              <div className="w-4" /> {/* Space for Coupler */}

              <div className="min-w-0 flex-1 space-y-2">
                <div className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
                  <div className="flex min-w-0 flex-wrap items-center gap-2">
                    <span className="text-[11px] font-bold tabular-nums text-muted-foreground/40">
                      {String(ordered.length + 1).padStart(2, "0")}
                    </span>
                    <span className="text-sm font-semibold tracking-tight">
                      {META.duckduckgo.label}
                    </span>
                    <Badge
                      variant="outline"
                      className={cn(
                        "rounded-full text-[10px] h-4 uppercase tracking-[0.08em] px-1.5",
                        META.duckduckgo.textClass,
                      )}
                    >
                      {META.duckduckgo.kind}
                    </Badge>
                    <Badge variant="secondary" className="h-4 px-1.5 py-0 text-[10px]">{t("tools.providerFallbackPinned")}</Badge>
                    <Badge variant="info" className="h-4 px-1.5 py-0 text-[10px]">{t("tools.providerStateAlwaysReady")}</Badge>
                  </div>

                  <div className="flex items-center gap-1.5 text-[9px] font-bold uppercase tracking-[0.1em] text-muted-foreground/60">
                    <Lock className="h-3 w-3" />
                    {t("tools.providerFallbackAlwaysOn")}
                  </div>
                </div>

                <div className="grid grid-cols-1 gap-2 md:grid-cols-[110px_minmax(0,1fr)]">
                  <div className="grid gap-1">
                    <Label className="text-[10px] uppercase tracking-wider text-muted-foreground/70">
                      {t("tools.maxResults")}
                    </Label>
                    <Input
                      type="number"
                      min={1}
                      value={duckduckgo.max_results ?? ""}
                      onChange={(event) =>
                        onDuckDuckGoPatch({
                          max_results: Number(event.target.value),
                        })
                      }
                      className="h-7 px-2 text-xs"
                      placeholder="5"
                    />
                  </div>

                  <div className="grid gap-1">
                    <Label className="text-[10px] uppercase tracking-wider text-muted-foreground/70 opacity-0">
                      .
                    </Label>
                    <div className="flex h-7 items-center rounded bg-muted/20 px-3 text-[10px] text-muted-foreground/70">
                      {t("tools.providerFallbackDescription")}
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
