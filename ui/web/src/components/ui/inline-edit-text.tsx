import { useState, useEffect, useRef, useCallback, KeyboardEvent, FocusEvent } from "react";
import { Pencil, Loader2 } from "lucide-react";
import { cn } from "@/lib/utils";

interface InlineEditTextProps {
  value: string;
  onSave: (newValue: string) => Promise<void> | void;
  placeholder?: string;
  maxLength?: number;
  minLength?: number;
  multiline?: boolean;
  disabled?: boolean;
  className?: string;       // applied to display text + input
  wrapperClassName?: string; // applied to outer container
  emptyLabel?: string;      // shown when value is empty in display mode
  ariaLabel?: string;
  onValidationError?: (error: string) => void; // fires when save rejected by validator
}

/**
 * Inline-editable text field. Click to edit, Enter to save, Esc to cancel.
 * - minLength default 1 (non-empty required). Set to 0 to allow empty.
 * - Trims whitespace before save. No-op if unchanged after trim.
 * - Blur saves if valid+changed; otherwise cancels.
 * - Follows CLAUDE.md mobile rules: text-base md:text-sm (16px mobile, no iOS zoom), min-h 44px on touch.
 */
export function InlineEditText({
  value,
  onSave,
  placeholder,
  maxLength = 255,
  minLength = 1,
  multiline = false,
  disabled = false,
  className,
  wrapperClassName,
  emptyLabel,
  ariaLabel,
  onValidationError,
}: InlineEditTextProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const [saving, setSaving] = useState(false);
  const inputRef = useRef<HTMLInputElement | HTMLTextAreaElement | null>(null);
  const savedViaKeyRef = useRef(false);
  // Snapshot of value at edit-start — used for no-op detection + stale-value guard.
  // Without this, a WS update to `value` mid-edit would cause blur-save to overwrite
  // the newer server value with the user's stale draft.
  const initialValueRef = useRef(value);
  // Track mount status to prevent setState-after-unmount (e.g. dialog backdrop
  // click triggers blur-save simultaneously with unmount).
  const isMountedRef = useRef(true);

  useEffect(() => {
    return () => {
      isMountedRef.current = false;
    };
  }, []);

  // Sync draft when external value changes AND we are not editing.
  // Intentional: during edit we keep the user's draft — stale-value guard handles races.
  useEffect(() => {
    if (!editing) setDraft(value);
  }, [value, editing]);

  // Focus + select-all on enter edit mode
  useEffect(() => {
    if (editing && inputRef.current) {
      inputRef.current.focus();
      inputRef.current.select();
    }
  }, [editing]);

  const safeSetEditing = useCallback((next: boolean) => {
    if (isMountedRef.current) setEditing(next);
  }, []);

  const safeSetSaving = useCallback((next: boolean) => {
    if (isMountedRef.current) setSaving(next);
  }, []);

  const safeSetDraft = useCallback((next: string) => {
    if (isMountedRef.current) setDraft(next);
  }, []);

  const startEdit = useCallback(() => {
    if (disabled || saving) return;
    initialValueRef.current = value;
    safeSetDraft(value);
    safeSetEditing(true);
  }, [disabled, saving, value, safeSetDraft, safeSetEditing]);

  const cancel = useCallback(() => {
    safeSetDraft(value);
    safeSetEditing(false);
  }, [value, safeSetDraft, safeSetEditing]);

  const attemptSave = useCallback(async () => {
    const trimmed = draft.trim();
    const initial = initialValueRef.current.trim();

    // No-op if draft matches the snapshot captured on edit-start.
    // Using snapshot (not current value) protects against losing the user's
    // edits if external value changed to match the draft by coincidence.
    if (trimmed === initial) {
      safeSetEditing(false);
      return;
    }

    // Validate minLength
    if (trimmed.length < minLength) {
      if (onValidationError) onValidationError("minLength");
      inputRef.current?.focus();
      return;
    }

    // Stale-value guard: if server value changed during edit, warn and cancel.
    // This keeps the user's draft in state for a moment, so they can copy it if needed.
    if (value.trim() !== initial) {
      if (onValidationError) onValidationError("stale");
      safeSetEditing(false);
      return;
    }

    safeSetSaving(true);
    try {
      await onSave(trimmed);
      safeSetEditing(false);
    } catch {
      // Parent handles toast. Revert draft.
      safeSetDraft(value);
      safeSetEditing(false);
    } finally {
      safeSetSaving(false);
    }
  }, [draft, value, minLength, onSave, onValidationError, safeSetDraft, safeSetEditing, safeSetSaving]);

  const onKeyDown = useCallback((e: KeyboardEvent<HTMLInputElement | HTMLTextAreaElement>) => {
    if (e.key === "Escape") {
      e.preventDefault();
      cancel();
      return;
    }
    // Enter saves. Shift+Enter in multiline = newline.
    if (e.key === "Enter" && (!multiline || !e.shiftKey)) {
      e.preventDefault();
      savedViaKeyRef.current = true;
      void attemptSave();
    }
  }, [cancel, attemptSave, multiline]);

  const onBlur = useCallback((_e: FocusEvent<HTMLInputElement | HTMLTextAreaElement>) => {
    // Avoid double-save if Enter already triggered save
    if (savedViaKeyRef.current) {
      savedViaKeyRef.current = false;
      return;
    }
    void attemptSave();
  }, [attemptSave]);

  if (editing) {
    const commonProps = {
      ref: inputRef as React.RefObject<HTMLInputElement & HTMLTextAreaElement>,
      value: draft,
      onChange: (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => safeSetDraft(e.target.value),
      onKeyDown,
      onBlur,
      maxLength,
      disabled: saving,
      "aria-label": ariaLabel,
      "aria-invalid": draft.trim().length < minLength,
      placeholder,
    };

    const inputBase = cn(
      "w-full min-w-0 rounded-md border border-input bg-transparent px-2 py-1",
      "text-base md:text-sm outline-none transition-shadow",
      "focus-visible:ring-1 focus-visible:ring-ring/50 focus-visible:border-ring",
      "disabled:opacity-60",
    );

    return (
      <div className={cn("inline-flex items-center gap-1.5 w-full", wrapperClassName)}>
        {multiline ? (
          <textarea
            {...commonProps}
            rows={2}
            className={cn(inputBase, "resize-none", className)}
          />
        ) : (
          <input
            {...commonProps}
            type="text"
            className={cn(inputBase, className)}
          />
        )}
        {saving && (
          <Loader2 className="h-4 w-4 shrink-0 animate-spin text-muted-foreground" aria-hidden />
        )}
      </div>
    );
  }

  const isEmpty = value.trim() === "";
  const display = isEmpty ? (emptyLabel ?? placeholder ?? "") : value;

  // A11y: aria-label only on <input> during edit. Display button uses visible text
  // as accessible name (Pencil icon is aria-hidden), so omit aria-label here to
  // avoid double-announce by screen readers.
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={(e) => { e.stopPropagation(); startEdit(); }}
      className={cn(
        "group inline-flex items-center gap-1.5 rounded-md px-1 py-0.5 -ml-1 -my-0.5",
        "hover:bg-muted/60 focus-visible:bg-muted/60 outline-none",
        "focus-visible:ring-1 focus-visible:ring-ring/50",
        "disabled:cursor-not-allowed disabled:opacity-60",
        // Mobile: ≥44px touch target via min-height on coarse pointers
        "text-left min-h-[32px] [@media(pointer:coarse)]:min-h-[44px]",
        wrapperClassName,
      )}
    >
      <span
        className={cn(
          "truncate",
          isEmpty && "italic text-muted-foreground",
          className,
        )}
      >
        {display}
      </span>
      <Pencil
        className="h-3.5 w-3.5 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100"
        aria-hidden
      />
    </button>
  );
}
