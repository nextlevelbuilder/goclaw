import { useRef, useState, useCallback } from "react";
import { X, Monitor, MousePointer, Eye, Maximize2, Minimize2 } from "lucide-react";
import { useScreencast } from "./hooks/use-screencast";
import { cn } from "@/lib/utils";

interface BrowserViewerProps {
  /** Token-based auth (for shared/external viewers) */
  token?: string;
  /** Direct targetId — uses authenticated WS (for chat panel) */
  targetId?: string;
  initialMode?: "view" | "takeover";
  className?: string;
  onClose?: () => void;
  onDisconnect?: () => void;
  showHeader?: boolean;
  tabTitle?: string;
  tabUrl?: string;
}

export function BrowserViewer({
  token,
  targetId,
  initialMode = "view",
  className,
  onClose,
  onDisconnect,
  showHeader = true,
  tabTitle,
  tabUrl,
}: BrowserViewerProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const [mode, setMode] = useState<"view" | "takeover">(initialMode);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  const { connected, error, fps, resolution } = useScreencast({
    token,
    targetId,
    canvasRef,
    mode,
    onDisconnect,
  });

  const toggleFullscreen = useCallback(() => {
    if (!containerRef.current) return;
    if (!document.fullscreenElement) {
      containerRef.current.requestFullscreen().then(() => setIsFullscreen(true));
    } else {
      document.exitFullscreen().then(() => setIsFullscreen(false));
    }
  }, []);

  return (
    <div
      ref={containerRef}
      className={cn(
        "flex flex-col bg-background",
        className,
      )}
    >
      {/* Header bar */}
      {showHeader && (
        <div className="flex shrink-0 items-center gap-2 border-b px-3 py-2">
          <Monitor className="h-4 w-4 text-muted-foreground" />
          <div className="flex min-w-0 flex-1 flex-col">
            {tabTitle && (
              <span className="truncate text-sm font-medium">{tabTitle}</span>
            )}
            {tabUrl && (
              <span className="truncate text-xs text-muted-foreground font-mono">
                {tabUrl}
              </span>
            )}
          </div>

          {/* Status */}
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <span
              className={cn(
                "inline-block h-2 w-2 rounded-full",
                connected ? "bg-green-500" : "bg-red-500",
              )}
            />
            {connected && (
              <>
                <span className="tabular-nums">{fps} fps</span>
                {resolution.w > 0 && (
                  <span className="tabular-nums">
                    {resolution.w}x{resolution.h}
                  </span>
                )}
              </>
            )}
          </div>

          {/* Mode toggle */}
          <div className="flex items-center rounded-md border">
            <button
              type="button"
              onClick={() => setMode("view")}
              className={cn(
                "flex items-center gap-1 px-2 py-1 text-xs rounded-l-md",
                mode === "view"
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:bg-accent",
              )}
              title="View only"
            >
              <Eye className="h-3 w-3" />
            </button>
            <button
              type="button"
              onClick={() => setMode("takeover")}
              className={cn(
                "flex items-center gap-1 px-2 py-1 text-xs rounded-r-md",
                mode === "takeover"
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:bg-accent",
              )}
              title="Take control"
            >
              <MousePointer className="h-3 w-3" />
            </button>
          </div>

          <button
            type="button"
            onClick={toggleFullscreen}
            className="rounded-md p-1.5 text-muted-foreground hover:bg-accent"
            title="Toggle fullscreen"
          >
            {isFullscreen ? (
              <Minimize2 className="h-4 w-4" />
            ) : (
              <Maximize2 className="h-4 w-4" />
            )}
          </button>

          {onClose && (
            <button
              type="button"
              onClick={onClose}
              className="rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
            >
              <X className="h-4 w-4" />
            </button>
          )}
        </div>
      )}

      {/* Canvas area */}
      <div className="relative flex flex-1 items-center justify-center overflow-hidden bg-neutral-900 min-h-0">
        {!connected && (
          <div className="absolute inset-0 z-10 flex items-center justify-center bg-neutral-900/80">
            {error ? (
              <div className="flex flex-col items-center gap-3 text-white">
                <span className="text-sm text-red-400">{error}</span>
                {onClose && (
                  <button
                    type="button"
                    onClick={onClose}
                    className="rounded-md border border-white/20 px-3 py-1.5 text-sm hover:bg-white/10"
                  >
                    Close
                  </button>
                )}
              </div>
            ) : (
              <div className="flex flex-col items-center gap-2 text-white">
                <div className="h-5 w-5 animate-spin rounded-full border-2 border-white border-t-transparent" />
                <span className="text-sm">Connecting...</span>
              </div>
            )}
          </div>
        )}
        <canvas
          ref={canvasRef}
          className={cn(
            "max-h-full max-w-full object-contain",
            mode === "takeover" ? "cursor-crosshair" : "cursor-default",
          )}
          width={1280}
          height={720}
        />
      </div>
    </div>
  );
}
