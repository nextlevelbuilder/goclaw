import { useState, useRef, useCallback, type KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";
import { Send, Square, Paperclip, X } from "lucide-react";
import { Button } from "@/components/ui/button";

export interface AttachedFile {
  file: File;
  /** Server path after upload, set during send */
  serverPath?: string;
}

interface ChatInputProps {
  onSend: (message: string, files?: AttachedFile[]) => void;
  onAbort: () => void;
  /** True when main agent or team tasks are active — controls stop button, file attach */
  isBusy: boolean;
  disabled?: boolean;
  files: AttachedFile[];
  onFilesChange: (files: AttachedFile[]) => void;
}

export function ChatInput({ onSend, onAbort, isBusy, disabled, files, onFilesChange }: ChatInputProps) {
  const { t } = useTranslation("common");
  const [value, setValue] = useState("");
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const handleSend = useCallback(() => {
    if ((!value.trim() && files.length === 0) || disabled) return;
    onSend(value, files.length > 0 ? files : undefined);
    setValue("");
    onFilesChange([]);
    if (textareaRef.current) {
      textareaRef.current.style.height = "auto";
    }
  }, [value, files, onSend, onFilesChange, disabled]);

  const handleKeyDown = useCallback(
    (e: KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        handleSend();
      }
    },
    [handleSend],
  );

  const handleInput = useCallback(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, 200) + "px";
  }, []);

  const handleFileSelect = useCallback(() => {
    fileInputRef.current?.click();
  }, []);

  const handleFileChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const selected = e.target.files;
    if (!selected) return;
    const newFiles: AttachedFile[] = Array.from(selected).map((f) => ({ file: f }));
    onFilesChange([...files, ...newFiles]);
    // Reset input so the same file can be re-selected
    e.target.value = "";
  }, [files, onFilesChange]);

  const removeFile = useCallback((index: number) => {
    onFilesChange(files.filter((_, i) => i !== index));
  }, [files, onFilesChange]);

  return (
    <div
      className="mx-3 mb-3 rounded-xl border bg-background/95 backdrop-blur-sm shadow-sm safe-bottom"
      style={{ paddingBottom: `calc(env(safe-area-inset-bottom) + var(--keyboard-height, 0px))` }}
    >
      {/* Attached files preview */}
      {files.length > 0 && (
        <div className="flex flex-wrap gap-1.5 px-4 pt-3">
          {files.map((af, i) => (
            <span
              key={i}
              className="inline-flex items-center gap-1 rounded-md bg-muted px-2 py-1 text-xs"
            >
              <span className="max-w-[150px] truncate">{af.file.name}</span>
              <button
                type="button"
                onClick={() => removeFile(i)}
                className="rounded-sm p-0.5 hover:bg-accent"
              >
                <X className="h-3 w-3" />
              </button>
            </span>
          ))}
        </div>
      )}

      <div className="flex items-end gap-2 p-4 pt-3">
        {/* File attach button */}
        <Button
          type="button"
          variant="ghost"
          size="icon-lg"
          onClick={handleFileSelect}
          disabled={disabled || isBusy}
          title={t("attachFile")}
          className="text-muted-foreground hover:text-foreground"
        >
          <Paperclip className="h-4 w-4" />
        </Button>
        <input
          ref={fileInputRef}
          type="file"
          multiple
          onChange={handleFileChange}
          className="hidden"
        />

        <textarea
          ref={textareaRef}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={handleKeyDown}
          onInput={handleInput}
          placeholder={t("sendMessage")}
          disabled={disabled}
          rows={1}
          className="flex-1 resize-none rounded-lg border bg-background px-4 py-2.5 text-base md:text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring disabled:opacity-50"
        />
        {isBusy ? (
          <div className="flex gap-1">
            <Button
              size="icon-lg"
              onClick={handleSend}
              disabled={!value.trim() || disabled}
              title={t("sendFollowUp")}
            >
              <Send className="h-4 w-4" />
            </Button>
            <Button
              variant="destructive"
              size="icon-lg"
              onClick={onAbort}
              title={t("stopGeneration")}
            >
              <Square className="h-4 w-4" />
            </Button>
          </div>
        ) : (
          <Button
            size="icon-lg"
            onClick={handleSend}
            disabled={(!value.trim() && files.length === 0) || disabled}
            title={t("sendMessageTitle")}
          >
            <Send className="h-4 w-4" />
          </Button>
        )}
      </div>
    </div>
  );
}
