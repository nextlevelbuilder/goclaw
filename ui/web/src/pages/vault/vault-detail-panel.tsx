import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { X, Pencil, Plus, FileText, Link2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { MarkdownRenderer } from "@/components/shared/markdown-renderer";
import { useVaultLinks, useVaultFileContent, useUpdateDocument, useDeleteDocument } from "./hooks/use-vault";
import { VaultLinkDialog } from "./vault-link-dialog";
import { VaultEditControls, DocTypeSelect, ScopeSelect, LinkBadge } from "./vault-detail-edit-section";
import type { VaultDocument } from "@/types/vault";

interface Props {
  doc: VaultDocument | null;
  open: boolean;
  onClose: () => void;
  onDeleted?: () => void;
}

export function VaultDetailPanel({ doc, open, onClose, onDeleted }: Props) {
  const { t } = useTranslation("vault");
  const { outlinks, backlinks, docNames } = useVaultLinks(doc?.id ?? null);
  const { content, loading: contentLoading, error: contentError } = useVaultFileContent(open && doc ? doc.path : null);
  const { update } = useUpdateDocument(doc?.id ?? "");
  const { remove } = useDeleteDocument(doc?.id ?? "");

  const [editMode, setEditMode] = useState(false);
  const [editTitle, setEditTitle] = useState("");
  const [editDocType, setEditDocType] = useState("");
  const [editScope, setEditScope] = useState("");
  const [saving, setSaving] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [linkDialogOpen, setLinkDialogOpen] = useState(false);

  // Reset edit state when doc changes
  useEffect(() => { setEditMode(false); setConfirmDelete(false); }, [doc?.id]);

  // Escape to close
  useEffect(() => {
    if (!open) return;
    const handler = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [open, onClose]);

  const startEdit = () => {
    if (!doc) return;
    setEditTitle(doc.title); setEditDocType(doc.doc_type); setEditScope(doc.scope);
    setEditMode(true);
  };

  const handleSave = async () => {
    setSaving(true);
    try { await update({ title: editTitle, doc_type: editDocType, scope: editScope }); setEditMode(false); }
    catch { /* toasted */ }
    finally { setSaving(false); }
  };

  const handleDelete = async () => {
    setSaving(true);
    try { await remove(); onDeleted?.(); onClose(); }
    catch { /* toasted */ }
    finally { setSaving(false); setConfirmDelete(false); }
  };

  if (!doc || !open) return null;

  const hash = doc.content_hash;
  const hashDisplay = hash.length > 16 ? `${hash.slice(0, 8)}...${hash.slice(-8)}` : hash;

  return (
    <>
      <div className="border-t bg-background flex flex-col" style={{ height: 240 }}>
        {/* Header */}
        <div className="flex items-center gap-2 px-3 py-1.5 border-b shrink-0">
          {editMode ? (
            <Input value={editTitle} onChange={(e) => setEditTitle(e.target.value)}
              className="h-7 text-sm font-medium flex-1" autoFocus />
          ) : (
            <span className="text-sm font-medium truncate flex-1">{doc.title || doc.path}</span>
          )}
          {editMode ? (
            <>
              <DocTypeSelect value={editDocType} onChange={setEditDocType} t={t} />
              <ScopeSelect value={editScope} onChange={setEditScope} t={t} />
            </>
          ) : (
            <>
              <Badge variant="secondary" className="text-2xs px-1.5 py-0">{t(`type.${doc.doc_type}`)}</Badge>
              <Badge variant="outline" className="text-2xs px-1.5 py-0">{t(`scope.${doc.scope}`)}</Badge>
            </>
          )}
          {!editMode && (
            <Button variant="ghost" size="xs" className="h-6 w-6 p-0" onClick={startEdit}>
              <Pencil className="h-3 w-3" />
            </Button>
          )}
          <Button variant="ghost" size="xs" className="h-6 w-6 p-0" onClick={onClose}>
            <X className="h-3.5 w-3.5" />
          </Button>
        </div>

        {/* Edit controls */}
        {editMode && (
          <div className="px-3 py-1 border-b shrink-0">
            <VaultEditControls saving={saving} confirmDelete={confirmDelete}
              onSave={handleSave} onCancel={() => { setEditMode(false); setConfirmDelete(false); }}
              onDeleteRequest={() => setConfirmDelete(true)} onDeleteConfirm={handleDelete}
              onDeleteCancel={() => setConfirmDelete(false)} t={t} />
          </div>
        )}

        {/* Content: left = markdown, right = meta */}
        <div className="flex-1 min-h-0 flex overflow-hidden">
          {/* Markdown preview */}
          <div className="flex-1 overflow-y-auto px-3 py-2 border-r">
            {contentLoading ? (
              <div className="space-y-2">
                <div className="h-3 w-3/4 animate-pulse rounded bg-muted" />
                <div className="h-3 w-1/2 animate-pulse rounded bg-muted" />
              </div>
            ) : contentError ? (
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <FileText className="h-3.5 w-3.5" /> {t("detail.fileNotFound")}
              </div>
            ) : content ? (
              <MarkdownRenderer content={content}
                className="text-xs [&_h1]:text-sm [&_h1]:font-semibold [&_h2]:text-xs [&_h2]:font-semibold [&_p]:text-xs [&_li]:text-xs [&_code]:text-2xs [&_pre]:overflow-x-auto [&_img]:max-w-full" />
            ) : (
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <FileText className="h-3.5 w-3.5" /> {t("detail.emptyContent")}
              </div>
            )}
          </div>

          {/* Meta column */}
          <div className="w-[220px] shrink-0 overflow-y-auto px-3 py-2 space-y-2 text-xs">
            <div className="font-mono text-2xs text-muted-foreground break-all" dir="rtl" title={doc.path}>
              {doc.path}
            </div>
            <div className="font-mono text-2xs text-muted-foreground">SHA: {hashDisplay}</div>

            {/* Outlinks */}
            <div className="space-y-1">
              <div className="flex items-center gap-1 text-muted-foreground">
                <Link2 className="h-3 w-3" />
                <span>{t("detail.outlinks")} ({outlinks.length})</span>
                <Button variant="ghost" size="xs" className="h-5 px-1 ml-auto" onClick={() => setLinkDialogOpen(true)}>
                  <Plus className="h-2.5 w-2.5" />
                </Button>
              </div>
              {outlinks.length > 0 && (
                <div className="flex flex-wrap gap-1">
                  {outlinks.map((l) => <LinkBadge key={l.id} link={l} docNames={docNames} t={t} />)}
                </div>
              )}
            </div>

            {/* Backlinks */}
            {backlinks.length > 0 && (
              <div className="space-y-1">
                <span className="text-muted-foreground">{t("detail.backlinks")} ({backlinks.length})</span>
                <div className="flex flex-wrap gap-1">
                  {backlinks.map((l) => (
                    <Badge key={l.from_doc_id} variant="secondary" className="text-2xs" title={l.path}>
                      {l.title || l.from_doc_id.slice(0, 8)}
                    </Badge>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>
      </div>

      {linkDialogOpen && (
        <VaultLinkDialog agentId={doc.agent_id ?? ""} fromDoc={doc}
          open={linkDialogOpen} onOpenChange={setLinkDialogOpen} />
      )}
    </>
  );
}
