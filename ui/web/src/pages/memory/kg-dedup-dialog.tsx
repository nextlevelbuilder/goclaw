import { useState } from "react";
import { Merge, X, Check, ScanSearch } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";
import { useTranslation } from "react-i18next";
import { useKGDedup } from "./hooks/use-knowledge-graph";
import type { KGDedupCandidate } from "@/types/knowledge-graph";

interface KGDedupDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  agentId: string;
  userId?: string;
}

export function KGDedupDialog({ open, onOpenChange, agentId, userId }: KGDedupDialogProps) {
  const { t } = useTranslation("memory");
  const { candidates, loading, scan, merge, dismiss } = useKGDedup(agentId, userId);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [scanning, setScanning] = useState(false);

  const handleMerge = async (candidate: KGDedupCandidate) => {
    setActionLoading(candidate.id);
    try {
      // Keep entity with higher confidence as target
      const target = candidate.entity_a.confidence >= candidate.entity_b.confidence
        ? candidate.entity_a : candidate.entity_b;
      const source = target.id === candidate.entity_a.id
        ? candidate.entity_b : candidate.entity_a;
      await merge(target.id, source.id);
    } finally {
      setActionLoading(null);
    }
  };

  const handleDismiss = async (candidateId: string) => {
    setActionLoading(candidateId);
    try {
      await dismiss(candidateId);
    } finally {
      setActionLoading(null);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-sm:inset-0 sm:max-w-2xl max-h-[80dvh] flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Merge className="h-4 w-4" />
            {t("kg.dedup.title")}
          </DialogTitle>
          <DialogDescription>{t("kg.dedup.description")}</DialogDescription>
        </DialogHeader>

        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            className="gap-1.5"
            disabled={scanning}
            onClick={async () => {
              setScanning(true);
              try { await scan(); } finally { setScanning(false); }
            }}
          >
            <ScanSearch className={"h-3.5 w-3.5" + (scanning ? " animate-spin" : "")} />
            {scanning ? t("kg.dedup.scanning") : t("kg.dedup.scan")}
          </Button>
        </div>

        <div className="flex-1 min-h-0 overflow-y-auto space-y-3">
          {loading ? (
            <p className="text-sm text-muted-foreground py-8 text-center">{t("kg.entity.loading")}</p>
          ) : candidates.length === 0 ? (
            <p className="text-sm text-muted-foreground py-8 text-center">{t("kg.dedup.noCandidates")}</p>
          ) : (
            candidates.map((c) => (
              <CandidateCard
                key={c.id}
                candidate={c}
                loading={actionLoading === c.id}
                disabled={!!actionLoading}
                onMerge={() => handleMerge(c)}
                onDismiss={() => handleDismiss(c.id)}
                t={t}
              />
            ))
          )}
        </div>

        {candidates.length > 0 && (
          <p className="text-xs text-muted-foreground text-center">
            {t("kg.dedup.count", { count: candidates.length })}
          </p>
        )}
      </DialogContent>
    </Dialog>
  );
}

function CandidateCard({
  candidate,
  loading,
  disabled,
  onMerge,
  onDismiss,
  t,
}: {
  candidate: KGDedupCandidate;
  loading: boolean;
  disabled: boolean;
  onMerge: () => void;
  onDismiss: () => void;
  t: (key: string, opts?: Record<string, unknown>) => string;
}) {
  const similarity = Math.round(candidate.similarity * 100);
  const a = candidate.entity_a;
  const b = candidate.entity_b;

  return (
    <div className="rounded-lg border p-3 space-y-2">
      {/* Similarity badge */}
      <div className="flex items-center justify-between">
        <Badge variant={similarity >= 98 ? "default" : "secondary"}>
          {similarity}% {t("kg.dedup.similar")}
        </Badge>
        <div className="flex gap-1">
          <Button
            variant="outline"
            size="sm"
            className="h-7 gap-1 text-xs"
            disabled={disabled}
            onClick={onDismiss}
          >
            <X className="h-3 w-3" /> {t("kg.dedup.dismiss")}
          </Button>
          <Button
            size="sm"
            className="h-7 gap-1 text-xs"
            disabled={disabled}
            onClick={onMerge}
          >
            {loading ? (
              <span className="animate-spin">...</span>
            ) : (
              <Check className="h-3 w-3" />
            )}
            {t("kg.dedup.merge")}
          </Button>
        </div>
      </div>

      {/* Side-by-side entities */}
      <div className="grid grid-cols-2 gap-2">
        <EntityCard entity={a} label="A" />
        <EntityCard entity={b} label="B" />
      </div>
    </div>
  );
}

function EntityCard({ entity, label }: { entity: KGDedupCandidate["entity_a"]; label: string }) {
  return (
    <div className="rounded-md bg-muted/50 p-2 space-y-1">
      <div className="flex items-center gap-1.5">
        <span className="text-[10px] font-mono text-muted-foreground">{label}</span>
        <span className="text-sm font-medium truncate">{entity.name}</span>
      </div>
      <div className="flex items-center gap-1">
        <Badge variant="outline" className="text-[10px] h-4">{entity.entity_type}</Badge>
        <span className="text-[10px] text-muted-foreground">{Math.round(entity.confidence * 100)}%</span>
      </div>
      {entity.description && (
        <p className="text-xs text-muted-foreground line-clamp-2">{entity.description}</p>
      )}
      <p className="font-mono text-[10px] text-muted-foreground/60 truncate">{entity.external_id}</p>
    </div>
  );
}
