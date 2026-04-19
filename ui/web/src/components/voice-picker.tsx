import { useState, useRef } from "react";
import { useTranslation } from "react-i18next";
import { RefreshCwIcon, ChevronDownIcon } from "lucide-react";
import { createPortal } from "react-dom";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";
import { useVoices, useRefreshVoices, type Voice } from "@/api/voices";
import { VoicePreviewButton } from "@/components/voice-preview-button";
import {
  getProviderDefinition,
  type TtsProviderId,
} from "@/data/tts-providers";

interface Props {
  value?: string;
  onChange: (id: string) => void;
  disabled?: boolean;
  /**
   * Controls picker mode:
   *   - undefined / "elevenlabs" → dynamic fetch from /v1/voices (legacy behavior).
   *   - "openai" | "edge" | "minimax" → hardcoded list from catalog.
   *   - "" (empty string) → disabled; shows "Configure TTS provider first".
   */
  provider?: TtsProviderId | "";
  placeholder?: string;
}

const LABEL_KEYS = ["gender", "accent", "age", "use_case"] as const;

function VoiceRow({ voice, selected, onSelect }: { voice: Voice; selected: boolean; onSelect: () => void }) {
  const labelEntries = LABEL_KEYS
    .filter((k) => voice.labels?.[k])
    .map((k) => voice.labels![k]);

  return (
    <div
      className={cn(
        "flex items-center gap-2 rounded-sm px-2 py-1.5 cursor-pointer hover:bg-accent hover:text-accent-foreground",
        selected && "bg-accent/60",
      )}
      onMouseDown={(e) => e.preventDefault()}
      onClick={onSelect}
    >
      <span className="flex-1 truncate text-sm">{voice.name}</span>
      <div className="flex shrink-0 items-center gap-1">
        {labelEntries.slice(0, 2).map((label) => (
          <Badge key={label} variant="outline" className="text-xs px-1 py-0">
            {label}
          </Badge>
        ))}
        <VoicePreviewButton previewUrl={voice.preview_url} voiceName={voice.name} />
      </div>
    </div>
  );
}

/**
 * Top-level picker. Dispatches to one of three sub-components based on `provider`:
 *   - "" → disabled empty-state
 *   - non-dynamic provider (openai/edge/minimax) → <Select> from catalog
 *   - undefined | "elevenlabs" → <DynamicVoicePicker> that fetches /v1/voices
 */
export function VoicePicker({ value, onChange, disabled, provider, placeholder }: Props) {
  if (provider === "") {
    return <EmptyStatePicker placeholder={placeholder} />;
  }
  const def = provider ? getProviderDefinition(provider) : null;
  if (def && !def.dynamic) {
    return (
      <StaticVoicePicker
        value={value}
        onChange={onChange}
        disabled={disabled}
        voices={def.voices}
        placeholder={placeholder}
      />
    );
  }
  return (
    <DynamicVoicePicker
      value={value}
      onChange={onChange}
      disabled={disabled}
    />
  );
}

function EmptyStatePicker({ placeholder }: { placeholder?: string }) {
  const { t } = useTranslation("tts");
  return (
    <button
      type="button"
      disabled
      className={cn(
        "border-input dark:bg-input/30 flex h-9 w-full items-center justify-between gap-2 rounded-md border bg-transparent px-3 py-2 text-base md:text-sm shadow-xs outline-none",
        "disabled:cursor-not-allowed disabled:opacity-50",
        "text-muted-foreground",
      )}
    >
      <span className="truncate">
        {placeholder ?? t("voice_picker.requires_provider")}
      </span>
      <ChevronDownIcon className="size-4 shrink-0 opacity-50" />
    </button>
  );
}

