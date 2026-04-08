import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, ChevronRight, Pencil, Plus } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { MarkdownRenderer } from "@/components/shared/markdown-renderer";
import { useVaultLinks, useVaultFileContent, useUpdateDocument, useDeleteDocument } from "./hooks/use-vault";
import { VaultLinkDialog } from "./vault-link-dialog";
import {
  VaultEditControls, DocTypeSelect, ScopeSelect, LinkBadge,
} from "./vault-detail-edit-section";
import type { VaultDocument } from "@/types/vault";

interface Props {
  doc: VaultDocument | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onDeleted?: () => void;
}

export function VaultDetailDialog({ doc, open, onOpenChange, onDeleted }: Props) {
  const { t } = useTranslation("vault");
  const { outlinks, backlinks, loading } = useVaultLinks(doc?.agent_id ?? "", doc?.id ?? null);

  const [editMode, setEditMode] = useState(false);
  const [editTitle, setEditTitle] = useState("");
  const [editDocType, setEditDocType] = useState("");
  const [editScope, setEditScope] = useState("");
  const [saving, setSaving] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [linkDialogOpen, setLinkDialogOpen] = useState(false);
  const [previewOpen, setPreviewOpen] = useState(false);

  const { content: fileContent, loading: contentLoading, error: contentError } = useVaultFileContent(
    previewOpen && doc ? doc.path : null,
  );
  const { update } = useUpdateDocument(doc?.agent_id ?? "", doc?.id ?? "");
  const { remove: removeDoc } = useDeleteDocument(doc?.agent_id ?? "", doc?.id ?? "");

  if (!doc) return null;

  const startEdit = () => {
    setEditTitle(doc.title);
    setEditDocType(doc.doc_type);
    setEditScope(doc.scope);
    setEditMode(true);
  };

  const cancelEdit = () => {
    setEditMode(false);
    setConfirmDelete(false);
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      await update({ title: editTitle, doc_type: editDocType, scope: editScope });
      setEditMode(false);
    } catch {
      // toasted in hook
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    setSaving(true);
    try {
      await removeDoc();
      onDeleted?.();
      onOpenChange(false);
    } catch {
      // toasted in hook
    } finally {
      setSaving(false);
      setConfirmDelete(false);
    }
  };

  return (
    <>
      <Dialog open={open} onOpenChange={(v) => { if (!saving) { cancelEdit(); onOpenChange(v); } }}>
        <DialogContent className="sm:max-w-2xl max-sm:inset-0 max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <div className="flex items-start justify-between gap-2 pr-6">
              <DialogTitle className="truncate flex-1">
                {editMode ? (
                  <Input
                    value={editTitle}
                    onChange={(e) => setEditTitle(e.target.value)}
                    className="text-base md:text-sm font-semibold"
                    autoFocus
                  />
                ) : (
                  doc.title || doc.path
                )}
              </DialogTitle>
              {!editMode && (
                <Button variant="ghost" size="xs" className="h-7 w-7 p-0 shrink-0" onClick={startEdit}>
                  <Pencil className="h-3.5 w-3.5" />
                </Button>
              )}
            </div>
          </DialogHeader>

          <div className="space-y-4 text-sm">
            {/* Metadata grid */}
            <div className="grid grid-cols-2 gap-2 text-xs">
              <div>
                <span className="text-muted-foreground">{t("fields.path")}:</span>
                <p className="font-mono truncate" title={doc.path}>{doc.path}</p>
              </div>
              <div>
                <span className="text-muted-foreground">{t("fields.docType")}:</span>
                {editMode
                  ? <DocTypeSelect value={editDocType} onChange={setEditDocType} t={t} />
                  : <p>{t(`type.${doc.doc_type}`)}</p>}
              </div>
              <div>
                <span className="text-muted-foreground">{t("fields.scope")}:</span>
                {editMode
                  ? <ScopeSelect value={editScope} onChange={setEditScope} t={t} />
                  : <p>{t(`scope.${doc.scope}`)}</p>}
              </div>
              <div>
                <span className="text-muted-foreground">Hash:</span>
                <p className="font-mono truncate" title={doc.content_hash}>{doc.content_hash.slice(0, 12)}...</p>
              </div>
            </div>

            {/* Content preview (collapsible, lazy-loaded) */}
            <button
              type="button"
              className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors w-full text-left"
              onClick={() => setPreviewOpen(!previewOpen)}
            >
              {previewOpen ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
              {t("detail.contentPreview")}
            </button>
            {previewOpen && (
              <div className="rounded-md border bg-muted/50 p-3 max-h-[300px] overflow-y-auto">
                {contentLoading ? (
                  <div className="h-[60px] animate-pulse rounded bg-muted" />
                ) : contentError ? (
                  <p className="text-xs text-muted-foreground italic">{t("detail.fileNotFound")}</p>
                ) : fileContent ? (
                  <MarkdownRenderer content={fileContent} className="text-sm" />
                ) : (
                  <p className="text-xs text-muted-foreground italic">{t("detail.emptyContent")}</p>
                )}
              </div>
            )}

            {/* Edit / delete controls */}
            {editMode && (
              <VaultEditControls
                saving={saving}
                confirmDelete={confirmDelete}
                onSave={handleSave}
                onCancel={cancelEdit}
                onDeleteRequest={() => setConfirmDelete(true)}
                onDeleteConfirm={handleDelete}
                onDeleteCancel={() => setConfirmDelete(false)}
                t={t}
              />
            )}

            {/* Links */}
            {loading ? (
              <div className="h-[60px] animate-pulse rounded-md bg-muted" />
            ) : (
              <>
                <div className="space-y-1">
                  <div className="flex items-center justify-between">
                    <h4 className="text-xs font-medium text-muted-foreground">
                      {t("detail.outlinks")} ({outlinks.length})
                    </h4>
                    <Button variant="ghost" size="xs" className="h-6 px-1 gap-1" onClick={() => setLinkDialogOpen(true)}>
                      <Plus className="h-3 w-3" />
                      <span className="text-xs">{t("addLink")}</span>
                    </Button>
                  </div>
                  {outlinks.length === 0 ? (
                    <p className="text-xs text-muted-foreground">{t("detail.noLinks")}</p>
                  ) : (
                    <div className="flex flex-wrap gap-1">
                      {outlinks.map((l) => (
                        <LinkBadge key={l.id} link={l} agentId={doc.agent_id} t={t} />
                      ))}
                    </div>
                  )}
                </div>

                <div className="space-y-1">
                  <h4 className="text-xs font-medium text-muted-foreground">
                    {t("detail.backlinks")} ({backlinks.length})
                  </h4>
                  {backlinks.length === 0 ? (
                    <p className="text-xs text-muted-foreground">{t("detail.noLinks")}</p>
                  ) : (
                    <div className="flex flex-wrap gap-1">
                      {backlinks.map((l) => (
                        <Badge key={l.id} variant="secondary" className="text-xs">
                          {l.link_type}: {l.from_doc_id.slice(0, 8)}
                        </Badge>
                      ))}
                    </div>
                  )}
                </div>
              </>
            )}

            {/* Metadata JSON */}
            {doc.metadata && Object.keys(doc.metadata).length > 0 && (
              <div className="space-y-1">
                <h4 className="text-xs font-medium text-muted-foreground">{t("detail.metadata")}</h4>
                <pre className="text-xs bg-muted p-2 rounded overflow-x-auto max-h-[120px]">
                  {JSON.stringify(doc.metadata, null, 2)}
                </pre>
              </div>
            )}
          </div>
        </DialogContent>
      </Dialog>

      {linkDialogOpen && (
        <VaultLinkDialog
          agentId={doc.agent_id}
          fromDoc={doc}
          open={linkDialogOpen}
          onOpenChange={setLinkDialogOpen}
        />
      )}
    </>
  );
}
