import { useEffect, useRef, useState, useCallback, type RefObject } from "react";
import { useAuthStore } from "@/stores/use-auth-store";

export interface UseScreencastOptions {
  /** Token-based WS URL (for shared/external viewers) */
  token?: string | null;
  /** Direct targetId — uses authenticated /browser/screencast/{targetId} WS (for chat panel) */
  targetId?: string | null;
  canvasRef: RefObject<HTMLCanvasElement | null>;
  mode: "view" | "takeover";
  onDisconnect?: () => void;
}

export interface UseScreencastReturn {
  connected: boolean;
  error: string | null;
  fps: number;
  resolution: { w: number; h: number };
  disconnect: () => void;
}

/** Max auto-retry attempts before giving up */
const MAX_RETRIES = 10;
/** Base delay between retries (increases with backoff, capped at 5s) */
const RETRY_BASE_MS = 1500;

export function useScreencast({
  token,
  targetId,
  canvasRef,
  mode,
  onDisconnect,
}: UseScreencastOptions): UseScreencastReturn {
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fps, setFps] = useState(0);
  const [resolution, setResolution] = useState({ w: 0, h: 0 });
  const wsRef = useRef<WebSocket | null>(null);
  const frameCountRef = useRef(0);
  const fpsIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  // Track native image dimensions in a ref so input handlers (attached once)
  // always read the latest value without needing to re-attach.
  const natRef = useRef({ w: 1280, h: 720 });
  // Track mode in a ref so input handlers react to the latest mode
  // without triggering a WS reconnect.
  const modeRef = useRef(mode);
  modeRef.current = mode;

  const disconnect = useCallback(() => {
    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }
  }, []);

  // WebSocket connection — depends only on token/targetId, NOT mode.
  // Either token (shared viewer) or targetId (authenticated chat panel) must be set.
  const wsKey = targetId ?? token;
  useEffect(() => {
    if (!wsKey || !canvasRef.current) return;

    const canvas = canvasRef.current;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    // Clear stale frames from previous stream (e.g. tab switch)
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    setConnected(false);
    setError(null);
    natRef.current = { w: 1280, h: 720 };

    let cancelled = false;
    let retryCount = 0;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;

    function connect() {
      if (cancelled) return;

      let hasViewport = false;

      const proto = location.protocol === "https:" ? "wss:" : "ws:";
      const wsPath = targetId
        ? `/browser/screencast/${targetId}`
        : `/browser/live/${token}/ws`;
      const authToken = targetId ? useAuthStore.getState().token : undefined;
      const ws = authToken
        ? new WebSocket(`${proto}//${location.host}${wsPath}`, [authToken])
        : new WebSocket(`${proto}//${location.host}${wsPath}`);
      ws.binaryType = "arraybuffer";
      wsRef.current = ws;

      let wasConnected = false;
      let serverError = false;
      let retryableError = false;

      ws.onopen = () => {
        wasConnected = true;
        retryCount = 0;
        setConnected(true);
        setError(null);
        frameCountRef.current = 0;
      };

      ws.onerror = () => {};

      ws.onclose = (e) => {
        if (cancelled) return;
        setConnected(false);

        if (!wasConnected && !serverError) {
          return;
        }

        // Auto-retry for retryable server errors (page not ready yet)
        if (retryableError && retryCount < MAX_RETRIES) {
          retryCount++;
          const delay = Math.min(RETRY_BASE_MS * Math.pow(1.5, retryCount - 1), 5000);
          setError(`Waiting for browser... (retry ${retryCount}/${MAX_RETRIES})`);
          retryTimer = setTimeout(connect, delay);
          return;
        }

        // Auto-retry on abnormal closure (1006) when never fully connected
        if (e.code === 1006 && !wasConnected && retryCount < MAX_RETRIES) {
          retryCount++;
          const delay = Math.min(RETRY_BASE_MS * Math.pow(1.5, retryCount - 1), 5000);
          setError(`Connecting... (retry ${retryCount}/${MAX_RETRIES})`);
          retryTimer = setTimeout(connect, delay);
          return;
        }

        const cleanClose = e.code === 1000 || e.code === 1001 || e.code === 1005;
        if (e.code === 1006) {
          setError("Session expired or connection failed");
        } else if (e.code === 1008 || e.code === 1011) {
          setError(e.reason || "Connection rejected by server");
        } else if (!cleanClose) {
          setError(e.reason || `Connection closed (code ${e.code})`);
        }
        if (!cleanClose && !serverError) {
          onDisconnect?.();
        }
      };

      ws.onmessage = (e) => {
        if (typeof e.data === "string") {
          try {
            const msg = JSON.parse(e.data);
            if (msg.error) {
              serverError = true;
              if (typeof msg.error === "string" && msg.error.includes("not found")) {
                retryableError = true;
              }
              setError(msg.error);
              ws.close();
            }
            if (msg.viewport) {
              hasViewport = true;
              setResolution({ w: msg.viewport.w, h: msg.viewport.h });
            }
          } catch {
            // not JSON, ignore
          }
          return;
        }
        if (!(e.data instanceof ArrayBuffer)) return;
        frameCountRef.current++;
        const blob = new Blob([e.data], { type: "image/jpeg" });
        const img = new Image();
        img.onload = () => {
          if (canvas.width !== img.width || canvas.height !== img.height) {
            canvas.width = img.width;
            canvas.height = img.height;
            natRef.current = { w: img.width, h: img.height };
            if (!hasViewport) {
              setResolution({ w: img.width, h: img.height });
            }
          }
          ctx!.drawImage(img, 0, 0);
          URL.revokeObjectURL(img.src);
        };
        img.src = URL.createObjectURL(blob);
      };
    }

    // --- Input handlers (attached once, read wsRef/natRef/modeRef) ---
    const sendInput = (data: Record<string, unknown>) => {
      const ws = wsRef.current;
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify(data));
      }
    };

    const xy = (ev: MouseEvent) => {
      const r = canvas.getBoundingClientRect();
      const nat = natRef.current;
      return {
        x: (ev.clientX - r.left) * (nat.w / r.width),
        y: (ev.clientY - r.top) * (nat.h / r.height),
      };
    };

    const onMouseDown = (ev: MouseEvent) => {
      if (modeRef.current !== "takeover") return;
      ev.preventDefault();
      canvas.focus();
      const p = xy(ev);
      sendInput({ type: "mousedown", x: p.x, y: p.y, button: ev.button });
    };

    const onMouseUp = (ev: MouseEvent) => {
      if (modeRef.current !== "takeover") return;
      ev.preventDefault();
      const p = xy(ev);
      sendInput({ type: "mouseup", x: p.x, y: p.y, button: ev.button });
    };

    const onClick = (ev: MouseEvent) => {
      if (modeRef.current !== "takeover") return;
      ev.preventDefault();
      const p = xy(ev);
      sendInput({ type: "click", x: p.x, y: p.y, button: ev.button });
    };

    const onDblClick = (ev: MouseEvent) => {
      if (modeRef.current !== "takeover") return;
      ev.preventDefault();
      const p = xy(ev);
      sendInput({ type: "dblclick", x: p.x, y: p.y, button: ev.button });
    };

    let lastMoveTs = 0;
    const onMouseMove = (ev: MouseEvent) => {
      if (modeRef.current !== "takeover") return;
      const now = Date.now();
      if (now - lastMoveTs < 50) return;
      lastMoveTs = now;
      const p = xy(ev);
      sendInput({ type: "mousemove", x: p.x, y: p.y });
    };

    const onWheel = (ev: WheelEvent) => {
      if (modeRef.current !== "takeover") return;
      ev.preventDefault();
      const p = xy(ev);
      sendInput({ type: "scroll", x: p.x, y: p.y, deltaX: ev.deltaX, deltaY: ev.deltaY });
    };

    const onKeyDown = (ev: KeyboardEvent) => {
      if (modeRef.current !== "takeover") return;
      ev.preventDefault();
      ev.stopPropagation();
      sendInput({
        type: "keydown", key: ev.key, code: ev.code,
        shift: ev.shiftKey, ctrl: ev.ctrlKey, alt: ev.altKey, meta: ev.metaKey,
      });
    };

    const onKeyUp = (ev: KeyboardEvent) => {
      if (modeRef.current !== "takeover") return;
      ev.preventDefault();
      ev.stopPropagation();
      sendInput({
        type: "keyup", key: ev.key, code: ev.code,
        shift: ev.shiftKey, ctrl: ev.ctrlKey, alt: ev.altKey, meta: ev.metaKey,
      });
    };

    const onContextMenu = (ev: MouseEvent) => {
      if (modeRef.current === "takeover") ev.preventDefault();
    };

    canvas.addEventListener("mousedown", onMouseDown);
    canvas.addEventListener("mouseup", onMouseUp);
    canvas.addEventListener("click", onClick);
    canvas.addEventListener("dblclick", onDblClick);
    canvas.addEventListener("mousemove", onMouseMove);
    canvas.addEventListener("wheel", onWheel, { passive: false });
    canvas.addEventListener("keydown", onKeyDown);
    canvas.addEventListener("keyup", onKeyUp);
    canvas.addEventListener("contextmenu", onContextMenu);
    canvas.tabIndex = 0;
    canvas.style.outline = "none";

    connect();

    // FPS counter
    fpsIntervalRef.current = setInterval(() => {
      setFps(frameCountRef.current);
      frameCountRef.current = 0;
    }, 1000);

    return () => {
      cancelled = true;
      if (retryTimer) clearTimeout(retryTimer);
      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }
      if (fpsIntervalRef.current) clearInterval(fpsIntervalRef.current);
      canvas.removeEventListener("mousedown", onMouseDown);
      canvas.removeEventListener("mouseup", onMouseUp);
      canvas.removeEventListener("click", onClick);
      canvas.removeEventListener("dblclick", onDblClick);
      canvas.removeEventListener("mousemove", onMouseMove);
      canvas.removeEventListener("wheel", onWheel);
      canvas.removeEventListener("keydown", onKeyDown);
      canvas.removeEventListener("keyup", onKeyUp);
      canvas.removeEventListener("contextmenu", onContextMenu);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wsKey]);

  return { connected, error, fps, resolution, disconnect };
}
