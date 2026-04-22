/**
 * VoicePicker — provider-aware voice selection component.
 *
 * Dispatch logic (Phase 2):
 *   - "" provider → disabled empty-state
 *   - gemini + static voices → PortalVoicePicker (search + row UI)
 *   - other provider with static voices[] + !voicesDynamic → StaticVoicePicker (Radix)
 *   - voices_dynamic=true OR minimax → DynamicVoicePicker → PortalVoicePicker
 *   - MiniMax first-fetch failure → FreeTextPicker fallback
 *
 * useTtsCapabilities() drives the dispatch; falls back to ElevenLabs-dynamic behavior
 * when capabilities are not yet loaded (avoids flash of wrong picker).
 */
import { useId, useState, useRef } from "react";
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
import { useVoices, useRefreshVoices } from "@/api/voices";
import { VoicePreviewButton } from "@/components/voice-preview-button";
import { useTtsCapabilities, type VoiceOption } from "@/api/tts-capabilities";
import type { TtsProviderId } from "@/data/tts-providers";
import { usePortalDropdownClose } from "@/hooks/use-portal-dropdown-close";

interface Props {
  value?: string;
  onChange: (id: string) => void;
  disabled?: boolean;
  /**
   * Controls picker mode:
   *   - "" → disabled empty-state
   *   - TtsProviderId → capabilities-driven dispatch
   *   - undefined → DynamicVoicePicker (ElevenLabs legacy)
   */
  provider?: TtsProviderId | "";
  placeholder?: string;
}

const LABEL_KEYS = ["gender", "accent", "age", "use_case", "style"] as const;

/**
 * Unified voice shape for PortalVoicePicker.
 * Static callers (Gemini) populate only voice_id + name; labels/preview_url optional.
 */
export interface PortalVoice {
  voice_id: string;
  name: string;
  labels?: Record<string, string>;
  preview_url?: string;
}

/** Maps a capability VoiceOption to PortalVoice. Labels passed through (e.g. Gemini style). */
function mapCapVoiceToPortal(v: VoiceOption): PortalVoice {
  return { voice_id: v.voice_id, name: v.name, labels: v.labels };
}