function StaticVoicePicker({
  value,
  onChange,
  disabled,
  voices,
  placeholder,
}: {
  value?: string;
  onChange: (id: string) => void;
  disabled?: boolean;
  voices: { value: string; label: string }[];
  placeholder?: string;
}) {
  const { t } = useTranslation("tts");
  return (
    <Select value={value ?? ""} onValueChange={onChange} disabled={disabled}>
      <SelectTrigger className="w-full">
        <SelectValue placeholder={placeholder ?? t("voice_placeholder")} />
      </SelectTrigger>
      <SelectContent>
        {voices.map((v) => (
          <SelectItem key={v.value} value={v.value}>
            {v.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function DynamicVoicePicker({
  value,
  onChange,
  disabled,
}: {
  value?: string;
  onChange: (id: string) => void;
  disabled?: boolean;
}) {
  const { t } = useTranslation("tts");
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const triggerRef = useRef<HTMLDivElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);

  const { data: voices = [], isLoading } = useVoices();
  const { mutate: refresh, isPending: refreshing } = useRefreshVoices();

  const selected = voices.find((v) => v.voice_id === value);

  const filtered = search.trim()
    ? voices.filter((v) => v.name.toLowerCase().includes(search.toLowerCase()))
    : voices;

  const handleOpen = () => {
    if (disabled) return;
    setOpen(true);
    setSearch("");
  };

  const handleSelect = (voice: Voice) => {
    onChange(voice.voice_id);
    setOpen(false);
    setSearch("");
  };

  const handleRefresh = (e: React.MouseEvent) => {
    e.stopPropagation();
    refresh();
  };

  const handleBlur = (e: React.FocusEvent) => {
    if (!e.currentTarget.contains(e.relatedTarget as Node)) {
      setOpen(false);
    }
  };

  const dropdownContent = open && (
    <div
      ref={dropdownRef}
      className="pointer-events-auto z-50 min-w-[280px] rounded-md border bg-popover text-popover-foreground shadow-md"
      style={(() => {
        if (!triggerRef.current) return {};
        const rect = triggerRef.current.getBoundingClientRect();
        const spaceBelow = window.innerHeight - rect.bottom;
        const dropH = 280;
        if (spaceBelow < dropH && rect.top > dropH) {
          return { position: "fixed" as const, bottom: window.innerHeight - rect.top + 4, left: rect.left, width: rect.width };
        }
        return { position: "fixed" as const, top: rect.bottom + 4, left: rect.left, width: rect.width };
      })()}
    >
      <div className="flex items-center gap-1 border-b px-2 py-1.5">
        <input
          autoFocus
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t("voice_placeholder")}
          className="flex-1 bg-transparent text-base md:text-sm outline-none placeholder:text-muted-foreground"
        />
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          title={t("voice_refresh")}
          disabled={refreshing}
          onClick={handleRefresh}
          className="shrink-0"
        >
          <RefreshCwIcon className={cn("size-4", refreshing && "animate-spin")} />
        </Button>
      </div>

      <div className="max-h-60 overflow-y-auto p-1">
        {isLoading ? (
          <div className="space-y-1 p-1">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
          </div>
        ) : filtered.length === 0 ? (
          <p className="py-4 text-center text-sm text-muted-foreground">
            {voices.length === 0 ? t("voice_save_config_first") : search ? t("voice_no_voices") : t("voice_loading")}
          </p>
        ) : (
          filtered.map((voice) => (
            <VoiceRow
              key={voice.voice_id}
              voice={voice}
              selected={voice.voice_id === value}
              onSelect={() => handleSelect(voice)}
            />
          ))
        )}
      </div>
    </div>
  );

  return (
    <div ref={triggerRef} className="relative" onBlur={handleBlur}>
      <button
        type="button"
        disabled={disabled}
        onClick={handleOpen}
        className={cn(
          "border-input dark:bg-input/30 flex h-9 w-full items-center justify-between gap-2 rounded-md border bg-transparent px-3 py-2 text-base md:text-sm shadow-xs transition-[color,box-shadow] outline-none",
          "focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-1",
          "disabled:cursor-not-allowed disabled:opacity-50",
          !selected && "text-muted-foreground",
        )}
      >
        <span className="truncate">
          {isLoading ? t("voice_loading") : selected?.name ?? t("voice_placeholder")}
        </span>
        <ChevronDownIcon className="size-4 shrink-0 opacity-50" />
      </button>

      {open && createPortal(dropdownContent, document.body)}
    </div>
  );
}
