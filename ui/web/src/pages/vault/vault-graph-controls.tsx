import type { RefObject } from "react";
import { ZoomIn, ZoomOut, Maximize2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useTranslation } from "react-i18next";

const NODE_LIMIT_OPTIONS = [100, 200, 300, 500] as const;

interface Props {
  docCount: number;
  linkCount: number;
  nodeLimit: number;
  isLimited: boolean;
  zoomDisplayRef: RefObject<HTMLSpanElement | null>;
  onNodeLimitChange: (limit: number) => void;
  onZoomIn: () => void;
  onZoomOut: () => void;
  onFitToView: () => void;
}

export function VaultGraphControls({
  docCount, linkCount, nodeLimit, isLimited, zoomDisplayRef,
  onNodeLimitChange, onZoomIn, onZoomOut, onFitToView,
}: Props) {
  const { t } = useTranslation("vault");

  return (
    <div className="flex items-center gap-3 px-3 py-1.5 border-t text-2xs text-muted-foreground">
      <span>{t("graphDocs", { count: docCount, defaultValue: "{{count}} docs" })}</span>
      <span>{t("graphLinks", { count: linkCount, defaultValue: "{{count}} links" })}</span>
      {isLimited && (
        <span>· {t("graphLimitNote", { limit: nodeLimit, total: docCount, defaultValue: "showing {{limit}} of {{total}}" })}</span>
      )}
      <div className="flex-1" />
      <div className="flex items-center gap-1">
        <Button variant="ghost" size="sm" className="h-6 px-1.5" onClick={onZoomOut}>
          <ZoomOut className="h-3 w-3" />
        </Button>
        <span ref={zoomDisplayRef} className="w-9 text-center">100%</span>
        <Button variant="ghost" size="sm" className="h-6 px-1.5" onClick={onZoomIn}>
          <ZoomIn className="h-3 w-3" />
        </Button>
      </div>
      <select
        value={nodeLimit}
        onChange={(e) => onNodeLimitChange(Number(e.target.value))}
        className="h-5 rounded border bg-background px-1 text-base md:text-2xs"
      >
        {NODE_LIMIT_OPTIONS.map((n) => (
          <option key={n} value={n}>{n} nodes</option>
        ))}
      </select>
      <Button variant="ghost" size="sm" className="h-6 px-1.5" onClick={onFitToView}>
        <Maximize2 className="h-3 w-3" />
      </Button>
    </div>
  );
}
