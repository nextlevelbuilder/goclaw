import { useState, useCallback } from "react";
import { useWsCall } from "@/hooks/use-ws-call";
import { useWsEvent } from "@/hooks/use-ws-event";

export type QrStatus = "idle" | "waiting" | "done" | "error";

export function useZaloQrLogin(instanceId: string | null) {
  const [qrPng, setQrPng] = useState<string | null>(null);
  const [status, setStatus] = useState<QrStatus>("idle");
  const [errorMsg, setErrorMsg] = useState("");
  const { call: startQR, loading } = useWsCall("zalo.personal.qr.start");

  const start = useCallback(async () => {
    if (!instanceId) return;
    setStatus("waiting");
    setQrPng(null);
    setErrorMsg("");
    try {
      await startQR({ instance_id: instanceId });
    } catch (err) {
      setStatus("error");
      setErrorMsg(err instanceof Error ? err.message : "Failed to start QR session");
    }
  }, [startQR, instanceId]);

  const reset = useCallback(() => {
    setStatus("idle");
    setQrPng(null);
    setErrorMsg("");
  }, []);

  useWsEvent(
    "zalo.personal.qr.code",
    useCallback(
      (payload: unknown) => {
        const p = payload as { instance_id: string; png_b64: string };
        if (p.instance_id !== instanceId) return;
        setQrPng(p.png_b64);
      },
      [instanceId],
    ),
  );

  useWsEvent(
    "zalo.personal.qr.done",
    useCallback(
      (payload: unknown) => {
        const p = payload as { instance_id: string; success: boolean; error?: string };
        if (p.instance_id !== instanceId) return;
        if (p.success) {
          setStatus("done");
        } else {
          setStatus("error");
          setErrorMsg(p.error ?? "QR login failed");
        }
      },
      [instanceId],
    ),
  );

  return { qrPng, status, errorMsg, loading, start, reset, retry: start };
}
