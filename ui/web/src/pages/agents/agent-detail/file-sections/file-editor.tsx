import { type ReactNode, useRef, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Save } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { FILE_DESCRIPTIONS } from "./file-utils";
import { ContactInsertSearch } from "./contact-insert-search";

interface FileEditorProps {
  fileName: string | null;
  content: string;
  onChange: (content: string) => void;
  loading: boolean;
  dirty: boolean;
  saving: boolean;
  canEdit: boolean;
  onSave: () => void;
  headerActions?: ReactNode;
  /** Show contact search box for inserting contact snippets into the editor. */
  contactSearchEnabled?: boolean;
}

export function FileEditor({
  fileName,
  content,
  onChange,
  loading,
  dirty,
  saving,
  canEdit,
  onSave,
  headerActions,
  contactSearchEnabled,
}: FileEditorProps) {
  const { t } = useTranslation("agents");
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const handleInsertText = useCallback(
    (text: string) => {
      const ta = textareaRef.current;
      if (!ta) {
        // Fallback: append at end
        onChange(content + (content.endsWith("\n") ? "" : "\n") + text);
        return;
      }
      const start = ta.selectionStart;
      const end = ta.selectionEnd;
      const before = content.slice(0, start);
      const after = content.slice(end);
      // Add newline prefix if cursor isn't at line start
      const needNewline = before.length > 0 && !before.endsWith("\n");
      const inserted = (needNewline ? "\n" : "") + text + "\n";
      onChange(before + inserted + after);
      // Restore cursor after inserted text
      requestAnimationFrame(() => {
        const pos = start + inserted.length;
        ta.selectionStart = pos;
        ta.selectionEnd = pos;
        ta.focus();
      });
    },
    [content, onChange],
  );

  if (!fileName) {
    return (
      <div className="flex flex-1 flex-col">
        {headerActions && (
          <div className="mb-2 flex justify-end gap-2">{headerActions}</div>
        )}
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          {canEdit ? t("files.selectFileToEdit") : t("files.selectFileToView")}
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-1 flex-col">
      <div className="mb-2 flex items-center gap-3">
        <div className="min-w-0 flex-1">
          <span className="text-sm font-medium">{fileName}</span>
          {FILE_DESCRIPTIONS[fileName] && (
            <span className="ml-2 text-xs text-muted-foreground">
              - {FILE_DESCRIPTIONS[fileName]}
            </span>
          )}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {headerActions}
          {canEdit && (
            <Button size="sm" onClick={onSave} disabled={!dirty || saving}>
              {!saving && <Save className="h-3.5 w-3.5" />}
              {saving ? t("files.saving") : t("files.save")}
            </Button>
          )}
        </div>
      </div>
      {contactSearchEnabled && canEdit && (
        <div className="mb-2">
          <ContactInsertSearch onInsert={handleInsertText} />
        </div>
      )}
      {loading && !content ? (
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          {t("files.loading")}
        </div>
      ) : (
        <Textarea
          ref={textareaRef}
          value={content}
          onChange={(e) => {
            if (!canEdit) return;
            onChange(e.target.value);
          }}
          readOnly={!canEdit}
          className={`flex-1 resize-none font-mono text-sm ${!canEdit ? "opacity-70" : ""}`}
          placeholder={t("files.contentPlaceholder")}
        />
      )}
    </div>
  );
}
