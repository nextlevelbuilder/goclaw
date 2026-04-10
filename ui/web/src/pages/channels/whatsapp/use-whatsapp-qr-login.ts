import { useState, useCallback } from "react";
import { useWsCall } from "@/hooks/use-ws-call";
import { useWsEvent } from "@/hooks/use-ws-event";

export type QrStatus = "idle" | "waiting" | "done" | "connected" | "error";

export function useWhatsAppQrLogin(instanceId: string | null) {
  const [qrPng, setQrPng] = useState<string | null>(null);
  const [status, setStatus] = useState<QrStatus>("idle");
  const [errorMsg, setErrorMsg] = useState("");
  const { call: startQR, loading } = useWsCall("whatsapp.qr.start");

  const start = useCallback(async (forceReauth = false) => {
    if (!instanceId) return;
    setStatus("waiting");
    setQrPng(null);
    setErrorMsg("");
    try {
      await startQR({ instance_id: instanceId, force_reauth: forceReauth });
    } catch (err) {
      setStatus("error");
      setErrorMsg(err instanceof Error ? err.message : "Failed to start QR session");
    }
  }, [startQR, instanceId]);

  /** Logout current WhatsApp session and start a fresh QR scan flow. */
  const triggerReauth = useCallback(() => start(true), [start]);

  const reset = useCallback(() => {
    setStatus("idle");
    setQrPng(null);
    setErrorMsg("");
  }, []);

  useWsEvent(
    "whatsapp.qr.code",
    useCallback(
      (payload: unknown) => {
        const p = payload as { instance_id: string; png_b64: string };
        if (p.instance_id !== instanceId) return;
        setQrPng(p.png_b64);
        setStatus("waiting");
      },
      [instanceId],
    ),
  );

  useWsEvent(
    "whatsapp.qr.done",
    useCallback(
      (payload: unknown) => {
        const p = payload as { instance_id: string; success: boolean; already_connected?: boolean; error?: string };
        if (p.instance_id !== instanceId) return;
        if (p.success) {
          // Distinguish: already connected before any QR vs. freshly authenticated via QR scan.
          setStatus(p.already_connected ? "connected" : "done");
        } else {
          setStatus("error");
          setErrorMsg(p.error ?? "QR authentication failed");
        }
      },
      [instanceId],
    ),
  );

  return {
    qrPng, status, errorMsg, loading, start, reset, retry: start, triggerReauth,
  };
}
