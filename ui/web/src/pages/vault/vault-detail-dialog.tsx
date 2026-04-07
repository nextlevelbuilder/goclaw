import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { useVaultLinks } from "./hooks/use-vault";
import type { VaultDocument } from "@/types/vault";

interface Props {
  doc: VaultDocument | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function VaultDetailDialog({ doc, open, onOpenChange }: Props) {
  const { t } = useTranslation("vault");
  const { outlinks, backlinks, loading } = useVaultLinks(doc?.agent_id ?? "", doc?.id ?? null);

  if (!doc) return null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg max-sm:inset-0">
        <DialogHeader>
          <DialogTitle className="truncate">{doc.title || doc.path}</DialogTitle>
        </DialogHeader>

        <div className="space-y-4 text-sm">
          {/* Metadata */}
          <div className="grid grid-cols-2 gap-2 text-xs">
            <div>
              <span className="text-muted-foreground">{t("columns.path")}:</span>
              <p className="font-mono truncate" title={doc.path}>{doc.path}</p>
            </div>
            <div>
              <span className="text-muted-foreground">{t("columns.type")}:</span>
              <p>{t(`type.${doc.doc_type}`)}</p>
            </div>
            <div>
              <span className="text-muted-foreground">{t("columns.scope")}:</span>
              <p>{t(`scope.${doc.scope}`)}</p>
            </div>
            <div>
              <span className="text-muted-foreground">Hash:</span>
              <p className="font-mono truncate" title={doc.content_hash}>{doc.content_hash.slice(0, 12)}...</p>
            </div>
          </div>

          {/* Links */}
          {loading ? (
            <div className="h-[60px] animate-pulse rounded-md bg-muted" />
          ) : (
            <>
              <div className="space-y-1">
                <h4 className="text-xs font-medium text-muted-foreground">{t("detail.outlinks")} ({outlinks.length})</h4>
                {outlinks.length === 0 ? (
                  <p className="text-xs text-muted-foreground">{t("detail.noLinks")}</p>
                ) : (
                  <div className="flex flex-wrap gap-1">
                    {outlinks.map((l) => (
                      <Badge key={l.id} variant="secondary" className="text-xs">
                        {l.link_type}: {l.to_doc_id.slice(0, 8)}
                      </Badge>
                    ))}
                  </div>
                )}
              </div>
              <div className="space-y-1">
                <h4 className="text-xs font-medium text-muted-foreground">{t("detail.backlinks")} ({backlinks.length})</h4>
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
  );
}