function VoiceRow({ voice, selected, onSelect }: { voice: PortalVoice; selected: boolean; onSelect: () => void }) {
  const labelEntries = LABEL_KEYS
    .filter((k) => voice.labels?.[k])
    .map((k) => voice.labels![k]);

  return (
    <div
      role="option"
      aria-selected={selected}
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

/** Top-level dispatcher — capabilities-aware routing. */
export function VoicePicker({ value, onChange, disabled, provider, placeholder }: Props) {
  const { data: caps = [] } = useTtsCapabilities();

  if (provider === "") {
    return <EmptyStatePicker placeholder={placeholder} />;
  }

  // Find capabilities for the current provider
  const providerCaps = provider ? caps.find((c) => c.provider === provider) : null;

  // voices_dynamic=true in custom_features → use dynamic fetch
  const voicesDynamic = providerCaps?.custom_features?.["voices_dynamic"] === true;
  // Static voices available in capabilities
  const staticVoices = providerCaps?.voices ?? [];

  // Gemini: static voices rendered through portal picker (search + row UI)
  if (provider === "gemini" && staticVoices.length > 0) {
    return (
      <PortalVoicePicker
        voices={staticVoices.map(mapCapVoiceToPortal)}
        value={value}
        onChange={onChange}
        disabled={disabled}
        placeholder={placeholder}
      />
    );
  }

  if (providerCaps && !voicesDynamic && staticVoices.length > 0) {
    // Static catalog available from capabilities (OpenAI path → Radix Select)
    return (
      <StaticVoicePicker
        value={value}
        onChange={onChange}
        disabled={disabled}
        voices={staticVoices.map((v) => ({ value: v.voice_id, label: v.name }))}
        placeholder={placeholder}
      />
    );
  }

  if (provider === "minimax" || voicesDynamic) {
    // MiniMax and any voices_dynamic provider: use dynamic picker with free-text fallback
    return (
      <DynamicVoicePicker
        value={value}
        onChange={onChange}
        disabled={disabled}
        allowFreeText={provider === "minimax"}
      />
    );
  }

  // Default: ElevenLabs and other dynamic providers
  return (
    <DynamicVoicePicker
      value={value}
      onChange={onChange}
      disabled={disabled}
      allowFreeText={false}
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

function FreeTextVoicePicker({
  value,
  onChange,
  disabled,
}: {
  value?: string;
  onChange: (id: string) => void;
  disabled?: boolean;
}) {
  const { t } = useTranslation("tts");
  return (
    <input
      type="text"
      className="border-input dark:bg-input/30 flex h-9 w-full rounded-md border bg-transparent px-3 py-2 text-base md:text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-1 disabled:cursor-not-allowed disabled:opacity-50"
      value={value ?? ""}
      onChange={(e) => onChange(e.target.value)}
      disabled={disabled}
      placeholder={t("voice_picker.enter_voice_id", "Enter voice_id manually")}
      aria-label={t("voice_label")}
    />
  );
}

/**
 * PortalVoicePicker — search + scrollable VoiceRow list rendered via portal.
 *
 * Used by:
 *   - DynamicVoicePicker (ElevenLabs, MiniMax) — passes voices from useVoices()
 *   - VoicePicker dispatcher for Gemini — passes capability static voices
 *
 * Owns: open/search state, triggerRef/dropdownRef, usePortalDropdownClose, createPortal.
 */
export function PortalVoicePicker({
  voices,
  value,
  onChange,
  disabled,
  isLoading,
  onRefresh,
  allowFreeText: _allowFreeText,
  placeholder,
}: {
  voices: PortalVoice[];
  value?: string;
  onChange: (voice_id: string) => void;
  disabled?: boolean;
  isLoading?: boolean;
  onRefresh?: () => void;
  allowFreeText?: boolean;
  placeholder?: string;
}) {
  const { t } = useTranslation("tts");
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const triggerRef = useRef<HTMLDivElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const listboxId = useId();

  const selected = voices.find((v) => v.voice_id === value);

  const filtered = search.trim()
    ? voices.filter((v) => v.name.toLowerCase().includes(search.toLowerCase()))
    : voices;

  const handleToggle = () => {
    if (disabled) return;
    setOpen((prev) => {
      if (prev) return false;
      setSearch("");
      return true;
    });
  };

  const handleSelect = (voice: PortalVoice) => {
    onChange(voice.voice_id);
    setOpen(false);
    setSearch("");
  };

  const handleRefresh = (e: React.MouseEvent) => {
    e.stopPropagation();
    onRefresh?.();
  };

  usePortalDropdownClose({
    open,
    onClose: () => setOpen(false),
    ignore: [triggerRef, dropdownRef],
  });

  const dropdownContent = open && (
    <div
      ref={dropdownRef}
      id={listboxId}
      role="listbox"
      aria-label={placeholder ?? t("voice_placeholder")}
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
          placeholder={placeholder ?? t("voice_placeholder")}
          className="flex-1 bg-transparent text-base md:text-sm outline-none placeholder:text-muted-foreground"
        />
        {onRefresh && (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            title={t("voice_refresh")}
            disabled={isLoading}
            onClick={handleRefresh}
            className="shrink-0"
          >
            <RefreshCwIcon className={cn("size-4", isLoading && "animate-spin")} />
          </Button>
        )}
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
    <div ref={triggerRef} className="relative">
      <button
        type="button"
        disabled={disabled}
        onClick={handleToggle}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? listboxId : undefined}
        className={cn(
          "border-input dark:bg-input/30 flex h-9 w-full items-center justify-between gap-2 rounded-md border bg-transparent px-3 py-2 text-base md:text-sm shadow-xs transition-[color,box-shadow] outline-none",
          "focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-1",
          "disabled:cursor-not-allowed disabled:opacity-50",
          !selected && "text-muted-foreground",
        )}
      >
        <span className="truncate">
          {isLoading ? t("voice_loading") : selected?.name ?? (placeholder ?? t("voice_placeholder"))}
        </span>
        <ChevronDownIcon className="size-4 shrink-0 opacity-50" />
      </button>

      {open && createPortal(dropdownContent, document.body)}
    </div>
  );
}

/**
 * DynamicVoicePicker — thin wrapper that fetches voices and delegates to PortalVoicePicker.
 * Handles free-text fallback for MiniMax on fetch error.
 */
function DynamicVoicePicker({
  value,
  onChange,
  disabled,
  allowFreeText,
}: {
  value?: string;
  onChange: (id: string) => void;
  disabled?: boolean;
  allowFreeText: boolean;
}) {
  const { data: voices = [], isLoading, isError } = useVoices();
  const { mutate: refresh, isPending: refreshing } = useRefreshVoices();

  // Fall back to free-text input when MiniMax voices fetch failed and list is empty
  if (allowFreeText && isError && voices.length === 0) {
    return <FreeTextVoicePicker value={value} onChange={onChange} disabled={disabled} />;
  }

  return (
    <PortalVoicePicker
      voices={voices}
      value={value}
      onChange={onChange}
      disabled={disabled}
      isLoading={isLoading || refreshing}
      onRefresh={() => refresh()}
      allowFreeText={allowFreeText}
    />
  );
}
