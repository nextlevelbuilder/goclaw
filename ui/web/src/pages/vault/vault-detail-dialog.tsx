import { useState, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Pencil, Plus, FileText, Link2, FileQuestion } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { MarkdownRenderer } from "@/components/shared/markdown-renderer";
import { useVaultLinks, useVaultFileContent, useVaultImageUrl, useUpdateDocument, useDeleteDocument } from "./hooks/use-vault";
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
  const { outlinks, backlinks, docNames, loading } = useVaultLinks(doc?.id ?? null);

  const [editMode, setEditMode] = useState(false);
  const [editTitle, setEditTitle] = useState("");
  const [editDocType, setEditDocType] = useState("");
  const [editScope, setEditScope] = useState("");
  const [saving, setSaving] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [linkDialogOpen, setLinkDialogOpen] = useState(false);

  // Determine if this is an image that can be rendered.
  const isImage = useMemo(() => {
    const mime = doc?.metadata?.mime_type as string | undefined;
    if (mime?.startsWith("image/")) return true;
    const ext = doc?.path.split(".").pop()?.toLowerCase() ?? "";
    return ["png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "ico"].includes(ext);
  }, [doc?.metadata, doc?.path]);

  const isMedia = doc?.doc_type === "media";
  // Phase 01: `document` docType covers PDFs / office docs — also binary, so
  // avoid the text-content fetch but still re-use the media image pathway
  // only for true media (images stay rendered, documents show a placeholder).
  const isBinary = isMedia || doc?.doc_type === "document";

  // Only fetch text content for non-binary files (media/document are binary and cannot be rendered as markdown).
  const { content: fileContent, loading: contentLoading, error: contentError } = useVaultFileContent(
    open && doc && !isBinary ? doc.path : null,
  );
  // Fetch image as authenticated blob URL for <img> rendering.
  const { url: imageUrl, error: imageError } = useVaultImageUrl(open && isMedia && isImage && doc ? doc.path : null);
  const { update } = useUpdateDocument(doc?.id ?? "");
  const { remove: removeDoc } = useDeleteDocument(doc?.id ?? "");

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

  const linkCount = outlinks.length + backlinks.length;

  return (
    <>
      <Dialog open={open} onOpenChange={(v) => { if (!saving) { cancelEdit(); onOpenChange(v); } }}>
        <DialogContent className="sm:max-w-4xl max-sm:inset-0 max-h-[90vh] flex flex-col overflow-hidden">
          {/* Header: title + badges */}
          <DialogHeader>
            <div className="flex items-start justify-between gap-2 pr-6">
              <div className="min-w-0 flex-1">
                {editMode ? (
                  <Input
                    value={editTitle}
                    onChange={(e) => setEditTitle(e.target.value)}
                    className="text-base md:text-sm font-semibold"
                    autoFocus
                  />
                ) : (
                  <DialogTitle className="truncate">{doc.title || doc.path}</DialogTitle>
                )}

                {/* Path + badges */}
                <div className="flex items-center gap-2 mt-1 flex-wrap">
                  <span className="text-xs-plus font-mono text-muted-foreground truncate max-w-[400px] direction-rtl text-left" dir="rtl" title={doc.path}>
                    {doc.path}
                  </span>
                  {editMode ? (
                    <>
                      <DocTypeSelect value={editDocType} onChange={setEditDocType} t={t} />
                      <ScopeSelect value={editScope} onChange={setEditScope} t={t} />
                    </>
                  ) : (
                    <>
                      <Badge variant="secondary" className="text-2xs px-1.5 py-0">
                        {t(`type.${doc.doc_type}`)}
                      </Badge>
                      <Badge variant="outline" className="text-2xs px-1.5 py-0">
                        {t(`scope.${doc.scope}`)}
                      </Badge>
                    </>
                  )}
                </div>
              </div>

              {!editMode && (
                <Button variant="ghost" size="xs" className="h-7 w-7 p-0 shrink-0" onClick={startEdit}>
                  <Pencil className="h-3.5 w-3.5" />
                </Button>
              )}
            </div>
          </DialogHeader>

          {/* Edit controls (when editing) */}
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

          {/* Content preview — always visible, scrollable */}
          <div className="flex-1 min-h-0 overflow-y-auto rounded-md border bg-muted/30 p-4">
            {isBinary ? (
              isMedia && isImage ? (
                <div className="flex items-center justify-center">
                  {imageUrl ? (
                    <img
                      src={imageUrl}
                      alt={doc.title || doc.path}
                      className="max-w-full max-h-[60vh] object-contain rounded"
                    />
                  ) : imageError ? (
                    <div className="flex items-center gap-2 text-sm text-muted-foreground">
                      <FileText className="h-4 w-4" />
                      <span>{t("detail.fileNotFound")}</span>
                    </div>
                  ) : (
                    <div className="h-32 w-32 animate-pulse rounded bg-muted" />
                  )}
                </div>
              ) : (
                <div className="flex flex-col items-center justify-center gap-3 py-8 text-muted-foreground">
                  <FileQuestion className="h-10 w-10" />
                  <div className="text-center space-y-1">
                    <p className="text-sm font-medium">{t("detail.binaryFile")}</p>
                    <p className="text-xs">{(doc.metadata?.mime_type as string) || doc.path.split(".").pop()?.toUpperCase()}</p>
                  </div>
                  {doc.summary && (
                    <p className="text-xs text-center max-w-md mt-2">{doc.summary}</p>
                  )}
                </div>
              )
            ) : contentLoading ? (
              <div className="space-y-2">
                <div className="h-4 w-3/4 animate-pulse rounded bg-muted" />
                <div className="h-4 w-1/2 animate-pulse rounded bg-muted" />
                <div className="h-4 w-5/6 animate-pulse rounded bg-muted" />
              </div>
            ) : contentError ? (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <FileText className="h-4 w-4" />
                <span>{t("detail.fileNotFound")}</span>
              </div>
            ) : fileContent ? (
              <MarkdownRenderer
                content={fileContent}
                className="text-sm [&_h1]:text-lg [&_h1]:font-semibold [&_h2]:text-base [&_h2]:font-semibold [&_h3]:text-sm [&_h3]:font-semibold [&_p]:text-sm [&_li]:text-sm [&_code]:text-xs [&_pre]:overflow-x-auto [&_pre]:max-w-full [&_img]:max-w-full"
              />
            ) : (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <FileText className="h-4 w-4" />
                <span>{t("detail.emptyContent")}</span>
              </div>
            )}
          </div>

          {/* Footer: links + hash */}
          <div className="space-y-3 pt-1">
            {/* Links */}
            {loading ? (
              <div className="h-8 animate-pulse rounded-md bg-muted" />
            ) : (
              <div className="flex items-center gap-4 text-xs">
                <div className="flex items-center gap-1.5 text-muted-foreground shrink-0">
                  <Link2 className="h-3.5 w-3.5" />
                  <span>{t("detail.outlinks")} ({outlinks.length})</span>
                </div>
                {outlinks.length > 0 ? (
                  <div className="flex items-center gap-1 flex-1 min-w-0 overflow-x-auto scrollbar-thin">
                    {outlinks.map((l) => (
                      <LinkBadge key={l.id} link={l} docNames={docNames} t={t} />
                    ))}
                  </div>
                ) : (
                  <span className="text-xs text-muted-foreground">{t("detail.noLinks")}</span>
                )}
              </div>
            )}

            {!loading && backlinks.length > 0 && (
              <div className="flex items-center gap-4 text-xs">
                <span className="text-muted-foreground shrink-0">{t("detail.backlinks")} ({backlinks.length})</span>
                <div className="flex items-center gap-1 min-w-0 overflow-x-auto scrollbar-thin">
                  {backlinks.map((l) => (
                    <Badge key={l.from_doc_id} variant="secondary" className="text-xs shrink-0" title={l.path}>
                      {l.title || l.from_doc_id.slice(0, 8)}
                    </Badge>
                  ))}
                </div>
              </div>
            )}

            {/* Bottom bar: hash + add link */}
            <div className="flex items-center justify-between text-2xs text-muted-foreground border-t pt-2">
              <div className="flex items-center gap-3">
                <span className="font-mono">SHA-256: {doc.content_hash.length > 16 ? `${doc.content_hash.slice(0, 8)}...${doc.content_hash.slice(-8)}` : doc.content_hash}</span>
                {linkCount > 0 && <span>{linkCount} link{linkCount !== 1 ? "s" : ""}</span>}
              </div>
              <Button variant="ghost" size="xs" className="h-6 px-1.5 gap-1" onClick={() => setLinkDialogOpen(true)}>
                <Plus className="h-3 w-3" />
                <span className="text-xs">{t("addLink")}</span>
              </Button>
            </div>

            {/* Metadata JSON (if present) */}
            {doc.metadata && Object.keys(doc.metadata).length > 0 && (
              <details className="text-xs">
                <summary className="text-muted-foreground cursor-pointer hover:text-foreground">
                  {t("detail.metadata")}
                </summary>
                <pre className="bg-muted p-2 rounded overflow-x-auto max-h-[120px] mt-1 text-xs-plus">
                  {JSON.stringify(doc.metadata, null, 2)}
                </pre>
              </details>
            )}
          </div>
        </DialogContent>
      </Dialog>

      {linkDialogOpen && (
        <VaultLinkDialog
          agentId={doc.agent_id ?? ""}
          fromDoc={doc}
          open={linkDialogOpen}
          onOpenChange={setLinkDialogOpen}
        />
      )}
    </>
  );
}
