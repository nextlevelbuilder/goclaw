import { useState } from "react";
import { Trash2, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { useDeleteLink } from "./hooks/use-vault";
import type { VaultLink } from "@/types/vault";

const DOC_TYPES = ["context", "memory", "note", "skill", "episodic"] as const;
const SCOPES = ["personal", "team", "shared"] as const;

interface EditControlsProps {
  saving: boolean;
  confirmDelete: boolean;
  onSave: () => void;
  onCancel: () => void;
  onDeleteRequest: () => void;
  onDeleteConfirm: () => void;
  onDeleteCancel: () => void;
  t: (k: string) => string;
}

export function VaultEditControls({
  saving, confirmDelete,
  onSave, onCancel, onDeleteRequest, onDeleteConfirm, onDeleteCancel, t,
}: EditControlsProps) {
  return (
    <div className="flex items-center gap-2 justify-between">
      <div className="flex gap-2">
        <Button size="sm" onClick={onSave} disabled={saving}>
          {saving ? t("saving") : t("save")}
        </Button>
        <Button size="sm" variant="outline" onClick={onCancel} disabled={saving}>
          {t("cancel")}
        </Button>
      </div>
      {!confirmDelete ? (
        <Button size="sm" variant="destructive" onClick={onDeleteRequest} disabled={saving}>
          <Trash2 className="h-3.5 w-3.5 mr-1" />
          {t("delete")}
        </Button>
      ) : (
        <div className="flex items-center gap-1">
          <span className="text-xs text-destructive">{t("confirmDelete")}</span>
          <Button size="xs" variant="destructive" onClick={onDeleteConfirm} disabled={saving} className="h-7 px-2">
            {t("yes")}
          </Button>
          <Button size="xs" variant="outline" onClick={onDeleteCancel} className="h-7 px-2">
            {t("no")}
          </Button>
        </div>
      )}
    </div>
  );
}

interface DocTypeSelectProps {
  value: string;
  onChange: (v: string) => void;
  t: (k: string) => string;
}

export function DocTypeSelect({ value, onChange, t }: DocTypeSelectProps) {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className="h-6 w-auto text-2xs px-1.5 py-0 gap-1">
        <SelectValue />
      </SelectTrigger>
      <SelectContent className="pointer-events-auto">
        {DOC_TYPES.map((dt) => (
          <SelectItem key={dt} value={dt} className="text-xs">{t(`type.${dt}`)}</SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

interface ScopeSelectProps {
  value: string;
  onChange: (v: string) => void;
  t: (k: string) => string;
}

export function ScopeSelect({ value, onChange, t }: ScopeSelectProps) {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className="h-6 w-auto text-2xs px-1.5 py-0 gap-1">
        <SelectValue />
      </SelectTrigger>
      <SelectContent className="pointer-events-auto">
        {SCOPES.map((s) => (
          <SelectItem key={s} value={s} className="text-xs">{t(`scope.${s}`)}</SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

/** Outlink badge with inline delete confirmation. */
export function LinkBadge({ link, agentId, t }: { link: VaultLink; agentId: string; t: (k: string) => string }) {
  const [confirmDelete, setConfirmDelete] = useState(false);
  const { remove } = useDeleteLink(agentId, link.id);

  const handleDelete = async () => {
    try {
      await remove();
    } catch {
      // toasted in hook
    }
    setConfirmDelete(false);
  };

  if (confirmDelete) {
    return (
      <span className="inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs bg-destructive/10 border-destructive/30">
        <span className="text-destructive">{t("deleteLink")}?</span>
        <button onClick={handleDelete} className="text-destructive font-bold hover:underline">✓</button>
        <button onClick={() => setConfirmDelete(false)} className="text-muted-foreground hover:underline">✗</button>
      </span>
    );
  }

  return (
    <Badge variant="secondary" className="text-xs group relative pr-1 gap-1">
      <span>{link.link_type}: {link.to_doc_id.slice(0, 8)}</span>
      <button
        onClick={() => setConfirmDelete(true)}
        className="opacity-0 group-hover:opacity-100 transition-opacity"
        title={t("deleteLink")}
      >
        <X className="h-3 w-3" />
      </button>
    </Badge>
  );
}
