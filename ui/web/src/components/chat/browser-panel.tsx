import { useState, useRef, useCallback } from "react";
import { GripVertical, Globe, X, Lock, ArrowLeft, ArrowRight, RotateCw } from "lucide-react";
import { BrowserViewer } from "@/pages/browser/browser-viewer";
import { cn } from "@/lib/utils";
import type { BrowserTab } from "@/pages/browser/hooks/use-browser-status";

interface BrowserPanelProps {
  targetId: string;
  tabTitle?: string | null;
  tabUrl?: string | null;
  tabs?: BrowserTab[];
  onClose: () => void;
  onSwitchTab?: (targetId: string, title?: string, url?: string) => void;
}

const MIN_WIDTH = 320;
const MAX_RATIO = 0.65;

export function BrowserPanel({ targetId, tabTitle, tabUrl, tabs, onClose, onSwitchTab }: BrowserPanelProps) {
  const [error, setError] = useState<string | null>(null);
  const [width, setWidth] = useState<number | null>(null);
  const [isDragging, setIsDragging] = useState(false);
  const wrapperRef = useRef<HTMLDivElement>(null);

  // Suppress disconnect errors during tab switches
  const switchingRef = useRef(false);

  const handleDisconnect = useCallback(() => {
    if (switchingRef.current) return;
    setError("Connection lost. Click Retry to reconnect.");
  }, []);

  const handleRetry = useCallback(() => {
    setError(null);
  }, []);

  const handleSwitchTab = useCallback((tid: string, title?: string, url?: string) => {
    if (tid !== targetId) {
      switchingRef.current = true;
      setTimeout(() => { switchingRef.current = false; }, 500);
      setError(null);
      onSwitchTab?.(tid, title, url);
    }
  }, [targetId, onSwitchTab]);

  // Horizontal resize handle
  const handlePointerDown = useCallback((e: React.PointerEvent) => {
    e.preventDefault();
    setIsDragging(true);
    const startX = e.clientX;
    const startW = wrapperRef.current?.offsetWidth ?? 500;

    const onMove = (ev: PointerEvent) => {
      const parent = wrapperRef.current?.parentElement;
      const maxW = parent ? parent.offsetWidth * MAX_RATIO : 1200;
      // Dragging left = increase width (handle is on the left side of the panel)
      setWidth(Math.min(maxW, Math.max(MIN_WIDTH, startW + (startX - ev.clientX))));
    };

    const onUp = () => {
      setIsDragging(false);
      document.removeEventListener("pointermove", onMove);
      document.removeEventListener("pointerup", onUp);
    };

    document.addEventListener("pointermove", onMove);
    document.addEventListener("pointerup", onUp);
  }, []);

  const activeTab = tabs?.find((t) => t.targetId === targetId);
  const displayUrl = activeTab?.url || tabUrl || "";
  const displayTitle = activeTab?.title || tabTitle || "";

  const panelStyle = width != null ? { width: `${width}px` } : undefined;

  return (
    <div className="flex h-full shrink-0">
      {/* Resize handle — left edge */}
      <div
        onPointerDown={handlePointerDown}
        className="group flex w-1.5 shrink-0 cursor-col-resize items-center justify-center border-l border-border select-none touch-none hover:bg-accent/50 active:bg-accent transition-colors"
      >
        <GripVertical className="h-4 w-4 text-muted-foreground/30 group-hover:text-muted-foreground transition-colors" />
      </div>

      {/* Panel content */}
      <div
        ref={wrapperRef}
        className={cn(
          "flex flex-col bg-background",
          width == null && "w-[45vw] min-w-[320px]",
        )}
        style={panelStyle}
      >
        {/* Header: BROWSER label + close */}
        <div className="flex shrink-0 items-center justify-between border-b px-3 py-1.5">
          <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Browser</span>
          <button
            type="button"
            onClick={onClose}
            className="rounded-md p-1 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        </div>

        {/* Tab bar */}
        {tabs && tabs.length > 0 && (
          <div className="shrink-0 flex items-center gap-0.5 overflow-x-auto border-b bg-muted/30 px-1 py-0.5">
            {tabs.map((tab) => (
              <button
                key={tab.targetId}
                type="button"
                onClick={() => handleSwitchTab(tab.targetId, tab.title, tab.url)}
                className={cn(
                  "group flex min-w-0 max-w-[180px] items-center gap-1.5 rounded-md px-2 py-1 text-xs transition-colors",
                  tab.targetId === targetId
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:bg-background/60 hover:text-foreground",
                )}
              >
                <Globe className="h-3 w-3 shrink-0" />
                <span className="truncate">
                  {tab.title || (() => { try { return new URL(tab.url || "about:blank").hostname; } catch { return "New Tab"; } })()}
                </span>
              </button>
            ))}
          </div>
        )}

        {/* URL bar */}
        {displayUrl && (
          <div className="shrink-0 flex items-center gap-1.5 border-b px-2 py-1">
            <div className="flex items-center gap-0.5 text-muted-foreground">
              <ArrowLeft className="h-3 w-3 opacity-30" />
              <ArrowRight className="h-3 w-3 opacity-30" />
              <RotateCw className="h-3 w-3 opacity-30" />
            </div>
            <div className="flex min-w-0 flex-1 items-center gap-1 rounded-md bg-muted/50 px-2 py-0.5">
              {displayUrl.startsWith("https") && (
                <Lock className="h-2.5 w-2.5 shrink-0 text-muted-foreground" />
              )}
              <span className="truncate text-xs text-muted-foreground font-mono">{displayUrl}</span>
            </div>
          </div>
        )}

        {/* Browser viewer */}
        <div className="relative flex flex-1 flex-col overflow-hidden min-h-0">
          {error ? (
            <div className="flex flex-1 flex-col items-center justify-center gap-3">
              <p className="text-sm text-destructive">{error}</p>
              <div className="flex gap-2">
                {!error.includes("not found") && (
                  <button
                    type="button"
                    onClick={handleRetry}
                    className="rounded-md border px-3 py-1.5 text-sm hover:bg-accent"
                  >
                    Retry
                  </button>
                )}
                <button
                  type="button"
                  onClick={onClose}
                  className="rounded-md border px-3 py-1.5 text-sm text-muted-foreground hover:bg-accent"
                >
                  Close
                </button>
              </div>
            </div>
          ) : (
            <BrowserViewer
              targetId={targetId}
              initialMode="takeover"
              onClose={onClose}
              onDisconnect={handleDisconnect}
              tabTitle={displayTitle || undefined}
              tabUrl={displayUrl || undefined}
              className="h-full"
              showHeader={false}
            />
          )}

          {/* Overlay blocks canvas from stealing pointer events during drag */}
          {isDragging && (
            <div className="absolute inset-0 z-50 cursor-col-resize" />
          )}
        </div>
      </div>
    </div>
  );
}
